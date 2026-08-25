package equipmentreroll

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// effectTiers 每种官方效果的 1~15 档数值（百分比，Tier 表示效果数值档位，非装备强化等级）。
// 来源：docs/zh_cn/nikke/EquipmentReroll/装备系统与洗词条研究.md（Nikke.gg / NIKKE Optimizer / Game8 三处一致）。
var effectTiers = map[string][15]float64{
	"优越代码伤害增加": {9.54, 10.94, 12.34, 13.75, 15.15, 16.55, 17.95, 19.35, 20.75, 22.15, 23.56, 24.96, 26.36, 27.76, 29.16},
	"命中率增加":    {4.77, 5.47, 6.18, 6.88, 7.59, 8.29, 9.00, 9.70, 10.40, 11.11, 11.81, 12.52, 13.22, 13.93, 14.63},
	"最大装弹数增加":  {27.84, 31.95, 36.06, 40.17, 44.28, 48.39, 52.50, 56.60, 60.71, 64.82, 68.93, 73.04, 77.15, 81.26, 85.37},
	"攻击力增加":    {4.77, 5.47, 6.18, 6.88, 7.59, 8.29, 9.00, 9.70, 10.40, 11.11, 11.81, 12.52, 13.22, 13.93, 14.63},
	"蓄力伤害增加":   {4.77, 5.47, 6.18, 6.88, 7.59, 8.29, 9.00, 9.70, 10.40, 11.11, 11.81, 12.52, 13.22, 13.93, 14.63},
	"蓄力速度增加":   {1.98, 2.28, 2.57, 2.86, 3.16, 3.45, 3.75, 4.04, 4.33, 4.63, 4.92, 5.21, 5.51, 5.80, 6.09},
	"暴击率增加":    {2.30, 2.64, 2.98, 3.32, 3.66, 4.00, 4.35, 4.69, 5.03, 5.37, 5.70, 6.05, 6.39, 6.73, 7.07},
	"暴击伤害增加":   {6.64, 7.62, 8.60, 9.58, 10.56, 11.54, 12.52, 13.50, 14.48, 15.46, 16.44, 17.42, 18.40, 19.38, 20.36},
	"防御力增加":    {4.77, 5.47, 6.18, 6.88, 7.59, 8.29, 9.00, 9.70, 10.40, 11.11, 11.81, 12.52, 13.22, 13.93, 14.63},
}

// parsePercentValue 解析 OCR 数值（如 "11.81%" / "11.81％" / "11.81"），返回百分比数值。
func parsePercentValue(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "％", "%")
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "%")
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// resolveEffectTier 根据效果名与 OCR 数值，求出对应档位并返回**校准后**的精确数值。
// 允许微小 OCR 抖动（阈值 0.05%），取最近档位；找不到可确认的档位时 ok=false（不校准）。
func resolveEffectTier(effect, ocrValue string) (tier int, calibratedValue string, ok bool) {
	tiers, exists := effectTiers[effect]
	if !exists {
		return 0, "", false
	}
	v, ok := parsePercentValue(ocrValue)
	if !ok {
		return 0, "", false
	}
	bestI, bestDiff := 0, 1e9
	for i, tv := range tiers {
		d := math.Abs(tv - v)
		if d < bestDiff {
			bestDiff = d
			bestI = i
		}
	}
	if bestDiff > 0.05 {
		return 0, "", false
	}
	return bestI + 1, fmt.Sprintf("%.2f%%", tiers[bestI]), true
}
