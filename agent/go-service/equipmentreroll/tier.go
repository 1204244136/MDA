package equipmentreroll

import (
	"fmt"
	"math"
	"regexp"
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
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	return v, true
}

var twoDecimalValuePattern = regexp.MustCompile(`^\d+\.\d{2}$`)

// resolveEffectTier 根据效果名与 OCR 数值，求出对应档位并返回**校准后**的精确数值。
// 优先接受与官方档位相差不超过 0.05% 的数值抖动；否则仅在 OCR 文本仍是两位小数、
// 且官方档位表中恰有一个候选与其相差一个字符时纠正误读（如 17.15% → 77.15%）。
// 找不到可确认的档位或候选不唯一时 ok=false（不校准）。
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
	if bestDiff <= 0.05 {
		return bestI + 1, fmt.Sprintf("%.2f%%", tiers[bestI]), true
	}

	raw, formatted := normalizedTierText(ocrValue)
	if !formatted {
		return 0, "", false
	}
	matched := -1
	for i, tv := range tiers {
		if !oneCharacterEditApart(raw, fmt.Sprintf("%.2f", tv)) {
			continue
		}
		if matched >= 0 {
			return 0, "", false // 多个官方档位都只差一个字符时不猜测。
		}
		matched = i
	}
	if matched < 0 {
		return 0, "", false
	}
	return matched + 1, fmt.Sprintf("%.2f%%", tiers[matched]), true
}

// normalizedTierText 去掉百分号并限制为两位小数的数值文本，避免把多余精度或
// 非数值内容纳入单字符纠错。返回的文本保留原整数位，用于逐字符比较。
func normalizedTierText(s string) (string, bool) {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "％", "%")
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "%")
	s = strings.TrimSpace(s)
	if !twoDecimalValuePattern.MatchString(s) {
		return "", false
	}
	return s, true
}

// oneCharacterEditApart 判断两个等格式数值串是否恰好相差一个字符：漏字、插字或单字替换。
// 不支持交换两位或差异更大的情况，避免把另一个合法档位误当成 OCR 错字。
func oneCharacterEditApart(a, b string) bool {
	if a == b || absInt(len(a)-len(b)) > 1 {
		return false
	}
	i, j, edits := 0, 0, 0
	for i < len(a) && j < len(b) {
		if a[i] == b[j] {
			i++
			j++
			continue
		}
		edits++
		if edits > 1 {
			return false
		}
		switch {
		case len(a) > len(b):
			i++
		case len(a) < len(b):
			j++
		default:
			i++
			j++
		}
	}
	if i < len(a) || j < len(b) {
		edits++
	}
	return edits == 1
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
