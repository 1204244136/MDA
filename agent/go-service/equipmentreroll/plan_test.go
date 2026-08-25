package equipmentreroll

import (
	"math"
	"testing"
)

func emptyEffects() [maxSlot]string {
	return [maxSlot]string{}
}

func TestPartHasEffect(t *testing.T) {
	effects := [maxSlot]string{"攻击力增加", "优越代码伤害增加", "蓄力速度增加"}
	if !PartHasEffect(effects, TargetEffectElementalDamage) {
		t.Fatal("expected part to have the four-elemental-damage target effect")
	}
	missing := [maxSlot]string{"攻击力增加", "蓄力速度增加", ""}
	if PartHasEffect(missing, TargetEffectElementalDamage) {
		t.Fatal("part without target effect should not match")
	}
	if PartHasEffect(emptyEffects(), TargetEffectElementalDamage) {
		t.Fatal("empty part should not match")
	}
	if PartHasEffect(effects, "") {
		t.Fatal("empty target should never match")
	}
}

func TestAllPartsSatisfiedQuota(t *testing.T) {
	quota := map[string]int{"最大装弹数增加": 1, "蓄力速度增加": 3}
	parts := make(map[string]partScan, len(equipmentParts))
	for i, part := range equipmentParts {
		var effects [maxSlot]string
		effects[0] = TargetEffectElementalDamage
		effects[1] = TargetEffectAttackIncrease
		switch i {
		case 0:
			effects[2] = "最大装弹数增加"
		default:
			effects[2] = "蓄力速度增加"
		}
		parts[part] = partScanFromArrays(effects, [maxSlot]string{}, [maxSlot]SlotLock{})
	}
	if !AllPartsSatisfiedQuota(parts, quota) {
		t.Fatal("all parts with full core and exact quota should be satisfied")
	}

	// 配额不匹配：把头部额外词条改成蓄力速度，则最大装弹数配额缺失。
	head := parts["头部"]
	head.Slots[2].Effect = "蓄力速度增加"
	parts["头部"] = head
	if AllPartsSatisfiedQuota(parts, quota) {
		t.Fatal("quota mismatch should not be satisfied")
	}

	// 某件缺核心不应满足。
	legs := parts["腿部"]
	legs.Slots[0].Effect = "命中率增加"
	parts["腿部"] = legs
	if AllPartsSatisfiedQuota(parts, quota) {
		t.Fatal("part missing core should not be satisfied")
	}
}

func TestPartNeedsRerollQuota(t *testing.T) {
	quota := map[string]int{"最大装弹数增加": 1, "蓄力速度增加": 3}
	parts := make(map[string]partScan, len(equipmentParts))
	for i, part := range equipmentParts {
		var effects [maxSlot]string
		effects[0] = TargetEffectElementalDamage
		effects[1] = TargetEffectAttackIncrease
		switch i {
		case 0:
			effects[2] = "最大装弹数增加"
		default:
			effects[2] = "蓄力速度增加"
		}
		parts[part] = partScanFromArrays(effects, [maxSlot]string{}, [maxSlot]SlotLock{})
	}
	for _, part := range equipmentParts {
		if PartNeedsRerollQuota(parts, part, quota) {
			t.Fatalf("part %s should not need reroll in exact quota state", part)
		}
	}

	// 头部额外词条改成蓄力速度后，最大装弹数配额缺失且头部超配额，应需要重洗。
	head := parts["头部"]
	head.Slots[2].Effect = "蓄力速度增加"
	parts["头部"] = head
	if !PartNeedsRerollQuota(parts, "头部", quota) {
		t.Fatal("head with overrepresented extra should need reroll")
	}
}

func TestAllPartsSatisfiedQuotaWithOneExtra(t *testing.T) {
	quota := map[string]int{"最大装弹数增加": 1}
	parts := make(map[string]partScan, len(equipmentParts))
	for i, part := range equipmentParts {
		var effects [maxSlot]string
		effects[0] = TargetEffectElementalDamage
		effects[1] = TargetEffectAttackIncrease
		if i == 0 {
			effects[2] = "最大装弹数增加"
		}
		parts[part] = partScanFromArrays(effects, [maxSlot]string{}, [maxSlot]SlotLock{})
	}
	if !AllPartsSatisfiedQuota(parts, quota) {
		t.Fatal("one extra quota on one part should satisfy custom quota")
	}
	for _, part := range equipmentParts {
		if PartNeedsRerollQuota(parts, part, quota) {
			t.Fatalf("part %s should not need reroll when one extra quota is satisfied", part)
		}
	}

	// 空配额或配额未满足时不应判定完成。
	if AllPartsSatisfiedQuota(parts, map[string]int{}) {
		t.Fatal("empty quota should not be satisfied")
	}
	if AllPartsSatisfiedQuota(parts, map[string]int{"最大装弹数增加": 1, "蓄力速度增加": 4}) {
		t.Fatal("quota not satisfied should not be satisfied")
	}
}

func TestCustomQuotaFourElementalPlusAmmo(t *testing.T) {
	quota := map[string]int{TargetEffectElementalDamage: 4, "最大装弹数增加": 1}
	parts := make(map[string]partScan, len(equipmentParts))
	for i, part := range equipmentParts {
		var effects [maxSlot]string
		effects[0] = TargetEffectElementalDamage
		if i == 0 {
			effects[1] = "最大装弹数增加"
		}
		parts[part] = partScanFromArrays(effects, [maxSlot]string{}, [maxSlot]SlotLock{})
	}
	if !AllPartsSatisfiedQuota(parts, quota) {
		t.Fatal("four elemental + one max ammo should satisfy custom quota")
	}
	for _, part := range equipmentParts {
		if PartNeedsRerollQuota(parts, part, quota) {
			t.Fatalf("part %s should not need reroll when custom quota is satisfied", part)
		}
	}
}

func TestDesiredLockSlotForQuota(t *testing.T) {
	// 四优：每件只需要 1 条优越代码，已有优越代码后不应锁定。
	quota := map[string]int{TargetEffectElementalDamage: 4}
	parts := make(map[string]partScan, len(equipmentParts))
	for _, part := range equipmentParts {
		effects := [maxSlot]string{TargetEffectElementalDamage, "", ""}
		parts[part] = partScanFromArrays(effects, [maxSlot]string{}, [maxSlot]SlotLock{})
	}
	if slot, ok := DesiredLockSlotForQuota(parts, "头部", quota, ""); ok {
		t.Fatalf("four elemental should not lock, got slot %d", slot)
	}

	// 配额示例（攻击力 4 + 优越代码 4）：头部已有攻击力在 3 号槽，还缺优越代码，应锁定 3 号。
	quota = map[string]int{TargetEffectElementalDamage: 4, TargetEffectAttackIncrease: 4}
	parts = make(map[string]partScan, len(equipmentParts))
	for _, part := range equipmentParts {
		var effects [maxSlot]string
		if part == "头部" {
			effects[2] = TargetEffectAttackIncrease
		}
		parts[part] = partScanFromArrays(effects, [maxSlot]string{}, [maxSlot]SlotLock{})
	}
	if slot, ok := DesiredLockSlotForQuota(parts, "头部", quota, ""); !ok || slot != 3 {
		t.Fatalf("quota atk4+elem4 head should lock slot 3, got slot %d ok=%v", slot, ok)
	}

	// 四优+装弹：头部已有装弹在 2 号槽，还缺优越代码，应锁定 2 号装弹。
	quota = map[string]int{TargetEffectElementalDamage: 4, "最大装弹数增加": 1}
	parts = make(map[string]partScan, len(equipmentParts))
	for _, part := range equipmentParts {
		var effects [maxSlot]string
		if part == "头部" {
			effects[1] = "最大装弹数增加"
		}
		parts[part] = partScanFromArrays(effects, [maxSlot]string{}, [maxSlot]SlotLock{})
	}
	if slot, ok := DesiredLockSlotForQuota(parts, "头部", quota, ""); !ok || slot != 2 {
		t.Fatalf("four elemental + ammo head should lock slot 2, got slot %d ok=%v", slot, ok)
	}
}

func TestDesiredLockSlotForQuotaAllowsTwoLocks(t *testing.T) {
	quota := map[string]int{
		TargetEffectElementalDamage: 4,
		TargetEffectAttackIncrease:  4,
		"最大装弹数增加":                   4,
	}
	parts := make(map[string]partScan, len(equipmentParts))
	for _, part := range equipmentParts {
		var effects [maxSlot]string
		if part == "头部" {
			effects[1] = TargetEffectAttackIncrease
			effects[2] = "最大装弹数增加"
		}
		parts[part] = partScanFromArrays(effects, [maxSlot]string{}, [maxSlot]SlotLock{})
	}

	slot1, ok1 := DesiredLockSlotForQuota(parts, "头部", quota, "")
	if !ok1 {
		t.Fatal("expected first lock for head with two quota effects")
	}
	head := parts["头部"]
	head.Slots[slot1-1].Lock = LockOneTime
	parts["头部"] = head

	slot2, ok2 := DesiredLockSlotForQuota(parts, "头部", quota, "")
	if !ok2 {
		t.Fatal("expected second lock after first lock applied")
	}
	if slot2 == slot1 {
		t.Fatalf("second lock should be a different slot, got %d", slot2)
	}

	// 第二把锁后再调用不应再锁第三把。
	head.Slots[slot2-1].Lock = LockOneTime
	parts["头部"] = head
	if slot, ok := DesiredLockSlotForQuota(parts, "头部", quota, ""); ok {
		t.Fatalf("should not allow third lock, got slot %d", slot)
	}
}

func TestExpectedModulesForQuotaSatisfied(t *testing.T) {
	quota := map[string]int{TargetEffectElementalDamage: 4, TargetEffectAttackIncrease: 4}
	parts := make(map[string]partScan, len(equipmentParts))
	for _, part := range equipmentParts {
		var effects [maxSlot]string
		effects[0] = TargetEffectElementalDamage
		effects[1] = TargetEffectAttackIncrease
		parts[part] = partScanFromArrays(effects, [maxSlot]string{}, [maxSlot]SlotLock{})
	}
	if got := expectedModulesForQuota(parts, quota); got != 0 {
		t.Fatalf("satisfied quota should have 0 expected modules, got %f", got)
	}
}

// TestDecideQuotaByExpectedCostAcceptsSlot3 复现用户的 23:05 决策案例：
// 当前（维持）= [装弹@1, 优@2, 命中@3]，候选（放弃）= [暴击@1, 空@2, 装弹@3]。
// 期望成本应为"候选更低"（装弹@3 可 100% 锁定保值，装弹@1 每轮 88% 概率丢失），
// 因此应接受候选（B），而不是旧积分制下因"少一条有效"而放弃。
func TestDecideQuotaByExpectedCostAcceptsSlot3(t *testing.T) {
	quota := map[string]int{
		TargetEffectElementalDamage: 4,
		TargetEffectAttackIncrease:  4,
		"最大装弹数增加":                   4,
	}
	mk := func(effects [maxSlot]string) partScan {
		return partScanFromArrays(effects, [maxSlot]string{}, [maxSlot]SlotLock{})
	}
	// 其余三件：攻2 / 优1 / 装弹3，使本件的装弹与优均为"其余尚未满足"的承重效果。
	others := map[string]partScan{
		"臂部": mk([maxSlot]string{TargetEffectAttackIncrease, "", ""}),
		"身躯": mk([maxSlot]string{TargetEffectAttackIncrease, TargetEffectElementalDamage, ""}),
		"腿部": mk([maxSlot]string{"最大装弹数增加", "最大装弹数增加", "最大装弹数增加"}),
	}
	stateA := make(map[string]partScan, len(equipmentParts))
	for p, s := range others {
		stateA[p] = s
	}
	stateA["头部"] = mk([maxSlot]string{"最大装弹数增加", TargetEffectElementalDamage, "命中率增加"})
	stateB := make(map[string]partScan, len(equipmentParts))
	for p, s := range others {
		stateB[p] = s
	}
	stateB["头部"] = mk([maxSlot]string{"暴击率增加", "", "最大装弹数增加"})

	costA := expectedModulesForQuota(stateA, quota)
	costB := expectedModulesForQuota(stateB, quota)
	if costB >= costA {
		t.Fatalf("slot3 quota state should be cheaper than slot1 quota state, costA=%f costB=%f", costA, costB)
	}
	// 直接验证决策方向：当前=A（维持），候选=B（放弃）→ 应接受 B；反之应维持。
	if got := decideQuotaByExpectedCost(stateA, stateB, quota); got != ResultDecisionAccept {
		t.Fatalf("candidate (slot3 quota) should be accepted over current (slot1 quota), got %v", got)
	}
	if got := decideQuotaByExpectedCost(stateB, stateA, quota); got != ResultDecisionKeep {
		t.Fatalf("candidate (slot1 quota) should be kept when current is slot3 quota, got %v", got)
	}
}

func TestChooseBestPartForQuota(t *testing.T) {
	quota := map[string]int{TargetEffectElementalDamage: 4, TargetEffectAttackIncrease: 4}
	parts := make(map[string]partScan, len(equipmentParts))
	// 头部已满足当前配额，其余三件为空。
	for i, part := range equipmentParts {
		var effects [maxSlot]string
		if i == 0 {
			effects[0] = TargetEffectElementalDamage
			effects[1] = TargetEffectAttackIncrease
		}
		parts[part] = partScanFromArrays(effects, [maxSlot]string{}, [maxSlot]SlotLock{})
	}
	part, ok := chooseBestPartForQuota(parts, quota, equipmentParts)
	if !ok {
		t.Fatal("expected a part to be chosen")
	}
	if part == "头部" {
		t.Fatal("global lookahead should not choose already satisfied head")
	}
}

func TestDecideResultPageQuota(t *testing.T) {
	// 单件期望成本降级路径：候选补齐了"其余三件视为空上下文"下的必需效果，成本更低 → 接受。
	quota := map[string]int{TargetEffectElementalDamage: 4, TargetEffectAttackIncrease: 4}
	current := [maxSlot]string{TargetEffectElementalDamage, "", ""}
	better := [maxSlot]string{TargetEffectElementalDamage, TargetEffectAttackIncrease, ""}
	scan := partScanFromArrays(current, [maxSlot]string{}, [maxSlot]SlotLock{})
	if got := DecideResultPageQuota(current, better, scan, quota); got != ResultDecisionAccept {
		t.Fatalf("changed to complete needed quota should accept, got %v", got)
	}
	if got := DecideResultPageQuota(better, current, scan, quota); got != ResultDecisionKeep {
		t.Fatalf("changed away from complete quota should keep, got %v", got)
	}
}

func TestForbiddenEffectSatisfaction(t *testing.T) {
	quota := map[string]int{TargetEffectElementalDamage: 4, "最大装弹数增加": -1}
	parts := make(map[string]partScan, len(equipmentParts))
	for i, part := range equipmentParts {
		var effects [maxSlot]string
		effects[0] = TargetEffectElementalDamage
		if i == 0 {
			effects[1] = "最大装弹数增加"
		}
		parts[part] = partScanFromArrays(effects, [maxSlot]string{}, [maxSlot]SlotLock{})
	}

	if AllPartsSatisfiedQuota(parts, quota) {
		t.Fatal("state with forbidden max ammo should not be satisfied")
	}
	if !PartNeedsRerollQuota(parts, "头部", quota) {
		t.Fatal("head with forbidden max ammo should need reroll")
	}
}

func TestChooseBestPartForQuotaPrioritizesForbiddenEffect(t *testing.T) {
	quota := map[string]int{TargetEffectElementalDamage: 4, "防御力增加": -1}
	parts := make(map[string]partScan, len(equipmentParts))
	for i, part := range equipmentParts {
		effects := [maxSlot]string{TargetEffectElementalDamage, "攻击力增加", ""}
		if i == 0 {
			effects[1] = "防御力增加"
		}
		parts[part] = partScanFromArrays(effects, [maxSlot]string{}, [maxSlot]SlotLock{})
	}

	if AllPartsSatisfiedQuota(parts, quota) {
		t.Fatal("forbidden effect must keep the global state unsatisfied")
	}
	part, ok := chooseBestPartForQuota(parts, quota, equipmentParts)
	if !ok || part != "头部" {
		t.Fatalf("forbidden-effect part should be selected for reroll, got part=%q ok=%v", part, ok)
	}
}

func TestQuotaIsValidRejectsOutOfRangeCounts(t *testing.T) {
	if quotaIsValid(map[string]int{TargetEffectElementalDamage: 5}) {
		t.Fatal("per-effect quota above four must be rejected")
	}
	if quotaIsValid(map[string]int{TargetEffectElementalDamage: -2, "攻击力增加": 1}) {
		t.Fatal("negative quota other than -1 must be rejected")
	}
	if !quotaIsValid(map[string]int{TargetEffectElementalDamage: 4, "攻击力增加": -1}) {
		t.Fatal("valid quota should remain accepted")
	}
}

func TestExpectedModulesForPartRejectsImpossibleLockedState(t *testing.T) {
	quota := map[string]int{
		TargetEffectElementalDamage: 4,
		TargetEffectAttackIncrease:  4,
		"最大装弹数增加":                   4,
	}
	scan := partScanFromArrays(
		[maxSlot]string{"防御力增加", "暴击率增加", ""},
		[maxSlot]string{"防御力增加", "暴击率增加", ""},
		[maxSlot]SlotLock{LockOneTime, LockOneTime, LockNone},
	)
	cost := expectedModulesForPartAllocated(scan, quota, []string{TargetEffectElementalDamage, TargetEffectAttackIncrease}, 0)
	if cost != 1e9 {
		t.Fatalf("impossible locked state must return infinity, got %v", cost)
	}
}

func TestSolveLinearSystemRejectsSingularMatrix(t *testing.T) {
	_, ok := solveLinearSystem(
		[][]float64{{1, 1}, {2, 2}},
		[]float64{1, 2},
	)
	if ok {
		t.Fatal("singular matrix must be rejected")
	}
}

func TestForbiddenOnlyQuotaIsInvalid(t *testing.T) {
	quota := map[string]int{"最大装弹数增加": -1, TargetEffectAttackIncrease: -1}
	if quotaIsValid(quota) {
		t.Fatal("forbidden-only quota must be rejected because it has no positive target")
	}
	parts := make(map[string]partScan, len(equipmentParts))
	for _, part := range equipmentParts {
		parts[part] = partScanFromArrays([maxSlot]string{}, [maxSlot]string{}, [maxSlot]SlotLock{})
	}
	if AllPartsSatisfiedQuota(parts, quota) {
		t.Fatal("forbidden-only quota must not be treated as satisfied")
	}
}

// TestChooseBestPartForQuotaKeepsGoingWithNegativeGain 复现 4攻4优4装弹 的中间状态：
// 四件装备都已至少带 1 条配额词条（无锁定），全局配额仅 6/12 尚未满足。
// 此时单步重抽的期望收益全为负，但按策略文档 §4.1「只要还有尚未满足的正数配额，就继续洗」，
// chooseBestPartForQuota 仍必须选出部位继续，不能因单步增益为负返回 ""（旧代码 bestGainPerCost=-1
// 会把这种状态当作“无可洗部位”提前结束任务）。
func TestChooseBestPartForQuotaKeepsGoingWithNegativeGain(t *testing.T) {
	quota := map[string]int{
		TargetEffectElementalDamage: 4,
		TargetEffectAttackIncrease:  4,
		"最大装弹数增加":                   4,
	}
	parts := make(map[string]partScan, len(equipmentParts))
	for _, part := range equipmentParts {
		var effects [maxSlot]string
		switch part {
		case "头部":
			effects = [maxSlot]string{"最大装弹数增加", TargetEffectElementalDamage, "命中率增加"}
		case "臂部":
			effects = [maxSlot]string{"防御力增加", TargetEffectAttackIncrease, ""}
		case "身躯":
			effects = [maxSlot]string{"最大装弹数增加", "暴击伤害增加", TargetEffectAttackIncrease}
		case "腿部":
			effects = [maxSlot]string{TargetEffectElementalDamage, "命中率增加", "暴击率增加"}
		}
		parts[part] = partScanFromArrays(effects, [maxSlot]string{}, [maxSlot]SlotLock{})
	}

	if AllPartsSatisfiedQuota(parts, quota) {
		t.Fatal("6/12 quota state should not be satisfied")
	}
	part, ok := chooseBestPartForQuota(parts, quota, equipmentParts)
	if !ok || part == "" {
		t.Fatalf("choose must keep selecting a part while quota is unmet, got part=%q ok=%v", part, ok)
	}
}

// TestDesiredLockSlotForQuotaKeepsNeededQuotaEffect 复现 4攻4优4装弹 的中间状态：
// 全局 优2/攻2/装弹4 时，头部(装弹@1, 优@2, 命中@3) 的 2 号槽“优越代码伤害增加”仍被
// 全局需要（优未满 4），必须锁定 2 号以防重抽丢失；臂部(防御@1, 攻@2, 空@3) 同理锁 2 号。
//
// 回归：expectedModulesForPartAllocated 最终查表必须使用压缩状态（非配额词条如“命中率增加”被压成
// other），旧代码用原始词条名查表导致 key 不匹配取到 map 零值 0，DP 误判“期望成本 0”，
// DesiredLockSlotForQuota 从不建议锁定（23:03-23:05 运行全程 active_locks=0 的根因）。
//
// 本测试按“先苦后甜”策略更新：4/4/4 为饱和配额（本件负责 优1/攻1/装弹1），
// 头部(A@1 装弹/2 优/3 命中) 与 臂部(A@1 防御/2 攻/3 空) 的 3 号槽都尚未持有本件负责效果，
// 因此应先便宜刷 3 号（不锁），而不是先锁已有效的 2 号；但 DP 层仍应识别 2 号槽为可锁/仍在需要
// （防止 compressedCurrent 回归把 DP 期望成本判成 0、导致从不建议锁定）。
func TestDesiredLockSlotForQuotaKeepsNeededQuotaEffect(t *testing.T) {
	quota := map[string]int{
		TargetEffectElementalDamage: 4,
		TargetEffectAttackIncrease:  4,
		"最大装弹数增加":                   4,
	}
	mk := func(effects [maxSlot]string) partScan {
		return partScanFromArrays(effects, [maxSlot]string{}, [maxSlot]SlotLock{})
	}
	headParts := map[string]partScan{
		"头部": mk([maxSlot]string{"最大装弹数增加", TargetEffectElementalDamage, "命中率增加"}),
		"臂部": mk([maxSlot]string{"最大装弹数增加", "", TargetEffectElementalDamage}),
		"身躯": mk([maxSlot]string{"最大装弹数增加", "暴击伤害增加", TargetEffectAttackIncrease}),
		"腿部": mk([maxSlot]string{TargetEffectAttackIncrease, "最大装弹数增加", "蓄力速度增加"}),
	}
	// 先苦后甜（饱和）：3 号未持本件负责效果 → 便宜刷 3 号，不锁 2 号。
	if slot, ok := DesiredLockSlotForQuota(headParts, "头部", quota, ""); ok {
		t.Fatalf("saturated head should hunt slot3 (no lock) under bitter-sweet, got slot=%d", slot)
	}
	// DP 层回归：2 号槽“优越代码”仍被识别为可锁且仍需（非 0），证明 compressedCurrent 未回归。
	required := allocateQuotaRequired(headParts, quota)["头部"]
	if slot, _ := bestLockSlotAndCostForRequired(headParts["头部"], quota, required, ""); slot != 2 {
		t.Fatalf("DP should still see slot2 (优越) lockable-needed, got slot=%d", slot)
	}

	armsParts := map[string]partScan{
		"头部": mk([maxSlot]string{"最大装弹数增加", TargetEffectElementalDamage, "命中率增加"}),
		"臂部": mk([maxSlot]string{"防御力增加", TargetEffectAttackIncrease, ""}),
		"身躯": mk([maxSlot]string{"最大装弹数增加", "暴击伤害增加", TargetEffectAttackIncrease}),
		"腿部": mk([maxSlot]string{TargetEffectAttackIncrease, "最大装弹数增加", "蓄力速度增加"}),
	}
	// 先苦后甜（饱和）：臂部 3 号为 空，未持本件负责效果 → 便宜刷 3 号，不锁 2 号。
	if slot, ok := DesiredLockSlotForQuota(armsParts, "臂部", quota, ""); ok {
		t.Fatalf("saturated arms should hunt slot3 (no lock) under bitter-sweet, got slot=%d", slot)
	}
}

// TestDesiredLockSlotBitterSweet 直接验证“先苦后甜”锁定位：
//   - 3 号尚未持有本件负责效果 → 便宜刷 3 号（返回 0，不锁）；
//   - 3 号已持有本件负责效果、2 号未持 → 锁 3 号（先保护已拿到的难结果），再刷 2 号；
//   - 2/3 号都持有 → 锁最高难度的 3 号。
func TestDesiredLockSlotBitterSweet(t *testing.T) {
	quota := map[string]int{
		"优越代码伤害增加": 4, "攻击力增加": 4, "最大装弹数增加": 4,
	}
	required := []string{"优越代码伤害增加", "攻击力增加", "最大装弹数增加"}

	// 3 号=命中率(非负责效果) → 先刷 3 号。
	scan := partScanFromArrays([maxSlot]string{"攻击力增加", "最大装弹数增加", "命中率增加"}, [maxSlot]string{}, [maxSlot]SlotLock{})
	if slot := desiredLockSlotBitterSweet(scan, quota, required, ""); slot != 0 {
		t.Fatalf("want hunt slot3 (0), got %d", slot)
	}

	// 3 号=优越(负责)、2 号=命中率(非负责) → 锁 3 号保护已拿到难结果。
	scan2 := partScanFromArrays([maxSlot]string{"攻击力增加", "命中率增加", "优越代码伤害增加"}, [maxSlot]string{}, [maxSlot]SlotLock{})
	if slot := desiredLockSlotBitterSweet(scan2, quota, required, ""); slot != 3 {
		t.Fatalf("want lock slot3 (protect secured 优), got %d", slot)
	}

	// 2/3 号都持有负责效果，但本件仍缺 1 种（1 号为 空）→ 锁最高难度的 3 号。
	scan3 := partScanFromArrays([maxSlot]string{"", "最大装弹数增加", "优越代码伤害增加"}, [maxSlot]string{}, [maxSlot]SlotLock{})
	if slot := desiredLockSlotBitterSweet(scan3, quota, required, ""); slot != 3 {
		t.Fatalf("want lock slot3 (highest required), got %d", slot)
	}
}

// TestEnumerateSlotOutcomesExcludesLockedEffect 验证“锁定效果参与排除池”：
// 已锁定槽位所持效果占用该装备名额，未锁定槽位的结果中不得再出现同一种效果。
func TestEnumerateSlotOutcomesExcludesLockedEffect(t *testing.T) {
	unlocked := []int{0, 2} // 1、3 号
	lockedEffects := []string{"优越代码伤害增加"}
	outcomes := enumerateSlotOutcomes(unlocked, lockedEffects)
	for _, oc := range outcomes {
		for _, slot := range unlocked {
			if oc.effects[slot] == "优越代码伤害增加" {
				t.Fatalf("locked effect 优越 must not be drawn in unlocked slot %d (got %#v)", slot, oc.effects)
			}
		}
	}
	// 概率和应约为 1（枚举结果归一）。
	sum := 0.0
	for _, oc := range outcomes {
		sum += oc.prob
	}
	if math.Abs(sum-1.0) > 1e-9 {
		t.Fatalf("probability sum = %f, want 1", sum)
	}
}

// TestLockingRequiredEffectLowersExpectedCost 验证 DP 在“锁定需求效果”下的期望成本更低：
// 锁定的需求效果占位并排除，未锁定槽位不会再把资源浪费在重复抽该效果上，因此期望更省。
func TestLockingRequiredEffectLowersExpectedCost(t *testing.T) {
	quota := map[string]int{
		"优越代码伤害增加": 4, "攻击力增加": 4, "最大装弹数增加": 4,
	}
	required := []string{"优越代码伤害增加", "攻击力增加", "最大装弹数增加"}
	// 未锁：slot2=攻 未锁定（缺 优）。
	unlocked := partScanFromArrays([maxSlot]string{"最大装弹数增加", "攻击力增加", "蓄力速度增加"}, [maxSlot]string{}, [maxSlot]SlotLock{})
	costU := expectedModulesForPartAllocated(unlocked, quota, required, 0)
	// 锁：slot2=攻 永久锁。
	locked := partScanFromArrays([maxSlot]string{"最大装弹数增加", "攻击力增加", "蓄力速度增加"}, [maxSlot]string{}, [maxSlot]SlotLock{LockNone, LockPermanent, LockNone})
	costL := expectedModulesForPartAllocated(locked, quota, required, 0)
	if costL >= costU {
		t.Fatalf("locking required 攻 should lower expected cost, locked=%.4f unlocked=%.4f", costL, costU)
	}
}

// TestBestLockSlotAndCostForRequiredMaterialAware 验证“锁定获取成本”按材料计入决策：
//   - 槽位判断（锁不锁/锁哪个）与材料无关（同一槽位，获取成本对所有候选取等价）；
//   - 用“订制模块”（永久蓝锁，一次性扣模块）时，期望成本 = 密钥版 + 获取成本（0→1=2 模块），
//     使模块锁定时决策更保守；用“自订密钥”时获取成本为 0。
//
// 视角：自订密钥耗尽、脚本回退用订制模组锁定时，锁定决策会据此把“买锁的模块”计入真实成本。
func TestBestLockSlotAndCostForRequiredMaterialAware(t *testing.T) {
	quota := map[string]int{
		TargetEffectElementalDamage: 4,
		TargetEffectAttackIncrease:  4,
		"最大装弹数增加":                   4,
	}
	parts := map[string]partScan{
		"头部": partScanFromArrays([maxSlot]string{"最大装弹数增加", TargetEffectElementalDamage, "命中率增加"}, [maxSlot]string{}, [maxSlot]SlotLock{}),
		"臂部": partScanFromArrays([maxSlot]string{"最大装弹数增加", "", TargetEffectElementalDamage}, [maxSlot]string{}, [maxSlot]SlotLock{}),
		"身躯": partScanFromArrays([maxSlot]string{"最大装弹数增加", "暴击伤害增加", TargetEffectAttackIncrease}, [maxSlot]string{}, [maxSlot]SlotLock{}),
		"腿部": partScanFromArrays([maxSlot]string{TargetEffectAttackIncrease, "最大装弹数增加", "蓄力速度增加"}, [maxSlot]string{}, [maxSlot]SlotLock{}),
	}
	scan := parts["头部"]
	required := allocateQuotaRequired(parts, quota)["头部"]

	slotKey, costKey := bestLockSlotAndCostForRequired(scan, quota, required, "自订密钥")
	slotMod, costMod := bestLockSlotAndCostForRequired(scan, quota, required, "订制模块")

	if slotKey == 0 {
		t.Fatalf("expected a lock slot in this state under key material, got 0")
	}
	if slotKey != slotMod {
		t.Fatalf("lock slot should not depend on material, key=%d mod=%d", slotKey, slotMod)
	}
	// 该件当前 0 锁 → 模块版应比密钥版多出 0→1 的获取成本(2 模块)。
	delta := costMod - costKey
	if delta < 1.99 || delta > 2.01 {
		t.Fatalf("module should add ~2 acquisition cost, key=%.4f mod=%.4f delta=%.4f", costKey, costMod, delta)
	}
}

func TestDesiredLockPlanUsesFallbackMaterial(t *testing.T) {
	quota := map[string]int{
		TargetEffectElementalDamage: 4,
		TargetEffectAttackIncrease:  4,
	}
	parts := map[string]partScan{
		"头部": partScanFromArrays([maxSlot]string{"命中率增加", TargetEffectElementalDamage, ""}, [maxSlot]string{}, [maxSlot]SlotLock{}),
		"臂部": partScanFromArrays([maxSlot]string{TargetEffectAttackIncrease, "", ""}, [maxSlot]string{}, [maxSlot]SlotLock{}),
		"身躯": partScanFromArrays([maxSlot]string{TargetEffectAttackIncrease, "", ""}, [maxSlot]string{}, [maxSlot]SlotLock{}),
		"腿部": partScanFromArrays([maxSlot]string{TargetEffectElementalDamage, "", ""}, [maxSlot]string{}, [maxSlot]SlotLock{}),
	}
	slot, material, ok := desiredLockPlanForInventory(parts, "头部", quota, Inventory{CustomModules: 5}, "自订密钥")
	if !ok || material != "订制模块" || slot == 0 {
		t.Fatalf("unaffordable key should produce an affordable module lock plan, slot=%d material=%q ok=%v", slot, material, ok)
	}
}

// TestExpectedModulesForQuotaAllocAware 验证“宏观分配感知”修复：
// 对“四件均 优@1/攻@2/蓄力@3，全局 优4/攻4/装弹0”的状态：
//   - 配额 4/4/1 的期望剩余成本必须显著低于 4/4/4（旧口径两者都按“每件都要补装弹”，同为 988.76）；
//   - 分配把“装弹1”只指派给 1 件（其余件只需 优/攻），避免稀缺配额被每件都要求。
func TestExpectedModulesForQuotaAllocAware(t *testing.T) {
	quota44 := map[string]int{TargetEffectElementalDamage: 4, TargetEffectAttackIncrease: 4, "最大装弹数增加": 4}
	quota41 := map[string]int{TargetEffectElementalDamage: 4, TargetEffectAttackIncrease: 4, "最大装弹数增加": 1}
	st1 := map[string]partScan{
		"头部": partScanFromArrays([maxSlot]string{TargetEffectElementalDamage, TargetEffectAttackIncrease, "蓄力速度增加"}, [maxSlot]string{}, [maxSlot]SlotLock{}),
		"臂部": partScanFromArrays([maxSlot]string{TargetEffectElementalDamage, TargetEffectAttackIncrease, "蓄力速度增加"}, [maxSlot]string{}, [maxSlot]SlotLock{}),
		"身躯": partScanFromArrays([maxSlot]string{TargetEffectElementalDamage, TargetEffectAttackIncrease, "蓄力速度增加"}, [maxSlot]string{}, [maxSlot]SlotLock{}),
		"腿部": partScanFromArrays([maxSlot]string{TargetEffectElementalDamage, TargetEffectAttackIncrease, "蓄力速度增加"}, [maxSlot]string{}, [maxSlot]SlotLock{}),
	}

	cost44 := expectedModulesForQuota(st1, quota44)
	cost41 := expectedModulesForQuota(st1, quota41)
	if cost44 <= 0 {
		t.Fatalf("4/4/4 should not be satisfied, got cost=%f", cost44)
	}
	if cost41 >= cost44 {
		t.Fatalf("sparse quota 4/4/1 should be strictly cheaper than 4/4/4, got cost41=%f cost44=%f", cost41, cost44)
	}
	if cost41 > cost44*0.5 {
		t.Fatalf("sparse quota should be much cheaper, cost41=%f cost44=%f", cost41, cost44)
	}

	// 分配：装弹1 只分给 1 件；其余件只需 优/攻。
	assigned := allocateQuotaRequired(st1, quota41)
	ammoHolders := 0
	for _, part := range equipmentParts {
		for _, e := range assigned[part] {
			if e == "最大装弹数增加" {
				ammoHolders++
			}
		}
	}
	if ammoHolders != 1 {
		t.Fatalf("4/4/1 should assign ammo to exactly 1 part, got %d (%v)", ammoHolders, assigned)
	}
}

// TestChooseBestPartPriorityFindCarrier 验证“先找承载者”调度：无 slot3 命中（无天然承载者）时，
// chooseBestPartForQuota 应从“未完整非承载者”（降格者 + 未定者）中择优寻找承载者——降格者也可通过
// 洗 slot3 翻盘成承载者，因此不被排除。
func TestChooseBestPartPriorityFindCarrier(t *testing.T) {
	quota := map[string]int{"优越代码伤害增加": 4, "攻击力增加": 4, "最大装弹数增加": 1}
	mk := func(e [maxSlot]string) partScan {
		return partScanFromArrays(e, [maxSlot]string{}, [maxSlot]SlotLock{})
	}
	// 头部/腿部 slot2=攻 → 降格者；臂部/身躯 slot2=蓄力/暴击 → 未定；均无 slot3 命中。
	parts := map[string]partScan{
		"头部": mk([maxSlot]string{"优越代码伤害增加", "攻击力增加", "命中率增加"}),
		"臂部": mk([maxSlot]string{"优越代码伤害增加", "蓄力速度增加", ""}),
		"身躯": mk([maxSlot]string{"优越代码伤害增加", "暴击率增加", ""}),
		"腿部": mk([maxSlot]string{"优越代码伤害增加", "攻击力增加", ""}),
	}
	if hasNaturalCarrier(parts, quota) {
		t.Fatal("test setup should have no natural carrier")
	}
	assigned := allocateQuotaRequired(parts, quota)
	part, ok := chooseBestPartForQuota(parts, quota, equipmentParts)
	if !ok {
		t.Fatal("expected a part to be chosen")
	}
	// 选出的应是未完整非承载者（降格者或未定者，而非承载者——本状态本无承载者）。
	if part == "" {
		t.Fatal("empty part")
	}
	if scan, ok2 := parts[part]; ok2 {
		if pieceCarrierRole(scan, quota) == "carrier" || partHasAllAssigned(scan, assigned[part]) {
			t.Fatalf("picked %q should be an incomplete non-carrier, got role=%s complete=%v", part, pieceCarrierRole(scan, quota), partHasAllAssigned(scan, assigned[part]))
		}
	}
}

// TestChooseBestPartPriorityExploreNonCarrier 验证“有承载者后先探索未完整非承载者（降格者+未定者）”：
// 承载者已完整(腿部 优+攻+装弹)时，chooseBestPartForQuota 应从“未完整非承载者”中择优（降格者或未定者），
// 而不洗已完整的承载者。
func TestChooseBestPartPriorityExploreNonCarrier(t *testing.T) {
	quota := map[string]int{"优越代码伤害增加": 4, "攻击力增加": 4, "最大装弹数增加": 1}
	mk := func(e [maxSlot]string) partScan {
		return partScanFromArrays(e, [maxSlot]string{}, [maxSlot]SlotLock{})
	}
	// 腿部=承载者且已完整；头部/躯干=未完整降格者；臂部=未定者(未完整)。
	parts := map[string]partScan{
		"头部": mk([maxSlot]string{"蓄力伤害增加", "优越代码伤害增加", "命中率增加"}),
		"臂部": mk([maxSlot]string{"优越代码伤害增加", "蓄力速度增加", ""}),
		"身躯": mk([maxSlot]string{"暴击伤害增加", "攻击力增加", ""}),
		"腿部": mk([maxSlot]string{"优越代码伤害增加", "攻击力增加", "最大装弹数增加"}),
	}
	if !hasNaturalCarrier(parts, quota) {
		t.Fatal("test setup should have a natural carrier (腿部 slot3=装弹)")
	}
	part, ok := chooseBestPartForQuota(parts, quota, equipmentParts)
	if !ok {
		t.Fatal("expected a part to be chosen")
	}
	if part == "腿部" {
		t.Fatalf("should explore an incomplete non-carrier piece, not the complete carrier 腿部")
	}
	// 选出的应是未完整非承载者。
	if scan, ok2 := parts[part]; ok2 {
		assigned := allocateQuotaRequired(parts, quota)
		if pieceCarrierRole(scan, quota) == "carrier" || partHasAllAssigned(scan, assigned[part]) {
			t.Fatalf("picked %q should be an incomplete non-carrier, got role=%s complete=%v", part, pieceCarrierRole(scan, quota), partHasAllAssigned(scan, assigned[part]))
		}
	}
}

// TestAllocateMultipleScarceDistinct 验证 4/4/1/1/1/1（多个稀缺效果）时，各稀缺效果分派给“不同”件。
func TestAllocateMultipleScarceDistinct(t *testing.T) {
	quota := map[string]int{
		"优越代码伤害增加": 4, "攻击力增加": 4,
		"最大装弹数增加": 1, "蓄力速度增加": 1, "暴击率增加": 1, "暴击伤害增加": 1,
	}
	mk := func(e [maxSlot]string) partScan {
		return partScanFromArrays(e, [maxSlot]string{}, [maxSlot]SlotLock{})
	}
	// 各件 优@1；其余空。
	parts := map[string]partScan{
		"头部": mk([maxSlot]string{"优越代码伤害增加", "", ""}),
		"臂部": mk([maxSlot]string{"优越代码伤害增加", "", ""}),
		"身躯": mk([maxSlot]string{"优越代码伤害增加", "", ""}),
		"腿部": mk([maxSlot]string{"优越代码伤害增加", "", ""}),
	}
	assigned := allocateQuotaRequired(parts, quota)
	scarce := []string{"最大装弹数增加", "蓄力速度增加", "暴击率增加", "暴击伤害增加"}
	count := map[string]int{}
	carrierOf := map[string]string{}
	for _, p := range equipmentParts {
		for _, e := range assigned[p] {
			for _, s := range scarce {
				if e == s {
					count[p]++
					carrierOf[s] = p
				}
			}
		}
	}
	for _, p := range equipmentParts {
		if count[p] > 1 {
			t.Fatalf("%s carries %d scarce effects, want <=1 (got %v)", p, count[p], assigned[p])
		}
	}
	for _, s := range scarce {
		if carrierOf[s] == "" {
			t.Fatalf("scarce effect %q has no carrier", s)
		}
	}
}

// TestAllocateMultipleScarceCapacity 验证“每件承载稀缺容量 = maxSlot − 满配额数”：
// 优4(满,占1槽) + 装弹1 + 暴击率1(稀缺) 时容量=2，允许同一件承载多个稀缺，且每件不超 3 槽。
func TestAllocateMultipleScarceCapacity(t *testing.T) {
	quota := map[string]int{"优越代码伤害增加": 4, "最大装弹数增加": 1, "暴击率增加": 1}
	mk := func(e [maxSlot]string) partScan {
		return partScanFromArrays(e, [maxSlot]string{}, [maxSlot]SlotLock{})
	}
	parts := map[string]partScan{
		"头部": mk([maxSlot]string{"优越代码伤害增加", "", ""}),
		"臂部": mk([maxSlot]string{"优越代码伤害增加", "", ""}),
		"身躯": mk([maxSlot]string{"优越代码伤害增加", "", ""}),
		"腿部": mk([maxSlot]string{"优越代码伤害增加", "", ""}),
	}
	assigned := allocateQuotaRequired(parts, quota)
	// 每件最多 3 条（满配额1 + 稀缺≤2）。
	hasAmmo, hasCrit := false, false
	for _, p := range equipmentParts {
		if len(assigned[p]) > 3 {
			t.Fatalf("%s exceeded 3 effects: %v", p, assigned[p])
		}
		for _, e := range assigned[p] {
			if e == "最大装弹数增加" {
				hasAmmo = true
			}
			if e == "暴击率增加" {
				hasCrit = true
			}
		}
	}
	if !hasAmmo || !hasCrit {
		t.Fatalf("both scarce effects must be assigned; assigned=%v", assigned)
	}
	// 满配额优应分给全部件。
	for _, p := range equipmentParts {
		found := false
		for _, e := range assigned[p] {
			if e == "优越代码伤害增加" {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s missing full-effect 优越", p)
		}
	}
}

// TestCarrierCostSwap 验证“显式交换/冒泡判定”：pickCheapestCarriersForEffect 按“补齐承载者
// required 的期望成本最低”选择承载者——成本更低者晋升为承载者、原承载者退行。
// 注：slot3 已命中（难出的 3 号槽已填）通常使该件成本最低，故 slot3 命中者常被选中；但选择
// 完全由成本决定，若某件更接近完整（成本更低）则会与其交换。
func TestCarrierCostSwap(t *testing.T) {
	quota := map[string]int{"优越代码伤害增加": 4, "攻击力增加": 4, "最大装弹数增加": 1}
	mk := func(e [maxSlot]string) partScan {
		return partScanFromArrays(e, [maxSlot]string{}, [maxSlot]SlotLock{})
	}
	// 头部 slot3=装弹（承载者候选，成本最低）；腿部 优+攻（只差装弹）；臂部/身躯 空（成本极高）。
	parts := map[string]partScan{
		"头部": mk([maxSlot]string{"防御力增加", "命中率增加", "最大装弹数增加"}),
		"臂部": mk([maxSlot]string{"", "", ""}),
		"身躯": mk([maxSlot]string{"", "", ""}),
		"腿部": mk([maxSlot]string{"优越代码伤害增加", "攻击力增加", "蓄力伤害增加"}),
	}
	// 满配额 优/攻 分给全部件后的 assigned（调用前需构造）。
	assigned := map[string][]string{}
	for _, p := range equipmentParts {
		assigned[p] = []string{"优越代码伤害增加", "攻击力增加"}
	}
	// 成本最低的承载者（用 DP 比较各件承载装弹的期望成本）。
	cheapest := ""
	bestCost := 1e18
	for _, p := range equipmentParts {
		req := append(append([]string{}, assigned[p]...), "最大装弹数增加")
		c := partExpectedCostForRequired(parts[p], quota, req, "")
		if c < bestCost {
			bestCost = c
			cheapest = p
		}
	}
	carriers := pickCheapestCarriersForEffect(parts, quota, assigned, "最大装弹数增加", 1, map[string]int{}, 1)
	if len(carriers) != 1 || carriers[0] != cheapest {
		t.Fatalf("pickCheapestCarriersForEffect should pick the lowest-cost carrier (%s, cost=%.2f), got %v", cheapest, bestCost, carriers)
	}
	// 直接验证 allocateQuotaRequired 也使用该成本择优（装弹承载者是最低成本件）。
	alloc := allocateQuotaRequired(parts, quota)
	carrier := ""
	for _, p := range equipmentParts {
		for _, e := range alloc[p] {
			if e == "最大装弹数增加" {
				carrier = p
			}
		}
	}
	if carrier != cheapest {
		t.Fatalf("allocateQuotaRequired carrier should be lowest-cost (%s), got %s", cheapest, carrier)
	}
}

// TestFrameworkArbitraryQuota 验证“角色阶梯 + 成本交换 + 调度”框架对任意配额的适配，
// 覆盖：满配额+多稀缺、无满配额（仅稀缺）、饱和（全满配额）三种结构。
func TestFrameworkArbitraryQuota(t *testing.T) {
	mk := func(e [maxSlot]string) partScan {
		return partScanFromArrays(e, [maxSlot]string{}, [maxSlot]SlotLock{})
	}
	isMissingEffect := func(p partScan, e string) bool { return !partHasEffect(p, e) }

	// Case A：优4/蓄力速度4/暴击率2（满：优、蓄力速度；稀缺：暴击率2）。
	quotaA := map[string]int{"优越代码伤害增加": 4, "蓄力速度增加": 4, "暴击率增加": 2}
	partsA := map[string]partScan{
		"头部": mk([maxSlot]string{"蓄力速度增加", "", "优越代码伤害增加"}), // slot3=优 → carrier
		"臂部": mk([maxSlot]string{"", "蓄力速度增加", ""}),         // slot2=蓄力速度(满) → demoted
		"身躯": mk([maxSlot]string{"优越代码伤害增加", "", ""}),
		"腿部": mk([maxSlot]string{"", "", ""}),
	}
	if pieceCarrierRole(partsA["头部"], quotaA) != "carrier" {
		t.Fatalf("头部 should be carrier (slot3=优), got %s", pieceCarrierRole(partsA["头部"], quotaA))
	}
	if pieceCarrierRole(partsA["臂部"], quotaA) != "demoted" {
		t.Fatalf("臂部 should be demoted (slot2=蓄力速度 full effect), got %s", pieceCarrierRole(partsA["臂部"], quotaA))
	}
	assignedA := allocateQuotaRequired(partsA, quotaA)
	critCount := 0
	for _, p := range equipmentParts {
		if len(assignedA[p]) > 3 {
			t.Fatalf("%s exceeds 3 effects: %v", p, assignedA[p])
		}
		for _, e := range assignedA[p] {
			if e == "暴击率增加" {
				critCount++
			}
		}
	}
	if critCount != 2 {
		t.Fatalf("暴击率2 should have exactly 2 carriers, got %d", critCount)
	}
	if _, ok := chooseBestPartForQuota(partsA, quotaA, equipmentParts); !ok {
		t.Fatal("chooseBestPartForQuota should return a part for quotaA")
	}

	// Case B：仅稀缺、无满配额（容量=3，单件可承载多个稀缺）。
	quotaB := map[string]int{"蓄力伤害增加": 1, "暴击率增加": 1, "防御力增加": 1}
	partsB := map[string]partScan{
		"头部": mk([maxSlot]string{"", "", ""}),
		"臂部": mk([maxSlot]string{"", "", ""}),
		"身躯": mk([maxSlot]string{"", "", ""}),
		"腿部": mk([maxSlot]string{"", "", ""}),
	}
	assignedB := allocateQuotaRequired(partsB, quotaB)
	for _, e := range []string{"蓄力伤害增加", "暴击率增加", "防御力增加"} {
		found := false
		for _, p := range equipmentParts {
			for _, a := range assignedB[p] {
				if a == e {
					found = true
				}
			}
		}
		if !found {
			t.Fatalf("scarce %q not assigned in quotaB: %v", e, assignedB)
		}
	}
	for _, p := range equipmentParts {
		if len(assignedB[p]) > 3 {
			t.Fatalf("%s exceeds 3 effects in quotaB: %v", p, assignedB[p])
		}
	}
	if _, ok := chooseBestPartForQuota(partsB, quotaB, equipmentParts); !ok {
		t.Fatal("chooseBestPartForQuota should return a part for quotaB")
	}

	// Case C：饱和（优4/攻4/装弹4），每件都应 优+攻+装弹。
	quotaC := map[string]int{"优越代码伤害增加": 4, "攻击力增加": 4, "最大装弹数增加": 4}
	partsC := map[string]partScan{
		"头部": mk([maxSlot]string{"", "", ""}),
		"臂部": mk([maxSlot]string{"", "", ""}),
		"身躯": mk([maxSlot]string{"", "", ""}),
		"腿部": mk([maxSlot]string{"", "", ""}),
	}
	assignedC := allocateQuotaRequired(partsC, quotaC)
	for _, p := range equipmentParts {
		have := map[string]bool{}
		for _, e := range assignedC[p] {
			have[e] = true
		}
		for _, e := range []string{"优越代码伤害增加", "攻击力增加", "最大装弹数增加"} {
			if !have[e] {
				t.Fatalf("%s missing full effect %q in saturated quota: %v", p, e, assignedC[p])
			}
		}
	}
	_ = isMissingEffect
}

// TestWashPriorityArmOverLeg 验证“顺位排序”由 位置+数量+权重 决定：
// 臂部（有效词条少、缺攻、蓄力@1 废槽）的 washPriority 应高于 腿部（已完成 优/攻 双词条件）。
// 在“身S 已成天然承载者、腿/臂/头同为降格者、且带锁+库存”的复现场景下，chooseBestPartForQuota
// 应优先洗 臂部，而不是 腿部（已完成降格者）。
func TestWashPriorityArmOverLeg(t *testing.T) {
	quota := map[string]int{"优越代码伤害增加": 4, "攻击力增加": 4, "最大装弹数增加": 2}
	mk := func(e [maxSlot]string) partScan {
		return partScanFromArrays(e, [maxSlot]string{}, [maxSlot]SlotLock{})
	}
	mkLocked := func(e [maxSlot]string, lockSlot int) partScan {
		var locks [maxSlot]SlotLock
		locks[lockSlot] = SlotLock(1)
		return partScanFromArrays(e, [maxSlot]string{}, locks)
	}
	parts := map[string]partScan{
		"头部": mk([maxSlot]string{"优越代码伤害增加", "攻击力增加", "命中率增加"}),
		"臂部": mk([maxSlot]string{"蓄力速度增加", "优越代码伤害增加", ""}),
		"身躯": mkLocked([maxSlot]string{"命中率增加", "攻击力增加", "最大装弹数增加"}, 1), // slot2=攻 已锁
		"腿部": mk([maxSlot]string{"优越代码伤害增加", "攻击力增加", ""}),
	}
	assigned := allocateQuotaRequired(parts, quota)
	wpArm := washPriority(parts["臂部"], quota, assigned["臂部"])
	wpLeg := washPriority(parts["腿部"], quota, assigned["腿部"])
	if wpArm <= wpLeg {
		t.Fatalf("washPriority(臂部)=%.2f should be > washPriority(腿部)=%.2f (臂部更该洗)", wpArm, wpLeg)
	}
	part, ok := chooseBestPartForQuota(parts, quota, equipmentParts)
	if !ok {
		t.Fatal("expected a part to be chosen")
	}
	if part == "腿部" {
		t.Fatalf("should prefer 臂部 (more behind) over 腿部 (complete 降格者), got 腿部")
	}
	// 物理有效词条数：臂部 < 腿部。
	effArm, effLeg := 0, 0
	for _, e := range parts["臂部"].Effects() {
		if slotIsPositiveQuota(e, quota) {
			effArm++
		}
	}
	for _, e := range parts["腿部"].Effects() {
		if slotIsPositiveQuota(e, quota) {
			effLeg++
		}
	}
	if effArm >= effLeg {
		t.Fatalf("arm should have fewer effective affixes than leg (arm=%d leg=%d)", effArm, effLeg)
	}
}

// TestShortTermOptimalAcceptsCountUp 验证“短期最优”：宏观成本持平（未变差）时，
// 洗词条后若物理有效词条数上升，即使该效果是超额/临时（下次会被洗掉）也应接受（ResultDecisionAccept）；
// 有效词条数不变则维持（Keep）。
func TestShortTermOptimalAcceptsCountUp(t *testing.T) {
	quota := map[string]int{"优越代码伤害增加": 4, "攻击力增加": 4, "最大装弹数增加": 1}
	mk := func(e [maxSlot]string) partScan {
		return partScanFromArrays(e, [maxSlot]string{}, [maxSlot]SlotLock{})
	}
	// 装弹配额已由 身躯 满足（身部 装弹@1）。头部是 优+攻 完整双词条件。
	others := map[string]partScan{
		"臂部": mk([maxSlot]string{"优越代码伤害增加", "攻击力增加", ""}),
		"身躯": mk([maxSlot]string{"最大装弹数增加", "攻击力增加", ""}),
		"腿部": mk([maxSlot]string{"优越代码伤害增加", "攻击力增加", ""}),
	}
	cur := make(map[string]partScan, len(equipmentParts))
	for p, s := range others {
		cur[p] = s
	}
	cur["头部"] = mk([maxSlot]string{"优越代码伤害增加", "攻击力增加", "命中率增加"}) // 2 有效(优/攻), 装弹超额?

	// 候选 A：头部 洗出 装弹@3（超额装弹，有效词条 2→3），但 装弹 配额已由身部满足 → 宏观成本持平。
	cand := make(map[string]partScan, len(equipmentParts))
	for p, s := range others {
		cand[p] = s
	}
	cand["头部"] = mk([maxSlot]string{"优越代码伤害增加", "攻击力增加", "最大装弹数增加"})

	// 候选 B：头部 洗出 非配额 蓄力 @3（有效词条数不变），与当前宏观持平。
	candB := make(map[string]partScan, len(equipmentParts))
	for p, s := range others {
		candB[p] = s
	}
	candB["头部"] = mk([maxSlot]string{"优越代码伤害增加", "攻击力增加", "蓄力速度增加"})

	curEff := globalEffectiveAffixCount(cur, quota)
	candEff := globalEffectiveAffixCount(cand, quota)
	candBEff := globalEffectiveAffixCount(candB, quota)
	if candEff != curEff+1 {
		t.Fatalf("candidate A should add exactly 1 effective affix (cur=%d candA=%d)", curEff, candEff)
	}
	if candBEff != curEff {
		t.Fatalf("candidate B should keep effective count (cur=%d candB=%d)", curEff, candBEff)
	}
	if got := decideQuotaByExpectedCost(cur, cand, quota); got != ResultDecisionAccept {
		t.Fatalf("count-up (cur=%d -> cand=%d) should SHORT-TERM accept, got %v", curEff, candEff, got)
	}
	if got := decideQuotaByExpectedCost(cur, candB, quota); got != ResultDecisionKeep {
		t.Fatalf("same count should Keep, got %v", got)
	}
}
