package equipmentreroll

import (
	"math/bits"
	"sort"
	"strings"
	"sync"
)

// 本文件实现多目标 OR 规划：每个目标是完整配额，满足任意一个即整体达标。
// 当前 UI 只有一份配额；这里的多目标由 substitution.go 按 substitution_affix1/2
// 选中的词条对展开产生，不再对应额外方案开关。换一对词条无需改这里。

// effectAlternative 是单件必须完整满足的一组条件；不同组之间只做 OR。
type effectAlternative struct {
	required  []string
	forbidden []string
}

type effectCostTarget struct {
	required  map[string]bool
	forbidden map[string]bool
}

// quotaAlternativePlan 先完成共享准备件，最后由 pivot 接受完整目标的任一残余组。
// 这样不会把不同全局方案的局部成功拼成不存在的目标，也能把最后一件的成功概率
// 真正合并。它与现有单方案相同，属于可执行策略的分解续程估值，而非四件全局 MDP。
type quotaAlternativePlan struct {
	cost    float64
	targets map[string][]effectAlternative
	pivot   string
	quota   map[string]int
}

var quotaAlternativeCache sync.Map

func canonicalQuotaAlternatives(goals []map[string]int) []map[string]int {
	byKey := make(map[string]map[string]int, len(goals))
	for _, goal := range goals {
		goal = normalizeQuota(goal)
		byKey[mapToCacheKey(goal)] = goal
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

func partSatisfiedEffectAlternatives(scan partScan, targets []effectAlternative) bool {
	for _, target := range targets {
		if partHasRequiredAndNoForbidden(scan.Effects(), requiredSet(target.required), forbiddenSet(target.forbidden)) {
			return true
		}
	}
	return false
}

func effectTargetReachable(locks effectCostLocks, unlocked int, required, forbidden map[string]bool) bool {
	missing := len(required)
	for i, effect := range locks.Effects {
		if locks.Mask&(1<<i) == 0 {
			continue
		}
		if forbidden[effect] {
			return false
		}
		if required[effect] {
			missing--
		}
	}
	return missing <= unlocked
}

func dynamicEffectAlternativePlan(scan partScan, alternatives []effectAlternative, material string) (int, float64) {
	byKey := make(map[string]effectAlternative, len(alternatives))
	for _, target := range alternatives {
		key := strSliceKey(target.required) + "#" + strSliceKey(target.forbidden)
		byKey[key] = target
	}
	keys := make([]string, 0, len(byKey))
	for key := range byKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return 0, costUnreachable
	}
	if len(keys) == 1 {
		target := byKey[keys[0]]
		return dynamicEffectPlan(scan, target.required, target.forbidden, nil, material)
	}
	key := "or:" + strings.Join(keys, "\x02") + "#" + lockMaterialOrDefault(material)
	dynamicEffectModels.Lock()
	model := dynamicEffectModels.models[key]
	if model == nil {
		model = &effectCostModel{
			required: make(map[string]bool), forbidden: make(map[string]bool),
			moduleLocks: lockMaterialOrDefault(material) == "订制模块",
			futureCosts: make(map[effectCostLocks]float64),
		}
		for index, targetKey := range keys {
			target := byKey[targetKey]
			r, f := requiredSet(target.required), forbiddenSet(target.forbidden)
			model.alternatives = append(model.alternatives, effectCostTarget{r, f})
			for effect := range r {
				model.required[effect] = true
			}
			if index == 0 {
				for effect := range f {
					model.forbidden[effect] = true
				}
			} else {
				for effect := range model.forbidden {
					if !f[effect] {
						delete(model.forbidden, effect)
					}
				}
			}
		}
		dynamicEffectModels.models[key] = model
	}
	dynamicEffectModels.Unlock()
	return model.plan(scan)
}

func allPartsSatisfiedQuotaAlternatives(parts map[string]partScan, goals []map[string]int) bool {
	for _, goal := range goals {
		if AllPartsSatisfiedQuota(parts, goal) {
			return true
		}
	}
	return false
}

func planQuotaAlternatives(parts map[string]partScan, goals []map[string]int, material string) quotaAlternativePlan {
	goals = canonicalQuotaAlternatives(goals)
	best := quotaAlternativePlan{cost: costUnreachable, targets: make(map[string][]effectAlternative)}
	if len(goals) == 0 {
		return best
	}
	var goalKeys []string
	for _, goal := range goals {
		if !quotaIsValid(goal) {
			return best
		}
		goalKeys = append(goalKeys, mapToCacheKey(goal))
	}
	if allPartsSatisfiedQuotaAlternatives(parts, goals) {
		best.cost = 0
		return best
	}
	key := effectPartsCacheKey(parts, nil, material) + "#" + strings.Join(goalKeys, "\x02")
	if cached, ok := quotaAlternativeCache.Load(key); ok {
		return cached.(quotaAlternativePlan)
	}
	// 原有每一组的完整策略始终保留，添加可接受组不会抬高成本。
	for _, goal := range goals {
		plan := planQuotaCost(parts, goal, material)
		if plan.cost >= best.cost-costTieEpsilon {
			continue
		}
		best = quotaAlternativePlan{cost: plan.cost, quota: goal, targets: make(map[string][]effectAlternative)}
		for _, part := range equipmentParts {
			best.targets[part] = []effectAlternative{{required: plan.required[part], forbidden: forbiddenEffects(goal)}}
		}
	}
	for mask := 1; mask < 1<<len(goals); mask++ {
		if bits.OnesCount(uint(mask)) < 2 {
			continue
		}
		var subset []map[string]int
		for i, goal := range goals {
			if mask&(1<<i) != 0 {
				subset = append(subset, goal)
			}
		}
		for _, pivot := range equipmentParts {
			candidate := sharedQuotaAlternativePlan(parts, subset, pivot, material, best.cost)
			if candidate.cost < best.cost-costTieEpsilon {
				best = candidate
			}
		}
	}
	quotaAlternativeCache.Store(key, best)
	return best
}

// sharedQuotaAlternativePlan 对其余三件的共同贡献做计数 DP。每效果最多只在最后
// 一件再补一条，因此共同计数至少 max(各组需求)-1；上限无需超过最大需求。
// 准备件遵守所有候选组的禁止项，最终 pivot 才按各组自己的禁止项做 OR 判定。
func sharedQuotaAlternativePlan(parts map[string]partScan, goals []map[string]int, pivot, material string, upperBound float64) quotaAlternativePlan {
	best := quotaAlternativePlan{cost: costUnreachable}
	maxNeeds, forbidden := make(map[string]int), make(map[string]bool)
	for _, goal := range goals {
		for effect, need := range goal {
			if need > maxNeeds[effect] {
				maxNeeds[effect] = need
			}
			if need < 0 {
				forbidden[effect] = true
			}
		}
	}
	var effects, forbiddenNames []string
	for effect := range maxNeeds {
		effects = append(effects, effect)
	}
	for effect := range forbidden {
		forbiddenNames = append(forbiddenNames, effect)
	}
	sort.Strings(effects)
	sort.Strings(forbiddenNames)
	lowerTotal := 0
	for _, effect := range effects {
		lowerTotal += max(0, maxNeeds[effect]-1)
	}
	if lowerTotal > 3*maxSlot {
		return best
	}
	type choice struct {
		mask     int
		required []string
		cost     float64
	}
	type path struct {
		cost  float64
		masks [4]int
	}
	states := map[int]path{0: {}}
	choices := make(map[string][]choice)
	completed := 0
	for partIndex, part := range equipmentParts {
		scan, exists := parts[part]
		if !exists {
			return best
		}
		if part == pivot {
			continue
		}
		completed++
		remaining := 3 - completed
		for mask := 0; mask < 1<<len(effects); mask++ {
			if bits.OnesCount(uint(mask)) > maxSlot {
				continue
			}
			candidate := choice{}
			valid := true
			for i, effect := range effects {
				if maxNeeds[effect] == 4 && mask&(1<<i) == 0 {
					valid = false
					break
				}
				if mask&(1<<i) != 0 {
					if forbidden[effect] {
						valid = false
						break
					}
					candidate.required = append(candidate.required, effect)
					candidate.mask |= 1 << (3 * i)
				}
			}
			if !valid {
				continue
			}
			_, candidate.cost = dynamicEffectPlan(scan, candidate.required, forbiddenNames, nil, material)
			if candidate.cost < upperBound {
				choices[part] = append(choices[part], candidate)
			}
		}
		next := make(map[int]path)
		keys := make([]int, 0, len(states))
		for counts := range states {
			keys = append(keys, counts)
		}
		sort.Ints(keys)
		for _, counts := range keys {
			previous := states[counts]
			for choiceIndex, candidate := range choices[part] {
				total := counts + candidate.mask
				valid := true
				for i, effect := range effects {
					count := (total >> (3 * i)) & 7
					if count > min(3, maxNeeds[effect]) || count+remaining < maxNeeds[effect]-1 {
						valid = false
						break
					}
				}
				cost := previous.cost + candidate.cost
				if !valid || cost >= upperBound {
					continue
				}
				if old, ok := next[total]; ok && old.cost <= cost+costTieEpsilon {
					continue
				}
				updated := previous
				updated.cost, updated.masks[partIndex] = cost, choiceIndex
				next[total] = updated
			}
		}
		states = next
	}
	keys := make([]int, 0, len(states))
	for counts := range states {
		keys = append(keys, counts)
	}
	sort.Ints(keys)
	for _, counts := range keys {
		path := states[counts]
		var targets []effectAlternative
		for _, goal := range goals {
			target := effectAlternative{forbidden: forbiddenEffects(goal)}
			for i, effect := range effects {
				if goal[effect] > ((counts >> (3 * i)) & 7) {
					target.required = append(target.required, effect)
				}
			}
			if len(target.required) <= maxSlot {
				targets = append(targets, target)
			}
		}
		_, pivotCost := dynamicEffectAlternativePlan(parts[pivot], targets, material)
		cost := path.cost + pivotCost
		if cost >= min(best.cost, upperBound)-costTieEpsilon {
			continue
		}
		best = quotaAlternativePlan{cost: cost, pivot: pivot, targets: make(map[string][]effectAlternative)}
		best.targets[pivot] = targets
		for i, part := range equipmentParts {
			if part != pivot {
				best.targets[part] = []effectAlternative{{required: choices[part][path.masks[i]].required, forbidden: forbiddenNames}}
			}
		}
	}
	return best
}

func expectedModulesForQuotaAlternativesWithMaterial(parts map[string]partScan, goals []map[string]int, material string) float64 {
	return planQuotaAlternatives(parts, goals, material).cost
}

func desiredLockSlotForQuotaAlternatives(parts map[string]partScan, part string, goals []map[string]int, material string) (int, bool) {
	if allPartsSatisfiedQuotaAlternatives(parts, goals) {
		return 0, false
	}
	plan := planQuotaAlternatives(parts, goals, material)
	scan, exists := parts[part]
	if !exists || countLocks(scan) >= maxSlot-1 || plan.cost >= costUnreachable {
		return 0, false
	}
	if plan.pivot == part {
		for _, other := range equipmentParts {
			if other != part && !partSatisfiedEffectAlternatives(parts[other], plan.targets[other]) {
				return 0, false
			}
		}
	}
	slot, _ := dynamicEffectAlternativePlan(scan, plan.targets[part], material)
	return slot, slot != 0
}

func chooseBestPartForQuotaAlternativesWithMaterial(parts map[string]partScan, goals []map[string]int, order []string, material string) (string, bool) {
	if allPartsSatisfiedQuotaAlternatives(parts, goals) {
		return "", false
	}
	plan := planQuotaAlternatives(parts, goals, material)
	if plan.cost >= costUnreachable {
		return "", false
	}
	if plan.pivot == "" {
		return chooseBestPartForQuotaWithMaterial(parts, plan.quota, order, material)
	}
	bestPart, bestCost := "", -1.0
	for _, part := range order {
		if part == plan.pivot {
			continue
		}
		scan, exists := parts[part]
		if !exists || partSatisfiedEffectAlternatives(scan, plan.targets[part]) {
			continue
		}
		_, cost := dynamicEffectAlternativePlan(scan, plan.targets[part], material)
		if cost > bestCost && cost < costUnreachable {
			bestPart, bestCost = part, cost
		}
	}
	if bestPart != "" {
		return bestPart, true
	}
	for _, part := range equipmentParts {
		if part != plan.pivot && !partSatisfiedEffectAlternatives(parts[part], plan.targets[part]) {
			return "", false
		}
	}
	for _, part := range order {
		if part == plan.pivot {
			return part, true
		}
	}
	return "", false
}

func decideQuotaAlternativesByExpectedCost(current, candidate map[string]partScan, goals []map[string]int, material string) ResultDecision {
	if allPartsSatisfiedQuotaAlternatives(candidate, goals) {
		return ResultDecisionAccept
	}
	currentCost := expectedModulesForQuotaAlternativesWithMaterial(current, goals, material)
	candidateCost := expectedModulesForQuotaAlternativesWithMaterial(candidate, goals, material)
	if candidateCost < currentCost-costDecisionEpsilon {
		return ResultDecisionAccept
	}
	return ResultDecisionKeep
}
