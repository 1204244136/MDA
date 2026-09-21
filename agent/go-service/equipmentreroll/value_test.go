package equipmentreroll

import (
	"fmt"
	"testing"
)

func valueTestSlot(effect string, tier int, lock SlotLock) slotScanData {
	return slotScanData{Effect: effect, Value: fmt.Sprintf("%.2f%%", effectTiers[effect][tier-1]), Lock: lock}
}

func valueTestConfig() carrierConfig {
	return carrierConfig{Operation: rerollOperationValue, Mode: rerollModeSingle, Part: "头部", ValueTargets: valueTarget{"攻击力增加": 11}}
}

func TestValuePlanScopes(t *testing.T) {
	cfg := valueTestConfig()
	parts := map[string]partScan{}
	for _, part := range equipmentParts {
		parts[part] = partScan{Slots: [maxSlot]slotScanData{valueTestSlot("攻击力增加", 11, LockNone)}}
	}
	parts["腿部"] = partScan{Slots: [maxSlot]slotScanData{valueTestSlot("攻击力增加", 2, LockNone)}}
	plan, err := planValueReroll(parts, cfg)
	if err != nil || !plan.Satisfied {
		t.Fatalf("single mode must ignore legs: %+v %v", plan, err)
	}
	cfg.Mode = rerollModeCharacter
	cfg.ValueTargets["攻击力增加"] = 44
	plan, err = planValueReroll(parts, cfg)
	if err != nil || plan.Part != "腿部" || plan.Pending != 1 || plan.Satisfied {
		t.Fatalf("character mode must include legs: %+v %v", plan, err)
	}
	delete(parts, "身躯")
	if _, err = planValueReroll(parts, cfg); err == nil {
		t.Fatal("incomplete character scan must fail")
	}
}

func TestValuePlanInvalidStates(t *testing.T) {
	for _, tc := range []struct {
		name string
		scan partScan
	}{
		{"missing effect", partScan{}},
		{"unknown value", partScan{Slots: [maxSlot]slotScanData{{Effect: "攻击力增加", Value: "9.99%"}}}},
		{"NaN", partScan{Slots: [maxSlot]slotScanData{{Effect: "攻击力增加", Value: "NaN"}}}},
		{"locked empty", partScan{Slots: [maxSlot]slotScanData{{Lock: LockPermanent}}}},
		{"duplicate", partScan{Slots: [maxSlot]slotScanData{valueTestSlot("攻击力增加", 11, LockNone), valueTestSlot("攻击力增加", 11, LockNone)}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := planValueReroll(map[string]partScan{"头部": tc.scan}, valueTestConfig()); err == nil {
				t.Fatal("unsafe state must not produce an executable plan")
			}
		})
	}
}

func TestValueLockPlanning(t *testing.T) {
	scan := partScan{Slots: [maxSlot]slotScanData{
		valueTestSlot("攻击力增加", 15, LockNone),
		valueTestSlot("最大装弹数增加", 12, LockNone),
		valueTestSlot("优越代码伤害增加", 2, LockNone),
	}}
	cfg := valueTestConfig()
	cfg.ValueTargets = valueTarget{"攻击力增加": 11, "最大装弹数增加": 11, "优越代码伤害增加": 11}
	plan, err := planValueRerollWithInventory(map[string]partScan{"头部": scan}, cfg, Inventory{999, 99999}, "")
	if err != nil || plan.Locks != ([maxSlot]SlotLock{LockOneTime, LockOneTime, LockNone}) {
		t.Fatalf("protect two high targets: %+v %v", plan, err)
	}
	plan, err = planValueRerollWithInventory(map[string]partScan{"头部": scan}, cfg, Inventory{999, 0}, "")
	if err != nil || plan.Locks != ([maxSlot]SlotLock{LockPermanent, LockPermanent, LockNone}) {
		t.Fatalf("fixed locks without keys: %+v %v", plan, err)
	}
	scan.Slots[2].Lock = LockPermanent
	plan, err = planValueReroll(map[string]partScan{"头部": scan}, cfg)
	if err != nil || len(plan.Release) == 0 || plan.Release[0] != 3 {
		t.Fatalf("release low lock: %+v %v", plan, err)
	}
}

func TestValueResultDecisions(t *testing.T) {
	base := partScan{Slots: [maxSlot]slotScanData{
		valueTestSlot("攻击力增加", 5, LockNone),
		valueTestSlot("最大装弹数增加", 12, LockNone),
	}}
	target := valueTarget{"攻击力增加": 11, "最大装弹数增加": 11}
	for _, tc := range []struct {
		name         string
		attack, ammo int
		want         ResultDecision
	}{
		{"progress", 6, 12, ResultDecisionAccept},
		{"meets target", 11, 12, ResultDecisionAccept},
		{"regression", 4, 13, ResultDecisionKeep},
		{"tradeoff", 11, 11, ResultDecisionAccept},
		{"no target progress", 5, 13, ResultDecisionKeep},
		{"tie", 5, 12, ResultDecisionKeep},
	} {
		t.Run(tc.name, func(t *testing.T) {
			values := [maxSlot]string{valueTestSlot("攻击力增加", tc.attack, LockNone).Value, valueTestSlot("最大装弹数增加", tc.ammo, LockNone).Value}
			got, err := decideValueResultForScope(map[string]partScan{"头部": base}, "头部", base.Effects(), values, carrierConfig{Operation: rerollOperationValue, Mode: rerollModeSingle, Part: "头部", ValueTargets: target})
			if err != nil || got != tc.want {
				t.Fatalf("got %v %v, want %v", got, err, tc.want)
			}
		})
	}
	changed := base.Effects()
	changed[0] = "命中率增加"
	if _, err := decideValueResultForScope(map[string]partScan{"头部": base}, "头部", changed, [maxSlot]string{"8.29%", base.Slots[1].Value}, carrierConfig{Operation: rerollOperationValue, Mode: rerollModeSingle, Part: "头部", ValueTargets: target}); err == nil {
		t.Fatal("changed effect must be rejected even if its value is valid")
	}
	base.Slots[1].Lock = LockOneTime
	if _, err := decideValueResultForScope(map[string]partScan{"头部": base}, "头部", base.Effects(), [maxSlot]string{"8.29%", "77.15%"}, carrierConfig{Operation: rerollOperationValue, Mode: rerollModeSingle, Part: "头部", ValueTargets: target}); err == nil {
		t.Fatal("changed locked value must be rejected")
	}
}

func TestValueCarrierConfig(t *testing.T) {
	for _, raw := range []string{
		`{"attach":{"operation":"value","mode":"single","part":"头部","value_tier_攻击力增加":11}}`,
		`{"attach":{"operation":"value","mode":"character","value_tier_攻击力增加":15,"value_tier_命中率增加":0}}`,
		`{}`,
	} {
		if err := parseCarrierConfig(raw).validateOperation(); err != nil {
			t.Fatalf("valid config rejected: %s: %v", raw, err)
		}
	}
	for _, raw := range []string{
		`{"attach":{"operation":"values"}}`,
		`{"attach":{"operation":42}}`,
		`{"attach":{"mode":"singel"}}`,
		`{"attach":{"operation":"value"}}`,
		`{"attach":{"operation":"value","value_tier_攻击力增加":"11"}}`,
		`{"attach":{"operation":"value","value_tier_攻击力增加":61}}`,
		`{"attach":{"operation":"value","value_tier_未知效果":11}}`,
		`{"attach":{"operation":"value","mode":"single","value_tier_攻击力增加":11}}`,
	} {
		if err := parseCarrierConfig(raw).validateOperation(); err == nil {
			t.Fatalf("invalid config accepted: %s", raw)
		}
	}
	for _, mode := range []rerollMode{rerollModeCharacter, rerollModeSingle} {
		cfg := valueTestConfig()
		cfg.Mode = mode
		if got := rerollDecisionTarget(cfg); got != "EquipmentRerollValueDecide" {
			t.Fatalf("value operation fell back to effects: %s", got)
		}
	}
}
