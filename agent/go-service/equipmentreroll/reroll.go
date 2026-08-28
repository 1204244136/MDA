package equipmentreroll

import (
	"encoding/json"
	"fmt"
	"image"
	"strings"
	"unicode"

	"github.com/1204244136/MDA/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// 本文件实现"洗词条"执行层的 MaaFramework Custom 组件：
//   - EquipmentRerollPartNeedRecognition：自定义配额决策——选择需要洗的部位 / 判断全部完成；
//   - EquipmentRerollResultDecideRecognition：效果变更结果页决策——点击"效果变更"或"效果维持"。
//
// 文档索引：docs/zh_cn/nikke/EquipmentReroll/洗词条策略与Agent逻辑.md
//
// 界面坐标说明：结果页坐标基于 1280x720 样本校准。

// partAll 表示一次判断四件装备是否全部达标。
const partAll = "all"

func routeEquipmentRerollEnd(ctx *maa.Context, currentTaskName string) error {
	if ctx == nil {
		return fmt.Errorf("task context is nil")
	}
	return ctx.OverrideNext(currentTaskName, []maa.NextItem{{Name: "EquipmentRerollEnd"}})
}

// 结果页决策通过 CustomRecognitionResult.Detail 传递业务意图（decision），
// 按钮点击由 Pipeline 节点 EquipmentRerollResultClickKeep/Accept（OCR 定位）完成；
// Go 不持有界面坐标，坐标只维护在 Pipeline。

// resultDecisionDetail 把结果页决策序列化为 Detail JSON，供 ResultRouteAction 路由。
func resultDecisionDetail(d ResultDecision) string {
	key := "keep"
	if d == ResultDecisionAccept {
		key = "accept"
	}
	return fmt.Sprintf(`{"decision":"%s"}`, key)
}

var resultChangedEffectSlotNodes = [maxSlot]string{
	"__EquipmentRerollResultChangedEffectSlot1",
	"__EquipmentRerollResultChangedEffectSlot2",
	"__EquipmentRerollResultChangedEffectSlot3",
}

// partNeedParam 是 EquipmentRerollPartNeedRecognition 的参数。
type partNeedParam struct {
	// Part 要判断的部位："头部"/"臂部"/"身躯"/"腿部"，或 "all" 表示判断四件全部达标。
	Part string `json:"part"`
	// GlobalQuota 自定义词条配额：-1 禁止 / 0 不要求 / 1~4 需求。
	GlobalQuota map[string]int `json:"global_quota"`
}

func (p *partNeedParam) normalize() {
	p.Part = strings.TrimSpace(p.Part)
	if len(p.GlobalQuota) > 0 {
		p.GlobalQuota = normalizeQuota(p.GlobalQuota)
	}
}

// EquipmentRerollPartNeedRecognition 判断某个部位是否还需要洗词条。
// 命中（hit）表示"该部位需要洗"（或 part=all 时表示"四件全部达标"），
// 由决策分发节点根据 next 顺序决定执行哪个部位。
type EquipmentRerollPartNeedRecognition struct{}

var _ maa.CustomRecognitionRunner = &EquipmentRerollPartNeedRecognition{}

func (r *EquipmentRerollPartNeedRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if arg == nil {
		log.Error().Str("component", "EquipmentReroll").Msg("custom recognition argument is nil")
		return nil, false
	}

	var params partNeedParam
	if err := json.Unmarshal([]byte(arg.CustomRecognitionParam), &params); err != nil {
		log.Error().Err(err).Str("component", "EquipmentReroll").Msg("failed to parse part need param")
		return nil, false
	}
	params.normalize()

	// 角色配额统一从承载点读取（attach.quota_* 优先，为空时回退本节点自带默认）。
	params.GlobalQuota = loadCarrierConfig(ctx).resolveQuota(params.GlobalQuota)
	if !quotaIsValid(params.GlobalQuota) {
		log.Error().Str("component", "EquipmentReroll").Int64("task_id", arg.TaskID).Msg("part need recognition requires a valid 1 to 12 affix quota")
		return nil, false
	}
	return r.runQuota(arg, params)
}

func (r *EquipmentRerollPartNeedRecognition) runQuota(arg *maa.CustomRecognitionArg, params partNeedParam) (*maa.CustomRecognitionResult, bool) {
	quota := normalizeQuota(params.GlobalQuota)
	if !quotaIsValid(quota) {
		log.Error().
			Str("component", "EquipmentReroll").
			Int64("task_id", arg.TaskID).
			Int("quota_total", quotaTotal(quota)).
			Msg("custom quota requires 1 to 12 affixes")
		return nil, false
	}

	parts, ok := GetEquipmentSlotScans(arg.TaskID)
	if !ok {
		log.Warn().
			Int64("task_id", arg.TaskID).
			Str("part", params.Part).
			Msg("equipment snapshot is incomplete; skip custom quota reroll")
		return nil, false
	}

	if params.Part == partAll {
		if AllPartsSatisfiedQuota(parts, quota) {
			log.Info().Str("component", "EquipmentReroll").Msg("all four parts satisfy custom quota target")
			// 用户可见摘要由后继 EquipmentRerollFinalSummaryAction 统一通过 focus 输出。
			return &maa.CustomRecognitionResult{Box: arg.Roi, Detail: "{}"}, true
		}
		return nil, false
	}

	if !PartNeedsRerollQuota(parts, params.Part, quota) {
		log.Info().Str("component", "EquipmentReroll").Str("part", params.Part).Msg("part already satisfies custom quota target")
		return nil, false
	}
	if err := setCurrentPart(arg.TaskID, params.Part); err != nil {
		log.Error().Err(err).Str("component", "EquipmentReroll").Msg("failed to mark current part for reroll")
		return nil, false
	}
	log.Info().Str("component", "EquipmentReroll").Int64("task_id", arg.TaskID).Str("part", params.Part).
		Msg("part needs reroll (custom quota)")
	return &maa.CustomRecognitionResult{Box: arg.Roi, Detail: "{}"}, true
}

// resultDecideParam 是 EquipmentRerollResultDecideRecognition 的参数。
type resultDecideParam struct {
	// GlobalQuota 自定义词条配额：-1 禁止 / 0 不要求 / 1~4 需求（角色模式）。
	GlobalQuota map[string]int `json:"global_quota"`
}

func (p *resultDecideParam) normalize() {
	if len(p.GlobalQuota) > 0 {
		p.GlobalQuota = normalizeQuota(p.GlobalQuota)
	}
}

// EquipmentRerollResultDecideRecognition 在效果变更结果页上读取
// 三个"变更效果"槽位，决策点击"效果变更"还是"效果维持"，并返回
// 应点击按钮的位置（供节点 action: Click 使用）。只有确认 OPTION CHANGE
// 结果页后才命中；任一槽位 OCR 不完整时返回 false，不进入任何点击分支。
type EquipmentRerollResultDecideRecognition struct{}

var _ maa.CustomRecognitionRunner = &EquipmentRerollResultDecideRecognition{}

func (r *EquipmentRerollResultDecideRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil || arg.Img == nil {
		log.Error().Str("component", "EquipmentReroll").Msg("invalid result decide context")
		return nil, false
	}

	var params resultDecideParam
	if err := json.Unmarshal([]byte(arg.CustomRecognitionParam), &params); err != nil {
		log.Error().Err(err).Str("component", "EquipmentReroll").Msg("failed to parse result decide param")
		return nil, false
	}
	params.normalize()

	// 识别统一复用 Pipeline 定义的内部节点，识别过程在 MaaFramework 调试中可见。
	titleDetail, err := ctx.RunRecognition("__EquipmentRerollResultPageTitle", arg.Img, nil)
	if err != nil || titleDetail == nil || !titleDetail.Hit {
		log.Debug().
			Str("component", "EquipmentReroll").
			Int64("task_id", arg.TaskID).
			Msg("OPTION CHANGE result page was not confirmed")
		return nil, false
	}

	changed, changedValues, changedRaw, ok := recognizeChangedEffects(ctx, arg.Img)
	if !ok {
		log.Warn().
			Str("component", "EquipmentReroll").
			Int64("task_id", arg.TaskID).
			Strs("raw_changed", changedRaw[:]).
			Msg("changed result effects are incomplete; keep existing snapshot and retry")
		return nil, false
	}

	part, ok := currentEffectPart(arg.TaskID)
	if !ok {
		log.Error().Str("component", "EquipmentReroll").Msg("result decide part is not set")
		return nil, false
	}

	// 全部任务选项统一从承载点读取；一次 Run 只读一次。
	cfg := loadCarrierConfig(ctx)
	if cfg.isSingle() {
		return r.decideSingle(arg, part, changed, changedValues, changedRaw, cfg)
	}

	// 角色配额：承载点 attach.quota_* 优先，为空时回退本节点自带默认。
	params.GlobalQuota = cfg.resolveQuota(params.GlobalQuota)
	if !quotaIsValid(params.GlobalQuota) {
		log.Error().Str("component", "EquipmentReroll").Int64("task_id", arg.TaskID).Msg("result decide requires a valid 1 to 12 affix quota")
		return nil, false
	}
	return r.decideQuota(arg, params, part, changed, changedValues, changedRaw)
}

// decideSingle 单件模式结果页决策：用单件期望剩余模块成本比较当前/候选状态。
func (r *EquipmentRerollResultDecideRecognition) decideSingle(arg *maa.CustomRecognitionArg, part string, changed, changedValues, changedRaw [maxSlot]string, cfg carrierConfig) (*maa.CustomRecognitionResult, bool) {
	scan, ok := GetPartScan(arg.TaskID, part)
	if !ok {
		log.Error().Str("component", "EquipmentReroll").Msg("single equipment result decide part scan is missing")
		return nil, false
	}
	if !cfg.singleTargetOK() {
		log.Error().
			Str("component", "EquipmentReroll").
			Int64("task_id", arg.TaskID).
			Int("want_count", len(cfg.Target.Want)).
			Str("problem", cfg.TargetProblem).
			Msg("single equipment result decide requires a valid 1 to 3 affix target")
		return nil, false
	}
	t := cfg.Target

	current := scan.Effects()
	decision := DecideResultPageSingle(changed, scan, t)
	if decision == ResultDecisionAccept {
		updatePartEffects(arg.TaskID, part, changed, changedValues)
	}
	expireOneTimeLocks(arg.TaskID, part)

	log.Info().
		Str("component", "EquipmentReroll").
		Int64("task_id", arg.TaskID).
		Str("part", part).
		Strs("current", current[:]).
		Strs("changed", changed[:]).
		Strs("changed_values", changedValues[:]).
		Strs("raw_changed", changedRaw[:]).
		Str("decision", decision.String()).
		Float64("current_cost", singleExpectedCost(scan, t)).
		Float64("candidate_cost", singleExpectedCostOfEffects(changed, scan, t)).
		Msg("single equipment result page decision made")

	return &maa.CustomRecognitionResult{Box: arg.Roi, Detail: resultDecisionDetail(decision)}, true
}

// singleExpectedCostOfEffects 构造把 changed 写入快照后的候选扫描，并计算单件期望成本。
func singleExpectedCostOfEffects(changed [maxSlot]string, scan partScan, t singleTarget) float64 {
	cand := scan
	for i := range cand.Slots {
		cand.Slots[i].Effect = changed[i]
	}
	return singleExpectedCost(cand, t)
}

func (r *EquipmentRerollResultDecideRecognition) decideQuota(arg *maa.CustomRecognitionArg, params resultDecideParam, part string, changed, changedValues, changedRaw [maxSlot]string) (*maa.CustomRecognitionResult, bool) {
	scan, ok := GetPartScan(arg.TaskID, part)
	if !ok {
		log.Error().Str("component", "EquipmentReroll").Msg("result decide part scan is missing")
		return nil, false
	}
	quota := normalizeQuota(params.GlobalQuota)
	if !quotaIsValid(quota) {
		log.Error().
			Str("component", "EquipmentReroll").
			Int64("task_id", arg.TaskID).
			Int("quota_total", quotaTotal(quota)).
			Msg("custom quota requires 1 to 12 affixes")
		return nil, false
	}

	current := scan.Effects()
	decision := ResultDecisionKeep
	decisionDetail := ""
	if allParts, okAll := GetEquipmentSlotScans(arg.TaskID); okAll {
		candidateParts := make(map[string]partScan, len(allParts))
		for p, s := range allParts {
			candidateParts[p] = s
		}
		candidate := candidateParts[part]
		for i := range candidate.Slots {
			candidate.Slots[i].Effect = changed[i]
			candidate.Slots[i].Value = changedValues[i]
		}
		candidateParts[part] = candidate
		// 方向 A：决策改为期望成本。候选全局期望剩余模块数严格更低才接受，
		// 而非旧积分制的"已匹配配额数 × 100 + 槽位结构分"。
		// 期望成本由 expectedModulesForQuota 计算（槽位获得概率 / 效果权重 / 同结果排除 / 锁定与重洗费用）。
		currentCost := expectedModulesForQuota(allParts, quota)
		candidateCost := expectedModulesForQuota(candidateParts, quota)
		if candidateCost < currentCost-1e-6 {
			decision = ResultDecisionAccept
		}
		decisionDetail = fmt.Sprintf("current_cost=%.2f candidate_cost=%.2f", currentCost, candidateCost)
	} else {
		// 全局快照缺失时的降级路径（单件期望成本比较）。
		decision = DecideResultPageQuota(current, changed, scan, quota)
	}
	if decision == ResultDecisionAccept {
		updatePartEffects(arg.TaskID, part, changed, changedValues)
	}
	expireOneTimeLocks(arg.TaskID, part)

	log.Info().
		Str("component", "EquipmentReroll").
		Int64("task_id", arg.TaskID).
		Strs("current", current[:]).
		Strs("changed", changed[:]).
		Strs("changed_values", changedValues[:]).
		Strs("raw_changed", changedRaw[:]).
		Str("decision", decision.String()).
		Str("expected_cost", decisionDetail).
		Msg("result page decision made (custom quota)")

	return &maa.CustomRecognitionResult{Box: arg.Roi, Detail: resultDecisionDetail(decision)}, true
}

// isTransientUnlockText 判断 OCR 文本是否为“已解除效果锁定”等瞬时提示。
// 该提示会在一次性锁解除后闪烁出现，槽位此时没有可读取的词条文本；
// 若被当作空槽处理，会导致结果页快照误刷新，因此需要让整次识别失败并重试。
func isTransientUnlockText(raw string) bool {
	compact := strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, raw)
	return strings.Contains(compact, "解除")
}

// recognizeChangedEffects 逐槽读取结果页的变更效果与数值。
// 复用 Pipeline 定义的 __EquipmentRerollResultChangedEffectSlotN 内部 OCR 节点
// （决策页沿用之前的锚点方案，不使用 Flag offset）。
// 槽位读到官方效果或"未获得效果"都有效；其他任何非空但未识别的文本（如 OCR 截断的
// “击增加]”）都不能当作空槽，必须让整次识别失败并重试，避免因漏读目标词条导致误决策。
// 瞬时“已解除效果锁定”同样视为无效帧。
// 返回：effects（词条名）、values（从 OCR 原文提取的数值）、raws（OCR 原文）。
func recognizeChangedEffects(ctx *maa.Context, img image.Image) ([maxSlot]string, [maxSlot]string, [maxSlot]string, bool) {
	if ctx == nil || img == nil {
		return [maxSlot]string{}, [maxSlot]string{}, [maxSlot]string{}, false
	}

	// 从所有槽位提取原始 OCR 数据
	effects, values, rawTexts, recognized := extractSlotOCRData(ctx, img)

	// 检查瞬时解锁状态（必须重试）
	if hasTransientUnlockState(rawTexts) {
		log.Warn().
			Str("component", "EquipmentReroll").
			Strs("raw_slots", rawTexts[:]).
			Msg("result page slot is in transient unlock state; retry recognition")
		return effects, values, rawTexts, false
	}

	// 验证识别完整性
	if !validateSlotRecognitionCompleteness(effects, values, rawTexts, recognized) {
		return effects, values, rawTexts, false
	}

	return effects, values, rawTexts, true
}

// extractSlotOCRData 读取结果页三个槽位的 OCR 结果。
func extractSlotOCRData(ctx *maa.Context, img image.Image) ([maxSlot]string, [maxSlot]string, [maxSlot]string, [maxSlot]bool) {
	var effects [maxSlot]string
	var values [maxSlot]string
	var rawTexts [maxSlot]string
	var recognizedFlags [maxSlot]bool

	for i, nodeName := range resultChangedEffectSlotNodes {
		detail, err := ctx.RunRecognition(nodeName, img, nil)
		if err != nil || detail == nil || !detail.Hit {
			continue
		}
		effect, raw, recognized := firstRecognizedEffect(detail)
		rawTexts[i] = raw
		values[i] = extractPercentValue(raw)
		recognizedFlags[i] = recognized
		if recognized {
			effects[i] = effect
		}
	}

	return effects, values, rawTexts, recognizedFlags
}

// hasTransientUnlockState 检查是否有槽位显示瞬时解锁通知。
func hasTransientUnlockState(rawTexts [maxSlot]string) bool {
	for _, raw := range rawTexts {
		if isTransientUnlockText(raw) {
			return true
		}
	}
	return false
}

// validateSlotRecognitionCompleteness 确保所有槽位都被正确识别。
// 三个槽位必须都读到官方效果或明确的“未获得”标记。
func validateSlotRecognitionCompleteness(effects, values, rawTexts [maxSlot]string, recognized [maxSlot]bool) bool {
	// 三个槽位都必须读到“官方效果”或明确的“未获得”，否则说明 OCR 不完整，
	// 返回失败重试，不能把残缺文本当作空槽继续决策。
	for i, raw := range rawTexts {
		if raw == "" || !recognized[i] {
			log.Warn().
				Str("component", "EquipmentReroll").
				Int("slot", i+1).
				Str("raw", raw).
				Strs("raw_slots", rawTexts[:]).
				Msg("result page slot OCR incomplete; retry recognition")
			return false
		}
		// 非空词条必须同时读到百分比；否则接受后无法展示数值/档位，
		// 让当前帧重试而不是写入不完整的快照。
		if effects[i] != "" && values[i] == "" {
			log.Warn().
				Str("component", "EquipmentReroll").
				Int("slot", i+1).
				Str("raw", raw).
				Msg("result page slot value OCR incomplete; retry recognition")
			return false
		}
	}
	return true
}

// customRecognitionDetail 从 CustomAction 的 RecognitionDetail 中提取自定义识别返回的 Detail 字符串。
// RecognitionDetail.DetailJson 是整个识别结果包装（all/best/filtered），
// 自定义识别的业务 detail 实际嵌套在 Best（或 All[0]）的 CustomRecognitionResult.Detail 中。
func customRecognitionDetail(arg *maa.CustomActionArg) string {
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

func resultRouteTarget(detail string) string {
	var decision struct {
		Decision string `json:"decision"`
	}
	if err := json.Unmarshal([]byte(detail), &decision); err == nil && decision.Decision == "keep" {
		return "EquipmentRerollResultClickKeep"
	}
	return "EquipmentRerollResultClickAccept"
}

// EquipmentRerollResultRouteAction 根据 EquipmentRerollResultDecideRecognition 返回的决策
// 路由到对应的点击节点：效果维持（返回效果变更详情页继续洗同一件）
// 或效果变更（接受变更后回人物页重扫调度）。决策通过 Detail JSON 传递，不依赖坐标。
type EquipmentRerollResultRouteAction struct{}

var _ maa.CustomActionRunner = &EquipmentRerollResultRouteAction{}

func (a *EquipmentRerollResultRouteAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		log.Error().Str("component", "EquipmentReroll").Msg("invalid result route context")
		return false
	}

	target := resultRouteTarget(customRecognitionDetail(arg))
	if err := ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: target}}); err != nil {
		log.Error().Err(err).Str("component", "EquipmentReroll").Msg("failed to route result page decision")
		return false
	}
	log.Info().
		Str("component", "EquipmentReroll").
		Int64("task_id", arg.TaskID).
		Str("target", target).
		Msg("result page decision routed")
	return true
}

// EquipmentRerollAfterAcceptRouteAction 在点击“效果变更”后分流：
//   - 当前部位仍未满足目标 → 不关闭确认页，直接回 EquipmentRerollKeepLockGate 继续锁定/重洗；
//   - 当前部位已满足目标 → 返回人物页重新调度。
//
// 注意：接受变更后实际停在“效果变更确认页”，因此不能回详情页 LockGate，应复用 Keep 分支的确认页锁定/重洗流程。
// 这样避免“洗同一件还要先关闭再重新打开详情页”的冗余动作。
type EquipmentRerollAfterAcceptRouteAction struct{}

var _ maa.CustomActionRunner = &EquipmentRerollAfterAcceptRouteAction{}

func (a *EquipmentRerollAfterAcceptRouteAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		log.Error().Str("component", "EquipmentReroll").Msg("invalid after-accept route context")
		return false
	}

	// 结果页决策已在识别阶段更新快照；接受按钮完成后立即输出当前部位，
	// 让用户能实时看到本次效果变更后的三条词条，再继续后续调度。
	if part, ok := currentEffectPart(arg.TaskID); ok {
		if scan, ok := GetPartScan(arg.TaskID, part); ok {
			maafocus.Print(ctx, buildPartEffectsMessage(part, scan, "tasker.equipment_reroll.effects_changed"))
		} else {
			log.Warn().Str("component", "EquipmentReroll").Int64("task_id", arg.TaskID).Str("part", part).
				Msg("accepted result snapshot is missing; skip user-facing effect summary")
		}
	} else {
		log.Warn().Str("component", "EquipmentReroll").Int64("task_id", arg.TaskID).
			Msg("accepted result part is missing; skip user-facing effect summary")
	}

	// 单件模式：接受后回单件决策（关闭页面 → SingleDecide 判断是否达标）；与角色模式保持一致的调度语义。
	targetNode := "EquipmentRerollReturnToDecide"
	if loadCarrierConfig(ctx).isSingle() {
		targetNode = "EquipmentRerollSingleReturnToDecide"
	}

	if err := ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: targetNode}}); err != nil {
		log.Error().Err(err).Str("component", "EquipmentReroll").Msg("failed to route after-accept")
		return false
	}
	log.Info().
		Str("component", "EquipmentReroll").
		Int64("task_id", arg.TaskID).
		Str("target", targetNode).
		Msg("after-accept routed")
	return true
}
