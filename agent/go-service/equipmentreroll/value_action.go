package equipmentreroll

import (
	"time"

	"github.com/1204244136/MDA/agent/go-service/pkg/i18n"
	"github.com/1204244136/MDA/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// EquipmentRerollConfigCheckAction 在扫描前拦截未知大类/模式和非法数值目标。
type EquipmentRerollConfigCheckAction struct{}

var _ maa.CustomActionRunner = &EquipmentRerollConfigCheckAction{}

func (a *EquipmentRerollConfigCheckAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		return false
	}
	cfg := loadCarrierConfig(ctx)
	if err := cfg.validateOperation(); err != nil {
		log.Error().Err(err).Msg("invalid equipment reroll config")
		maafocus.Print(ctx, i18n.T("tasker.equipment_reroll.config_invalid"))
		return false
	}
	return true
}

// EquipmentRerollValueDecideAction 复用扫描快照生成数值重洗计划。
// 已达标走共用摘要；尚未达标打开目标部位，通过共用锁定/费用/结果链执行。
type EquipmentRerollValueDecideAction struct{}

var _ maa.CustomActionRunner = &EquipmentRerollValueDecideAction{}

func (a *EquipmentRerollValueDecideAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		return false
	}
	clearValuePlan(arg.TaskID)
	started := time.Now()
	plan, err := planValueRerollWithInventory(getScannedParts(arg.TaskID), loadCarrierConfig(ctx), valueInventory(arg.TaskID), "")
	if err != nil {
		log.Error().Err(err).Int64("task_id", arg.TaskID).Msg("cannot plan value reroll")
		maafocus.Print(ctx, i18n.T("tasker.equipment_reroll.value_invalid", err.Error()))
		return false
	}
	if plan.Satisfied {
		maafocus.Print(ctx, i18n.T("tasker.equipment_reroll.value_satisfied"))
		return routeEquipmentRerollEnd(ctx, arg.CurrentTaskName) == nil
	}
	log.Info().Int64("task_id", arg.TaskID).Dur("elapsed_ms", time.Since(started)).Interface("plan", plan).Msg("value reroll plan prepared")
	maafocus.Print(ctx, i18n.T("tasker.equipment_reroll.value_pending", plan.Pending, plan.Part))
	if err := setCurrentPart(arg.TaskID, plan.Part); err != nil {
		log.Error().Err(err).Msg("failed to select value reroll part")
		return false
	}
	target, ok := choosePartRouteTarget(plan.Part)
	if !ok {
		return false
	}
	if err := ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: target}}); err != nil {
		log.Error().Err(err).Msg("failed to open value reroll part")
		return false
	}
	return true
}
