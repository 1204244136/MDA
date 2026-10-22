package equipmentreroll

import (
	"math"
	"math/rand/v2"
	"testing"
)

// 独立抽样、按实际配额入口加锁与取舍，检查禁项清除的解析成本能否由执行策略兑现。
func TestForbiddenPolicyMatchesIndependentSimulation(t *testing.T) {
	quota := map[string]int{"攻击力增加": 4, "优越代码伤害增加": 4, "最大装弹数增加": -1}
	required := []string{"攻击力增加", "优越代码伤害增加"}
	for caseIndex, tc := range []struct {
		name     string
		initial  partScan
		material string
	}{
		{"modules_from_invalid", partScanOf("防御力增加", "命中率增加", "蓄力伤害增加"), "订制模块"},
		{"modules_cleanup", partScanOf("攻击力增加", "优越代码伤害增加", "最大装弹数增加"), "订制模块"},
		{"modules_mixed_temporary", partScanFromArrays(
			[maxSlot]string{"攻击力增加", "优越代码伤害增加", "最大装弹数增加"},
			[maxSlot]string{}, [maxSlot]SlotLock{LockNone, LockOneTime, LockNone},
		), "订制模块"},
		{"keys_from_invalid", partScanOf("防御力增加", "命中率增加", "蓄力伤害增加"), "自订密钥"},
		{"keys_cleanup", partScanOf("攻击力增加", "优越代码伤害增加", "最大装弹数增加"), "自订密钥"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, expected := bestLockSlotAndCostForRequired(tc.initial, quota, required, tc.material)
			rng := rand.New(rand.NewPCG(20261022, uint64(caseIndex)))
			parts := make(map[string]partScan, len(equipmentParts))
			for _, part := range equipmentParts {
				parts[part] = partScanOf("攻击力增加", "优越代码伤害增加", "")
			}
			lockDecisions := make(map[partScan]int)
			resultDecisions := make(map[[2]partScan]ResultDecision)
			const trials = 10000
			total, sumSquares := 0.0, 0.0
			for trial := 0; trial < trials; trial++ {
				scan, modules := tc.initial, 0
				for round := 0; !forbiddenSimulationSatisfied(scan); round++ {
					if round >= 10000 {
						t.Fatal("forbidden cleanup did not complete within 10000 rolls")
					}
					for {
						slot, known := lockDecisions[scan]
						if !known {
							parts["头部"] = scan
							slot, _ = DesiredLockSlotForQuota(parts, "头部", quota, tc.material)
							lockDecisions[scan] = slot
						}
						if slot == 0 {
							break
						}
						if slot < 1 || slot > maxSlot || countLocks(scan) >= 2 || scan.Slots[slot-1].Lock != LockNone {
							t.Fatalf("invalid next lock %d for %v", slot, scan)
						}
						effect := scan.Slots[slot-1].Effect
						if effect != "攻击力增加" && effect != "优越代码伤害增加" {
							t.Fatalf("policy tried to lock forbidden or unwanted effect %q", effect)
						}
						if tc.material == "订制模块" {
							modules += 2 + countLocks(scan)
							scan.Slots[slot-1].Lock = LockPermanent
						} else {
							scan.Slots[slot-1].Lock = LockOneTime
						}
					}
					modules += 1 + countLocks(scan)
					candidate := effectStrategyIndependentDraw(scan, rng)
					pair := [2]partScan{scan, candidate}
					decision, known := resultDecisions[pair]
					if !known {
						// 4 攻 4 优令本件责任固定；降级入口与全局比较仅相差另外三件的常量成本。
						decision = decideResultPageQuotaWithMaterial(scan.Effects(), candidate.Effects(), scan, quota, tc.material)
						resultDecisions[pair] = decision
					}
					if decision == ResultDecisionAccept {
						scan = candidate
					}
					// 单次锁无论接受或维持都会消耗，下一轮必须由真实锁定策略重新申请。
					scan = effectPostRollScan(scan)
				}
				cost := float64(modules)
				total += cost
				sumSquares += cost * cost
			}
			mean := total / trials
			standardError := math.Sqrt((sumSquares/trials - mean*mean) / trials)
			if math.Abs(mean-expected) > 5*standardError+.05 {
				t.Errorf("simulation %.6f +/- %.6f, analytic %.6f", mean, 5*standardError, expected)
			}
			t.Logf("simulation %.6f, analytic %.6f, standard error %.6f", mean, expected, standardError)
		})
	}
}

func forbiddenSimulationSatisfied(scan partScan) bool {
	attack, elemental := false, false
	for _, slot := range scan.Slots {
		switch slot.Effect {
		case "攻击力增加":
			attack = true
		case "优越代码伤害增加":
			elemental = true
		case "最大装弹数增加":
			return false
		}
	}
	return attack && elemental
}
