package equipmentreroll

import (
	"math"
	"testing"
)

func multiGoalExample() []map[string]int {
	return []map[string]int{
		{TargetEffectAttackIncrease: 4, TargetEffectElementalDamage: 4, "暴击率增加": 4},
		{TargetEffectAttackIncrease: 4, TargetEffectElementalDamage: 4, "暴击率增加": 3, "最大装弹数增加": 1},
	}
}

func multiGoalParts(scan partScan) map[string]partScan {
	parts := make(map[string]partScan, len(equipmentParts))
	for _, part := range equipmentParts {
		parts[part] = scan
	}
	return parts
}

func TestMultiGoalUnionStrictlyReducesExampleCost(t *testing.T) {
	goals := multiGoalExample()
	for _, scan := range []partScan{
		{},
		partScanOf("防御力增加", TargetEffectElementalDamage, ""),
	} {
		parts := multiGoalParts(scan)
		a := expectedModulesForQuotaWithMaterial(parts, goals[0], "订制模块")
		b := expectedModulesForQuotaWithMaterial(parts, goals[1], "订制模块")
		combined := expectedModulesForQuotaAlternativesWithMaterial(parts, goals, "订制模块")
		t.Logf("single A %.9f, B %.9f, union %.9f", a, b, combined)
		if combined >= min(a, b)-1e-6 || combined <= 0 {
			t.Fatalf("union %.9f should strictly improve singles %.9f/%.9f", combined, a, b)
		}
	}
}

func TestMultiGoalLastPieceUsesUnionProbability(t *testing.T) {
	goals := multiGoalExample()
	complete := partScanOf(TargetEffectAttackIncrease, TargetEffectElementalDamage, "暴击率增加")
	parts := multiGoalParts(complete)
	last := partScanOf("防御力增加", TargetEffectElementalDamage, TargetEffectAttackIncrease)
	last.Slots[1].Lock, last.Slots[2].Lock = LockPermanent, LockPermanent
	parts[equipmentParts[3]] = last
	cost := expectedModulesForQuotaAlternativesWithMaterial(parts, goals, "订制模块")
	// 唯一未锁槽从移除攻/优后的池中抽取；暴击或装弹任一种都让整组目标达标。
	want := 3 * (1 - effectWeights[TargetEffectAttackIncrease] - effectWeights[TargetEffectElementalDamage]) /
		(effectWeights["暴击率增加"] + effectWeights["最大装弹数增加"])
	if math.Abs(cost-want) > 1e-7 {
		t.Fatalf("joint terminal probability cost %.9f, want %.9f", cost, want)
	}
	if part, ok := chooseBestPartForQuotaAlternativesWithMaterial(parts, goals, equipmentParts[:], "订制模块"); !ok || part != equipmentParts[3] {
		t.Fatalf("last piece = %q/%v", part, ok)
	}
	if part, ok := chooseBestPartForQuotaAlternativesWithMaterial(parts, goals, equipmentParts[:3], "订制模块"); ok || part != "" {
		t.Fatal("decision piece must belong to eligible order after preparation completes")
	}
}

func TestMultiGoalNoMixedCompletionAndForbiddenRemainLocal(t *testing.T) {
	parts := multiGoalParts(partScanOf(TargetEffectAttackIncrease, TargetEffectElementalDamage, "暴击率增加"))
	parts[equipmentParts[3]] = partScanOf(TargetEffectAttackIncrease, TargetEffectElementalDamage, "防御力增加")
	if allPartsSatisfiedQuotaAlternatives(parts, multiGoalExample()) {
		t.Fatal("4攻4优3暴 plus garbage is not either complete target")
	}
	goals := []map[string]int{
		{TargetEffectAttackIncrease: 4, "最大装弹数增加": -1},
		{TargetEffectAttackIncrease: 4, "最大装弹数增加": 1},
	}
	parts[equipmentParts[3]] = partScanOf(TargetEffectAttackIncrease, TargetEffectElementalDamage, "最大装弹数增加")
	if !allPartsSatisfiedQuotaAlternatives(parts, goals) {
		t.Fatal("second target can require an effect forbidden by first target")
	}
	if cost := expectedModulesForQuotaAlternativesWithMaterial(parts, goals, "订制模块"); cost != 0 {
		t.Fatalf("accepted second target cost %.9f", cost)
	}
}

func TestMultiGoalDuplicateAndOrderAreInvariant(t *testing.T) {
	parts, goals := multiGoalParts(partScan{}), multiGoalExample()
	for _, material := range []string{"订制模块", "自订密钥"} {
		combined := expectedModulesForQuotaAlternativesWithMaterial(parts, goals, material)
		reordered := expectedModulesForQuotaAlternativesWithMaterial(parts, []map[string]int{goals[1], goals[0], goals[1]}, material)
		if math.Abs(combined-reordered) > 1e-9 {
			t.Fatalf("goal order/duplicate changed cost %.9f => %.9f", combined, reordered)
		}
		single := expectedModulesForQuotaWithMaterial(parts, goals[0], material)
		duplicate := expectedModulesForQuotaAlternativesWithMaterial(parts, []map[string]int{goals[0], goals[0]}, material)
		if math.Abs(single-duplicate) > 1e-9 {
			t.Fatalf("duplicate goal cost %.9f, single %.9f", duplicate, single)
		}
	}
}

func TestMultiGoalPlanWaitsForSharedPiecesBeforePivot(t *testing.T) {
	parts, goals := multiGoalParts(partScan{}), multiGoalExample()
	plan := planQuotaAlternatives(parts, goals, "订制模块")
	if plan.pivot == "" {
		t.Fatal("example should retain a genuine OR decision piece")
	}
	part, ok := chooseBestPartForQuotaAlternativesWithMaterial(parts, goals, equipmentParts[:], "订制模块")
	if !ok || part == plan.pivot {
		t.Fatalf("shared preparation must precede pivot %q, picked %q/%v", plan.pivot, part, ok)
	}
	if slot, need := desiredLockSlotForQuotaAlternatives(parts, plan.pivot, goals, "订制模块"); need || slot != 0 {
		t.Fatal("must not lock the decision piece before shared preparation")
	}
	if part, ok := chooseBestPartForQuotaAlternativesWithMaterial(parts, goals, []string{plan.pivot}, "订制模块"); ok || part != "" {
		t.Fatal("must not select pivot when eligible order excludes unfinished preparation")
	}
	if part, ok := chooseBestPartForQuotaAlternativesWithMaterial(parts, goals, nil, "订制模块"); ok || part != "" {
		t.Fatal("empty eligible order must not select any part")
	}
}

func TestMultiGoalUnionLocksAndTemporaryLocks(t *testing.T) {
	targets := []effectAlternative{
		{required: []string{TargetEffectAttackIncrease, TargetEffectElementalDamage, "暴击率增加"}},
		{required: []string{TargetEffectAttackIncrease, TargetEffectElementalDamage, "最大装弹数增加"}},
	}
	scan := partScanOf("防御力增加", TargetEffectElementalDamage, TargetEffectAttackIncrease)
	slot, cost := dynamicEffectAlternativePlan(scan, targets, "订制模块")
	if slot == 0 {
		t.Fatal("valuable shared effect should be protected")
	}
	scan.Slots[slot-1].Lock = LockPermanent
	_, continuation := dynamicEffectAlternativePlan(scan, targets, "订制模块")
	if math.Abs(cost-float64(lockAcquireCost(0))-continuation) > 1e-7 {
		t.Fatalf("selected lock does not match estimated policy: %.9f vs %.9f", cost, continuation)
	}
	scan.Slots[slot-1].Lock = LockOneTime
	_, temporary := dynamicEffectAlternativePlan(scan, targets, "订制模块")
	if temporary <= continuation || temporary >= costUnreachable {
		t.Fatalf("temporary protection must expire: %.9f vs permanent %.9f", temporary, continuation)
	}
	scan.Slots[slot-1].Effect = "防御力增加"
	scan.Slots[slot-1].Lock = LockPermanent
	if _, unreachable := dynamicEffectAlternativePlan(scan, targets, "订制模块"); unreachable != costUnreachable {
		t.Fatalf("irrelevant permanent effect leaves both full targets unreachable: %.9f", unreachable)
	}
}

func TestMultiGoalCostGuidesResultTowardEitherPlan(t *testing.T) {
	goals := multiGoalExample()
	parts := multiGoalParts(partScanOf("防御力增加", "", ""))
	for _, third := range []string{"暴击率增加", "最大装弹数增加"} {
		candidate := multiGoalParts(partScanOf("防御力增加", "", ""))
		candidate[equipmentParts[0]] = partScanOf("防御力增加", TargetEffectElementalDamage, third)
		if decision := decideQuotaAlternativesByExpectedCost(parts, candidate, goals, "订制模块"); decision != ResultDecisionAccept {
			t.Fatalf("candidate progress toward %q should be accepted, got %v", third, decision)
		}
		if decision := decideQuotaAlternativesByExpectedCost(candidate, parts, goals, "订制模块"); decision != ResultDecisionKeep {
			t.Fatalf("losing useful %q progress should be rejected, got %v", third, decision)
		}
	}
}

func TestMultiGoalConvergesToCurrentCostAdvantage(t *testing.T) {
	goals := multiGoalExample()
	initial := multiGoalParts(partScan{})
	completeA := partScanOf(TargetEffectAttackIncrease, TargetEffectElementalDamage, "暴击率增加")
	towardA := multiGoalParts(completeA)
	aCosts := independentGoalCosts(towardA, goals, "订制模块")
	if aCosts[0] != 0 || aCosts[1] <= aCosts[0] {
		t.Fatalf("completed A must be strictly preferable: %v", aCosts)
	}
	if cost := expectedModulesForQuotaAlternativesWithMaterial(towardA, goals, "订制模块"); cost != 0 {
		t.Fatalf("completed A leaves union cost %.9f", cost)
	}
	if part, ok := chooseBestPartForQuotaAlternativesWithMaterial(towardA, goals, equipmentParts, "订制模块"); ok || part != "" {
		t.Fatal("completed A must stop rather than pursue B")
	}
	if decision := decideQuotaAlternativesByExpectedCost(initial, towardA, goals, "订制模块"); decision != ResultDecisionAccept {
		t.Fatal("A completion should be accepted from the initial state")
	}

	// 保留前三件的暴击，最后一件已经锁住装弹，只有优越尚未获得。
	// 这时 A 不再可达，联合策略必须继续补 B，不能执着于初始方案。
	towardB := multiGoalParts(completeA)
	last := equipmentParts[len(equipmentParts)-1]
	pendingB := partScanOf("防御力增加", TargetEffectAttackIncrease, "最大装弹数增加")
	pendingB.Slots[2].Lock = LockPermanent
	towardB[last] = pendingB
	bCosts := independentGoalCosts(towardB, goals, "订制模块")
	joint := expectedModulesForQuotaAlternativesWithMaterial(towardB, goals, "订制模块")
	if bCosts[0] != costUnreachable || bCosts[1] <= 0 || bCosts[1] >= costUnreachable || math.Abs(joint-bCosts[1]) > 1e-7 {
		t.Fatalf("permanent ammo should converge to unfinished B: %v, joint %.9f", bCosts, joint)
	}
	if part, ok := chooseBestPartForQuotaAlternativesWithMaterial(towardB, goals, equipmentParts, "订制模块"); !ok || part != last {
		t.Fatalf("B completion must continue on %q, got %q/%v", last, part, ok)
	}
	if decision := decideQuotaAlternativesByExpectedCost(initial, towardB, goals, "订制模块"); decision != ResultDecisionAccept {
		t.Fatal("finite B progress should be accepted from the same initial state")
	}
	finishedB := multiGoalParts(completeA)
	pendingB.Slots[0].Effect = TargetEffectElementalDamage
	finishedB[last] = pendingB
	if decision := decideQuotaAlternativesByExpectedCost(towardB, finishedB, goals, "订制模块"); decision != ResultDecisionAccept {
		t.Fatal("B completion must be accepted")
	}
	if part, ok := chooseBestPartForQuotaAlternativesWithMaterial(finishedB, goals, equipmentParts, "订制模块"); ok || part != "" {
		t.Fatal("completed B must stop rather than return to unreachable A")
	}
	t.Logf("A-favored independent costs A=%.9f B=%.9f; B-favored A=unreachable B=%.9f joint=%.9f", aCosts[0], aCosts[1], bCosts[1], joint)
}

func TestMultiGoalThreeIndependentTargetsAndPermanentLocks(t *testing.T) {
	goals := []map[string]int{
		{TargetEffectAttackIncrease: 1, TargetEffectElementalDamage: 1, "暴击率增加": 1},
		{"最大装弹数增加": 1, "防御力增加": 1, "命中率增加": 1},
		{"蓄力速度增加": 1, "蓄力伤害增加": 1, "暴击伤害增加": 1},
	}
	parts := multiGoalParts(partScan{})
	combined := expectedModulesForQuotaAlternativesWithMaterial(parts, goals, "订制模块")
	for _, goal := range goals {
		if single := expectedModulesForQuotaWithMaterial(parts, goal, "订制模块"); combined > single+1e-7 {
			t.Fatalf("three-goal policy %.9f exceeds single %.9f", combined, single)
		}
	}
	if combined <= 0 || combined >= costUnreachable {
		t.Fatalf("three independent valid targets have finite positive estimate, got %.9f", combined)
	}
	goals = multiGoalExample()
	locked := partScanOf(TargetEffectAttackIncrease, TargetEffectElementalDamage, "最大装弹数增加")
	locked.Slots[2].Lock = LockPermanent
	parts = multiGoalParts(partScan{})
	parts[equipmentParts[0]] = locked
	a := expectedModulesForQuotaWithMaterial(parts, goals[0], "订制模块")
	b := expectedModulesForQuotaWithMaterial(parts, goals[1], "订制模块")
	combined = expectedModulesForQuotaAlternativesWithMaterial(parts, goals, "订制模块")
	if a != costUnreachable || b >= costUnreachable || math.Abs(combined-b) > 1e-7 {
		t.Fatalf("permanent ammo must eliminate A and retain B: A %.9f, B %.9f, combined %.9f", a, b, combined)
	}
}
