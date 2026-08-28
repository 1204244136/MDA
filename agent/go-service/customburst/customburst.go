// Package customburst 实现「自定义爆裂」（CustomBurst）任务。
//
// 本包是 CustomBurst 这一整个特性的实现：外层是「自定义爆裂」任务（入口 CustomBurstMain），
// 底层是它的「快速爆裂」（FastBurst）框架（检测面板 + 快速释放）。两者不是并列任务，
// FastBurst 只是 CustomBurst 的底层框架，没有独立任务入口。
//
// 命名约定（本包内如何区分两者）：
//   - FastBurst*：底层框架的检测原语/汇总（FastBurstResult、FastBurstPanelRecognition、
//     FastBurstSlot*、FastBurstHex* 等），以及 ClickKey 原语（FastBurstClickKey*）。
//   - CustomBurst*：任务层的流程/动作（CustomBurstMain、CustomBurstLoop、
//     CustomBurstRouteAction、CustomBurstSafetyGateRecognition、
//     CustomBurstReturnToLowFrequencyRecognition 等）。
//
// 层次关系（重要约定）：
//   - 「快速爆裂」（FastBurst）是「自定义爆裂」（CustomBurst）的底层框架，不是并行的任务。
//     它负责右侧爆裂面板的检测（阶段 / 人数 / 冷却）与"快速释放"逻辑，不可取消；没有独立任务入口。
//   - 「自定义爆裂」（CustomBurst，任务入口 CustomBurstMain）在此底层框架之上，允许用户
//     配置"多轮爆裂轴"（最多 6 轮，每轮 3 阶段各选一个角色 A/S/D），指定角色在冷却时等待其冷却结束。
//     当前轮全部阶段未指定角色时，使用高频循环中的固定 ASD 按键序列；混合配置仍按阶段路由。
//
// ========== 职责边界（Pipeline / Go）==========
// 与 AGENTS.md / MaaEnd 约定一致："Pipeline 管流程，Go 管难点；识别留给 Pipeline"。
//   - Pipeline（识别 + 流程 + 动作）：
//   - 原子识别：ColorMatch 子节点（充能横条 / 六边形阶段色 / 槽位灰条 / 冷却变暗），全部携带
//     ROI / 颜色区间 / count / roi_offset；
//   - 流程控制：入口等待战斗画面 → 低频等待充能条 → 高频循环 → 末尾判暂停/结算（全部 pipeline 节点）;
//   - 动作：ClickKey 发键节点；配置承载节点 CustomBurstConfig；
//   - Go（汇总 + 难点/业务）：
//   - 检测汇总（detect.go）：FastBurstPanelRecognition 复用上述子识别节点（ctx.RunRecognition）
//     聚合出 FastBurstResult——只描述"看到了什么"；
//   - 业务/路由（本文件）：读选项（attach）→ 按当前轮+检测阶段跟轴决策 → 路由释放/等待 →
//     轮状态机（面板消失推进下一轮）→ focus 输出。
//
// 注意：识别参数（ROI/颜色/count/roi_offset）全部在 Pipeline JSON，本包不硬编码它们。
package customburst

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/1204244136/MDA/agent/go-service/pkg/i18n"
	"github.com/1204244136/MDA/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// carrierNode 是配置承载节点：选项通过 pipeline_override 写它的 attach 顶层键
// （MaaFramework 对 attach 按 key 浅合并，多个 option 写不同顶层键互不覆盖）。
const carrierNode = "CustomBurstConfig"

// slotKey 槽位序号（1 起）对应的释放按键字母，也定义未指定阶段的 ASD 顺序。
var slotKey = map[int]string{1: "A", 2: "S", 3: "D"}

// clickKeyNode 返回某槽位对应的按键动作 Pipeline 节点名。
// 节点使用 MaaFramework 内置 ClickKey 点按时长，调用方只需发起一次任务。
func clickKeyNode(slot int) string { return "FastBurstClickKey" + slotKey[slot] }

// ---------- 配置读取（选项经 attach 注入承载节点） ----------

const (
	maxRounds = 6
	maxStages = 3
)

// burstConfig 是「自定义爆裂」的用户配置（多轮爆裂轴）。
// Rounds[round][stage] 为该轮第 stage 阶段要释放的角色键（A/S/D），空串表示"不指定"
// （进入高频 ASD 循环）。
// RoundCount 为使用的轮数（1..maxRounds）。指定角色冷却时固定"等待其冷却结束"（不替换）。
type burstConfig struct {
	Rounds     [maxRounds][maxStages]string
	RoundCount int // 使用几轮（1..6），引擎只在这 N 轮间循环
}

// defaultBurstConfig 默认：单轮、各阶段"不指定"（空串 → 高频 ASD 循环），后续轮同样默认为空。
func defaultBurstConfig() burstConfig {
	var cfg burstConfig
	cfg.RoundCount = 1
	return cfg
}

// attachRoundKey 返回第 r 轮第 s 阶段在 attach 中的键，如 "r1s1"。
func attachRoundKey(r, s int) string { return fmt.Sprintf("r%ds%d", r, s) }

// loadBurstConfig 从承载节点 CustomBurstConfig 的 attach 读取用户配置。
func loadBurstConfig(ctx *maa.Context) burstConfig {
	cfg := defaultBurstConfig()
	if ctx == nil {
		return cfg
	}
	raw, err := ctx.GetNodeJSON(carrierNode)
	if err != nil {
		log.Warn().Err(err).Str("component", "CustomBurst").Msg("failed to read burst config carrier node")
		return cfg
	}
	var data struct {
		Attach map[string]json.RawMessage `json:"attach"`
	}
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return cfg
	}
	for r := 1; r <= maxRounds; r++ {
		for s := 1; s <= maxStages; s++ {
			if v := attachString(data.Attach, attachRoundKey(r, s)); v != "" {
				cfg.Rounds[r-1][s-1] = v
			}
		}
	}
	if v, ok := attachInt(data.Attach, "round_count"); ok && v >= 1 && v <= maxRounds {
		cfg.RoundCount = v
	}
	return cfg
}

func attachString(attach map[string]json.RawMessage, key string) string {
	raw, ok := attach[key]
	if !ok {
		return ""
	}
	var v string
	if err := json.Unmarshal(raw, &v); err != nil {
		return ""
	}
	return strings.TrimSpace(v)
}

func attachInt(attach map[string]json.RawMessage, key string) (int, bool) {
	raw, ok := attach[key]
	if !ok {
		return 0, false
	}
	var v int
	if err := json.Unmarshal(raw, &v); err != nil {
		return 0, false
	}
	return v, true
}

// keyToSlot 按键字母（A/S/D）转槽位号（1/2/3）。
func keyToSlot(key string) int {
	switch strings.ToUpper(strings.TrimSpace(key)) {
	case "A":
		return 1
	case "S":
		return 2
	case "D":
		return 3
	default:
		return 0
	}
}

func containsInts(list []int, v int) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// ---------- 自定义动作：依据配置路由动作（释放/等待）+ focus 输出 ----------

// slotState 记录每个任务当前阶段已经消费的槽位，避免同槽或兜底槽位连续重复释放。
type slotState struct {
	mu      sync.Mutex
	pressed map[int64]map[int]bool // taskID -> slot -> 当前阶段已消费/指定槽位已释放待冷却
}

func newSlotState() *slotState {
	return &slotState{pressed: map[int64]map[int]bool{}}
}

func (s *slotState) get(taskID int64, slot int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m, ok := s.pressed[taskID]; ok {
		return m[slot]
	}
	return false
}

func (s *slotState) set(taskID int64, slot int, v bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pressed[taskID] == nil {
		s.pressed[taskID] = map[int]bool{}
	}
	s.pressed[taskID][slot] = v
}

// setAll 标记当前阶段所有槽位均已消费。
// 兜底路由只应在一个阶段释放一次；头像/冷却动画会让后续帧暴露出另一个就绪槽位，
// 若只记录实际按下的槽位，就会在同一阶段再次释放。
func (s *slotState) setAll(taskID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pressed[taskID] == nil {
		s.pressed[taskID] = map[int]bool{}
	}
	for slot := 1; slot <= 3; slot++ {
		s.pressed[taskID][slot] = true
	}
}

func (s *slotState) all(taskID int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.pressed[taskID]
	if m == nil {
		return false
	}
	for slot := 1; slot <= 3; slot++ {
		if !m[slot] {
			return false
		}
	}
	return true
}

// reset 清空一个任务所有槽位的"已释放待冷却"标记（每轮爆裂结束时调用，使新周期各槽可再次释放）。
func (s *slotState) reset(taskID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.pressed, taskID)
}

var burstState = newSlotState()

// roundTracker 跟踪每个任务当前处于爆裂轴第几轮（0 起）、上一帧面板是否存在、
// 最后确认的阶段和阶段转换窗口。面板不在场（无阶段色也无头像）→ 本轮结束，推进到下一轮。
type roundTracker struct {
	mu              sync.Mutex
	round           map[int64]int
	wasPresent      map[int64]bool
	absentSince     map[int64]time.Time
	lastStage       map[int64]int
	consumedStage   map[int64]map[int]bool
	transitionStage map[int64]int
	lastAttempt     map[int64]stageAttempt
	fallbackNext    map[int64]int
	config          map[int64]burstConfig
	lastSafetyProbe map[int64]time.Time
	returnToLow     map[int64]bool
}

type stageAttempt struct {
	stage int
	slot  int
	at    time.Time
}

const (
	// phaseRetryInterval 是阶段未推进时重试同一候选键的最小间隔。
	// 之前降到 100ms 曾让“同稳定阶段未推进”被更频繁重发（出现过阶段Ⅰ被重发两次、
	// 标签误导/节奏不一致）。为稳定性回调到 150ms，减少多余重发；
	// 若阶段确实未推进，仍会按此间隔重试，不设置零时长 KeyDown/KeyUp。
	phaseRetryInterval = 150 * time.Millisecond
	// panelAbsentGrace 防止爆裂播片/阶段切换时的短暂空窗被误判为整轮结束。
	// 日志中已经观察到 300~400ms 的空窗，因此这里留出更大的余量；
	// 它只影响结束后的回落判定，不增加三个阶段的按键间隔。
	panelAbsentGrace    = 800 * time.Millisecond
	safetyProbeInterval = time.Second
)

func newRoundTracker() *roundTracker {
	return &roundTracker{
		round:           map[int64]int{},
		wasPresent:      map[int64]bool{},
		absentSince:     map[int64]time.Time{},
		lastStage:       map[int64]int{},
		consumedStage:   map[int64]map[int]bool{},
		transitionStage: map[int64]int{},
		lastAttempt:     map[int64]stageAttempt{},
		fallbackNext:    map[int64]int{},
		config:          map[int64]burstConfig{},
		lastSafetyProbe: map[int64]time.Time{},
		returnToLow:     map[int64]bool{},
	}
}

var burstRound = newRoundTracker()

// configFor 在一次爆裂周期内缓存爆裂轴配置。
// pipeline_override 在任务启动后不会动态变化，因此无需每帧读取并解析承载节点；
// 每轮爆裂结束时由 resetLastStage 清理，避免跨周期复用配置。
// 优化：使用双重检查锁模式减少锁竞争（高频调用场景）。
func (t *roundTracker) configFor(ctx *maa.Context, taskID int64) burstConfig {
	// Fast path: check without lock
	t.mu.Lock()
	if cfg, ok := t.config[taskID]; ok {
		t.mu.Unlock()
		return cfg
	}
	t.mu.Unlock()

	// Slow path: load config outside lock
	cfg := loadBurstConfig(ctx)

	// Store under lock
	t.mu.Lock()
	// Double-check: another goroutine might have loaded it while we were parsing
	if existing, ok := t.config[taskID]; ok {
		t.mu.Unlock()
		return existing
	}
	t.config[taskID] = cfg
	t.mu.Unlock()

	return cfg
}

func (t *roundTracker) current(taskID int64) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.round[taskID]
}

func (t *roundTracker) advance(taskID int64, roundCount int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if roundCount < 1 {
		roundCount = 1
	}
	t.round[taskID] = (t.round[taskID] + 1) % roundCount
}

func (t *roundTracker) was(taskID int64) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.wasPresent[taskID]
}

func (t *roundTracker) setWas(taskID int64, v bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.wasPresent[taskID] = v
	if v {
		delete(t.absentSince, taskID)
	}
}

// shouldEndAfterAbsent 仅在面板持续消失超过窗口后才确认本轮结束。
// 爆裂面板在阶段播片和角色头像切换时可能短暂完全不可见，不能用单帧空结果清状态。
func (t *roundTracker) shouldEndAfterAbsent(taskID int64) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.wasPresent[taskID] {
		return false
	}
	now := time.Now()
	if since, ok := t.absentSince[taskID]; ok {
		return now.Sub(since) >= panelAbsentGrace
	}
	t.absentSince[taskID] = now
	return false
}

// stageWasConsumed 返回当前爆裂周期内某阶段是否已经成功释放过。
func (t *roundTracker) stageWasConsumed(taskID int64, stage int) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.consumedStage[taskID][stage]
}

// markStageConsumed 记录当前爆裂周期内某阶段已经成功释放。
func (t *roundTracker) markStageConsumed(taskID int64, stage int) {
	if stage < 1 || stage > maxStages {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.consumedStage[taskID] == nil {
		t.consumedStage[taskID] = map[int]bool{}
	}
	t.consumedStage[taskID][stage] = true
}

// nextStage 返回当前阶段色消失时可抢先释放的下一阶段；同一过渡期间只返回同一个预测值。
func (t *roundTracker) nextStage(taskID int64) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	if stage := t.transitionStage[taskID]; stage >= 2 && stage <= maxStages {
		return stage
	}
	last := t.lastStage[taskID]
	if last >= 1 && last < maxStages {
		return last + 1
	}
	return 0
}

// beginTransition 开始一次阶段过渡预测，返回 true 表示本帧首次进入预测阶段。
func (t *roundTracker) beginTransition(taskID int64, stage int) bool {
	if stage < 2 || stage > maxStages {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if pending := t.transitionStage[taskID]; pending != 0 {
		if pending != stage {
			return false
		}
		// 过渡帧可能先看到“目标槽位尚未就绪/仍在冷却”，保留重试机会；
		// 只有已经成功下发过该阶段的按键后，才锁住本次过渡，避免重复释放。
		attempt, attempted := t.lastAttempt[taskID]
		return !attempted || attempt.stage != stage
	}
	t.transitionStage[taskID] = stage
	return true
}

// observeStage 记录实际看到的阶段。
// matchedPrediction 表示预发的本阶段键尚未让画面跳过本阶段，应重发本阶段；
// cancelledPrediction 表示阶段色误丢失或预发无效，回到原阶段后也应重发。
func (t *roundTracker) observeStage(taskID int64, stage int) (changed, matchedPrediction, cancelledPrediction bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.transitionStage[taskID] == stage {
		delete(t.transitionStage, taskID)
		t.lastStage[taskID] = stage
		return false, true, false
	}
	if t.transitionStage[taskID] != 0 {
		delete(t.transitionStage, taskID)
		if t.lastStage[taskID] == stage {
			return false, false, true
		}
	}
	if t.lastStage[taskID] == stage {
		return false, false, false
	}
	t.lastStage[taskID] = stage
	return true, false, false
}

// recordAttempt 记录一次成功下发的 ClickKey。只有阶段没有推进时才按自然识别周期重试同一候选键。
func (t *roundTracker) recordAttempt(taskID int64, stage, slot int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.lastAttempt[taskID] = stageAttempt{stage: stage, slot: slot, at: time.Now()}
}

func (t *roundTracker) shouldRetry(taskID int64, stage int) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	attempt, ok := t.lastAttempt[taskID]
	return ok && attempt.stage == stage && time.Since(attempt.at) >= phaseRetryInterval
}

// shouldResetStage 决定是否清空当前阶段的防重复状态。
// 阶段Ⅲ进入时仍必须允许首次释放；但已处于阶段Ⅲ时，爆裂播片通常会持续超过
// phaseRetryInterval，不能再因“未推进”重复释放。
func shouldResetStage(stage int, changed, matched, cancelled, retry bool) bool {
	return changed || cancelled || (stage < maxStages && (matched || retry))
}

func (t *roundTracker) attemptSlot(taskID int64, stage int) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	attempt, ok := t.lastAttempt[taskID]
	if !ok || attempt.stage != stage {
		return 0
	}
	return attempt.slot
}

func (t *roundTracker) lastAttemptSlot(taskID int64) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.lastAttempt[taskID].slot
}

// fallbackSlot 返回未指定整轮的下一枚按键槽位。游标只在按键成功后推进，
// 因而动作失败时会重试同一枚键，不会悄悄跳过 ASD 序列中的一项。
func (t *roundTracker) fallbackSlot(taskID int64) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	if slot := t.fallbackNext[taskID]; slot >= 1 && slot <= maxStages {
		return slot
	}
	t.fallbackNext[taskID] = 1
	return 1
}

// advanceFallbackSlot 在未指定整轮成功发键后推进 A→S→D 游标，D 后回到 A。
func (t *roundTracker) advanceFallbackSlot(taskID int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	slot := t.fallbackNext[taskID]
	if slot < 1 || slot >= maxStages {
		t.fallbackNext[taskID] = 1
		return
	}
	t.fallbackNext[taskID] = slot + 1
}

// shouldProbeSafety 节流暂停/结算检测。它不控制发键，仅把较慢的 OCR
// 从每一帧爆裂识别路径移到独立的低频 Pipeline 分支。
func (t *roundTracker) shouldProbeSafety(taskID int64) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	last := t.lastSafetyProbe[taskID]
	if last.IsZero() {
		t.lastSafetyProbe[taskID] = time.Now()
		return false
	}
	if time.Since(last) < safetyProbeInterval {
		return false
	}
	t.lastSafetyProbe[taskID] = time.Now()
	return true
}

// markReturnToLow 在一轮爆裂面板消失后请求回到低频充能检测。
// 该标记由高频循环后的单次 Pipeline 状态门消费，避免全爆裂期间继续高频识别。
func (t *roundTracker) markReturnToLow(taskID int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.returnToLow[taskID] = true
}

// takeReturnToLow 原子消费一次低频回退请求。
func (t *roundTracker) takeReturnToLow(taskID int64) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.returnToLow[taskID] {
		return false
	}
	delete(t.returnToLow, taskID)
	return true
}

// resetLastStage 在爆裂结束时清空阶段记录，使下一轮首阶段视为新阶段。
func (t *roundTracker) resetLastStage(taskID int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.wasPresent, taskID)
	t.lastStage[taskID] = 0
	delete(t.absentSince, taskID)
	delete(t.consumedStage, taskID)
	delete(t.transitionStage, taskID)
	delete(t.lastAttempt, taskID)
	delete(t.fallbackNext, taskID)
	delete(t.config, taskID)
	delete(t.lastSafetyProbe, taskID)
}

// CustomBurstRouteAction 依据用户配置的阶段-角色路由释放动作，并输出检测信息。
type CustomBurstRouteAction struct{}

var _ maa.CustomActionRunner = &CustomBurstRouteAction{}

func customDetail(arg *maa.CustomActionArg) string {
	if arg == nil || arg.RecognitionDetail == nil || arg.RecognitionDetail.Results == nil {
		return ""
	}
	if arg.RecognitionDetail.Results.Best != nil {
		if cr, ok := arg.RecognitionDetail.Results.Best.AsCustom(); ok {
			return cr.Detail
		}
	}
	if len(arg.RecognitionDetail.Results.All) > 0 {
		if cr, ok := arg.RecognitionDetail.Results.All[0].AsCustom(); ok {
			return cr.Detail
		}
	}
	return ""
}

func (a *CustomBurstRouteAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		log.Error().Str("component", "CustomBurst").Msg("custom burst action argument is nil")
		return false
	}

	// 参考 MaaEnd：长循环/高频动作每帧检查停止信号，保证用户能及时手动停止。
	if ctx.GetTasker().Stopping() {
		log.Info().Str("component", "CustomBurst").Msg("task stopping signal received, skip burst action")
		return true
	}

	detail := customDetail(arg)
	if detail == "" {
		return true
	}
	var res FastBurstResult
	if err := json.Unmarshal([]byte(detail), &res); err != nil {
		log.Warn().Err(err).Str("component", "CustomBurst").Msg("failed to parse burst panel detail")
		return true
	}

	taskID := arg.TaskID
	cfg := burstRound.configFor(ctx, taskID)
	curRound := burstRound.current(taskID) % cfg.RoundCount
	fallbackLoop := isFallbackLoopRound(cfg, curRound)

	// 面板不在场（无阶段色也无头像）＝ 一轮爆裂结束，推进到下一轮并清空防重复标记。
	// 整轮未指定时，高频循环已经由灰色爆裂条进入；按键不等待阶段色、槽位或 ReadySlots。
	// 只有在此前确实看到过面板且空窗超过保护窗口后，才回落到低频充能检测。
	if !res.Present && fallbackLoop && burstRound.was(taskID) && burstRound.shouldEndAfterAbsent(taskID) {
		burstRound.advance(taskID, cfg.RoundCount)
		burstRound.markReturnToLow(taskID)
		burstRound.resetLastStage(taskID)
		burstState.reset(taskID)
		burstRound.setWas(taskID, false)
		return true
	}
	if !res.Present && !fallbackLoop {
		if burstRound.shouldEndAfterAbsent(taskID) {
			burstRound.advance(taskID, cfg.RoundCount)
			burstRound.markReturnToLow(taskID)
			burstRound.resetLastStage(taskID)
			burstState.reset(taskID)
			burstRound.setWas(taskID, false)
		}
		return true
	}
	if res.Present {
		burstRound.setWas(taskID, true)
	}

	// 阶段决策：显式爆裂轴在阶段色缺失时按预测的下一阶段抢先发键；
	// 整轮未指定时在高频循环中直接按固定 ASD 顺序。
	// 注意：不再用固定间隔盲发（先前 fastseq 会连续多次 ctx.RunTask 触发嵌套子任务，
	// 在“指定/就绪角色冷却中仍强发”时导致框架任务器阻塞、任务无法被 PostStop 中断）。
	// 显式轴仍仅在“检测到目标角色就绪”时经 ClickKey 发键；默认轴不依赖槽位识别。
	decisionStage := res.Stage
	if fallbackLoop {
		decisionStage = burstRound.fallbackSlot(taskID)
	} else if res.Stage == 0 {
		if burstRound.stageWasConsumed(taskID, maxStages) {
			return true
		}
		// 阶段色刚消失时抢先按预测的下一阶段。
		decisionStage = res.TransitionStage
		if decisionStage == 0 {
			return true
		}
		// 同一过渡窗口只预发一次：仅首次进入时允许发键；后续帧直接等待阶段确认
		// （observeStage 会通过 matched/cancelled 处理），避免 shouldRetry/attemptSlot
		// 在同一过渡内重复发键（曾出现“阶段Ⅱ被预发两次”并拖慢节奏）。
		if !burstRound.beginTransition(taskID, decisionStage) {
			return true
		}
		burstState.reset(taskID)
	} else if changed, matched, cancelled := burstRound.observeStage(taskID, res.Stage); !burstRound.stageWasConsumed(taskID, maxStages) && shouldResetStage(res.Stage, changed, matched, cancelled, burstRound.shouldRetry(taskID, res.Stage)) {
		// 阶段变更、预发未跳过当前阶段，或当前阶段未推进超过一次自然识别周期时，
		// 允许按当前阶段再发一次。最终阶段(Ⅲ)matched 不再 reset，避免全爆裂播片期间冗余重发。
		burstState.reset(taskID)
	}

	routeRes := res
	routeRes.Stage = decisionStage
	var act, actKey string
	var pressedSlot int
	if fallbackLoop {
		act, actKey, pressedSlot = fallbackLoopAction(burstRound.fallbackSlot(taskID))
	} else if res.Stage == 0 {
		act, actKey, pressedSlot = transitionAction(&routeRes, cfg, curRound,
			burstRound.attemptSlot(taskID, decisionStage), burstRound.lastAttemptSlot(taskID))
	} else {
		act, actKey, pressedSlot = routeAction(&routeRes, cfg, curRound, func(slot int) bool {
			return burstState.get(taskID, slot)
		})
	}

	// 应用路由决策：释放时发键并记录防重复；指定角色冷却时标记其可再次释放。
	released := false
	if pressedSlot != 0 {
		switch act {
		case "release":
			if pressKey(ctx, pressedSlot) {
				if fallbackLoop {
					burstRound.advanceFallbackSlot(taskID)
				} else {
					burstRound.recordAttempt(taskID, decisionStage, pressedSlot)
					burstRound.markStageConsumed(taskID, decisionStage)
					if isFallbackStage(cfg, curRound, decisionStage) {
						burstState.setAll(taskID)
					} else {
						burstState.set(taskID, pressedSlot, true)
					}
				}
				released = true
			}
		case "wait":
			burstState.set(taskID, pressedSlot, false)
		}
	}

	// 仅在实际释放时输出一条简洁 focus，其余帧静默，避免过渡/无角色等冗余刷屏。
	if released {
		log.Info().
			Str("component", "CustomBurst").
			Int("observed_stage", res.Stage).
			Int("decision_stage", decisionStage).
			Str("key", actKey).
			Msg("burst key dispatched")
		maafocus.Print(ctx, buildReleaseMessage(curRound, decisionStage, actKey))
	}
	return true
}

func isFallbackStage(cfg burstConfig, round, stage int) bool {
	return round >= 0 && round < maxRounds && stage >= 1 && stage <= maxStages && cfg.Rounds[round][stage-1] == ""
}

// isFallbackLoopRound 判断当前轮是否完全未指定爆裂轴。
// 只有整轮为空时才允许高频 ASD 盲循环；混合配置继续使用阶段驱动，避免盲按穿透到指定阶段。
func isFallbackLoopRound(cfg burstConfig, round int) bool {
	if round < 0 || round >= maxRounds {
		return false
	}
	for _, key := range cfg.Rounds[round] {
		if strings.TrimSpace(key) != "" {
			return false
		}
	}
	return true
}

// fallbackLoopAction 返回高频 ASD 循环的当前按键。
func fallbackLoopAction(slot int) (string, string, int) {
	key := slotKey[slot]
	if key == "" {
		return "none", "", 0
	}
	return "release", key, slot
}

// transitionAction 在阶段色消失窗口抢先选择下一阶段的键。
// 显式爆裂轴只在当前过渡帧确认目标槽位就绪时发键；未指定轴不在过渡帧
// 发键，而是等待实际阶段切换后按固定 ASD 顺序，避免阶段动画空窗导致串键。
func transitionAction(res *FastBurstResult, cfg burstConfig, round, sameStageSlot, previousSlot int) (string, string, int) {
	_ = sameStageSlot
	_ = previousSlot
	if round < 0 || round >= maxRounds || res.Stage < 1 || res.Stage > maxStages {
		return "none", "", 0
	}
	if key := cfg.Rounds[round][res.Stage-1]; key != "" {
		slot := keyToSlot(key)
		if slot == 0 {
			return "none", "", 0
		}
		if containsInts(res.CDSlots, slot) {
			return "wait", key, slot
		}
		if !containsInts(res.ReadySlots, slot) {
			return "none", "", 0
		}
		return "release", key, slot
	}

	// 未指定阶段由实际阶段确认后的 routeAction 负责按固定 ASD 顺序释放。
	return "none", "", 0
}

// routeAction 依据配置（多轮爆裂轴）与检测结果做纯决策，返回 (动作, 动作键, 涉及槽位)。
// 引擎"按检测到的阶段跟轴走"：取当前轮在第 res.Stage 阶段配置的角色，就绪则释放、冷却则等待。
// 重置（角色把爆裂拉回某阶段）由引擎对检测阶段的实时跟随自然适配。动作：
// release（释放该角色）、wait（等待其冷却）、notpresent（该角色未出现）、done（刚释放待冷却）、
// none（无就绪/无动作）。涉及槽位用于释放/防重复释放状态记录。
func routeAction(res *FastBurstResult, cfg burstConfig, round int, pressed func(int) bool) (string, string, int) {
	if round < 0 || round >= maxRounds {
		round = 0
	}
	var curKey string
	var curSlot int
	if res.Stage >= 1 && res.Stage <= maxStages {
		curKey = cfg.Rounds[round][res.Stage-1]
		if curKey != "" {
			curSlot = keyToSlot(curKey)
		}
	}

	switch {
	case curSlot != 0 && containsInts(res.CDSlots, curSlot):
		// 指定角色冷却中：固定"等待其冷却结束"（严格按轴，不替换）。
		return "wait", curKey, curSlot
	case curSlot != 0 && containsInts(res.PresentSlots, curSlot):
		// 指定角色就绪：若刚释放过则等待 CD 显示（done），否则释放。
		if pressed(curSlot) {
			return "done", curKey, 0
		}
		return "release", curKey, curSlot
	case curSlot != 0:
		// 指定角色未在当前阶段出现。
		return "notpresent", curKey, 0
	default:
		// 混合配置中的未指定阶段仍使用旧的就绪槽位兜底；整轮完全未指定时，
		// 由 CustomBurstRouteAction 的 fallbackLoop 分支直接执行 ASD 循环。
		return fbDecision(res, pressed)
	}
}

// fbDecision 快速爆裂兜底：自上而下第一个未"刚释放待冷却"的就绪槽位。
func fbDecision(res *FastBurstResult, pressed func(int) bool) (string, string, int) {
	for _, slot := range res.ReadySlots {
		if pressed(slot) {
			continue
		}
		return "release", slotKey[slot], slot
	}
	return "none", "", 0
}

// pressKey 通过一次 ctx.RunAction 直接执行 Pipeline 的 ClickKey 动作节点。
// 参考 MaaEnd：动作节点为纯 action（无 recognition），用 ctx.RunAction 直接执行，
// 避免对每个按键都 ctx.RunTask 跑一条完整 PipelineTask——后者在快速连续触发时
// 会与宿主 pipeline 在任务器上争用，导致嵌套任务阻塞、任务无法被 PostStop 中断。
// 点按时长由 MaaFramework/控制器实现，避免 Go 手动制造过短的 KeyDown -> KeyUp。
func pressKey(ctx *maa.Context, slot int) bool {
	if ctx == nil || slot < 1 || slot > 3 {
		log.Warn().Str("component", "CustomBurst").Int("slot", slot).Msg("skip key press: invalid context/slot")
		return false
	}
	if _, err := ctx.RunAction(clickKeyNode(slot), maa.Rect{}, "", nil); err != nil {
		log.Warn().Err(err).Str("component", "CustomBurst").Str("node", clickKeyNode(slot)).Msg("burst key action failed")
		return false
	}
	return true
}

// buildReleaseMessage 生成释放时的简洁 focus 文案："第N轮·爆裂X阶段 · 按K释放"。
func buildReleaseMessage(round, stage int, key string) string {
	return i18n.T("tasker.custom_burst.released", round+1, stage, key)
}
