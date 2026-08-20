package equipmentreroll

import (
	"encoding/json"
	"image"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// 本文件实现"洗词条"执行层的 MaaFramework Custom 组件：
//   - EquipmentRerollPartNeedRecognition：四优决策——选择需要洗的部位 / 判断全部完成；
//   - EquipmentRerollResultDecideRecognition：效果变更结果页决策——点击"效果变更"或"效果维持"。
//
// 界面坐标说明：结果页坐标基于 1280x720 样本校准。

// partAll 表示一次判断四件装备是否全部达标。
const partAll = "all"

// 结果页"效果变更"/"效果维持"按钮位置（决策后由节点 action: Click 点击）。
// 结果页标题与变更效果槽位的 OCR 识别定义在 Pipeline（__EquipmentRerollResult* 节点），
// 由 EquipmentRerollResultDecideRecognition 通过 ctx.RunRecognition 复用，识别过程可在 MaaFramework 调试中查看。
var (
	resultAcceptButtonBox = maa.Rect{645, 505, 165, 55} // "效果变更"按钮（接受变更）
	resultKeepButtonBox   = maa.Rect{480, 505, 160, 55} // "效果维持"按钮（保留当前）
)

var resultChangedEffectSlotNodes = [maxSlot]string{
	"__EquipmentRerollResultChangedEffectSlot1",
	"__EquipmentRerollResultChangedEffectSlot2",
	"__EquipmentRerollResultChangedEffectSlot3",
}

// partNeedParam 是 EquipmentRerollPartNeedRecognition 的参数。
type partNeedParam struct {
	// Part 要判断的部位："头部"/"臂部"/"身躯"/"腿部"，或 "all" 表示判断四件全部达标。
	Part string `json:"part"`
	// TargetEffect 四优模板的目标效果名称，缺省为"优越代码伤害增加"。
	TargetEffect string `json:"target_effect"`
}

func (p *partNeedParam) normalize() {
	p.Part = strings.TrimSpace(p.Part)
	p.TargetEffect = strings.TrimSpace(p.TargetEffect)
	if p.TargetEffect == "" {
		p.TargetEffect = TargetEffectElementalDamage
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

	parts, ok := GetEquipmentEffects(arg.TaskID)
	if !ok {
		// 四件装备的识别快照不完整，安全起见不进入洗词条动作。
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

// resultDecideParam 是 EquipmentRerollResultDecideRecognition 的参数。
type resultDecideParam struct {
	// TargetEffect 四优模板的目标效果名称，缺省为"优越代码伤害增加"。
	TargetEffect string `json:"target_effect"`
}

func (p *resultDecideParam) normalize() {
	p.TargetEffect = strings.TrimSpace(p.TargetEffect)
	if p.TargetEffect == "" {
		p.TargetEffect = TargetEffectElementalDamage
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
	parts, ok := GetEquipmentEffects(arg.TaskID)
	if !ok {
		log.Error().Str("component", "EquipmentReroll").Msg("result decide snapshot is incomplete")
		return nil, false
	}
	current := parts[part]

	decision := DecideResultPage(current, changed, params.TargetEffect)
	box := resultKeepButtonBox
	if decision == ResultDecisionAccept {
		box = resultAcceptButtonBox
	}

	// 只有接受变更时才刷新快照；维持分支必须保留原快照，避免一次 OCR
	// 缺行或错行把已有词条覆盖成空槽位。
	// 接受时同时更新数值并保留锁定关系（洗4优不增删锁）。
	if decision == ResultDecisionAccept {
		updatePartEffects(arg.TaskID, part, changed, changedValues)
	}

	log.Info().
		Str("component", "EquipmentReroll").
		Int64("task_id", arg.TaskID).
		Strs("current", current[:]).
		Strs("changed", changed[:]).
		Strs("changed_values", changedValues[:]).
		Strs("raw_changed", changedRaw[:]).
		Str("decision", decision.String()).
		Ints("button", box[:]).
		Msg("result page decision made")

	return &maa.CustomRecognitionResult{Box: box, Detail: "{}"}, true
}

// recognizeChangedEffects 逐槽读取结果页的变更效果与数值。
// 复用 Pipeline 定义的 __EquipmentRerollResultChangedEffectSlotN 内部 OCR 节点
// （决策页沿用之前的锚点方案，不使用 Flag offset）。
// 槽位读到官方效果或"未获得效果"都有效；读到其他非效果文本（如材料数量
// "订制模组 1033"）或 OCR 不可用时按空槽处理，不视为失败——只要至少一个
// 槽位有效即可继续决策，避免布局/文本噪声导致结果页识别失败而陷入重试死循环。
// 返回：effects（词条名）、values（从 OCR 原文提取的数值）、raws（OCR 原文）。
func recognizeChangedEffects(ctx *maa.Context, img image.Image) ([maxSlot]string, [maxSlot]string, [maxSlot]string, bool) {
	var effects [maxSlot]string
	var values [maxSlot]string
	var rawTexts [maxSlot]string
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
		if !recognized {
			continue
		}
		effects[i] = effect
		validCount++
	}
	if validCount == 0 {
		return effects, values, rawTexts, false
	}
	return effects, values, rawTexts, true
}

// EquipmentRerollResultRouteAction 根据 EquipmentRerollResultDecideRecognition 返回的
// 按钮位置路由到对应的点击节点：效果维持（返回效果变更详情页继续洗同一件）
// 或效果变更（接受变更后回人物页重扫调度）。
type EquipmentRerollResultRouteAction struct{}

var _ maa.CustomActionRunner = &EquipmentRerollResultRouteAction{}

func (a *EquipmentRerollResultRouteAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		log.Error().Str("component", "EquipmentReroll").Msg("invalid result route context")
		return false
	}

	target := "EquipmentRerollResultClickAccept"
	if arg.Box == resultKeepButtonBox {
		target = "EquipmentRerollResultClickKeep"
	}
	if err := ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: target}}); err != nil {
		log.Error().Err(err).Str("component", "EquipmentReroll").Msg("failed to route result page decision")
		return false
	}
	log.Info().
		Str("component", "EquipmentReroll").
		Int64("task_id", arg.TaskID).
		Str("target", target).
		Ints("box", arg.Box[:]).
		Msg("result page decision routed")
	return true
}
