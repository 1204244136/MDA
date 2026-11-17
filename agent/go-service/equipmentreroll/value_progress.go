package equipmentreroll

import (
	"fmt"
	"strings"
)

// valueEffectPercent 把「n 条同名效果、档位合计 tierSum」换算成百分比合计。
//
// 档位表是等差数列，合计百分比与档位和线性对应（误差仅来自档位表的两位小数取舍），
// 所以按首档值与步长插值即可，不必枚举分配方式。
func valueEffectPercent(effect string, count, tierSum int) (float64, bool) {
	tiers, ok := effectTiers[effect]
	if !ok || count <= 0 || tierSum < count {
		return 0, false
	}
	step := (tiers[14] - tiers[0]) / 14
	return float64(count)*tiers[0] + step*float64(tierSum-count), true
}

// valueScopeCurrentPercent 汇总范围内该效果的实际百分比合计（取快照里已校准的数值）。
func valueScopeCurrentPercent(parts map[string]partScan, cfg carrierConfig, effect string) (float64, bool) {
	var sum float64
	found := false
	for _, part := range cfg.valueScope() {
		scan, ok := parts[part]
		if !ok {
			continue
		}
		for _, slot := range scan.Slots {
			if slot.Effect != effect {
				continue
			}
			if value, ok := parsePercentValue(slot.Value); ok {
				sum += value
				found = true
			}
		}
	}
	return sum, found
}

// buildValueProgressMessage 生成数值目标的进度摘要，供 focus 输出。
//
// 每个目标拆为两行，避免窄日志栏中途折行：
// 首行给出效果名、条目数与「差N档 / 已达标」；
// 次行缩进给出「当前/目标档（当前% / ≈目标%）」。
// 目标不可达等配置问题由调用方走错误路径，本函数返回空串。
func buildValueProgressMessage(parts map[string]partScan, cfg carrierConfig) string {
	progress, err := evaluateValueScope(parts, cfg)
	if err != nil {
		return ""
	}
	lines := make([]string, 0, len(cfg.ValueTargets)*2)
	for _, effect := range officialEffects {
		target, ok := cfg.ValueTargets[effect]
		if !ok || target <= 0 {
			continue
		}
		count := progress.Minimum[effect]
		current := progress.Totals[effect]
		status := "已达标"
		if current < target {
			status = fmt.Sprintf("差%d档", target-current)
		}
		header := fmt.Sprintf("%s（%d条）%s", effect, count, status)

		detail := fmt.Sprintf("  %d/%d档", current, target)
		curPct, curOK := valueScopeCurrentPercent(parts, cfg, effect)
		tgtPct, tgtOK := valueEffectPercent(effect, count, target)
		switch {
		case curOK && tgtOK:
			detail += fmt.Sprintf("（%.1f%% / ≈%.1f%%）", curPct, tgtPct)
		case curOK:
			detail += fmt.Sprintf("（%.1f%%）", curPct)
		case tgtOK:
			detail += fmt.Sprintf("（≈%.1f%%）", tgtPct)
		}
		lines = append(lines, header, detail)
	}
	if len(lines) == 0 {
		return ""
	}
	return "【数值目标】\n" + strings.Join(lines, "\n")
}
