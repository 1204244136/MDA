package equipmentreroll

import (
	"math"
	"testing"
)

func TestEffectOutcomesKeepOriginalEffectCombinationProbability(t *testing.T) {
	// 官方 2026-09-17 公告只在同效果、同数值时重抽数值，效果组合概率不变。
	for _, tier := range []int{1, 8, 15} {
		scan := partScan{Slots: [maxSlot]slotScanData{valueTestSlot("攻击力增加", tier, LockNone)}}
		outcomes := conditionedEffectOutcomes(scan, []int{0, 1, 2}, nil, requiredSet([]string{"攻击力增加"}), nil, nil)
		mass, same := 0.0, 0.0
		for _, out := range outcomes {
			mass += out.prob
			if out.values[0] == "攻击力增加" && out.values[1] == "" && out.values[2] == "" {
				same += out.prob
			}
		}
		// 第1槽抽到攻击10%，第2、3槽都空50%×70%，与旧数值档位无关。
		if math.Abs(mass-1) > 1e-12 || math.Abs(same-.035) > 1e-12 {
			t.Fatalf("T%d: mass=%g same effects=%g, want 1 and .035", tier, mass, same)
		}
	}
}

func TestEffectOutcomesExcludeLockedEffectsFromOtherSlots(t *testing.T) {
	scan := partScan{Slots: [maxSlot]slotScanData{
		valueTestSlot("防御力增加", 1, LockNone),
		{},
		valueTestSlot("攻击力增加", 15, LockPermanent),
	}}
	outcomes := conditionedEffectOutcomes(scan, []int{0, 1}, []string{"攻击力增加"}, requiredSet([]string{"攻击力增加", "优越代码伤害增加"}), nil, nil)
	mass, firstElemental := 0.0, 0.0
	for _, out := range outcomes {
		mass += out.prob
		for _, effect := range out.values {
			if effect == "攻击力增加" {
				t.Fatal("locked attack appeared again in an unlocked slot")
			}
		}
		if out.values[0] == "优越代码伤害增加" {
			firstElemental += out.prob
		}
	}
	if math.Abs(mass-1) > 1e-12 || math.Abs(firstElemental-1.0/9) > 1e-12 {
		t.Fatalf("mass=%g first elemental=%g, want 1 and 1/9", mass, firstElemental)
	}
}

func TestEffectFirstPassageUsesGeometricEffectProbability(t *testing.T) {
	outcomes := []slotOutcome{
		{effects: [maxSlot]string{"攻击力增加"}, prob: .2},
		{effects: [maxSlot]string{"防御力增加"}, prob: .8},
	}
	success := func(effects [maxSlot]string) bool { return effects[0] == "攻击力增加" }
	for _, tier := range []int{1, 8, 15} {
		scan := partScan{Slots: [maxSlot]slotScanData{valueTestSlot("防御力增加", tier, LockNone)}}
		got := effectFirstPassageCost(scan, []int{0}, outcomes, success, 2)
		if math.Abs(got-10) > 1e-12 {
			t.Fatalf("T%d: cost=%g, want 2/.2=10", tier, got)
		}
	}
	complete := partScan{Slots: [maxSlot]slotScanData{valueTestSlot("攻击力增加", 1, LockNone)}}
	if got := effectFirstPassageCost(complete, []int{0}, outcomes, success, 2); got != 0 {
		t.Fatalf("completed target cost=%g, want 0", got)
	}
	if got := effectFirstPassageCost(partScan{}, []int{0}, outcomes[:0], success, 2); got != costUnreachable {
		t.Fatalf("unreachable target cost=%g, want %g", got, costUnreachable)
	}
}
