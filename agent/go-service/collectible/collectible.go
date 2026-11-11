package collectible

import (
	"encoding/json"
	"fmt"
	"image"
	"regexp"
	"strconv"
	"strings"
	"sync"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const maxStage = 15

// codeRev 是本次决策代码的版本戳，出现在日志里用于确认跑的是哪一版。
const codeRev = "collectible-v10-fail-fast"

var (
	stagePattern = regexp.MustCompile(`(\d+)\s*阶段`)
	expPattern   = regexp.MustCompile(`(\d+)\s*/\s*(\d+)`)
	digitPattern = regexp.MustCompile(`(\d+)`)
)

// nodeRefs 是 Pipeline 传入的识别节点名。
//
// Go 侧不写死任何 Pipeline 节点名：识别参数与节点命名都属于 Pipeline，
// 这里只按名字调用，改动 Pipeline 时同步改 custom_action_param 即可。
type nodeRefs struct {
	// StageNode / ExpNode 读面板状态。
	StageNode string `json:"stage_node"`
	ExpNode   string `json:"exp_node"`

	// RepeatNode 读对话框里的「当前/上限」次数。
	RepeatNode string `json:"repeat_node"`
	// HeldNodes / SelectedNodes 按档位取「持有数」与「选中态」识别节点。
	HeldNodes     map[string]string `json:"held_nodes"`
	SelectedNodes map[string]string `json:"selected_nodes"`
	// DialogNodes 按档位取保养对话框里显示的工具箱名识别节点。
	DialogNodes map[string]string `json:"dialog_nodes"`
	// PickNodes / SelectNodes 按档位取「选中该档箱子」的目标节点。
	PickNodes   map[string]string `json:"pick_nodes"`
	SelectNodes map[string]string `json:"select_nodes"`
}

// tierNode 从「档位 → 节点名」映射里取节点名，缺失时报错。
func tierNode(m map[string]string, t int) (string, bool) {
	name := strings.TrimSpace(m[tierName(t)])
	if name == "" {
		log.Error().Str("component", "Collectible").Str("tier", tierName(t)).Msg("custom param missing node for tier")
		return "", false
	}
	return name, true
}

var (
	stateMu    sync.Mutex
	grades     = map[int64]string{}
	planCounts = map[int64]int{}
	planTiers  = map[int64]int{}
	// repeatClicks 记录还需要点几次「+」；repeatPending 表示下一次进 SetRepeat 要先读一次当前次数。
	// 不能每轮都靠 OCR 判断是否到目标：读数会落后一帧，而且到上限后「+」会变灰，
	// 继续点就会卡死（R 级阶段上限只有 5）。
	repeatClicks  = map[int64]int{}
	repeatPending = map[int64]bool{}
	tierRetries   = map[int64]int{}
)

// storeGrade 记录本任务的收藏品等级（R/SR），供后续决策复用。
func storeGrade(taskID int64, grade string) {
	stateMu.Lock()
	defer stateMu.Unlock()
	grades[taskID] = grade
}

// loadGrade 读取本任务已记录的收藏品等级。
func loadGrade(taskID int64) string {
	stateMu.Lock()
	defer stateMu.Unlock()
	return grades[taskID]
}

// currentGrade 返回本任务记录的等级，缺省按 SR 处理。
func currentGrade(taskID int64) string {
	if g := loadGrade(taskID); g != "" {
		return g
	}
	return "SR"
}

// storePlanCount 记录本轮计划要连续保养几次。
func storePlanCount(taskID int64, n int) {
	stateMu.Lock()
	defer stateMu.Unlock()
	planCounts[taskID] = n
}

// loadPlanCount 读取本轮计划的批量次数。
func loadPlanCount(taskID int64) int {
	stateMu.Lock()
	defer stateMu.Unlock()
	return planCounts[taskID]
}

// storePlanTier 记录本轮计划使用的档位。
func storePlanTier(taskID int64, tier int) {
	stateMu.Lock()
	defer stateMu.Unlock()
	planTiers[taskID] = tier
}

// loadPlanTier 读取本轮计划的档位。
func loadPlanTier(taskID int64) int {
	stateMu.Lock()
	defer stateMu.Unlock()
	if t, ok := planTiers[taskID]; ok {
		return t
	}
	return -1
}

// resetRepeat 在每轮保养开始前重置次数相关的计数。
func resetRepeat(taskID int64) {
	stateMu.Lock()
	defer stateMu.Unlock()
	delete(repeatClicks, taskID)
	delete(tierRetries, taskID)
	repeatPending[taskID] = true
}

// takeRepeatClick 消耗一次点击配额；返回 true 表示还需要点一次「+」。
func takeRepeatClick(taskID int64) bool {
	stateMu.Lock()
	defer stateMu.Unlock()
	if repeatClicks[taskID] > 0 {
		repeatClicks[taskID]--
		return true
	}
	return false
}

// initRepeatClicks 按「目标 − 当前」算出还需要点几次「+」。
func initRepeatClicks(taskID int64, target, cur int) {
	stateMu.Lock()
	defer stateMu.Unlock()
	n := target - cur
	if n < 0 {
		n = 0
	}
	repeatClicks[taskID] = n
	repeatPending[taskID] = false
}

// repeatNeedsInit 判断是否需要初始化点击计数。
func repeatNeedsInit(taskID int64) bool {
	stateMu.Lock()
	defer stateMu.Unlock()
	return repeatPending[taskID]
}

// bumpTierRetry 记录档位核对失败次数并返回当前次数。
func bumpTierRetry(taskID int64) int {
	stateMu.Lock()
	defer stateMu.Unlock()
	tierRetries[taskID]++
	return tierRetries[taskID]
}

// SetGradeParam 是等级记录动作的入参。
type SetGradeParam struct {
	Grade string `json:"grade"`
}

// SetGradeAction 记录本任务的收藏品等级（R 级升满换 SR 后由 Pipeline 调用）。
type SetGradeAction struct{}

var _ maa.CustomActionRunner = &SetGradeAction{}

// Run 实现 maa.CustomActionRunner。
func (a *SetGradeAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if arg == nil {
		return false
	}
	var params SetGradeParam
	if arg.CustomActionParam != "" {
		if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
			log.Error().Err(err).Str("component", "Collectible").Msg("failed to parse set grade params")
			return false
		}
	}
	grade := strings.ToUpper(strings.TrimSpace(params.Grade))
	if grade != "R" && grade != "SR" {
		log.Error().Str("component", "Collectible").Str("grade", grade).Msg("invalid grade")
		return false
	}
	storeGrade(arg.TaskID, grade)
	log.Info().Str("component", "Collectible").Int64("task_id", arg.TaskID).Str("grade", grade).Msg("collectible grade recorded")
	return true
}

// DecideParam 是决策动作的入参：识别节点、目标节点都由 Pipeline 给出。
type DecideParam struct {
	// Grade 可选；留空时用 SetGrade 动作记录过的等级。
	Grade string   `json:"grade"`
	Nodes nodeRefs `json:"nodes"`
	// MaxedNode / NoMaterialNode 是满级与「三档工具箱都不够」的出口节点。
	MaxedNode      string `json:"maxed_node"`
	NoMaterialNode string `json:"no_material_node"`
}

// DecideAction 读取保养面板状态（识别节点由 Pipeline 指定），
// 按成本模型挑一档工具箱与批量次数，并改写下一步。
type DecideAction struct{}

var _ maa.CustomActionRunner = &DecideAction{}

// Run 实现 maa.CustomActionRunner。
func (a *DecideAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		return false
	}
	var params DecideParam
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
		log.Error().Err(err).Str("component", "Collectible").Msg("failed to parse decide params")
		return false
	}
	grade := strings.ToUpper(strings.TrimSpace(params.Grade))
	if grade == "" {
		grade = currentGrade(arg.TaskID)
	}

	img, err := ctx.GetTasker().GetController().CacheImage()
	if err != nil || img == nil {
		return giveUp("decide: no image")
	}

	stageText := readOCRText(ctx, img, params.Nodes.StageNode)
	if stage, ok := parseNumberText(stageText, stagePattern); ok && stage >= maxStage {
		// 满级就没什么可保养的：不必再读经验/持有数/选中档，直接走满级出口
		dumpDecision(decisionDump{Grade: grade, Stage: stage, Maxed: true})
		log.Info().Str("component", "Collectible").Int("stage", stage).Msg("collectible reached max stage")
		return override(ctx, arg, params.MaxedNode)
	}
	expText := readOCRText(ctx, img, params.Nodes.ExpNode)
	if observed := gradeFromExpCap(expText, grade); observed != grade {
		log.Warn().
			Str("component", "Collectible").
			Str("recorded", grade).
			Str("from_exp_cap", observed).
			Str("exp_text", expText).
			Msg("grade corrected by exp cap")
		storeGrade(arg.TaskID, observed)
		grade = observed
	}
	heldTexts := [3]string{}
	for t := 0; t < 3; t++ {
		node, ok := tierNode(params.Nodes.HeldNodes, t)
		if !ok {
			return false
		}
		heldTexts[t] = readOCRText(ctx, img, node)
	}
	// 三卡面板上的经验条是「预升级」显示：当前选中的档位会先加一次经验再展示，
	// 真实经验要把这一档的增量减掉。认不出选中档不当作读失败，见 parsePanelState 的注释。
	selected := -1
	if s, ok := selectedTierOnPanel(ctx, img, params.Nodes.SelectedNodes); ok {
		selected = s
	} else {
		log.Warn().Str("component", "Collectible").Msg("no tier card selected, treat preview exp as real exp")
	}
	state, err := parsePanelState(panelRead{
		stageText: stageText,
		expText:   expText,
		heldTexts: heldTexts,
		Selected:  selected,
	})
	if err != nil {
		// 关键读数缺失时不猜（尤其不能用 0 兜底）：按政策不做兜底，直接失败由用户处理
		return giveUp(err.Error())
	}
	stage, exp, held := state.Stage, state.Exp, state.Held

	log.Info().
		Str("component", "Collectible").
		Str("stage_text", stageText).
		Str("exp_text", expText).
		Int("preview_exp", state.PreviewExp).
		Interface("held", held).
		Interface("held_texts", heldTexts).
		Str("data_file", resolveProbPath()).
		Str("code_rev", codeRev).
		Msg("collectible state read")

	choice := decide(grade, stage, exp, held)
	dumpDecision(decisionDump{
		Grade: grade, Stage: stage, Exp: exp, PreviewExp: state.PreviewExp, Held: held,
		Tier: choice.Tier, Count: choice.Count, Cost: choice.Cost, Maxed: choice.Maxed, NoMatl: choice.NoMaterial,
	})
	switch {
	case choice.Maxed:
		log.Info().Str("component", "Collectible").Int("stage", stage).Msg("collectible reached max stage")
		return override(ctx, arg, params.MaxedNode)
	case choice.NoMaterial:
		log.Info().Str("component", "Collectible").Int("stage", stage).Ints("held", held[:]).Msg("all toolbox tiers below cost, stop")
		return override(ctx, arg, params.NoMaterialNode)
	}

	target, ok := tierNode(params.Nodes.PickNodes, choice.Tier)
	if !ok {
		return false
	}
	storePlanCount(arg.TaskID, choice.Count)
	storePlanTier(arg.TaskID, choice.Tier)
	resetRepeat(arg.TaskID)
	log.Info().
		Str("component", "Collectible").
		Str("grade", grade).
		Int("stage", stage).
		Int("exp", exp).
		Str("tier", tierName(choice.Tier)).
		Int("count", choice.Count).
		Float64("expected_cost", choice.Cost).
		Ints("held", held[:]).
		Msg("pick toolbox for next maintain")
	return override(ctx, arg, target)
}

// giveUp 用于「流程本身没问题、只是状态读不出来」的场合。
//
// 按既定政策**不做任何兜底**：不猜、不重试、不写友好收尾节点，打一条说明日志后直接失败。
// 这类情况基本都是用户侧的问题（界面被遮挡、界面状态不对、手动干预），
// 日志说明白就够了，任务让它自己报错，由用户决定要不要停。
func giveUp(reason string) bool {
	log.Error().
		Str("component", "Collectible").
		Str("reason", reason).
		Msg("state unreadable: flow is fine, cannot continue")
	return false
}

// gradeFromExpCap 用经验条上限校正等级。
//
// 等级是**任务级记忆**（Pipeline 读收藏品徽章后调 SetGradeR/SR 记下来），一旦记错：
// 采样会把样本归到错的档（等级记成 R、维护的其实是 SR 级收藏品时，
// 该档数值会被写进 R 的记录），决策也会用错的阶段上限与概率表。
//
// 经验条上限是当场可验的观测：R 级每阶段 1000、SR 级 3000。
// 实机状态读数里「等级 ↔ 上限」零反例，所以以观测为准，读不出来才退回记忆值。
func gradeFromExpCap(expText, recorded string) string {
	_, maxN, ok := parseFraction(expText)
	if !ok || maxN <= 0 {
		return recorded
	}
	switch maxN {
	case stageCap("R"):
		return "R"
	case stageCap("SR"):
		return "SR"
	default:
		return recorded
	}
}

// panelRead 是三卡面板上读到的原始文本（未解析）。
type panelRead struct {
	stageText string
	expText   string
	heldTexts [3]string
	// Selected 是当前选中的档位下标；-1 表示没认出选中态。
	Selected int
}

// panelState 是决策需要的最小状态。
type panelState struct {
	Stage      int
	Exp        int
	PreviewExp int
	Held       [3]int
}

// parsePanelState 把面板读数折算成决策输入。
//
// 两条不能猜的规则：
//   - 阶段与经验是决策的状态变量，读不到必须报错（用 0 兜底等于「这阶段还没动」，会多规划一整阶段）；
//   - 三档持有数读不到同样必须报错（用 0 兜底会把这一档判成不够用，轻则改喂更贵的档，
//     重则三档全 0 直接走「工具箱不足」出口）。
//
// 选中档读不到则不算失败：面板没有选中档时也不做预升级，经验条读数本身就是真实经验。
// 这里刻意不硬失败——万一某个面板状态永远认不出蓝色横幅，硬失败会让流程反复重试到任务失败，
// 而经验最多只差一档增量。
func parsePanelState(r panelRead) (panelState, error) {
	var st panelState
	stage, ok := parseNumberText(r.stageText, stagePattern)
	if !ok {
		return st, fmt.Errorf("stage 未识别: %q", r.stageText)
	}
	st.Stage = stage
	if stage >= maxStage {
		return st, nil // 满级不用再读经验与持有数
	}
	previewExp, ok := parseNumberText(r.expText, expPattern)
	if !ok {
		return st, fmt.Errorf("exp 未识别: %q", r.expText)
	}
	st.PreviewExp = previewExp
	st.Exp = previewExp
	if r.Selected >= 0 && r.Selected < 3 {
		st.Exp = previewExp - tierGain[r.Selected]
		if st.Exp < 0 {
			st.Exp = 0
		}
	}
	for t := 0; t < 3; t++ {
		v, ok := parseNumberText(r.heldTexts[t], digitPattern)
		if !ok {
			return st, fmt.Errorf("%s 持有数未识别: %q", tierName(t), r.heldTexts[t])
		}
		st.Held[t] = v
	}
	return st, nil
}

// SetRepeatParam 是次数设置动作的入参。
type SetRepeatParam struct {
	Nodes    nodeRefs `json:"nodes"`
	PlusNode string   `json:"plus_node"`
	MaxNode  string   `json:"max_node"`
	// Next 是设置完次数后的候选列表（可含 [JumpBack] 前缀），由 Pipeline 给出。
	Next []string `json:"next"`
}

// SetRepeatAction 把保养对话框的「重复次数」调到计划值。
//
// 对话框默认是 1，所以只需要按 + 递增；每次点完回到这里再决定下一步。
// 两条硬性约束：
//   - 目标正好等于上限时直接点 MAX，省掉 N−1 次「+」；
//   - 「+」到上限后会消失（变灰不可识别），所以点「+」的分支必须带兜底，
//     否则父列表会一直重试到超时。
type SetRepeatAction struct{}

var _ maa.CustomActionRunner = &SetRepeatAction{}

// Run 实现 maa.CustomActionRunner。
func (a *SetRepeatAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		return false
	}
	var params SetRepeatParam
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
		log.Error().Err(err).Str("component", "Collectible").Msg("failed to parse set repeat params")
		return false
	}
	target := loadPlanCount(arg.TaskID)
	if target < 1 {
		target = 1
	}
	proceed := func() bool {
		return overrideList(ctx, arg, params.Next...)
	}
	clickPlus := func() bool {
		return overrideList(ctx, arg, append([]string{params.PlusNode}, params.Next...)...)
	}

	img, err := ctx.GetTasker().GetController().CacheImage()
	if err != nil || img == nil {
		// 取不到帧就不动次数（更不会去点 MAX）：稍后再读一次。
		return giveUp("set repeat: no image")
	}

	if repeatNeedsInit(arg.TaskID) {
		text := readOCRText(ctx, img, params.Nodes.RepeatNode)
		cur, maxN, ok := parseFraction(text)
		if !ok {
			// 读不出「当前/上限」就保持对话框默认的 1：只会少用材料，回来后重新规划即可。
			log.Warn().Str("component", "Collectible").Str("repeat_text", text).Msg("repeat count not recognized, keep default")
			initRepeatClicks(arg.TaskID, 1, 1)
			return proceed()
		}
		if maxN > 0 && target > maxN {
			target = maxN
		}
		log.Info().
			Str("component", "Collectible").
			Int("current", cur).
			Int("max", maxN).
			Int("target", target).
			Msg("set repeat count")
		if maxN > 0 && target >= maxN && cur < maxN {
			// 目标就是上限：点一次 MAX 即可，省掉 N-1 次「+」
			initRepeatClicks(arg.TaskID, cur, cur)
			log.Info().Str("component", "Collectible").Msg("repeat target equals max, use MAX button")
			return override(ctx, arg, params.MaxNode)
		}
		initRepeatClicks(arg.TaskID, target, cur)
	}

	if takeRepeatClick(arg.TaskID) {
		// 带上兜底：「+」不可识别（已到上限）时直接去保养，避免卡死
		return clickPlus()
	}

	// 配额用完后直接去保养，不再回读次数核对：
	// 游戏里次数文本刷新比点击慢接近 1s，回读会把已经生效的点击当成没生效，
	// 补点后计划 SSR×2 被点成 3，白吃一个高级工具。少点一次只是这轮少用一点高档料，不会浪费。
	return proceed()
}

// TierCheckParam 是档位核对动作的入参。
type TierCheckParam struct {
	Nodes nodeRefs `json:"nodes"`
	// OkNode 是档位一致时的目标；MismatchNode 是档位不符、需要关掉对话框重选时的目标。
	OkNode       string `json:"ok_node"`
	MismatchNode string `json:"mismatch_node"`
	// MaxRetry 是允许的核对失败次数，超过后接受现状，默认 2。
	MaxRetry int `json:"max_retry"`
}

// TierCheckAction 核对保养对话框里显示的工具箱是否与计划一致。
// 点箱卡偶尔会没选中，对话框里就会是上一次的档位；对不上就关掉对话框重新走一轮。
type TierCheckAction struct{}

var _ maa.CustomActionRunner = &TierCheckAction{}

// Run 实现 maa.CustomActionRunner。
func (a *TierCheckAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		return false
	}
	var params TierCheckParam
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
		log.Error().Err(err).Str("component", "Collectible").Msg("failed to parse tier check params")
		return false
	}
	maxRetry := params.MaxRetry
	if maxRetry <= 0 {
		maxRetry = 2
	}
	want := loadPlanTier(arg.TaskID)
	if want < 0 {
		return override(ctx, arg, params.OkNode)
	}
	img, err := ctx.GetTasker().GetController().CacheImage()
	if err != nil || img == nil {
		// 取不到帧就核对不了档位；不能当成「核对通过」，也不能判失败结束任务。
		return giveUp("tier check: no image")
	}
	got := -1
	for t := 0; t < 3; t++ {
		node, ok := tierNode(params.Nodes.DialogNodes, t)
		if !ok {
			return false
		}
		if nodeHit(ctx, img, node) {
			got = t
			break
		}
	}
	if got < 0 {
		return giveUp("dialog tier not recognized")
	}
	if got == want {
		return override(ctx, arg, params.OkNode)
	}
	if bumpTierRetry(arg.TaskID) >= maxRetry {
		log.Warn().
			Str("component", "Collectible").
			Str("want", tierName(want)).
			Str("got", tierName(got)).
			Msg("dialog tier still mismatched, accept it")
		return override(ctx, arg, params.OkNode)
	}
	log.Warn().
		Str("component", "Collectible").
		Str("want", tierName(want)).
		Str("got", tierName(got)).
		Msg("dialog tier mismatch, close and repick")
	return override(ctx, arg, params.MismatchNode)
}

// selectedTierOnPanel 判断三卡面板上当前选中的是哪一档。
// 选中的卡片 ITEM 横幅会变成蓝色（未选中是深色），由 Pipeline 的颜色匹配节点直接给出。
func selectedTierOnPanel(ctx *maa.Context, img image.Image, nodes map[string]string) (int, bool) {
	hits := [3]bool{}
	for t := 0; t < 3; t++ {
		node, ok := tierNode(nodes, t)
		if !ok {
			return -1, false
		}
		hits[t] = nodeHit(ctx, img, node)
	}
	return firstHitTier(hits)
}

// firstHitTier 返回第一个命中的档位；没有命中返回 false。
func firstHitTier(hits [3]bool) (int, bool) {
	for t, ok := range hits {
		if ok {
			return t, true
		}
	}
	return -1, false
}

// nodeHit 跑一次识别节点并返回是否命中。
func nodeHit(ctx *maa.Context, img image.Image, node string) bool {
	detail, err := ctx.RunRecognition(node, img, nil)
	return err == nil && detail != nil && detail.Hit
}

// tierName 返回档位名字，同时用作 tier -> 节点映射的键。
func tierName(t int) string {
	switch t {
	case tierSR:
		return "SR"
	case tierSSR:
		return "SSR"
	default:
		return "R"
	}
}

// readOCRText 跑一次识别节点并返回 Best 的原始 OCR 文本。
func readOCRText(ctx *maa.Context, img image.Image, node string) string {
	if node == "" {
		return ""
	}
	detail, err := ctx.RunRecognition(node, img, nil)
	if err != nil || detail == nil || !detail.Hit || detail.Results == nil || detail.Results.Best == nil {
		return ""
	}
	ocr, ok := detail.Results.Best.AsOCR()
	if !ok {
		return ""
	}
	return ocr.Text
}

// parseNumberText 从 OCR 文本里按正则取第一个数字。
func parseNumberText(text string, pattern *regexp.Regexp) (int, bool) {
	m := pattern.FindStringSubmatch(text)
	if len(m) == 0 {
		return 0, false
	}
	group := m[0]
	if len(m) >= 2 {
		group = m[1] // 有捕获组时优先用第一组，避免把前后缀带进数字
	}
	value, err := strconv.Atoi(strings.TrimSpace(group))
	if err != nil {
		return 0, false
	}
	return value, true
}

// parseFraction 解析 "3/15" 这类文本。
func parseFraction(text string) (int, int, bool) {
	m := expPattern.FindStringSubmatch(text)
	if len(m) < 3 {
		return 0, 0, false
	}
	cur, err1 := strconv.Atoi(strings.TrimSpace(m[1]))
	maxN, err2 := strconv.Atoi(strings.TrimSpace(m[2]))
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return cur, maxN, true
}

// jumpBackPrefix 是 next 列表里跳回栈的写法前缀。
const jumpBackPrefix = "[JumpBack]"

// overrideList 把当前节点的下一步改写成候选列表（第一个不可识别时自动落到后续候选）。
// 名字以 [JumpBack] 开头时按跳回栈处理（执行完交还本列表重新识别）。
func overrideList(ctx *maa.Context, arg *maa.CustomActionArg, targets ...string) bool {
	if len(targets) == 0 {
		log.Error().Str("component", "Collectible").Msg("override list is empty")
		return false
	}
	items := make([]maa.NextItem, 0, len(targets))
	for _, t := range targets {
		item := maa.NextItem{Name: t}
		if strings.HasPrefix(t, jumpBackPrefix) {
			item.Name = strings.TrimPrefix(t, jumpBackPrefix)
			item.JumpBack = true
		}
		items = append(items, item)
	}
	if err := ctx.OverrideNext(arg.CurrentTaskName, items); err != nil {
		log.Error().Err(err).Str("component", "Collectible").Str("targets", strings.Join(targets, ",")).Msg("failed to override next list")
		return false
	}
	return true
}

// override 把当前节点的下一步改写成指定节点。
func override(ctx *maa.Context, arg *maa.CustomActionArg, target string) bool {
	if target == "" {
		log.Error().Str("component", "Collectible").Msg("override target is empty")
		return false
	}
	if err := ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: target}}); err != nil {
		log.Error().
			Err(err).
			Str("component", "Collectible").
			Str("target", target).
			Msg("failed to override next")
		return false
	}
	return true
}
