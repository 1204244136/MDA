package equipmentreroll

import (
	"math/bits"
	"sort"
	"sync"
)

// quotaCostPlan 只在当前快照中分配各件负责的效果；每次接受结果后重新求解。
// 单件成本包含未来逐步加锁，分配目标则精确最小化四件单件成本之和。
// 这仍是分解后的续程估值，不宣称四件随机过程的全局最优解。
type quotaCostPlan struct {
	required map[string][]string
	cost     float64
}

var quotaCostCache sync.Map

func effectPartsCacheKey(parts map[string]partScan, quota map[string]int, material string) string {
	clean := make(map[string]partScan, len(parts))
	for part, scan := range parts {
		for i := range scan.Slots {
			scan.Slots[i].Value = ""
		}
		clean[part] = scan
	}
	return partsCacheKey(clean, quota) + "#" + lockMaterialOrDefault(material)
}

func planQuotaCost(parts map[string]partScan, quota map[string]int, material string) quotaCostPlan {
	quota = normalizeQuota(quota)
	key := effectPartsCacheKey(parts, quota, material)
	if cached, ok := quotaCostCache.Load(key); ok {
		return cached.(quotaCostPlan)
	}
	var effects []string
	for effect, need := range quota {
		if need > 0 {
			effects = append(effects, effect)
		}
	}
	sort.Strings(effects)
	result := quotaCostPlan{required: make(map[string][]string), cost: costUnreachable}
	if len(effects) > len(officialEffects) || !quotaIsValid(quota) {
		return result
	}
	goal := 0
	for i, effect := range effects {
		goal |= quota[effect] << (3 * i)
	}
	type choice struct {
		counts   int
		required []string
		cost     float64
	}
	type path struct {
		cost  float64
		masks [4]int
	}
	states := map[int]path{0: {}}
	choices := make([][]choice, len(equipmentParts))
	for partIndex, part := range equipmentParts {
		scan, exists := parts[part]
		if !exists {
			return result
		}
		for mask := 0; mask < 1<<len(effects); mask++ {
			if bits.OnesCount(uint(mask)) > maxSlot {
				continue
			}
			candidate := choice{}
			valid := true
			for i, effect := range effects {
				if quota[effect] == len(equipmentParts) && mask&(1<<i) == 0 {
					valid = false
					break
				}
				if mask&(1<<i) != 0 {
					candidate.required = append(candidate.required, effect)
					candidate.counts |= 1 << (3 * i)
				}
			}
			if !valid {
				continue
			}
			_, candidate.cost = bestLockSlotAndCostForRequired(scan, quota, candidate.required, material)
			if candidate.cost < costUnreachable {
				choices[partIndex] = append(choices[partIndex], candidate)
			}
		}
		next := make(map[int]path)
		// 稳定迭代，避免浮点持平时由 map 顺序随机改变承载者。
		keys := make([]int, 0, len(states))
		for counts := range states {
			keys = append(keys, counts)
		}
		sort.Ints(keys)
		for _, counts := range keys {
			previous := states[counts]
			for choiceIndex, candidate := range choices[partIndex] {
				total := counts + candidate.counts
				valid := true
				for i, effect := range effects {
					count := (total >> (3 * i)) & 7
					if count > quota[effect] || count+len(equipmentParts)-partIndex-1 < quota[effect] {
						valid = false
						break
					}
				}
				if !valid {
					continue
				}
				cost := previous.cost + candidate.cost
				if old, exists := next[total]; exists && old.cost <= cost+costTieEpsilon {
					continue
				}
				updated := previous
				updated.cost = cost
				updated.masks[partIndex] = choiceIndex
				next[total] = updated
			}
		}
		states = next
	}
	if best, ok := states[goal]; ok {
		result.cost = best.cost
		for i, part := range equipmentParts {
			result.required[part] = choices[i][best.masks[i]].required
		}
	}
	quotaCostCache.Store(key, result)
	return result
}

func allocateQuotaRequiredWithMaterial(parts map[string]partScan, quota map[string]int, material string) map[string][]string {
	// 不暴露缓存内的 map/slice，防止调用方修改后污染后续规划。
	allocated := make(map[string][]string, len(equipmentParts))
	for part, required := range planQuotaCost(parts, quota, material).required {
		allocated[part] = append([]string(nil), required...)
	}
	return allocated
}

func expectedModulesForQuotaWithMaterial(parts map[string]partScan, quota map[string]int, material string) float64 {
	if AllPartsSatisfiedQuota(parts, quota) {
		return 0
	}
	return planQuotaCost(parts, quota, material).cost
}
