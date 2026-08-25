package equipmentreroll

import (
	"strings"

	"github.com/1204244136/MDA/agent/go-service/pkg/i18n"
	"github.com/1204244136/MDA/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// 本文件实现"洗单个装备词条"（单件模式）的 MaaFramework Custom 组件：
//   - EquipmentRerollSingleScanRouteAction：单件模式全量扫描入口——只打开用户选定的
//     那一件装备详情，避免为了洗一件而扫全部四件；
//   - EquipmentRerollSingleDecideAction：单件任务决策入口——判断选定部位是否满足目标，
//     满足则结束，否则打开该部位详情进入锁定/效果变更流程。
//
// 其余流程节点（打开详情、锁定、效果变更、结果页）复用角色模式的原子化节点与组件，
// 组件通过承载点 EquipmentRerollLockNeed 的 attach.mode 切换为单件模式。
//
// 文档索引：docs/zh_cn/nikke/EquipmentReroll/洗词条策略与Agent逻辑.md（单件模式章节）

// EquipmentRerollSingleScanRouteAction 在确认处于“详情资讯”页后，把扫描起点直接指向
// 用户选定的部位详情。单件模式只需要这一件的词条快照加一次物资检测，扫全部四件是纯浪费。
type EquipmentRerollSingleScanRouteAction struct{}

var _ maa.CustomActionRunner = &EquipmentRerollSingleScanRouteAction{}

func (a *EquipmentRerollSingleScanRouteAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		log.Error().Str("component", "EquipmentReroll").Msg("single equipment scan route argument is nil")
		return false
	}
	cfg := loadCarrierConfig(ctx)
	part := cfg.Part
	if !isEquipmentPart(part) {
		log.Error().Str("component", "EquipmentReroll").Int64("task_id", arg.TaskID).Str("part", part).Msg("single equipment scan requires a valid part")
		return false
	}
	target, ok := choosePartRouteTarget(part)
	if !ok {
		log.Error().Str("component", "EquipmentReroll").Str("part", part).Msg("unknown single equipment scan part target")
		return false
	}
	if err := ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: target}}); err != nil {
		log.Error().Err(err).Str("component", "EquipmentReroll").Str("part", part).Msg("failed to route single equipment scan part")
		return false
	}
	log.Info().
		Str("component", "EquipmentReroll").
		Int64("task_id", arg.TaskID).
		Str("part", part).
		Str("target", target).
		Msg("single equipment scan opened only the selected part")
	return true
}

// EquipmentRerollSingleDecideAction 在物资检测完成 / 一次效果变更接受后运行，
// 判断用户选定的单件装备是否满足词条目标，并路由到结束摘要或打开对应部位详情。
type EquipmentRerollSingleDecideAction struct{}

var _ maa.CustomActionRunner = &EquipmentRerollSingleDecideAction{}

func (a *EquipmentRerollSingleDecideAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		log.Error().Str("component", "EquipmentReroll").Msg("single equipment decide argument is nil")
		return false
	}

	// 部位与目标统一从承载点 EquipmentRerollLockNeed 的 attach 读取。
	cfg := loadCarrierConfig(ctx)
	part := strings.TrimSpace(cfg.Part)
	if !isEquipmentPart(part) {
		log.Error().Str("component", "EquipmentReroll").Int64("task_id", arg.TaskID).Str("part", part).Msg("single equipment decide requires a valid part")
		return routeSingleEnd(ctx, arg.CurrentTaskName, i18n.T("tasker.equipment_reroll.single_part_invalid"))
	}
	if !cfg.singleTargetOK() {
		log.Error().
			Str("component", "EquipmentReroll").
			Int64("task_id", arg.TaskID).
			Str("part", part).
			Int("want_count", len(cfg.Target.Want)).
			Str("problem", cfg.TargetProblem).
			Msg("single equipment decide requires a valid 1 to 3 affix target")
		return routeSingleEnd(ctx, arg.CurrentTaskName, i18n.T("tasker.equipment_reroll.single_target_invalid"))
	}
	t := cfg.Target

	scan, ok := GetPartScan(arg.TaskID, part)
	if !ok {
		log.Error().Str("component", "EquipmentReroll").Int64("task_id", arg.TaskID).Str("part", part).Msg("single equipment decide scan is missing")
		return routeSingleEnd(ctx, arg.CurrentTaskName, "")
	}

	if singlePartSatisfied(scan, t) {
		log.Info().Str("component", "EquipmentReroll").Int64("task_id", arg.TaskID).Str("part", part).Msg("single equipment target satisfied; finish")
		if err := ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "EquipmentRerollFinalSummary"}}); err != nil {
			log.Error().Err(err).Str("component", "EquipmentReroll").Msg("failed to route single equipment final summary")
			return false
		}
		return true
	}

	// 不可达拦截：已有锁定与槽位限定冲突时，再洗多少次也不可能达标。这里必须结束任务，
	// 否则 singlePartSatisfied 恒 false、结果页成本恒等，会一路洗到订制模块耗尽为止。
	if singleTargetUnreachable(scan, t) {
		effects := scan.Effects()
		log.Error().
			Str("component", "EquipmentReroll").
			Int64("task_id", arg.TaskID).
			Str("part", part).
			Strs("effects", effects[:]).
			Msg("single equipment target is unreachable under current locks; ending task")
		return routeSingleEnd(ctx, arg.CurrentTaskName, i18n.T("tasker.equipment_reroll.single_target_unreachable", part))
	}

	if err := setCurrentPart(arg.TaskID, part); err != nil {
		log.Error().Err(err).Str("component", "EquipmentReroll").Str("part", part).Msg("failed to mark single equipment part")
		return false
	}
	openTarget, ok := choosePartRouteTarget(part)
	if !ok {
		log.Error().Str("component", "EquipmentReroll").Str("part", part).Msg("unknown single equipment part target")
		return false
	}
	if err := ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: openTarget}}); err != nil {
		log.Error().Err(err).Str("component", "EquipmentReroll").Str("part", part).Msg("failed to route single equipment part")
		return false
	}
	log.Info().
		Str("component", "EquipmentReroll").
		Int64("task_id", arg.TaskID).
		Str("part", part).
		Str("target", openTarget).
		Msg("single equipment decide opened part")
	return true
}

// routeSingleEnd 单件任务配置非法 / 快照缺失 / 目标不可达时路由到结束。
// notice 非空时通过 focus 告知用户原因——这些都是用户改配置才能解决的问题，
// 只写日志等于让用户对着“任务跑完但什么也没做”猜原因。
func routeSingleEnd(ctx *maa.Context, currentTaskName, notice string) bool {
	if notice != "" {
		maafocus.Print(ctx, notice)
	}
	if err := routeEquipmentRerollEnd(ctx, currentTaskName); err != nil {
		log.Error().Err(err).Str("component", "EquipmentReroll").Msg("failed to route single equipment end")
		return false
	}
	return true
}
