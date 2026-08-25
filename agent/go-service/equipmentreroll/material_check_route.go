package equipmentreroll

import (
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// EquipmentRerollAfterMaterialCheckAction 在“物资检测”（进入效果锁定页读取材料库存）退出后做最终路由：
//   - 独立运行装备检测（EquipmentDetect / 入口 EquipmentRerollScanMain）→ EquipmentRerollFinalSummary → EquipmentRerollEnd；
//   - 完整洗词条任务（入口 EquipmentRerollMain）→ EquipmentRerollDecide（进入决策）。
//
// 依据任务入口区分（isStandaloneScanEntry），判定属“复杂逻辑”，故用 Go。
type EquipmentRerollAfterMaterialCheckAction struct{}

var _ maa.CustomActionRunner = &EquipmentRerollAfterMaterialCheckAction{}

func materialCheckRouteTarget(standalone bool) string {
	if standalone {
		return "EquipmentRerollFinalSummary"
	}
	return "EquipmentRerollDecide"
}

func (a *EquipmentRerollAfterMaterialCheckAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		log.Error().Str("component", "EquipmentReroll").Msg("after material check arg is nil")
		return false
	}
	target := materialCheckRouteTarget(isStandaloneScanEntry(ctx, arg))
	if err := ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: target}}); err != nil {
		log.Error().Err(err).Str("component", "EquipmentReroll").Str("target", target).Msg("failed to route after material check")
		return false
	}
	log.Info().Str("component", "EquipmentReroll").Str("target", target).Msg("after material check routed")
	return true
}
