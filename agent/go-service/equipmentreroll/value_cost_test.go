package equipmentreroll

import (
	"math"
	"testing"
)

func singleValueTestCostTable(tiers, targets [maxSlot]int, locks [maxSlot]SlotLock) valueCostTable {
	effects := [maxSlot]string{"攻击力增加", "最大装弹数增加", "优越代码伤害增加"}
	cfg := valueTestConfig()
	cfg.ValueTargets = valueTarget{}
	scan := partScan{}
	for i, tier := range tiers {
		if tier > 0 {
			scan.Slots[i] = valueTestSlot(effects[i], tier, locks[i])
		}
		if targets[i] > 0 {
			cfg.ValueTargets[effects[i]] = targets[i]
		}
	}
	parts := map[string]partScan{"头部": scan}
	progress, _ := evaluateValueScope(parts, cfg)
	return newValueCostTable(parts, "头部", cfg, progress, locks)
}

func valueLogTestParts() (map[string]partScan, carrierConfig) {
	cfg := valueTestConfig()
	cfg.Mode = rerollModeCharacter
	cfg.ValueTargets = valueTarget{"攻击力增加": 44, "最大装弹数增加": 44, "优越代码伤害增加": 44}
	parts := map[string]partScan{}
	for i, part := range equipmentParts {
		tiers := [4][3]int{{5, 11, 11}, {5, 9, 11}, {9, 3, 12}, {2, 6, 5}}[i]
		scan := partScan{Slots: [maxSlot]slotScanData{
			valueTestSlot("攻击力增加", tiers[0], LockNone),
			valueTestSlot("最大装弹数增加", tiers[1], LockNone),
			valueTestSlot("优越代码伤害增加", tiers[2], LockNone),
		}}
		if part == "身躯" {
			scan.Slots[2].Lock = LockPermanent
		}
		parts[part] = scan
	}
	return parts, cfg
}

func TestValueCostLogRegressions(t *testing.T) {
	parts, cfg := valueLogTestParts()
	base := parts["腿部"]
	for _, tc := range []struct {
		name  string
		tiers [maxSlot]int
		want  ResultDecision
	}{
		{"15:18:45 attack T10 elemental T15 ammo T5", [maxSlot]int{10, 5, 15}, ResultDecisionAccept},
		{"15:22:58 elemental T13 ammo T3", [maxSlot]int{4, 3, 13}, ResultDecisionAccept},
		{"15:23:17 elemental T14 ammo T2", [maxSlot]int{7, 2, 14}, ResultDecisionAccept},
		{"15:23:22 all improve", [maxSlot]int{5, 10, 8}, ResultDecisionAccept},
		{"all regress", [maxSlot]int{1, 5, 4}, ResultDecisionKeep},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var values [maxSlot]string
			for i, tier := range tc.tiers {
				values[i] = valueTestSlot(base.Slots[i].Effect, tier, LockNone).Value
			}
			got, before, after, err := evaluateValueResult(parts, "腿部", base.Effects(), values, cfg)
			t.Logf("remaining modules %.6f -> %.6f, decision %s", before, after, got.String())
			if err != nil || got != tc.want {
				t.Fatalf("got %v %v want %v", got, err, tc.want)
			}
			if got == ResultDecisionAccept && after >= before-valueCostEpsilon {
				t.Fatal("log sample should strictly reduce cost")
			}
		})
	}
}

func TestValueCostTradeoffAndNoOscillation(t *testing.T) {
	// 同为单件总 T34：T15/T5/T14 的续程只需补一条，优于 T11/T11/T12 补两条。
	table := singleValueTestCostTable([maxSlot]int{11, 11, 12}, [maxSlot]int{12, 12, 12}, [maxSlot]SlotLock{})
	before, after := [maxSlot]int{11, 11, 12}, [maxSlot]int{15, 5, 14}
	if !table.accepts(before, after) || table.accepts(after, before) {
		t.Fatal("cost tradeoff or reverse decision incorrect")
	}
	// 总 T 更高也不能拆掉已经完成的两项，换回三项都没达标。
	table = singleValueTestCostTable([maxSlot]int{12, 12, 1}, [maxSlot]int{12, 12, 12}, [maxSlot]SlotLock{})
	if table.accepts([maxSlot]int{12, 12, 1}, [maxSlot]int{11, 11, 11}) {
		t.Fatal("raw T sum must not override remaining cost")
	}
}

func TestValueEffectCostAllocation(t *testing.T) {
	// 穷举每件保留/提升门槛，与背包最优解独立对照。
	slots := []valueCostSlot{{14, 3, 5}, {1, 2, 2}, {7, 1, 0}, {8, 3, 3}}
	for _, target := range []int{30, 31, 44, 59, 60} {
		best := math.Inf(1)
		var walk func(int, int, float64)
		walk = func(i, total int, cost float64) {
			if i == len(slots) {
				if total >= target {
					best = min(best, cost)
				}
				return
			}
			s := slots[i]
			walk(i+1, total+s.Tier, cost)
			for tier := s.Tier + 1; tier <= 15; tier++ {
				p := 0.0
				for j := tier - 1; j < 15; j++ {
					p += tierProbabilities[j]
				}
				walk(i+1, total+tier, cost+float64(s.Acquire)+float64(s.Round)/p)
			}
		}
		walk(0, 0, 0)
		if got := valueEffectCost(slots, target); math.Abs(got-best) > 1e-8 {
			t.Fatalf("target %d: %g vs %g", target, got, best)
		}
	}
}

func TestValueCostTableUsesPostRoundLocks(t *testing.T) {
	tiers, targets := [maxSlot]int{15, 1}, [maxSlot]int{15, 15}
	free := singleValueTestCostTable(tiers, targets, [maxSlot]SlotLock{})
	once := singleValueTestCostTable(tiers, targets, [maxSlot]SlotLock{LockOneTime})
	fixed := singleValueTestCostTable(tiers, targets, [maxSlot]SlotLock{LockPermanent})
	if free.cost(tiers) != once.cost(tiers) || math.Abs(free.cost(tiers)-fixed.cost(tiers)-2) > 1e-8 {
		t.Fatal("one-time lock expired; permanent acquisition is sunk")
	}
}

func TestValueCostLimitedKeys(t *testing.T) {
	// 仅能支付一轮单次锁。失败后转固定锁：2 + (98/99)*(2+198)。
	// 不得把 20 把密钥当成可无限重复使用，也不能再次收取已有固定锁费用。
	cfg := valueTestConfig()
	cfg.ValueTargets = valueTarget{"攻击力增加": 15, "最大装弹数增加": 15}
	scan := partScan{Slots: [maxSlot]slotScanData{
		valueTestSlot("攻击力增加", 15, LockNone),
		valueTestSlot("最大装弹数增加", 14, LockNone),
	}}
	p, err := planValueRerollWithInventory(map[string]partScan{"头部": scan}, cfg, Inventory{1000, 20}, "")
	if err != nil || p.Locks != ([maxSlot]SlotLock{LockOneTime, LockNone, LockNone}) {
		t.Fatalf("expected one key round then fixed fallback: %+v %v", p, err)
	}
	want := 2.0 + (98.0/99)*(2+198)
	if math.Abs(p.ExpectedTotalModules-want) > 1e-8 || math.Abs(p.ExpectedKeys-20) > 1e-8 {
		t.Fatalf("finite keys: %+v, want modules %g, keys 20", p, want)
	}
}

func TestValueCostPlannerAndResultAgree(t *testing.T) {
	parts, cfg := valueLogTestParts()
	progress, err := evaluateValueScope(parts, cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, locks := range [][maxSlot]SlotLock{{}, {LockPermanent}, {LockOneTime}, {LockPermanent, LockOneTime}} {
		scan := parts["腿部"]
		for i, lock := range locks {
			scan.Slots[i].Lock = lock
		}
		parts["腿部"] = scan
		table := newValueCostTable(parts, "腿部", cfg, progress, locks)
		before, _ := valuePartTiers(scan)
		for _, tiers := range [][maxSlot]int{{10, 5, 15}, {7, 2, 14}, {1, 1, 1}, {15, 15, 15}} {
			var values [maxSlot]string
			for i, lock := range locks {
				if lock != LockNone {
					tiers[i] = before[i]
				}
				values[i] = valueTestSlot(scan.Slots[i].Effect, tiers[i], lock).Value
			}
			decision, err := decideValueResultForScope(parts, "腿部", scan.Effects(), values, cfg)
			if err != nil || (decision == ResultDecisionAccept) != table.accepts(before, tiers) {
				t.Fatalf("locks %v tiers %v: planner/result mismatch %v %v", locks, tiers, decision, err)
			}
		}
	}
}
