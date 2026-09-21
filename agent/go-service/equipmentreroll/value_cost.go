package equipmentreroll

import "math"

const valueCostEpsilon = 1e-8

// valueCostSlot 描述独立补齐一种效果时的一件装备。未来采用固定锁保护其他目标效果。
// 不兑换密钥；实际下一轮仍由 valueLockPlans 枚举两种材料及预算。
type valueCostSlot struct {
	Tier, Round, Acquire int
}

// valueEffectCost 用背包 DP 分配角色总 T，而非强迫四件达到同一档。
// 每件可保留现值，或洗到某个更高门槛。未来按独立抽样、到门槛才接受估算，
// 不计超出门槛的随机富余；不同效果的锁费用也未联合摊销，因此只是保守的续程估值。
// 实际下一轮的整组重复排除、候选取舍及锁费由规划层精确枚举。
func valueEffectCost(slots []valueCostSlot, target int) float64 {
	var tail [16]float64
	for t := 15; t >= 1; t-- {
		tail[t] = tierProbabilities[t-1]
		if t < 15 {
			tail[t] += tail[t+1]
		}
	}
	dp := make([]float64, target+1)
	for i := 1; i <= target; i++ {
		dp[i] = math.Inf(1)
	}
	for _, slot := range slots {
		next := make([]float64, target+1)
		for i := range next {
			next[i] = math.Inf(1)
		}
		for total, cost := range dp {
			if math.IsInf(cost, 1) {
				continue
			}
			for tier := slot.Tier; tier <= 15; tier++ {
				c := cost
				if tier > slot.Tier {
					c += float64(slot.Acquire) + float64(slot.Round)/tail[tier]
				}
				t := min(target, total+tier)
				next[t] = min(next[t], c)
			}
		}
		dp = next
	}
	return dp[target]
}

// valueCostTable 预计算当前装备各槽的 15 个候选成本。单件内效果不重复，
// 因而各效果的角色总量 DP 可以分开查表；枚举 15^3 个结果时不再运行 DP。
type valueCostTable struct {
	Costs    [maxSlot][16]float64
	Required [maxSlot]int
	Other    float64
}

func newValueCostTable(parts map[string]partScan, part string, cfg carrierConfig, progress valueProgress, locks [maxSlot]SlotLock) valueCostTable {
	table := valueCostTable{Required: localValueRequirements(parts[part], progress.Totals, cfg.ValueTargets)}
	// 遍历固定效果顺序，避免 map 顺序引入浮点累计及决策差异。
	for _, effect := range officialEffects {
		target, selected := cfg.ValueTargets[effect]
		if !selected {
			continue
		}
		var slots []valueCostSlot
		local, index := -1, -1
		for _, p := range cfg.valueScope() {
			scan := parts[p]
			tiers, _ := valuePartTiers(scan)
			for i, slot := range scan.Slots {
				if slot.Effect != effect {
					continue
				}
				protected, retained := 0, 0
				for j, other := range scan.Slots {
					if j == i || cfg.ValueTargets[other.Effect] == 0 {
						continue
					}
					protected++
					lock := other.Lock
					if p == part {
						lock = locks[j]
					}
					// 本轮单次锁在结果产生后过期，不能当作未来免费保护。
					if lock == LockPermanent {
						retained++
					}
				}
				acquire := 0
				for j := retained; j < protected; j++ {
					acquire += LockCost("订制模块", j)
				}
				if p == part {
					local, index = i, len(slots)
				}
				slots = append(slots, valueCostSlot{tiers[i], RerollModuleCost(protected), acquire})
			}
		}
		if local < 0 {
			table.Other += valueEffectCost(slots, target)
			continue
		}
		for tier := 1; tier <= 15; tier++ {
			slots[index].Tier = tier
			table.Costs[local][tier] = valueEffectCost(slots, target)
		}
	}
	return table
}

func (t valueCostTable) cost(tiers [maxSlot]int) float64 {
	cost := t.Other
	for i, tier := range tiers {
		cost += t.Costs[i][tier]
	}
	return cost
}

// accepts 是规划概率和实机结果的唯一取舍规则。成本相同才用截断总 T 打破平局；
// 不把任一效果下降设为硬否决，也不以总 T 增长覆盖成本恶化。
func (t valueCostTable) accepts(before, after [maxSlot]int) bool {
	delta := t.cost(before) - t.cost(after)
	if math.Abs(delta) > valueCostEpsilon {
		return delta > 0
	}
	gain := 0
	for i, need := range t.Required {
		gain += min(after[i], need) - min(before[i], need)
	}
	return gain > 0
}
