package equipmentreroll

import (
	"sort"
	"strings"
)

// 可替换词条对的校验错误码；rerollConfigErrorMessage 负责转成用户文案。
const (
	substitutionProblemIncomplete = "substitution: needs two affixes"
	substitutionProblemDuplicated = "substitution: affixes must differ"
	substitutionProblemUnknown    = "substitution: unknown affix"
)

// substitutionProblem 校验「允许词条替换」选中的词条对。
// 空串表示可用；开关未开启时由调用方跳过，不检查残留值。
func substitutionProblem(group [2]string) string {
	first, second := strings.TrimSpace(group[0]), strings.TrimSpace(group[1])
	if first == "" || second == "" {
		return substitutionProblemIncomplete
	}
	if first == second {
		return substitutionProblemDuplicated
	}
	if !isOfficialEffect(first) || !isOfficialEffect(second) {
		return substitutionProblemUnknown
	}
	return ""
}

// substitutionGoalQuotas 把一份配额按可替换词条对展开为若干完整等价目标，
// 交给既有 OR 规划器使用。两项词条的正数需求合并为合计条数，任一项出现都计入。
//
// 每个目标仍是一份完整配额，因此可以直接复用 planQuotaAlternatives 与
// AllPartsSatisfiedQuota：任一目标达标即整体达标，正好等价于「两项合计条数达标」。
// 关闭开关或词条对无效时调用方不会进入这里，仍然只有原始配额一个目标。
func substitutionGoalQuotas(quota map[string]int, group [2]string) []map[string]int {
	quota = normalizeQuota(quota)
	if substitutionProblem(group) != "" {
		return []map[string]int{quota}
	}
	var members []string
	total := 0
	for _, effect := range group {
		effect = strings.TrimSpace(effect)
		count := quota[effect]
		if count < 0 {
			// 禁止词条不能被另一项替代，保持原样由每个目标继续约束。
			continue
		}
		// 未填写配额的一项也要参与分摊：用户把 3 条需求写在 X 上时，
		// Y 虽然选「不要求」，仍应能顶替其中任意条数。
		members = append(members, effect)
		if count > 0 {
			total += count
		}
	}
	if len(members) < 2 || total == 0 {
		return []map[string]int{quota}
	}
	// 每种效果一件装备最多一条，单成员上限就是装备件数。
	if total > len(members)*len(equipmentParts) {
		return []map[string]int{quota}
	}
	var expanded []map[string]int
	for _, counts := range substitutionDistributions(len(members), total, len(equipmentParts)) {
		next := make(map[string]int, len(quota)+len(members))
		for effect, count := range quota {
			next[effect] = count
		}
		for i, effect := range members {
			delete(next, effect)
			if counts[i] > 0 {
				next[effect] = counts[i]
			}
		}
		expanded = append(expanded, next)
	}
	return canonicalQuotas(expanded)
}

// substitutionDistributions 枚举把 total 条需求分给 count 个成员的方案，每个成员不超过 limit 条。
func substitutionDistributions(count, total, limit int) [][]int {
	var result [][]int
	var walk func(index, remaining int, current []int)
	walk = func(index, remaining int, current []int) {
		if index == count-1 {
			if remaining <= limit {
				distribution := append([]int(nil), current...)
				result = append(result, append(distribution, remaining))
			}
			return
		}
		for value := 0; value <= min(limit, remaining); value++ {
			next := append(append([]int(nil), current...), value)
			walk(index+1, remaining-value, next)
		}
	}
	walk(0, total, nil)
	return result
}

// canonicalQuotas 去重并按稳定顺序排列，保证同一配置每次展开得到相同目标顺序。
func canonicalQuotas(quotas []map[string]int) []map[string]int {
	byKey := make(map[string]map[string]int, len(quotas))
	for _, quota := range quotas {
		normalized := normalizeQuota(quota)
		byKey[mapToCacheKey(normalized)] = normalized
	}
	keys := make([]string, 0, len(byKey))
	for key := range byKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]map[string]int, 0, len(keys))
	for _, key := range keys {
		result = append(result, byKey[key])
	}
	return result
}
