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
//   - 单件模式：只扫用户选定的那一件，扫完即关闭详情页并进入决策；
//   - 角色模式非腿部：关闭详情页 → 打开下一部位详情；
//   - 角色模式腿部：关闭详情页后由 EquipmentRerollAfterMaterialCheck 分支：
//     独立扫描 → 结束；洗词条任务 → 对应模式的决策。
//
// 材料库存不再由扫描末尾的独立“物资检测”读取——新版客户端装备详情页的词条行已不可点击，
// 旧路径（点词条进效果锁定页）会一直点不动而卡死；改为在进入效果锁定页时顺便读一次。
//
// 注意：中间切换部位的“关闭”必须用 [JumpBack] 回跳父节点后再打开下一部位。
func scanNextItems(part string, single bool) ([]maa.NextItem, bool) {
	if single || part == "腿部" {
		return []maa.NextItem{
			{Name: "[JumpBack]EquipmentRerollScanCloseDetails"},
			{Name: "EquipmentRerollAfterMaterialCheck"},
		}, true
	}
	nextByPart := map[string]string{
		"头部": "EquipmentRerollOpenArmsDetails",
		"臂部": "EquipmentRerollOpenTorsoDetails",
		"身躯": "EquipmentRerollOpenLegsDetails",
	}
	next, ok := nextByPart[part]
	if !ok {
		return nil, false
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

// buildPartEffectsMessage 生成一个部位的四行用户可见词条消息。
// key 用于区分初次扫描与接受效果变更后的提示文案。
func buildPartEffectsMessage(part string, scan partScan, key string) string {
	lines := displayScanLines(scan, emptySlotDisplayLabel())
	return fmt.Sprintf(
		i18n.T(key),
		part,
		lines[0],
		lines[1],
		lines[2],
	)
}

// EquipmentRerollScanRouteAction 全量扫描路由：扫描完最后一个槽位后
// 执行细粒度的已有锁前置检查（合规且符合锁定策略的已有锁予以保留，冲突/非法锁报错拦截），
// 再展示该部位扫描摘要（词条 / 数值 / 锁定）并把流程路由到下一部位；独立扫描入口保留原有观察行为。
type EquipmentRerollScanRouteAction struct{}

var _ maa.CustomActionRunner = &EquipmentRerollScanRouteAction{}

func (a *EquipmentRerollScanRouteAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
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

	standalone := isStandaloneScanEntry(ctx, arg)
	if !standalone {
		res := validatePreexistingLocks(ctx, arg.TaskID, part, scan)
		if !res.Passed {
			log.Error().
				Str("component", "EquipmentReroll").
				Int64("task_id", arg.TaskID).
				Str("part", part).
				Str("message", res.Message).
				Msg("precheck found invalid equipment lock; task failed")
			// 通过标准输出发送提示，避免为一次失败通知创建临时 Go focus 节点并污染 Maa 日志。
			maafocus.PrintLargeContentTrimNewline(res.Message)
			// CustomAction 返回 false 即表示当前任务失败；不要再调用 PostStop，
			// 否则 Maa 会额外创建“停止任务”的伪任务。
			return false
		}
		if res.IsInfo && res.Message != "" {
			log.Info().
				Str("component", "EquipmentReroll").
				Int64("task_id", arg.TaskID).
				Str("part", part).
				Str("message", res.Message).
				Msg("precheck validated existing equipment lock")
			maafocus.Print(ctx, res.Message)
		}
	}

	maafocus.Print(ctx, buildPartEffectsMessage(part, scan, "tasker.equipment_reroll.effects"))
	cfg := loadCarrierConfig(ctx)
	if shouldReportInitialEstimate(standalone, part, cfg) {
		maafocus.Print(ctx, initialEstimateMessage(arg.TaskID, getScannedParts(arg.TaskID), cfg))
	}

	next, ok := scanNextItems(part, cfg.isSingle())
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
		Strs("next", nextNames).
		Msg("equipment scan routed")
	return true
}
