package equipmentreroll

import (
	"encoding/json"
	"fmt"
	"image"
	"strings"
	"unicode"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// 本文件实现"洗词条"执行层的 MaaFramework Custom 组件：
//   - EquipmentRerollPartNeedRecognition：四优/四攻四优决策——选择需要洗的部位 / 判断全部完成；
//   - EquipmentRerollResultDecideRecognition：效果变更结果页决策——点击"效果变更"或"效果维持"。
//
// 界面坐标说明：结果页坐标基于 1280x720 样本校准。

// partAll 表示一次判断四件装备是否全部达标。
const partAll = "all"

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
// 兼容旧任务：仅传 target_effect（四优）；新模板可传 template 或 target_effects。
type partNeedParam struct {
	// Part 要判断的部位："头部"/"臂部"/"身躯"/"腿部"，或 "all" 表示判断四件全部达标。
	Part string `json:"part"`
	// TargetEffect 单目标效果名称（四优），缺省为"优越代码伤害增加"。
	TargetEffect string `json:"target_effect"`
	// TargetEffects 多目标效果名称（四攻四优传 ["优越代码伤害增加","攻击力增加"]）。
	TargetEffects []string `json:"target_effects"`
	// Template 模板标识：FourElementalDamage / FourAttackFourElementalDamage，缺省按 TargetEffect 推断。
	Template string `json:"template"`
}

func (p *partNeedParam) normalize() {
	p.Part = strings.TrimSpace(p.Part)
	p.TargetEffect = strings.TrimSpace(p.TargetEffect)
	p.Template = strings.TrimSpace(p.Template)
	for i, e := range p.TargetEffects {
		p.TargetEffects[i] = strings.TrimSpace(e)
	}
	if len(p.TargetEffects) == 0 && p.TargetEffect != "" {
		// 旧任务单字段兼容
	}
	if p.Template == "" {
		if len(p.TargetEffects) == 2 {
			p.Template = "FourAttackFourElementalDamage"
		} else if p.TargetEffect == TargetEffectElementalDamage || p.TargetEffect == "" {
			p.Template = "FourElementalDamage"
		}
	}
	if p.TargetEffect == "" && len(p.TargetEffects) == 0 {
		p.TargetEffect = TargetEffectElementalDamage
	}
}

func (p *partNeedParam) isFourAtkFourElem() bool {
	if p.Template == "FourAttackFourElementalDamage" || p.Template == "FourAtkFourElem" {
		return true
	}
	if len(p.TargetEffects) == 2 {
		hasElem, hasAtk := false, false
		for _, e := range p.TargetEffects {
			if e == TargetEffectElementalDamage {
				hasElem = true
			}
			if e == TargetEffectAttackIncrease {
				hasAtk = true
			}
		}
		if hasElem && hasAtk {
			return true
		}
	}
	return false
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

	// 四攻四优分支
	if params.isFourAtkFourElem() {
		return r.runFourAtkFourElem(arg, params)
	}

	// 四优分支（兼容旧）
	parts, ok := GetEquipmentEffects(arg.TaskID)
	if !ok {
		log.Warn().
			Int64("task_id", arg.TaskID).
			Str("part", params.Part).
			Msg("equipment snapshot is incomplete; skip reroll")
		return nil, false
	}

	if params.Part == partAll {
		if AllPartsSatisfied(parts, params.TargetEffect) {
			log.Info().Str("component", "EquipmentReroll").Msg("all four parts satisfy four-elemental-damage target")
			return &maa.CustomRecognitionResult{Box: arg.Roi, Detail: "{}"}, true
		}
		return nil, false
	}

	effects, ok := parts[params.Part]
	if !ok {
		log.Warn().
			Int64("task_id", arg.TaskID).
			Str("part", params.Part).
			Msg("part not found in equipment snapshot")
		return nil, false
	}
	if PartHasEffect(effects, params.TargetEffect) {
		log.Info().
			Str("component", "EquipmentReroll").
			Str("part", params.Part).
			Msg("part already satisfies four-elemental-damage target")
		return nil, false
	}

	if err := setCurrentPart(arg.TaskID, params.Part); err != nil {
		log.Error().Err(err).Str("component", "EquipmentReroll").Msg("failed to mark current part for reroll")
		return nil, false
	}
	log.Info().
		Str("component", "EquipmentReroll").
		Int64("task_id", arg.TaskID).
		Str("part", params.Part).
		Msg("part needs reroll")
	return &maa.CustomRecognitionResult{Box: arg.Roi, Detail: "{}"}, true
}

func (r *EquipmentRerollPartNeedRecognition) runFourAtkFourElem(arg *maa.CustomRecognitionArg, params partNeedParam) (*maa.CustomRecognitionResult, bool) {
	parts, ok := GetEquipmentSlotScans(arg.TaskID)
	if !ok {
		// 兼容：若完整快照缺失，退化为词条快照判断
		effParts, ok2 := GetEquipmentEffects(arg.TaskID)
		if !ok2 {
			log.Warn().Int64("task_id", arg.TaskID).Msg("equipment snapshot is incomplete; skip reroll")
			return nil, false
		}
		if params.Part == partAll {
			if AllPartsSatisfiedFourAtkFourElem(effParts) {
				log.Info().Str("component", "EquipmentReroll").Msg("all four parts satisfy four-atk-four-elem target")
				return &maa.CustomRecognitionResult{Box: arg.Roi, Detail: "{}"}, true
			}
			return nil, false
		}
		effects, ok := effParts[params.Part]
		if !ok {
			return nil, false
		}
		if PartHasBothEffects(effects) {
			return nil, false
		}
		if err := setCurrentPart(arg.TaskID, params.Part); err != nil {
			return nil, false
		}
		return &maa.CustomRecognitionResult{Box: arg.Roi, Detail: "{}"}, true
	}

	if params.Part == partAll {
		// 转成 [maxSlot]string map 供 AllPartsSatisfiedFourAtkFourElem 复用
		m := make(map[string][maxSlot]string, len(parts))
		for p, scan := range parts {
			m[p] = scan.Effects()
		}
		if AllPartsSatisfiedFourAtkFourElem(m) {
			log.Info().Str("component", "EquipmentReroll").Msg("all four parts satisfy four-atk-four-elem target")
			return &maa.CustomRecognitionResult{Box: arg.Roi, Detail: "{}"}, true
		}
		return nil, false
	}

	scan, ok := parts[params.Part]
	if !ok {
		log.Warn().Int64("task_id", arg.TaskID).Str("part", params.Part).Msg("part not found in equipment snapshot")
		return nil, false
	}
	if PartHasBothEffects(scan.Effects()) {
		log.Info().Str("component", "EquipmentReroll").Str("part", params.Part).Msg("part already satisfies four-atk-four-elem target")
		return nil, false
	}
	if err := setCurrentPart(arg.TaskID, params.Part); err != nil {
		log.Error().Err(err).Str("component", "EquipmentReroll").Msg("failed to mark current part for reroll")
		return nil, false
	}
	effects := scan.Effects()
	log.Info().Str("component", "EquipmentReroll").Int64("task_id", arg.TaskID).Str("part", params.Part).
		Strs("effects", effects[:]).Msg("part needs reroll (four-atk-four-elem)")
	return &maa.CustomRecognitionResult{Box: arg.Roi, Detail: "{}"}, true
}

// resultDecideParam 是 EquipmentRerollResultDecideRecognition 的参数。
type resultDecideParam struct {
	// TargetEffect 单目标效果名称，四优缺省。
	TargetEffect string `json:"target_effect"`
	// TargetEffects 多目标（四攻四优）。
	TargetEffects []string `json:"target_effects"`
	Template      string   `json:"template"`
}

func (p *resultDecideParam) normalize() {
	p.TargetEffect = strings.TrimSpace(p.TargetEffect)
	p.Template = strings.TrimSpace(p.Template)
	for i, e := range p.TargetEffects {
		p.TargetEffects[i] = strings.TrimSpace(e)
	}
	if len(p.TargetEffects) == 0 && p.TargetEffect == "" {
		p.TargetEffect = TargetEffectElementalDamage
	}
	if p.Template == "" {
		if len(p.TargetEffects) == 2 {
			p.Template = "FourAttackFourElementalDamage"
		} else if p.TargetEffect != "" {
			p.Template = "FourElementalDamage"
		}
	}
}

func (p *resultDecideParam) isFourAtkFourElem() bool {
	if p.Template == "FourAttackFourElementalDamage" || p.Template == "FourAtkFourElem" {
		return true
	}
	if len(p.TargetEffects) == 2 {
		hasElem, hasAtk := false, false
		for _, e := range p.TargetEffects {
			if e == TargetEffectElementalDamage {
				hasElem = true
			}
			if e == TargetEffectAttackIncrease {
				hasAtk = true
			}
		}
		return hasElem && hasAtk
	}
	return false
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

	// 四攻四优：需完整快照以判定锁定价值
	if params.isFourAtkFourElem() {
		return r.decideFourAtkFourElem(arg, params, part, changed, changedValues, changedRaw)
	}

	parts, ok := GetEquipmentEffects(arg.TaskID)
	if !ok {
		log.Error().Str("component", "EquipmentReroll").Msg("result decide snapshot is incomplete")
		return nil, false
	}
	current := parts[part]

	decision := DecideResultPage(current, changed, params.TargetEffect)

	if decision == ResultDecisionAccept {
		updatePartEffects(arg.TaskID, part, changed, changedValues)
	}
	// 维持或接受后，一次性锁均失效（自订密钥特性）；永久锁保留。
	// 四优无锁，可安全调用。
	expireOneTimeLocks(arg.TaskID, part)

	log.Info().
		Str("component", "EquipmentReroll").
		Int64("task_id", arg.TaskID).
		Strs("current", current[:]).
		Strs("changed", changed[:]).
		Strs("changed_values", changedValues[:]).
		Strs("raw_changed", changedRaw[:]).
		Str("decision", decision.String()).
		Msg("result page decision made")

	return &maa.CustomRecognitionResult{Box: arg.Roi, Detail: resultDecisionDetail(decision)}, true
}

func (r *EquipmentRerollResultDecideRecognition) decideFourAtkFourElem(arg *maa.CustomRecognitionArg, params resultDecideParam, part string, changed, changedValues, changedRaw [maxSlot]string) (*maa.CustomRecognitionResult, bool) {
	scan, ok := GetPartScan(arg.TaskID, part)
	if !ok {
		log.Error().Str("component", "EquipmentReroll").Msg("result decide part scan is missing")
		return nil, false
	}
	current := scan.Effects()
	decision := DecideResultPageFourAtkFourElem(current, changed, scan)
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
		Str("template", "FourAttackFourElementalDamage").
		Msg("result page decision made (four-atk-four-elem)")

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
	var effects [maxSlot]string
	var values [maxSlot]string
	var rawTexts [maxSlot]string
	var recognizedFlags [maxSlot]bool
	if ctx == nil || img == nil {
		return effects, values, rawTexts, false
	}
	validCount := 0
	for i, nodeName := range resultChangedEffectSlotNodes {
		detail, err := ctx.RunRecognition(nodeName, img, nil)
		if err != nil || detail == nil || !detail.Hit {
			continue
		}
		effect, raw, recognized := firstRecognizedEffect(detail)
		rawTexts[i] = raw
		values[i] = extractPercentValue(raw)
		recognizedFlags[i] = recognized
		if !recognized {
			continue
		}
		effects[i] = effect
		validCount++
	}
	// 瞬时“已解除效果锁定”出现时，当前帧不可信，返回失败让 Pipeline 重试。
	for _, raw := range rawTexts {
		if isTransientUnlockText(raw) {
			log.Warn().
				Str("component", "EquipmentReroll").
				Strs("raw_slots", rawTexts[:]).
				Msg("result page slot is in transient unlock state; retry recognition")
			return effects, values, rawTexts, false
		}
	}
	// 三个槽位都必须读到“官方效果”或明确的“未获得”，否则说明 OCR 不完整，
	// 返回失败重试，不能把残缺文本当作空槽继续决策。
	for i, raw := range rawTexts {
		if raw == "" || !recognizedFlags[i] {
			log.Warn().
				Str("component", "EquipmentReroll").
				Int("slot", i+1).
				Str("raw", raw).
				Strs("raw_slots", rawTexts[:]).
				Msg("result page slot OCR incomplete; retry recognition")
			return effects, values, rawTexts, false
		}
	}
	if validCount == 0 {
		return effects, values, rawTexts, false
	}
	return effects, values, rawTexts, true
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

	target := "EquipmentRerollResultClickAccept"
	if detail := customRecognitionDetail(arg); detail != "" {
		var d struct {
			Decision string `json:"decision"`
		}
		if err := json.Unmarshal([]byte(detail), &d); err == nil && d.Decision == "keep" {
			target = "EquipmentRerollResultClickKeep"
		}
	}
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

	target := "EquipmentRerollReturnToDecide"
	if part, ok := currentEffectPart(arg.TaskID); ok {
		if scan, ok2 := GetPartScan(arg.TaskID, part); ok2 {
			if isFourAtkTemplateFromLockNeed(ctx) {
				if !PartHasBothEffects(scan.Effects()) {
					target = "EquipmentRerollKeepLockGate"
				}
			} else {
				if !PartHasEffect(scan.Effects(), TargetEffectElementalDamage) {
					target = "EquipmentRerollKeepLockGate"
				}
			}
		}
	}

	if err := ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: target}}); err != nil {
		log.Error().Err(err).Str("component", "EquipmentReroll").Msg("failed to route after-accept")
		return false
	}
	log.Info().
		Str("component", "EquipmentReroll").
		Int64("task_id", arg.TaskID).
		Str("target", target).
		Msg("after-accept routed")
	return true
}
