package equipmentreroll

import (
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// EquipmentRerollPrepareRerollCostAction 在“效果变更确认”前记录待消耗订制模块数。
// 消耗量由当前锁定数推导，并在写入 pending 前校验任务级库存余额。
type EquipmentRerollPrepareRerollCostAction struct{}

var _ maa.CustomActionRunner = &EquipmentRerollPrepareRerollCostAction{}

func (a *EquipmentRerollPrepareRerollCostAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		log.Error().Str("component", "EquipmentReroll").Msg("prepare reroll cost argument is nil")
		return false
	}

	part, ok := currentEffectPart(arg.TaskID)
	if !ok {
		log.Error().Str("component", "EquipmentReroll").Int64("task_id", arg.TaskID).Msg("prepare reroll cost part is not set")
		return false
	}
	scan, ok := GetPartScan(arg.TaskID, part)
	if !ok {
		log.Error().Str("component", "EquipmentReroll").Int64("task_id", arg.TaskID).Str("part", part).Msg("prepare reroll cost scan is missing")
		return false
	}

	inv, inventoryReady := getInventory(arg.TaskID)
	if !inventoryReady {
		log.Warn().Str("component", "EquipmentReroll").Int64("task_id", arg.TaskID).Msg("reroll cost requires initialized material inventory; ending task")
		if err := routeEquipmentRerollEnd(ctx, arg.CurrentTaskName); err != nil {
			log.Error().Err(err).Str("component", "EquipmentReroll").Msg("failed to route end without inventory")
			return false
		}
		return true
	}
	modules, affordable := affordableRerollCost(inv, countLocks(scan))
	if !affordable {
		log.Warn().
			Str("component", "EquipmentReroll").
			Int64("task_id", arg.TaskID).
			Int("custom_modules", inv.CustomModules).
			Int("required_modules", modules).
			Msg("insufficient custom modules for effect change; ending task")
		if err := routeEquipmentRerollEnd(ctx, arg.CurrentTaskName); err != nil {
			log.Error().Err(err).Str("component", "EquipmentReroll").Msg("failed to route end for insufficient modules")
			return false
		}
		return true
	}
	setPendingRerollCost(arg.TaskID, modules)

	log.Info().
		Str("component", "EquipmentReroll").
		Int64("task_id", arg.TaskID).
		Str("part", part).
		Int("active_locks", countLocks(scan)).
		Int("modules", modules).
		Msg("prepared effect change module cost")
	return true
}

// EquipmentRerollRecordRerollCostAction 在效果变更确认成功后，把待记录消耗正式写入 Materials。
// 若待记录值缺失，则按当前锁定数兜底计算，避免漏记。
type EquipmentRerollRecordRerollCostAction struct{}

var _ maa.CustomActionRunner = &EquipmentRerollRecordRerollCostAction{}

func (a *EquipmentRerollRecordRerollCostAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		log.Error().Str("component", "EquipmentReroll").Msg("record reroll cost argument is nil")
		return false
	}

	modules := consumePendingRerollCost(arg.TaskID)
	if modules <= 0 {
		part, ok := currentEffectPart(arg.TaskID)
		if !ok {
			log.Error().Str("component", "EquipmentReroll").Int64("task_id", arg.TaskID).Msg("record reroll cost part is not set")
			return false
		}
		scan, ok := GetPartScan(arg.TaskID, part)
		if !ok {
			log.Error().Str("component", "EquipmentReroll").Int64("task_id", arg.TaskID).Str("part", part).Msg("record reroll cost scan is missing")
			return false
		}
		modules = RerollModuleCost(countLocks(scan))
	}
	inv, inventoryReady := getInventory(arg.TaskID)
	if !inventoryReady || inv.CustomModules < modules {
		log.Warn().
			Str("component", "EquipmentReroll").
			Int64("task_id", arg.TaskID).
			Int("custom_modules", inv.CustomModules).
			Int("required_modules", modules).
			Msg("recorded reroll cost is no longer affordable; ending task")
		if err := routeEquipmentRerollEnd(ctx, arg.CurrentTaskName); err != nil {
			log.Error().Err(err).Str("component", "EquipmentReroll").Msg("failed to route end for unaffordable reroll")
			return false
		}
		return true
	}
	recordRerollModuleCost(arg.TaskID, modules)

	log.Info().
		Str("component", "EquipmentReroll").
		Int64("task_id", arg.TaskID).
		Int("modules", modules).
		Msg("recorded effect change module cost")
	return true
}
