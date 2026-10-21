package equipmentreroll

import (
	"math"
	"math/bits"
	"math/rand/v2"
	"reflect"
	"testing"
)

func effectStrategyScreenshotParts() map[string]partScan {
	return map[string]partScan{
		"头部": partScanFromArrays([maxSlot]string{"攻击力增加", "", ""}, [maxSlot]string{"6.18%", "", ""}, [maxSlot]SlotLock{}),
		"臂部": partScanFromArrays([maxSlot]string{"命中率增加", "优越代码伤害增加", "暴击率增加"}, [maxSlot]string{"11.81%", "23.56%", "5.70%"}, [maxSlot]SlotLock{}),
		"身躯": partScanFromArrays([maxSlot]string{"蓄力伤害增加", "", ""}, [maxSlot]string{"11.81%", "", ""}, [maxSlot]SlotLock{}),
		"腿部": partScanFromArrays([maxSlot]string{"最大装弹数增加", "防御力增加", ""}, [maxSlot]string{"68.93%", "11.81%", ""}, [maxSlot]SlotLock{}),
	}
}

func TestEffectStrategyInitialEstimateUsesFutureLocks(t *testing.T) {
	parts := effectStrategyScreenshotParts()
	cfg := carrierConfig{
		Mode:      rerollModeCharacter,
		Operation: rerollOperationEffect,
		Quota:     map[string]int{"优越代码伤害增加": 4, "攻击力增加": 4, "最大装弹数增加": 2},
	}
	cost, err := initialModuleEstimate(parts, cfg)
	if err != nil {
		t.Fatal(err)
	}
	// 此输入曾报告 941.674 个模块；独立动态策略核算为 133.15839968。
	if math.Abs(cost-133.15839968) > 1e-6 || math.Ceil(cost) != 134 {
		t.Fatalf("screenshot estimate = %.9f, want approximately 133.16 with future locks", cost)
	}
	for part := range parts {
		parts[part] = partScan{}
	}
	emptyCost, err := initialModuleEstimate(parts, cfg)
	if err != nil || math.Abs(emptyCost-136.72238724) > 1e-6 {
		t.Fatalf("empty equipment estimate = %.9f, %v, want 136.72238724", emptyCost, err)
	}
}

func TestEffectStrategyBothUsefulSlotsHaveSameCompletionCost(t *testing.T) {
	quota := map[string]int{"优越代码伤害增加": 4, "攻击力增加": 4, "最大装弹数增加": 4}
	required := []string{"优越代码伤害增加", "攻击力增加", "最大装弹数增加"}
	// 锁住 2、3 号后，两种排列都只缺 1 号攻击。锁费 2+3，单次费用 3，
	// 攻击概率为 0.10/(1-0.10-0.12)，所以总期望是 5+3/(0.10/0.78)=28.4。
	for _, effects := range [][maxSlot]string{
		{"防御力增加", "最大装弹数增加", "优越代码伤害增加"},
		{"防御力增加", "优越代码伤害增加", "最大装弹数增加"},
	} {
		scan := partScanFromArrays(effects, [maxSlot]string{}, [maxSlot]SlotLock{})
		_, cost := bestLockSlotAndCostForRequired(scan, quota, required, "订制模块")
		if math.Abs(cost-28.4) > 1e-6 {
			t.Errorf("effects %v: cost = %.9f, want 28.4 including both new locks", effects, cost)
		}
	}
}

func TestEffectStrategyExecutionUsesTheEstimatedLock(t *testing.T) {
	quota := map[string]int{"优越代码伤害增加": 4, "攻击力增加": 4, "最大装弹数增加": 4}
	required := []string{"优越代码伤害增加", "攻击力增加", "最大装弹数增加"}
	for _, effects := range [][maxSlot]string{
		{"命中率增加", "优越代码伤害增加", "暴击率增加"},
		{"蓄力速度增加", "最大装弹数增加", ""},
		{"防御力增加", "最大装弹数增加", "优越代码伤害增加"},
	} {
		scan := partScanFromArrays(effects, [maxSlot]string{}, [maxSlot]SlotLock{})
		parts := map[string]partScan{}
		for _, part := range equipmentParts {
			parts[part] = scan
		}
		estimatedSlot, cost := bestLockSlotAndCostForRequired(scan, quota, required, "订制模块")
		actualSlot, need := DesiredLockSlotForQuota(parts, "头部", quota, "订制模块")
		if actualSlot != estimatedSlot || need != (estimatedSlot != 0) {
			t.Errorf("effects %v: estimated next lock %d, execution %d/%v", effects, estimatedSlot, actualSlot, need)
			continue
		}
		if !need {
			continue
		}
		// 立即执行估价选中的锁，不应改变剩余期望与这笔锁费之和。
		scan.Slots[actualSlot-1].Lock = LockPermanent
		_, after := bestLockSlotAndCostForRequired(scan, quota, required, "订制模块")
		if math.Abs(cost-(2+after)) > 1e-6 {
			t.Errorf("effects %v: before %.9f, after first lock plus acquisition %.9f", effects, cost, 2+after)
		}
	}
}

func TestEffectStrategyRetainingUsefulSecondSlotImprovesContinuation(t *testing.T) {
	baseline := partScanOf("防御力增加", "", "")
	retained := partScanOf("防御力增加", "优越代码伤害增加", "")
	threeQuota := map[string]int{"优越代码伤害增加": 4, "攻击力增加": 4, "最大装弹数增加": 4}
	threeRequired := []string{"优越代码伤害增加", "攻击力增加", "最大装弹数增加"}
	_, threeBase := bestLockSlotAndCostForRequired(baseline, threeQuota, threeRequired, "订制模块")
	_, threeRetained := bestLockSlotAndCostForRequired(retained, threeQuota, threeRequired, "订制模块")
	// 三词条时仍可能继续不锁，已有的 2 号槽可以被忽略，不应反而抬高成本。
	if threeRetained > threeBase+1e-6 {
		t.Errorf("useful slot 2 must not worsen continuation: retained %.9f, baseline %.9f", threeRetained, threeBase)
	}
	quota := map[string]int{"优越代码伤害增加": 4, "攻击力增加": 4}
	required := []string{"优越代码伤害增加", "攻击力增加"}
	_, baseCost := bestLockSlotAndCostForRequired(baseline, quota, required, "订制模块")
	_, retainedCost := bestLockSlotAndCostForRequired(retained, quota, required, "订制模块")
	if !(retainedCost < baseCost) {
		t.Fatalf("useful slot 2 should improve continuation: retained %.9f, baseline %.9f", retainedCost, baseCost)
	}
	if got := DecideResultPageSingle(retained.Effects(), baseline, parseSingleTarget(map[string]int{
		"优越代码伤害增加": 0, "攻击力增加": 0,
	})); got != ResultDecisionAccept {
		t.Fatalf("single result should accept useful slot 2, got %v", got)
	}
}

func TestEffectStrategyGlobalRetainedTargetsNeverCostMoreThanEmpty(t *testing.T) {
	blank := make(map[string]partScan)
	for _, part := range equipmentParts {
		blank[part] = partScan{}
	}
	for _, quota := range []map[string]int{
		{"优越代码伤害增加": 4, "攻击力增加": 4, "最大装弹数增加": 2},
		{"优越代码伤害增加": 3, "攻击力增加": 2, "最大装弹数增加": 1},
		{"优越代码伤害增加": 4, "攻击力增加": 4, "最大装弹数增加": 1, "蓄力速度增加": 1},
	} {
		for _, material := range []string{"订制模块", "自订密钥"} {
			baseline := expectedModulesForQuotaWithMaterial(blank, quota, material)
			for _, parts := range []map[string]partScan{
				effectStrategyScreenshotParts(),
				{
					"头部": partScanOf("最大装弹数增加", "优越代码伤害增加", ""),
					"臂部": partScanOf("攻击力增加", "", "优越代码伤害增加"),
					"身躯": partScanOf("蓄力速度增加", "攻击力增加", "最大装弹数增加"),
					"腿部": partScanOf("优越代码伤害增加", "最大装弹数增加", "攻击力增加"),
				},
			} {
				cost := expectedModulesForQuotaWithMaterial(parts, quota, material)
				if cost > baseline+1e-6 {
					t.Errorf("quota %v, material %s: retained %.9f exceeds empty %.9f", quota, material, cost, baseline)
				}
			}
		}
	}
}

// 穷举各效果由哪几件承担，直接比较完整分配；不复用生产环境的计数状态 DP。
func effectStrategyBruteAllocationCost(parts map[string]partScan, quota map[string]int, effects []string, material string) float64 {
	assigned := make(map[string][]string)
	best := costUnreachable
	var visit func(int)
	visit = func(index int) {
		if index == len(effects) {
			cost := 0.0
			for _, part := range equipmentParts {
				_, local := bestLockSlotAndCostForRequired(parts[part], quota, assigned[part], material)
				cost += local
			}
			best = math.Min(best, cost)
			return
		}
		effect := effects[index]
		for mask := 0; mask < 1<<len(equipmentParts); mask++ {
			if bits.OnesCount(uint(mask)) != quota[effect] {
				continue
			}
			valid := true
			for i, part := range equipmentParts {
				if mask&(1<<i) != 0 && len(assigned[part]) >= maxSlot {
					valid = false
				}
			}
			if !valid {
				continue
			}
			for i, part := range equipmentParts {
				if mask&(1<<i) != 0 {
					assigned[part] = append(assigned[part], effect)
				}
			}
			visit(index + 1)
			for i, part := range equipmentParts {
				if mask&(1<<i) != 0 {
					assigned[part] = assigned[part][:len(assigned[part])-1]
				}
			}
		}
	}
	visit(0)
	return best
}

func TestEffectStrategyGlobalAllocationMatchesExhaustiveCandidates(t *testing.T) {
	parts := effectStrategyScreenshotParts()
	for _, quota := range []map[string]int{
		{"优越代码伤害增加": 4, "攻击力增加": 4, "最大装弹数增加": 2},
		{"优越代码伤害增加": 3, "攻击力增加": 2, "最大装弹数增加": 1},
		{"优越代码伤害增加": 4, "攻击力增加": 4, "最大装弹数增加": 1, "蓄力速度增加": 1},
	} {
		effects := positiveQuotaEffects(quota)
		for _, material := range []string{"订制模块", "自订密钥"} {
			want := effectStrategyBruteAllocationCost(parts, quota, effects, material)
			got := expectedModulesForQuotaWithMaterial(parts, quota, material)
			if math.Abs(got-want) > 1e-6 {
				t.Errorf("quota %v, material %s: allocated %.9f, exhaustive %.9f", quota, material, got, want)
			}
		}
	}
}

func TestEffectStrategyReturnedAllocationCannotPolluteCachedPlan(t *testing.T) {
	parts := effectStrategyScreenshotParts()
	quota := map[string]int{"优越代码伤害增加": 4, "攻击力增加": 4, "最大装弹数增加": 2}
	for _, material := range []string{"订制模块", "自订密钥"} {
		assigned := allocateQuotaRequiredWithMaterial(parts, quota, material)
		want := make(map[string][]string, len(assigned))
		for part, required := range assigned {
			want[part] = append([]string(nil), required...)
		}
		before := expectedModulesForQuotaWithMaterial(parts, quota, material)
		assigned["头部"][0] = "防御力增加"
		assigned["身躯"] = []string{"不存在的效果"}
		delete(assigned, "腿部")
		assigned["不存在的部位"] = []string{"攻击力增加"}
		got := allocateQuotaRequiredWithMaterial(parts, quota, material)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("material %s: caller mutation polluted cached allocation: got %v, want %v", material, got, want)
		}
		if after := expectedModulesForQuotaWithMaterial(parts, quota, material); math.Abs(before-after) > 1e-6 {
			t.Errorf("material %s: caller mutation changed cached cost %.9f -> %.9f", material, before, after)
		}
	}
}

func TestEffectStrategySingleRestrictionsAndExistingLocks(t *testing.T) {
	target := parseSingleTarget(map[string]int{"攻击力增加": 1, "最大装弹数增加": 2, "优越代码伤害增加": 3})
	scan := partScanOf("防御力增加", "最大装弹数增加", "优越代码伤害增加")
	_, cost := bestLockSlotAndCostForTarget(scan, singleQuota(target), sortedTargetEffects(target), nil, "订制模块", slotAllowMap(target))
	if math.Abs(cost-28.4) > 1e-6 {
		t.Errorf("single slot restrictions should preserve both useful slots: cost %.9f, want 28.4", cost)
	}
	scan.Slots[1].Lock, scan.Slots[2].Lock = LockPermanent, LockPermanent
	if cost := singleExpectedCost(scan, target); math.Abs(cost-23.4) > 1e-6 {
		t.Errorf("existing locks must not be charged again: cost %.9f, want 23.4", cost)
	}
	if slot, need := singleDesiredLockSlot(scan, target, "订制模块"); need || slot != 0 {
		t.Errorf("both useful slots already locked: unexpected new lock %d/%v", slot, need)
	}
	wrongSlot := parseSingleTarget(map[string]int{"最大装弹数增加": 3})
	if !singleTargetUnreachable(scan, wrongSlot) {
		t.Fatal("an effect locked outside its required slot must remain unreachable")
	}
}

func TestEffectStrategyForbiddenLockedSlotRemainsUnreachable(t *testing.T) {
	quota := map[string]int{"攻击力增加": 1, "防御力增加": -1}
	scan := partScanOf("攻击力增加", "", "防御力增加")
	scan.Slots[2].Lock = LockPermanent
	_, cost := bestLockSlotAndCostForRequired(scan, quota, []string{"攻击力增加"}, "订制模块")
	if cost < costUnreachable {
		t.Fatalf("locked forbidden affix must remain unreachable, got %.9f", cost)
	}
	scan.Slots[2].Lock = LockNone
	_, cost = bestLockSlotAndCostForRequired(scan, quota, []string{"攻击力增加"}, "订制模块")
	if cost <= 0 || cost >= costUnreachable || math.IsNaN(cost) {
		t.Fatalf("unlocked forbidden affix should be removable with finite positive cost, got %.9f", cost)
	}
}

func TestEffectStrategyResultDecisionIgnoresConsumedOneTimeLocks(t *testing.T) {
	quota := map[string]int{"优越代码伤害增加": 4, "攻击力增加": 4, "最大装弹数增加": 4}
	target := parseSingleTarget(map[string]int{"优越代码伤害增加": 0, "攻击力增加": 0, "最大装弹数增加": 0})
	cases := []struct {
		current, candidate [maxSlot]string
		locks              [maxSlot]SlotLock
	}{
		{[maxSlot]string{"最大装弹数增加", "优越代码伤害增加", "暴击率增加"}, [maxSlot]string{"攻击力增加", "优越代码伤害增加", ""}, [maxSlot]SlotLock{LockNone, LockOneTime, LockNone}},
		{[maxSlot]string{"攻击力增加", "最大装弹数增加", ""}, [maxSlot]string{"攻击力增加", "", "优越代码伤害增加"}, [maxSlot]SlotLock{LockOneTime, LockNone, LockNone}},
		{[maxSlot]string{"优越代码伤害增加", "最大装弹数增加", "防御力增加"}, [maxSlot]string{"最大装弹数增加", "攻击力增加", "防御力增加"}, [maxSlot]SlotLock{LockNone, LockNone, LockOneTime}},
		{[maxSlot]string{"命中率增加", "优越代码伤害增加", "最大装弹数增加"}, [maxSlot]string{"攻击力增加", "优越代码伤害增加", "最大装弹数增加"}, [maxSlot]SlotLock{LockNone, LockOneTime, LockPermanent}},
	}
	for _, tc := range cases {
		current := partScanFromArrays(tc.current, [maxSlot]string{}, tc.locks)
		candidate := partScanFromArrays(tc.candidate, [maxSlot]string{}, tc.locks)
		clearedCurrent, clearedCandidate := current, candidate
		for i := range clearedCurrent.Slots {
			if clearedCurrent.Slots[i].Lock == LockOneTime {
				clearedCurrent.Slots[i].Lock = LockNone
			}
			if clearedCandidate.Slots[i].Lock == LockOneTime {
				clearedCandidate.Slots[i].Lock = LockNone
			}
		}
		gotSingle := decideSingleCandidateWithMaterial(current, candidate, target, "订制模块")
		wantSingle := decideSingleCandidateWithMaterial(clearedCurrent, clearedCandidate, target, "订制模块")
		if gotSingle != wantSingle {
			t.Errorf("single %v -> %v: consumed lock changed decision %v -> %v", tc.current, tc.candidate, wantSingle, gotSingle)
		}
		gotQuota := decideResultPageQuotaWithMaterial(tc.current, tc.candidate, current, quota, "订制模块")
		wantQuota := decideResultPageQuotaWithMaterial(tc.current, tc.candidate, clearedCurrent, quota, "订制模块")
		if gotQuota != wantQuota {
			t.Errorf("quota %v -> %v: consumed lock changed decision %v -> %v", tc.current, tc.candidate, wantQuota, gotQuota)
		}
		if got, want := DecideResultPageQuota(tc.current, tc.candidate, current, quota), DecideResultPageQuota(tc.current, tc.candidate, clearedCurrent, quota); got != want {
			t.Errorf("default quota %v -> %v: consumed lock changed decision %v -> %v", tc.current, tc.candidate, want, got)
		}
	}
}

// 概率与抽样独立写出，不调用生产枚举器，避免枚举错误同时污染解析和模拟。
func effectStrategyIndependentDraw(scan partScan, rng *rand.Rand) partScan {
	effects := [9]string{"优越代码伤害增加", "命中率增加", "最大装弹数增加", "攻击力增加", "蓄力伤害增加", "蓄力速度增加", "暴击率增加", "暴击伤害增加", "防御力增加"}
	weights := [9]float64{.10, .12, .12, .10, .12, .12, .12, .10, .10}
	obtain := [maxSlot]float64{1, .5, .3}
	used := make(map[string]bool, maxSlot)
	for _, slot := range scan.Slots {
		if slot.Lock != LockNone {
			used[slot.Effect] = true
		}
	}
	for i := range scan.Slots {
		if scan.Slots[i].Lock != LockNone {
			continue
		}
		scan.Slots[i] = slotScanData{}
		if rng.Float64() >= obtain[i] {
			continue
		}
		total := 0.0
		for j, effect := range effects {
			if !used[effect] {
				total += weights[j]
			}
		}
		draw := rng.Float64() * total
		for j, effect := range effects {
			if used[effect] {
				continue
			}
			draw -= weights[j]
			if draw < 0 {
				scan.Slots[i].Effect = effect
				used[effect] = true
				break
			}
		}
	}
	return scan
}

func TestEffectStrategyModulePolicyMatchesIndependentSimulation(t *testing.T) {
	for _, target := range []singleTarget{
		parseSingleTarget(map[string]int{"优越代码伤害增加": 0, "攻击力增加": 0}),
		parseSingleTarget(map[string]int{"优越代码伤害增加": 0, "攻击力增加": 0, "最大装弹数增加": 0}),
	} {
		rng := rand.New(rand.NewPCG(20261021, uint64(len(target.Want))))
		initial := partScanOf("防御力增加", "", "")
		expected := singleExpectedCostWithMaterial(initial, target, "订制模块")
		// 缓存纯公开决策的返回值只为减少 1 万次模拟中的重复 API 查询。
		lockDecisions := make(map[partScan]int)
		resultDecisions := make(map[[2]partScan]ResultDecision)
		const trials = 10000
		total, sumSquares := 0.0, 0.0
		for trial := 0; trial < trials; trial++ {
			scan, modules := initial, 0
			for round := 0; !singlePartSatisfied(scan, target); round++ {
				if round >= 10000 {
					t.Fatal("simulation did not complete within 10000 rolls")
				}
				for {
					slot, known := lockDecisions[scan]
					if !known {
						slot, _ = singleDesiredLockSlot(scan, target, "订制模块")
						lockDecisions[scan] = slot
					}
					if slot == 0 {
						break
					}
					if scan.Slots[slot-1].Lock != LockNone {
						t.Fatal("policy tried to lock an already locked slot")
					}
					modules += 2 + countLocks(scan)
					scan.Slots[slot-1].Lock = LockPermanent
				}
				modules += 1 + countLocks(scan)
				candidate := effectStrategyIndependentDraw(scan, rng)
				pair := [2]partScan{scan, candidate}
				decision, known := resultDecisions[pair]
				if !known {
					decision = decideSingleCandidateWithMaterial(scan, candidate, target, "订制模块")
					resultDecisions[pair] = decision
				}
				if decision == ResultDecisionAccept {
					scan = candidate
				}
			}
			cost := float64(modules)
			total += cost
			sumSquares += cost * cost
		}
		mean := total / trials
		standardError := math.Sqrt((sumSquares/trials - mean*mean) / trials)
		if math.Abs(mean-expected) > 5*standardError+.05 {
			t.Errorf("target %v: simulation %.6f +/- %.6f, analytic %.6f", target.Want, mean, 5*standardError, expected)
		}
		t.Logf("target %v: simulation %.6f, analytic %.6f, standard error %.6f", target.Want, mean, expected, standardError)
	}
}
