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
// 每行一个目标效果：条目数、当前合计档位与实际百分比、目标合计档位与对应百分比，
// 以及「还差几档 / 已达标」。目标是硬下限，所以这里只陈述现状，不做估计。
// 目标不可达等配置问题由调用方走错误路径，本函数返回空串。
func buildValueProgressMessage(parts map[string]partScan, cfg carrierConfig) string {
	progress, err := evaluateValueScope(parts, cfg)
	if err != nil {
		return ""
	}
	lines := make([]string, 0, len(cfg.ValueTargets))
	for _, effect := range officialEffects {
		target, ok := cfg.ValueTargets[effect]
		if !ok || target <= 0 {
			continue
		}
		count := progress.Minimum[effect]
		current := progress.Totals[effect]
		line := fmt.Sprintf("%s：%d 条，当前 %d 档", effect, count, current)
		if pct, ok := valueScopeCurrentPercent(parts, cfg, effect); ok {
			line += fmt.Sprintf("（%.1f%%）", pct)
		}
		line += fmt.Sprintf("，目标 %d 档", target)
		if pct, ok := valueEffectPercent(effect, count, target); ok {
			line += fmt.Sprintf("（≈%.1f%%）", pct)
		}
		if current >= target {
			line += "，已达标"
		} else {
			line += fmt.Sprintf("，还差 %d 档", target-current)
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return ""
	}
	return "【数值目标】\n" + strings.Join(lines, "\n")
}
