package equipmentreroll

import (
	"math"
	"testing"
)

func TestValueStatsMatchExhaustiveConditioning(t *testing.T) {
	for _, tc := range []struct {
		tiers, required [maxSlot]int
		locks           [maxSlot]SlotLock
	}{
		{[maxSlot]int{14, 1, 9}, [maxSlot]int{15, 15, 0}, [maxSlot]SlotLock{}},
		{[maxSlot]int{15, 12, 2}, [maxSlot]int{11, 11, 11}, [maxSlot]SlotLock{LockPermanent, LockOneTime, LockNone}},
		{[maxSlot]int{5, 12, 0}, [maxSlot]int{11, 11, 0}, [maxSlot]SlotLock{}},
		{[maxSlot]int{15, 15, 15}, [maxSlot]int{15, 15, 15}, [maxSlot]SlotLock{}},
	} {
		table := singleValueTestCostTable(tc.tiers, tc.required, tc.locks)
		mass, success, gain := 0.0, 0.0, 0.0
		var walk func(int, [maxSlot]int, float64)
		walk = func(i int, candidate [maxSlot]int, p float64) {
			if i == maxSlot {
				if candidate == tc.tiers {
					return
				}
				mass += p
				if table.accepts(tc.tiers, candidate) {
					success += p
					gain += p * table.cost(candidate)
				}
				return
			}
			if tc.tiers[i] == 0 || tc.locks[i] != LockNone {
				candidate[i] = tc.tiers[i]
				walk(i+1, candidate, p)
				return
			}
			for k, w := range tierProbabilities {
				candidate[i] = k + 1
				walk(i+1, candidate, p*w)
			}
		}
		walk(0, [maxSlot]int{}, 1)
		p, g := valueOutcomeStats(tc.tiers, tc.locks, table)
		if math.Abs(p-success/mass) > 1e-10 || math.Abs(g-gain/mass) > 1e-10 {
			t.Fatalf("stats %g %g vs enumeration %g %g", p, g, success/mass, gain/mass)
		}
	}
	// 仅一个可洗 T1 槽时，完全相同被排除，下一次必然提高；T14→15 为 1/99。
	p, _ := valueOutcomeStats([maxSlot]int{1}, [maxSlot]SlotLock{}, singleValueTestCostTable([maxSlot]int{1}, [maxSlot]int{2}, [maxSlot]SlotLock{}))
	if math.Abs(p-1) > 1e-12 {
		t.Fatal(p)
	}
	p, _ = valueOutcomeStats([maxSlot]int{14}, [maxSlot]SlotLock{}, singleValueTestCostTable([maxSlot]int{14}, [maxSlot]int{15}, [maxSlot]SlotLock{}))
	if math.Abs(p-1.0/99) > 1e-12 {
		t.Fatal(p)
	}
}

func TestCharacterTotalTargetsAndAllocation(t *testing.T) {
	cfg := valueTestConfig()
	cfg.Mode = rerollModeCharacter
	cfg.ValueTargets = valueTarget{"攻击力增加": 31}
	parts := map[string]partScan{}
	for i, part := range equipmentParts {
		parts[part] = partScan{Slots: [maxSlot]slotScanData{valueTestSlot("攻击力增加", []int{14, 1, 7, 8}[i], LockNone)}}
	}
	p, err := planValueReroll(parts, cfg)
	if err != nil || p.Part != "臂部" {
		t.Fatalf("wash T1, not T14: %+v %v", p, err)
	}
	cfg.ValueTargets["攻击力增加"] = 30
	p, err = planValueReroll(parts, cfg)
	if err != nil || !p.Satisfied {
		t.Fatalf("total reached: %+v %v", p, err)
	}
	parts["腿部"] = partScan{}
	cfg.ValueTargets["攻击力增加"] = 46
	if _, err = planValueReroll(parts, cfg); err == nil {
		t.Fatal("three copies cannot reach T46")
	}
	cfg.ValueTargets["攻击力增加"] = 45
	if _, err = planValueReroll(parts, cfg); err != nil {
		t.Fatal(err)
	}
	progress, err := evaluateValueScope(parts, cfg)
	if err != nil || progress.Minimum["攻击力增加"] != 3 || progress.Maximum["攻击力增加"] != 45 {
		t.Fatal(progress, err)
	}
}

func TestValueLocksBudgetSunkCostAndRelease(t *testing.T) {
	scan := partScan{Slots: [maxSlot]slotScanData{valueTestSlot("攻击力增加", 15, LockPermanent), valueTestSlot("最大装弹数增加", 1, LockPermanent)}}
	cfg := valueTestConfig()
	cfg.ValueTargets = valueTarget{"攻击力增加": 15, "最大装弹数增加": 15}
	p, err := planValueRerollWithInventory(map[string]partScan{"头部": scan}, cfg, Inventory{1000, 0}, "")
	if err != nil || len(p.Release) != 1 || p.Release[0] != 2 || p.FirstModules != 2 || len(p.Steps) != 0 {
		t.Fatalf("keep sunk high lock, release low lock: %+v %v", p, err)
	}
	if slot, mat := nextValueLockChange(scan, p); slot != 2 || mat != valueReleaseMaterial {
		t.Fatal(slot, mat)
	}
	for _, inv := range []Inventory{{0, 999}, {1, 999}, {2, 19}, {3, 49}, {8, 50}} {
		valueLockPlans(scan, inv, func(p valuePlan, _, _, _ int) {
			if p.FirstModules > inv.CustomModules || p.FirstKeys > inv.CustomLockKeys {
				t.Fatalf("budget exceeded %+v %+v", p, inv)
			}
		})
	}
	// 无关锁应解除，不为它增加每轮洗练费用。
	cfg.ValueTargets = valueTarget{"最大装弹数增加": 15}
	p, err = planValueRerollWithInventory(map[string]partScan{"头部": scan}, cfg, Inventory{1000, 0}, "")
	if err != nil || len(p.Release) != 2 || p.FirstModules != 1 {
		t.Fatal(p, err)
	}
}

func TestValuePlanClearedOnResult(t *testing.T) {
	const id int64 = 92177
	t.Cleanup(func() { clearMonitorState(id) })
	_ = setCurrentPart(id, "头部")
	storeValuePlan(id, valuePlan{Part: "头部"})
	stageResultDecision(id, "头部", ResultDecisionKeep, [maxSlot]string{}, [maxSlot]string{})
	if _, ok := currentValuePlan(id); ok {
		t.Fatal("expired round plan reused")
	}
}
