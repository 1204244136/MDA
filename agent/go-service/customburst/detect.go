// detect.go：检测/汇总职责。
//
// 本文件只做"把 Pipeline 的原子识别结果汇总成结构化检测结果"（FastBurstResult），
// 不承载"看到之后要做什么/怎么算"的业务。所有原子识别（六边形颜色、灰条、冷却变暗）
// 均由 Pipeline ColorMatch 子节点承担（ROI/颜色区间/count/roi_offset 都在 pipeline JSON），
// 这里仅通过 ctx.RunRecognition 复用它们并汇总。
//
// 与项目/AGENTS.md 约定一致：基本识别留给 Pipeline；Go 只做汇总/难点。
// 本文件属于 CustomBurst 特性底层的「快速爆裂」（FastBurst）框架的检测汇总层。
package customburst

import (
	"encoding/json"
	"image"
	"sort"
	"strconv"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// ---------- 检测结果 ----------

// FastBurstResult 是检测层输出的结构化结果（Detail 为 JSON）。
// 仅描述"看到了什么"：面板是否存在、当前阶段以及路由所需槽位的冷却/就绪状态。
type FastBurstResult struct {
	Present         bool     `json:"present"`                    // 是否检测到爆裂面板（六边形图标）
	Stage           int      `json:"stage"`                      // 爆裂阶段 1/2/3
	TransitionStage int      `json:"transition_stage,omitempty"` // 阶段色消失时预测的下一阶段（仅用于过渡期抢先发键）
	Count           int      `json:"count"`                      // 当前阶段角色数
	PresentSlots    []int    `json:"present_slots"`              // 有头像的槽位（1 起）
	CDSlots         []int    `json:"cd_slots"`                   // 处于冷却的槽位
	ReadySlots      []int    `json:"ready_slots"`                // 就绪（有头像且未冷却）的槽位
	ReadyKey        string   `json:"ready_key"`                  // 自上而下第一个就绪槽位对应的按键 A/S/D（空=无）
	ReadyKeys       []string `json:"ready_keys"`                 // 所有就绪槽位对应的按键
}

// ---------- Pipeline 子识别节点复用 ----------

// runRecognition 调用一个 Pipeline 识别节点并返回原始结果。
// 识别参数（ROI / 颜色区间 / count / roi_offset）全部在 Pipeline JSON 中维护。
func runRecognition(ctx *maa.Context, nodeName string, img image.Image) (*maa.RecognitionDetail, bool) {
	if ctx == nil || img == nil || nodeName == "" {
		return nil, false
	}
	detail, err := ctx.RunRecognition(nodeName, img, nil)
	if err != nil {
		log.Warn().Err(err).Str("component", "FastBurst").Str("node", nodeName).Msg("run fast burst sub-recognition failed")
		return nil, false
	}
	return detail, detail != nil
}

// runHit 调用一个 Pipeline 识别节点（ColorMatch 等），返回是否命中。
func runHit(ctx *maa.Context, nodeName string, img image.Image) bool {
	detail, ok := runRecognition(ctx, nodeName, img)
	return ok && detail.Hit
}

func slotFromCombinedDetail(detail *maa.RecognitionDetail) int {
	if detail == nil {
		return 0
	}
	for _, child := range detail.CombinedResult {
		if child == nil || child.Box[2] <= 0 || child.Box[3] <= 0 {
			continue
		}
		switch child.Name {
		case "FastBurstSlot1":
			return 1
		case "FastBurstSlot2":
			return 2
		case "FastBurstSlot3":
			return 3
		}
	}
	return 0
}

// detectFallbackSlots 用一个 Or 先找到自上而下第一个有头像的槽位，
// 再按需确认冷却和后续槽位。无头像时只需一次识别，避免每帧固定检查 3 个槽位。
func detectFallbackSlots(ctx *maa.Context, img image.Image) (presentSlots, cdSlots []int, ok bool) {
	detail, ok := runRecognition(ctx, "FastBurstAnySlot", img)
	if !ok {
		return nil, nil, false
	}
	firstSlot := slotFromCombinedDetail(detail)
	if firstSlot == 0 {
		return nil, nil, true
	}

	for slot := firstSlot; slot <= 3; slot++ {
		if slot != firstSlot && !runHit(ctx, "FastBurstSlot"+strconv.Itoa(slot), img) {
			continue
		}
		presentSlots = append(presentSlots, slot)
		if runHit(ctx, "FastBurstSlot"+strconv.Itoa(slot)+"CDDark", img) {
			cdSlots = append(cdSlots, slot)
			continue
		}
		// 和原逐槽扫描保持一致：找到首个就绪槽即可，不再读取更后面的槽位。
		break
	}
	sort.Ints(presentSlots)
	sort.Ints(cdSlots)
	return presentSlots, cdSlots, true
}

// detectStage uses one Pipeline Or node so the three stage color checks share
// the same image/agent round trip. For combined results MaaFramework exposes
// the child box but not a child Hit flag; a non-empty box is the hit signal.
func detectStage(ctx *maa.Context, img image.Image) int {
	detail, err := ctx.RunRecognition("FastBurstStage", img, nil)
	if err != nil || detail == nil {
		if err != nil {
			log.Warn().Err(err).Str("component", "FastBurst").Msg("run burst stage recognition failed")
		}
		return 0
	}
	return stageFromDetail(detail)
}

func stageFromDetail(detail *maa.RecognitionDetail) int {
	if detail == nil {
		return 0
	}
	for _, child := range detail.CombinedResult {
		if child == nil || child.Box[2] <= 0 || child.Box[3] <= 0 {
			continue
		}
		switch child.Name {
		case "FastBurstHexGreen":
			return 1
		case "FastBurstHexOrange":
			return 2
		case "FastBurstHexRed":
			return 3
		}
	}
	return 0
}

// detectPanel 复用 Pipeline 子识别节点做检测，Go 只做结果汇总。
// 阶段用一个 Or 节点汇总；整轮未指定时由动作层直接按固定 ASD 顺序，
// 不扫描槽位和冷却，也不等待 ReadySlots。混合配置中的未指定阶段仍保留
// 原有的首个就绪槽位兜底，避免影响同一轮中已经指定的阶段。
func detectPanel(ctx *maa.Context, img image.Image, taskID int64) *FastBurstResult {
	res := &FastBurstResult{}
	if ctx == nil || img == nil {
		return res
	}

	res.Stage = detectStage(ctx, img)
	cfg := burstRound.configFor(ctx, taskID)
	round := burstRound.current(taskID)
	if cfg.RoundCount > 0 {
		round %= cfg.RoundCount
	}
	fallbackLoop := isFallbackLoopRound(cfg, round)

	if res.Stage == 0 {
		// 指定/混合配置在阶段Ⅲ已经成功释放后，不再把阶段回退误当作新的过渡窗口；
		// 仍用槽位存在性维持面板结束检测，让短暂丢色不会触发重复按键。
		// 整轮未指定时不走该分支，避免为了生命周期判断再次扫描槽位。
		if !fallbackLoop && burstRound.stageWasConsumed(taskID, maxStages) {
			res.Present = runHit(ctx, "FastBurstAnySlot", img)
			return res
		}
		// A missing stage color during the known Ⅰ→Ⅱ or Ⅱ→Ⅲ transition is
		// actionable: the game accepts the upcoming stage key before its color
		// appears. Keep the observed stage at 0 and expose the prediction
		// separately so logs and state transitions remain truthful.
		res.TransitionStage = burstRound.nextStage(taskID)
		if res.TransitionStage != 0 {
			// Ⅰ/Ⅱ 已经出现过而阶段色暂时消失，按转换窗口处理。纯 ASD 模式
			// 不扫描槽位，仍直接把该空窗视为高频循环的一部分。
			res.Present = true
		} else if fallbackLoop && !burstRound.was(taskID) {
			// 灰色充能条已命中即可进入高频循环。首次看到阶段色之前，
			// 不为等待面板而扫描槽位，动作层直接继续 ASD。
			return res
		} else if fallbackLoop {
			// 纯 ASD 模式在最终阶段结束后不再扫描槽位；交给动作层的
			// 面板消失状态机确认本轮结束。
			return res
		} else {
			// 面板出现后保留一次轻量存在性识别，仅用于判断爆裂何时结束，
			// 不参与未指定整轮的按键选择。
			res.Present = runHit(ctx, "FastBurstAnySlot", img)
		}
		if !res.Present {
			return res
		}
	}

	detectionStage := res.Stage
	if detectionStage == 0 {
		detectionStage = res.TransitionStage
	}
	if detectionStage == 0 {
		// 槽位灰条在播片/回落期间可能短暂残留；没有阶段色或已知过渡时，
		// 不把单独的槽位命中当作爆裂面板存在，避免误开启新周期。
		return res
	}
	target := keyToSlot(cfg.Rounds[round][detectionStage-1])

	if target != 0 {
		// A configured axis needs only its target slot and cooldown state.
		if !burstRound.stageWasConsumed(taskID, detectionStage) && runHit(ctx, "FastBurstSlot"+strconv.Itoa(target), img) {
			res.PresentSlots = []int{target}
			if runHit(ctx, "FastBurstSlot"+strconv.Itoa(target)+"CDDark", img) {
				res.CDSlots = []int{target}
			} else {
				res.ReadySlots = []int{target}
				res.ReadyKeys = []string{slotKey[target]}
			}
		}
	} else {
		if !fallbackLoop {
			// 混合配置中的未指定阶段仍需提供旧的首个就绪槽位兜底；
			// 整轮未指定则完全跳过该扫描，由动作层直接循环 ASD。
			res.PresentSlots, res.CDSlots, _ = detectFallbackSlots(ctx, img)
			for _, slot := range res.PresentSlots {
				if !containsInts(res.CDSlots, slot) {
					res.ReadySlots = append(res.ReadySlots, slot)
				}
			}
			for _, slot := range res.ReadySlots {
				res.ReadyKeys = append(res.ReadyKeys, slotKey[slot])
			}
		}
	}

	// 阶段色命中或过渡期头像命中即代表面板在场；槽位信息按路由需要可能是部分汇总。
	res.Present = true

	res.Count = len(res.PresentSlots)
	if len(res.ReadySlots) > 0 {
		res.ReadyKey = slotKey[res.ReadySlots[0]]
	}

	for _, s := range [][]int{res.PresentSlots, res.CDSlots, res.ReadySlots} {
		sort.Ints(s)
	}
	return res
}

// ---------- 自定义识别：检测层（复用 Pipeline 子节点并返回结构化结果） ----------

// FastBurstPanelRecognition 汇总检测右侧爆裂面板状态，返回结构化 Detail（JSON）。
type FastBurstPanelRecognition struct{}

var _ maa.CustomRecognitionRunner = &FastBurstPanelRecognition{}

func (r *FastBurstPanelRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil || arg.Img == nil {
		log.Error().Str("component", "FastBurst").Msg("invalid burst panel recognition context")
		return nil, false
	}

	res := detectPanel(ctx, arg.Img, arg.TaskID)
	detail := marshalDetail(res)

	logger := log.Debug()
	if res.Present {
		logger = log.Info()
	}
	logger.
		Str("component", "FastBurst").
		Bool("present", res.Present).
		Int("stage", res.Stage).
		Int("transition_stage", res.TransitionStage).
		Int("count", res.Count).
		Ints("present_slots", res.PresentSlots).
		Ints("cd_slots", res.CDSlots).
		Ints("ready_slots", res.ReadySlots).
		Str("ready_key", res.ReadyKey).
		Msg("burst panel detected")

	// Box 为检测结果框（供调试定位），覆盖面板区域；非识别参数（识别参数都在子节点）。
	return &maa.CustomRecognitionResult{Box: maa.Rect{1150, 278, 130, 235}, Detail: detail}, true
}

// CustomBurstSafetyGateRecognition 每隔一小段时间放行一次暂停/结算分支。
// 这是任务流程节流而非画面识别；实际 UI 判断仍由 Pipeline 的 OCR/ColorMatch 节点完成。
type CustomBurstSafetyGateRecognition struct{}

var _ maa.CustomRecognitionRunner = &CustomBurstSafetyGateRecognition{}

func (r *CustomBurstSafetyGateRecognition) Run(_ *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if arg == nil || !burstRound.shouldProbeSafety(arg.TaskID) {
		return nil, false
	}
	return &maa.CustomRecognitionResult{Box: maa.Rect{0, 0, 1, 1}}, true
}

// CustomBurstReturnToLowFrequencyRecognition 在一轮爆裂完成后只命中一次，
// 让 Pipeline 从高频循环回到低频充能条检测。它是任务流程状态门，不承担画面识别。
type CustomBurstReturnToLowFrequencyRecognition struct{}

var _ maa.CustomRecognitionRunner = &CustomBurstReturnToLowFrequencyRecognition{}

func (r *CustomBurstReturnToLowFrequencyRecognition) Run(_ *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if arg == nil || !burstRound.takeReturnToLow(arg.TaskID) {
		return nil, false
	}
	return &maa.CustomRecognitionResult{Box: maa.Rect{0, 0, 1, 1}}, true
}

// marshalDetail 把检测结果序列化为 Detail JSON。
func marshalDetail(res *FastBurstResult) string {
	b, _ := json.Marshal(res)
	return string(b)
}
