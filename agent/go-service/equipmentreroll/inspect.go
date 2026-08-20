package equipmentreroll

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/1204244136/MDA/agent/go-service/pkg/i18n"
	"github.com/1204244136/MDA/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type scanBeginParam struct {
	Part string `json:"part"`
}

// ScanBeginAction 初始化一次部位全量扫描状态（词条 / 数值 / 锁定）。
type ScanBeginAction struct{}

var _ maa.CustomActionRunner = &ScanBeginAction{}

func (a *ScanBeginAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if arg == nil {
		log.Error().Str("component", "EquipmentReroll").Msg("scan begin argument is nil")
		return false
	}

	var params scanBeginParam
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
		log.Error().Err(err).Str("component", "EquipmentReroll").Msg("failed to parse scan begin part")
		return false
	}
	params.Part = strings.TrimSpace(params.Part)
	if !isEquipmentPart(params.Part) {
		log.Error().Str("component", "EquipmentReroll").Str("part", params.Part).Msg("invalid scan begin part")
		return false
	}
	if err := beginScan(arg.TaskID, params.Part); err != nil {
		log.Error().Err(err).Str("component", "EquipmentReroll").Str("part", params.Part).Msg("failed to initialize scan")
		return false
	}
	log.Info().Str("component", "EquipmentReroll").Str("part", params.Part).Msg("equipment scan initialized")
	return true
}

// scanNextItems 返回当前部位扫描完成后的路由：
//   - 非腿部：关闭详情页 → 打开下一部位详情；
//   - 腿部 + stopAfterScan=false：关闭详情页 → EquipmentRerollDecide（完整洗词条任务继续）；
//   - 腿部 + stopAfterScan=true：普通引用关闭详情页，任务结束（独立运行 EquipmentRerollScanMain 时使用）。
// 注意：中间切换部位的“关闭”必须用 [JumpBack] 回跳父节点后再打开下一部位；
// 独立收尾的“关闭”不能带 [JumpBack]，否则会回到父节点重复找关闭按钮导致超时。
func scanNextItems(part string, stopAfterScan ...bool) ([]maa.NextItem, bool) {
	nextByPart := map[string]string{
		"头部": "EquipmentRerollOpenArmsDetails",
		"臂部": "EquipmentRerollOpenTorsoDetails",
		"身躯": "EquipmentRerollOpenLegsDetails",
		"腿部": "EquipmentRerollDecide",
	}
	next, ok := nextByPart[part]
	if !ok {
		return nil, false
	}
	if part == "腿部" && len(stopAfterScan) > 0 && stopAfterScan[0] {
		// 独立全量扫描收尾：普通引用而非 [JumpBack]。
		// [JumpBack] 会在关闭后回到父节点 EquipmentRerollScanSlot3 重新识别，
		// 导致已关闭页面再次查找关闭按钮而超时；普通引用执行完关闭即结束任务。
		return []maa.NextItem{
			{Name: "EquipmentRerollScanCloseDetails"},
		}, true
	}
	return []maa.NextItem{
		{Name: "[JumpBack]EquipmentRerollScanCloseDetails"},
		{Name: next},
	}, true
}

// isStandaloneScanEntry 判断当前任务是否为独立运行的全量扫描入口。
// 完整洗词条任务从 EquipmentRerollMain 进入时，Scan 节点属于同一个 task，
// task entry 为 EquipmentRerollMain；直接从调试页运行 EquipmentRerollScanMain 时，
// task entry 为 EquipmentRerollScanMain。
func isStandaloneScanEntry(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		return false
	}
	detail, err := ctx.GetTasker().GetTaskDetail(arg.TaskID)
	if err != nil || detail == nil {
		log.Warn().
			Str("component", "EquipmentReroll").
			Int64("task_id", arg.TaskID).
			Msg("failed to read task entry; assume full reroll task")
		return false
	}
	return detail.Entry == "EquipmentRerollScanMain"
}

// scanRouteParam 是 EquipmentRerollScanRouteAction 的参数。
type scanRouteParam struct {
	Slot   int  `json:"slot"`
	IsLast bool `json:"is_last"`
}

// EquipmentRerollScanRouteAction 全量扫描路由：扫描完最后一个槽位后
// 展示该部位扫描摘要（词条 / 数值 / 锁定），并把流程路由到下一部位；
// 腿部扫描完成后按当前任务入口决定是否继续进入 EquipmentRerollDecide。
type EquipmentRerollScanRouteAction struct{}

var _ maa.CustomActionRunner = &EquipmentRerollScanRouteAction{}

func (a *EquipmentRerollScanRouteAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if arg == nil {
		log.Error().Str("component", "EquipmentReroll").Msg("scan route argument is nil")
		return false
	}

	var params scanRouteParam
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
		log.Error().Err(err).Str("component", "EquipmentReroll").Msg("failed to parse scan route param")
		return false
	}
	if params.Slot < minSlot || params.Slot > maxSlot {
		log.Error().Str("component", "EquipmentReroll").Int("slot", params.Slot).Msg("invalid scan route slot")
		return false
	}
	if !params.IsLast {
		return true
	}

	part, ok := currentEffectPart(arg.TaskID)
	if !ok {
		log.Error().Str("component", "EquipmentReroll").Int64("task_id", arg.TaskID).Msg("scan route part is not initialized")
		return false
	}
	scan, ok := currentPartScan(arg.TaskID)
	if !ok {
		log.Error().Str("component", "EquipmentReroll").Int64("task_id", arg.TaskID).Msg("scan route scan state is missing")
		return false
	}

	lines := displayScanLines(scan, i18n.T("tasker.equipment_reroll.empty_slot"))
	maafocus.Print(ctx, fmt.Sprintf(
		i18n.T("tasker.equipment_reroll.effects"),
		part,
		lines[0],
		lines[1],
		lines[2],
	))

	// 独立运行 EquipmentRerollScanMain 时，扫描完腿部后停止，不进入后续洗词条任务；
	// 完整 EquipmentRerollMain 任务内运行时，继续路由到 EquipmentRerollDecide。
	standalone := isStandaloneScanEntry(ctx, arg)
	next, ok := scanNextItems(part, standalone)
	if !ok {
		log.Error().Str("component", "EquipmentReroll").Str("part", part).Msg("no next node for equipment part")
		return false
	}
	if err := ctx.OverrideNext(arg.CurrentTaskName, next); err != nil {
		log.Error().Err(err).Str("component", "EquipmentReroll").Str("part", part).Msg("failed to route next equipment part")
		return false
	}
	nextNames := make([]string, 0, len(next))
	for _, item := range next {
		nextNames = append(nextNames, item.Name)
	}
	log.Info().
		Str("component", "EquipmentReroll").
		Int64("task_id", arg.TaskID).
		Str("part", part).
		Bool("standalone", standalone).
		Strs("next", nextNames).
		Msg("equipment scan routed")
	return true
}
