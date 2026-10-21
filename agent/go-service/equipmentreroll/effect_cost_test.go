package equipmentreroll

import (
	"math"
	"testing"
)

func TestDynamicEffectPlanAdaptiveLocks(t *testing.T) {
	tests := []struct {
		name     string
		effects  [maxSlot]string
		required []string
		wantSlot int
		wantCost float64
	}{
		{
			name:     "two effects from no protected progress",
			effects:  [maxSlot]string{"防御力增加", "", ""},
			required: []string{TargetEffectElementalDamage, TargetEffectAttackIncrease},
			wantCost: 19.348965465767584,
		},
		{
			name:     "keep useful slot two for two effects",
			effects:  [maxSlot]string{"命中率增加", TargetEffectElementalDamage, "暴击率增加"},
			required: []string{TargetEffectElementalDamage, TargetEffectAttackIncrease},
			wantSlot: 2,
			wantCost: 15.784977908689221,
		},
		{
			name:     "three effects include future locks",
			effects:  [maxSlot]string{TargetEffectAttackIncrease, "", ""},
			required: []string{TargetEffectElementalDamage, TargetEffectAttackIncrease, "最大装弹数增加"},
			wantCost: 49.01222815405522,
		},
		{
			name:     "three effects do not prematurely lock slot two",
			effects:  [maxSlot]string{"命中率增加", TargetEffectElementalDamage, "暴击率增加"},
			required: []string{TargetEffectElementalDamage, TargetEffectAttackIncrease, "最大装弹数增加"},
			wantCost: 49.01222815405522,
		},
		{
			name:     "both useful lockable slots are priced",
			effects:  [maxSlot]string{"防御力增加", "最大装弹数增加", TargetEffectElementalDamage},
			required: []string{TargetEffectElementalDamage, TargetEffectAttackIncrease, "最大装弹数增加"},
			wantSlot: 3,
			wantCost: 28.4,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			scan := partScanFromArrays(test.effects, [maxSlot]string{}, [maxSlot]SlotLock{})
			slot, cost := dynamicEffectPlan(scan, test.required, nil, nil, "订制模块")
			if slot != test.wantSlot || math.Abs(cost-test.wantCost) > 1e-7 {
				t.Fatalf("plan = (%d, %.10f), want (%d, %.10f)", slot, cost, test.wantSlot, test.wantCost)
			}
		})
	}
}

func TestDynamicEffectPlanLockAcquisitionMatchesContinuation(t *testing.T) {
	scan := partScanFromArrays(
		[maxSlot]string{"防御力增加", "最大装弹数增加", TargetEffectElementalDamage},
		[maxSlot]string{}, [maxSlot]SlotLock{},
	)
	required := []string{TargetEffectElementalDamage, TargetEffectAttackIncrease, "最大装弹数增加"}
	for acquired := 0; acquired < 2; acquired++ {
		slot, cost := dynamicEffectPlan(scan, required, nil, nil, "订制模块")
		if slot != 3-acquired {
			t.Fatalf("step %d lock = %d", acquired, slot)
		}
		scan.Slots[slot-1].Lock = LockPermanent
		_, continuation := dynamicEffectPlan(scan, required, nil, nil, "订制模块")
		if math.Abs(cost-float64(lockAcquireCost(acquired))-continuation) > 1e-7 {
			t.Fatalf("step %d cost %.8f does not include lock and continuation %.8f", acquired, cost, continuation)
		}
	}
	if slot, cost := dynamicEffectPlan(scan, required, nil, nil, "订制模块"); slot != 0 || math.Abs(cost-23.4) > 1e-7 {
		t.Fatalf("two permanent locks = (%d, %.10f), want (0, 23.4)", slot, cost)
	}
}

func TestDynamicEffectPlanForbiddenAndSlotConstraints(t *testing.T) {
	required := []string{TargetEffectAttackIncrease}
	allow := map[string]map[int]bool{TargetEffectAttackIncrease: {2: true}}
	scan := partScanFromArrays([maxSlot]string{"防御力增加", TargetEffectAttackIncrease, ""}, [maxSlot]string{}, [maxSlot]SlotLock{})
	if slot, cost := dynamicEffectPlan(scan, required, nil, allow, "订制模块"); slot != 0 || cost <= 0 || cost >= costUnreachable {
		t.Fatalf("unlocked wrong-position effect = (%d, %.8f)", slot, cost)
	}
	scan.Slots[1].Lock = LockPermanent
	if _, cost := dynamicEffectPlan(scan, required, nil, allow, "订制模块"); cost != costUnreachable {
		t.Fatalf("permanent effect in forbidden position = %.8f", cost)
	}
	scan = partScanFromArrays([maxSlot]string{"命中率增加", "防御力增加", ""}, [maxSlot]string{}, [maxSlot]SlotLock{LockNone, LockPermanent, LockNone})
	if _, cost := dynamicEffectPlan(scan, required, []string{"防御力增加"}, nil, "订制模块"); cost != costUnreachable {
		t.Fatalf("forbidden permanent effect = %.8f", cost)
	}
	scan.Slots[1].Lock = LockNone
	if _, cost := dynamicEffectPlan(scan, required, []string{"防御力增加"}, nil, "订制模块"); cost <= 0 || cost >= costUnreachable {
		t.Fatalf("removable forbidden effect = %.8f", cost)
	}
}

func TestDynamicEffectPlanOneTimeLocksExpire(t *testing.T) {
	required := []string{TargetEffectAttackIncrease}
	allow := map[string]map[int]bool{TargetEffectAttackIncrease: {1: true}}
	scan := partScanFromArrays(
		[maxSlot]string{"命中率增加", "防御力增加", ""},
		[maxSlot]string{}, [maxSlot]SlotLock{LockNone, LockOneTime, LockNone},
	)
	// 一次性错槽锁到本轮结束就失效，后续仍能把攻击洗到第 2 槽。
	if _, cost := dynamicEffectPlan(scan, required, nil, allow, "订制模块"); cost <= 0 || cost >= costUnreachable {
		t.Fatalf("temporary unrelated lock must not make the target unreachable: %.8f", cost)
	}
	if _, cost := dynamicEffectPlan(scan, required, []string{"防御力增加"}, allow, "订制模块"); cost <= 0 || cost >= costUnreachable {
		t.Fatalf("temporary forbidden lock can be removed after this roll: %.8f", cost)
	}
	scan.Slots[1].Lock = LockPermanent
	if _, cost := dynamicEffectPlan(scan, required, nil, allow, "订制模块"); cost != costUnreachable {
		t.Fatalf("permanent unrelated effect occupies the only allowed slot: %.8f", cost)
	}
}

func TestDynamicEffectPlanOneTimeProtectionIsNotFreeForever(t *testing.T) {
	required := []string{TargetEffectElementalDamage, TargetEffectAttackIncrease, "最大装弹数增加"}
	scan := partScanFromArrays(
		[maxSlot]string{"防御力增加", "最大装弹数增加", TargetEffectElementalDamage},
		[maxSlot]string{}, [maxSlot]SlotLock{LockNone, LockOneTime, LockPermanent},
	)
	_, temporaryCost := dynamicEffectPlan(scan, required, nil, nil, "订制模块")
	_, renewableKeyCost := dynamicEffectPlan(scan, required, nil, nil, "自订密钥")
	scan.Slots[1].Lock = LockPermanent
	_, permanentCost := dynamicEffectPlan(scan, required, nil, nil, "订制模块")
	if temporaryCost <= permanentCost || math.Abs(permanentCost-23.4) > 1e-7 || math.Abs(renewableKeyCost-permanentCost) > 1e-7 {
		t.Fatalf("temporary/module %.8f, renewable key %.8f, permanent %.8f", temporaryCost, renewableKeyCost, permanentCost)
	}
}

func TestDynamicEffectPlanCompletedAndOverlockedStates(t *testing.T) {
	required := []string{TargetEffectAttackIncrease}
	scan := partScanFromArrays(
		[maxSlot]string{"命中率增加", "防御力增加", "蓄力速度增加"},
		[maxSlot]string{}, [maxSlot]SlotLock{LockPermanent, LockPermanent, LockOneTime},
	)
	if _, cost := dynamicEffectPlan(scan, required, nil, nil, "订制模块"); cost != costUnreachable {
		t.Fatalf("three locked slots = %.8f", cost)
	}
	scan.Slots[0].Effect = TargetEffectAttackIncrease
	if slot, cost := dynamicEffectPlan(scan, required, nil, nil, "订制模块"); slot != 0 || cost != 0 {
		t.Fatalf("already complete = (%d, %.8f)", slot, cost)
	}
}

func TestDynamicEffectPlanRespectsExistingFirstSlotLock(t *testing.T) {
	required := []string{TargetEffectElementalDamage, TargetEffectAttackIncrease, "最大装弹数增加"}
	scan := partScanFromArrays(
		[maxSlot]string{"防御力增加", "", ""},
		[maxSlot]string{}, [maxSlot]SlotLock{LockPermanent, LockNone, LockNone},
	)
	if _, cost := dynamicEffectPlan(scan, required, nil, nil, "订制模块"); cost != costUnreachable {
		t.Fatalf("unrelated permanent first slot leaves insufficient capacity: %.8f", cost)
	}
	scan.Slots[0].Effect = TargetEffectAttackIncrease
	if slot, cost := dynamicEffectPlan(scan, required, nil, nil, "订制模块"); slot != 0 || cost <= 0 || cost >= costUnreachable {
		t.Fatalf("useful permanent first slot can still complete slots two and three: (%d, %.8f)", slot, cost)
	}
	scan.Slots[0].Lock = LockOneTime
	scan.Slots[0].Effect = "防御力增加"
	if _, cost := dynamicEffectPlan(scan, required, nil, nil, "订制模块"); cost <= 0 || cost >= costUnreachable {
		t.Fatalf("temporary first slot releases its capacity after one roll: %.8f", cost)
	}
}
