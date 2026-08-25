package equipmentreroll

import (
	"encoding/json"

	"github.com/1204244136/MDA/agent/go-service/pkg/i18n"
	"github.com/1204244136/MDA/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// EquipmentRerollChoosePartAction 在 EquipmentRerollDecide 后执行全局 1 步前瞻，
// 从四件装备中选择期望收益最高的部位，并直接路由到对应装备详情页。
//
// 文档索引：docs/zh_cn/nikke/EquipmentReroll/洗词条策略与Agent逻辑.md
type EquipmentRerollChoosePartAction struct{}

var _ maa.CustomActionRunner = &EquipmentRerollChoosePartAction{}

func choosePartRouteTarget(part string) (string, bool) {
	targets := map[string]string{
		"头部": "EquipmentRerollOpenHeadDetails",
		"臂部": "EquipmentRerollOpenArmsDetails",
		"身躯": "EquipmentRerollOpenTorsoDetails",
		"腿部": "EquipmentRerollOpenLegsDetails",
	}
	target, ok := targets[part]
	return target, ok
}

func (a *EquipmentRerollChoosePartAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		log.Error().Str("component", "EquipmentReroll").Msg("choose part action argument is nil")
		return false
	}

	var params struct {
		GlobalQuota map[string]int `json:"global_quota"`
	}
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
		log.Error().Err(err).Str("component", "EquipmentReroll").Int64("task_id", arg.TaskID).Msg("failed to parse choose part param")
		return false
	}
	// 角色配额统一从承载点读取（attach.quota_* 优先，为空时回退本节点自带默认）。
	quota := loadCarrierConfig(ctx).resolveQuota(params.GlobalQuota)

	// 配额合法性校验：正数合计必须在 1~12 条内，避免无休止洗词条。
	if !quotaIsValid(quota) {
		log.Error().Str("component", "EquipmentReroll").Int64("task_id", arg.TaskID).Int("quota_total", quotaTotal(quota)).Msg("custom quota requires 1 to 12 affixes")
		if err := routeEquipmentRerollEnd(ctx, arg.CurrentTaskName); err != nil {
			log.Error().Err(err).Str("component", "EquipmentReroll").Msg("failed to route end for invalid quota")
			return false
		}
		return true
	}

	parts, ok := GetEquipmentSlotScans(arg.TaskID)
	if !ok {
		log.Error().Str("component", "EquipmentReroll").Int64("task_id", arg.TaskID).Msg("choose part snapshot is incomplete")
		return false
	}

	// 不可达拦截：配额与现有永久锁冲突（例如禁止词条被永久锁死、需求数超过可用槽位）时，
	// 再洗多少次也不可能达标。必须结束任务，否则会一路洗到订制模块耗尽为止。
	if expectedModulesForQuota(parts, quota) >= costUnreachable {
		log.Error().
			Str("component", "EquipmentReroll").
			Int64("task_id", arg.TaskID).
			Msg("custom quota is unreachable under current locks; ending task")
		maafocus.Print(ctx, i18n.T("tasker.equipment_reroll.quota_unreachable"))
		if err := routeEquipmentRerollEnd(ctx, arg.CurrentTaskName); err != nil {
			log.Error().Err(err).Str("component", "EquipmentReroll").Msg("failed to route end for unreachable quota")
			return false
		}
		return true
	}

	part, ok := chooseBestPartForQuota(parts, quota, equipmentParts)
	if !ok {
		log.Warn().Str("component", "EquipmentReroll").Int64("task_id", arg.TaskID).Msg("no part selected by global lookahead; end task")
		if err := ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "EquipmentRerollEnd"}}); err != nil {
			log.Error().Err(err).Str("component", "EquipmentReroll").Msg("failed to route end after no selected part")
			return false
		}
		return true
	}

	if err := setCurrentPart(arg.TaskID, part); err != nil {
		log.Error().Err(err).Str("component", "EquipmentReroll").Str("part", part).Msg("failed to mark current part")
		return false
	}

	target, ok := choosePartRouteTarget(part)
	if !ok {
		log.Error().Str("component", "EquipmentReroll").Str("part", part).Msg("unknown part target")
		return false
	}

	if err := ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: target}}); err != nil {
		log.Error().Err(err).Str("component", "EquipmentReroll").Str("part", part).Msg("failed to route chosen part")
		return false
	}

	log.Info().
		Str("component", "EquipmentReroll").
		Int64("task_id", arg.TaskID).
		Str("part", part).
		Str("target", target).
		Msg("global lookahead chose part")
	return true
}
