package equipmentreroll

import (
	"fmt"
	"strings"

	"github.com/1204244136/MDA/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// EquipmentRerollFinalSummaryAction 在任务结束前通过 focus 输出人类可读摘要。
// 完整洗词条任务输出最终四件装备详情（含档位）与本次消耗材料；独立装备检测已经
// 在每个部位完成时实时输出详情，因此独立检测这里只补充库存，避免重复播放整份快照。
type EquipmentRerollFinalSummaryAction struct{}

var _ maa.CustomActionRunner = &EquipmentRerollFinalSummaryAction{}

func (a *EquipmentRerollFinalSummaryAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		log.Error().Str("component", "EquipmentReroll").Msg("final summary action context is nil")
		return false
	}
	message := ""
	if isStandaloneScanEntry(ctx, arg) {
		message = buildStandaloneSummaryMessage(arg.TaskID)
	} else {
		message = buildFinalSummaryMessage(arg.TaskID)
	}
	if message != "" {
		maafocus.Print(ctx, message)
	}
	return true
}

// buildStandaloneSummaryMessage 生成独立装备检测的结尾消息。
// 四件装备详情已由 EquipmentRerollScanRouteAction 按部位实时输出，这里只输出库存。
func buildStandaloneSummaryMessage(taskID int64) string {
	inv, ok := getInventory(taskID)
	if !ok {
		return ""
	}
	return fmt.Sprintf("【库存】订制模块 %d / 自订密钥 %d", inv.CustomModules, inv.CustomLockKeys)
}

// buildFinalSummaryMessage 生成洗词条任务结束摘要：已扫描装备的详情（含档位）+ 本次消耗材料。
// 角色模式为四件，单件模式只有用户选定的那一件。
func buildFinalSummaryMessage(taskID int64) string {
	var sb strings.Builder
	sb.WriteString("【装备详情】")
	parts := getScannedParts(taskID)
	if len(parts) > 0 {
		for _, part := range equipmentParts {
			scan, ok := parts[part]
			if !ok {
				continue
			}
			sb.WriteString("\n" + part + ":")
			// 与扫描/变更日志复用同一套槽位格式化逻辑；最终详情保持原有不显示锁标签。
			for _, line := range formatScanLines(scan, emptySlotDisplayLabel(), nil) {
				sb.WriteString("\n" + line)
			}
		}
	} else {
		sb.WriteString("（快照不完整）")
	}
	usage, _ := GetMaterialUsage(taskID)
	sb.WriteString("\n【消耗材料】")
	sb.WriteString(fmt.Sprintf("\n订制模块 %d", usage.CustomModules))
	sb.WriteString(fmt.Sprintf("\n自订密钥 %d", usage.CustomLockKeys))
	return sb.String()
}
