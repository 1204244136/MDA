package equipmentreroll

import (
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// EquipmentRerollAfterMaterialCheckAction 在四件装备扫描完成（详情页已关闭）后做最终路由：
//   - 独立运行装备扫描（入口 EquipmentRerollScanMain，仅调试用）→ EquipmentRerollFinalSummary → EquipmentRerollEnd；
//   - 洗词条任务角色模式（mode=character）→ EquipmentRerollDecide（进入角色级决策）；
//   - 洗词条任务单件模式（mode=single）→ EquipmentRerollSingleDecide（进入单件决策）。
//
// 材料库存不再由扫描末尾的独立“物资检测”读取，改为在进入效果锁定页时顺便读一次，
// 因此本节点只承担扫描后的模式分流。
//
// 角色/单件是同一任务 EquipmentReroll（入口 EquipmentRerollMain）下的互斥模式，
// 模式由承载点 EquipmentRerollLockNeed 的 attach.mode 决定（属于“复杂逻辑”，故用 Go 判定）。
type EquipmentRerollAfterMaterialCheckAction struct{}

var _ maa.CustomActionRunner = &EquipmentRerollAfterMaterialCheckAction{}

func materialCheckRouteTarget(standalone, single bool) string {
	if standalone {
		return "EquipmentRerollFinalSummary"
	}
	if single {
		return "EquipmentRerollSingleDecide"
	}
	return "EquipmentRerollDecide"
}

func (a *EquipmentRerollAfterMaterialCheckAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		log.Error().Str("component", "EquipmentReroll").Msg("after material check arg is nil")
		return false
	}
	cfg := loadCarrierConfig(ctx)
	standalone := isStandaloneScanEntry(ctx, arg)
	target := materialCheckRouteTarget(standalone, cfg.isSingle())
	if !standalone {
		if err := cfg.validateOperation(); err != nil {
			log.Error().Err(err).Msg("invalid reroll operation")
			return false
		}
		target = rerollDecisionTarget(cfg)
	}
	if err := ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: target}}); err != nil {
		log.Error().Err(err).Str("component", "EquipmentReroll").Str("target", target).Msg("failed to route after material check")
		return false
	}
	log.Info().Str("component", "EquipmentReroll").Str("mode", string(cfg.Mode)).Str("target", target).Msg("after material check routed")
	return true
}
