package equipmentreroll

import (
	"fmt"
	"math"
)

type valueLockStep struct {
	Slot     int
	Material string
}

// valuePlan 是一轮不可拆分的选装/解锁/加锁方案；执行层按顺序落实，不临时改材料。
type valuePlan struct {
	Part                 string
	Pending              int
	Satisfied            bool
	Locks                [maxSlot]SlotLock
	Release              []int
	Steps                []valueLockStep
	Probability          float64 // 一轮获得可接受进展的概率（排除整组重复后）
	RemainingModules     float64 // 接受结果后的平均续程成本估值
	ExpectedTotalModules float64 // 首次接受成本 + 接受后的续程估值
	ExpectedModules      float64 // 保持本方案直到首次进展，包含固定锁首次费
	ExpectedKeys         float64
	FirstModules         int
	FirstKeys            int
}

// valueOutcomeStats 枚举完整结果，复用实机的成本取舍规则。返回接受概率及
// 未除以接受概率的续程成本；只排除整组重复，无关效果也参与条件化。
func valueOutcomeStats(tiers [maxSlot]int, locks [maxSlot]SlotLock, table valueCostTable) (float64, float64) {
	repeat := 1.0
	for i, tier := range tiers {
		if tier == 0 || locks[i] != LockNone {
			continue
		}
		repeat *= tierProbabilities[tier-1]
	}
	if repeat >= 1 {
		return 0, 0
	}
	prob, remaining := 0.0, 0.0
	var walk func(int, [maxSlot]int, float64)
	walk = func(i int, candidate [maxSlot]int, mass float64) {
		if i == maxSlot {
			if table.accepts(tiers, candidate) {
				prob += mass
				remaining += mass * table.cost(candidate)
			}
			return
		}
		if tiers[i] == 0 || locks[i] != LockNone {
			candidate[i] = tiers[i]
			walk(i+1, candidate, mass)
			return
		}
		for k, p := range tierProbabilities {
			candidate[i] = k + 1
			walk(i+1, candidate, mass*p)
		}
	}
	walk(0, [maxSlot]int{}, 1)
	return prob / (1 - repeat), remaining / (1 - repeat)
}

// valueLockPlans 枚举保留/免费解除、单次/固定锁组合，并固定新增锁顺序。
// 费用按“保留固定锁 → 新增固定锁 → 单次锁”排序计入，旧固定锁不重复收费。
func valueLockPlans(scan partScan, inv Inventory, visit func(valuePlan, int, int, int)) {
	var desired [maxSlot]SlotLock
	var choose func(int)
	choose = func(i int) {
		if i < maxSlot {
			for _, lock := range []SlotLock{LockNone, LockPermanent, LockOneTime} {
				if scan.Slots[i].Effect == "" && lock != LockNone {
					continue
				}
				desired[i] = lock
				choose(i + 1)
			}
			return
		}
		p := valuePlan{Locks: desired}
		retained, total, keyCost := 0, 0, 0
		var added []int
		for j, lock := range desired {
			old := scan.Slots[j].Lock
			if old != LockNone && old != lock {
				p.Release = append(p.Release, j+1)
			}
			if lock == LockNone {
				continue
			}
			total++
			if lock == LockPermanent && old == lock {
				retained++
			} else if lock == LockPermanent {
				added = append(added, j)
			}
		}
		if total >= maxSlot {
			return
		}
		acquire := 0
		for _, j := range added {
			acquire += LockCost("订制模块", retained)
			retained++
			p.Steps = append(p.Steps, valueLockStep{j + 1, "订制模块"})
		}
		for j, lock := range desired {
			if lock != LockOneTime {
				continue
			}
			keyCost += LockCost("自订密钥", retained)
			retained++
			if scan.Slots[j].Lock != lock {
				p.Steps = append(p.Steps, valueLockStep{j + 1, "自订密钥"})
			}
		}
		round := RerollModuleCost(total)
		p.FirstModules, p.FirstKeys = round+acquire, keyCost
		if p.FirstModules > inv.CustomModules || keyCost > inv.CustomLockKeys {
			return
		}
		visit(p, round, acquire, keyCost)
	}
	choose(0)
}

func betterValuePlan(a, b valuePlan) bool {
	if b.Part == "" {
		return true
	}
	// 比较完成全部目标的估值，不再按每有效 T 排序。
	x, y := a.ExpectedTotalModules, b.ExpectedTotalModules
	if math.Abs(x-y) > 1e-9 {
		return x < y
	}
	x, y = a.ExpectedKeys, b.ExpectedKeys
	if math.Abs(x-y) > 1e-9 {
		return x < y
	}
	return len(a.Release)+len(a.Steps) < len(b.Release)+len(b.Steps)
}

func planValueReroll(parts map[string]partScan, cfg carrierConfig) (valuePlan, error) {
	return planValueRerollWithInventory(parts, cfg, optimisticLockInventory, "")
}

// planValueRerollWithInventory 使用首次接受期望加续程成本，不宣称四件十二槽的全局最优 DP。
// 限定 part 用于确认页，接受结果后重新对全部装备比较。
func planValueRerollWithInventory(parts map[string]partScan, cfg carrierConfig, inv Inventory, only string) (valuePlan, error) {
	progress, err := evaluateValueScope(parts, cfg)
	if err != nil {
		return valuePlan{}, err
	}
	best := valuePlan{Pending: progress.Pending, Satisfied: progress.Pending == 0}
	if best.Satisfied {
		return best, nil
	}
	for _, part := range cfg.valueScope() {
		if only != "" && only != part {
			continue
		}
		scan := parts[part]
		tiers, _ := valuePartTiers(scan)
		// 单次锁结果后过期；续程成本只受固定锁集合影响，同一集合复用查表。
		tables := map[int]valueCostTable{}
		getTable := func(locks [maxSlot]SlotLock) valueCostTable {
			mask := 0
			for i, lock := range locks {
				if lock == LockPermanent {
					mask |= 1 << i
				}
			}
			table, exists := tables[mask]
			if !exists {
				table = newValueCostTable(parts, part, cfg, progress, locks)
				tables[mask] = table
			}
			return table
		}
		valueLockPlans(scan, inv, func(p valuePlan, round, acquire, keys int) {
			table := getTable(p.Locks)
			prob, remaining := valueOutcomeStats(tiers, p.Locks, table)
			if prob <= 1e-12 {
				return
			}
			p.Part, p.Pending = part, progress.Pending
			p.Probability, p.RemainingModules = prob, remaining/prob
			p.ExpectedModules = float64(acquire) + float64(round)/prob
			p.ExpectedKeys = float64(keys) / prob
			// 有限密钥用尽后转固定锁：按到达该分支的概率计费，不能把“期望够用”当无限预算。
			if keys > 0 {
				n := inv.CustomLockKeys / keys
				failure := math.Pow(1-prob, float64(n))
				// 已有固定锁不重付；把剩下单次锁按当前固定锁数逐个固定。
				fixed, conversion := 0, 0
				for _, lock := range p.Locks {
					if lock == LockPermanent {
						fixed++
					}
				}
				fallback := p.Locks
				for i, lock := range p.Locks {
					if lock == LockOneTime {
						conversion += LockCost("订制模块", fixed)
						fixed++
						fallback[i] = LockPermanent
					}
				}
				// 转固定锁后续程成本与接受集合都会变化，不能只追加转换费。
				fallbackProb, fallbackRemaining := valueOutcomeStats(tiers, fallback, getTable(fallback))
				if failure > 1e-12 {
					if fallbackProb <= 1e-12 {
						return
					}
					p.ExpectedModules = float64(acquire) + float64(round)*(1-failure)/prob + failure*(float64(conversion)+float64(round)/fallbackProb)
					p.RemainingModules = (1-failure)*remaining/prob + failure*fallbackRemaining/fallbackProb
				}
				p.ExpectedKeys = float64(keys) * (1 - failure) / prob
			}
			p.ExpectedTotalModules = p.ExpectedModules + p.RemainingModules
			if betterValuePlan(p, best) {
				best = p
			}
		})
	}
	if best.Part == "" {
		return best, fmt.Errorf("no affordable value reroll can advance the targets")
	}
	return best, nil
}
