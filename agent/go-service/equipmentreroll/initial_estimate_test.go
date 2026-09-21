package equipmentreroll

import (
	"maps"
	"math"
	"reflect"
	"testing"
)

func TestInitialEstimateFourModes(t *testing.T) {
	for _, operation := range []rerollOperation{rerollOperationEffect, rerollOperationValue} {
		for _, mode := range []rerollMode{rerollModeCharacter, rerollModeSingle} {
			t.Run(string(operation)+"/"+string(mode), func(t *testing.T) {
				cfg := carrierConfig{Operation: operation, Mode: mode, Part: "头部",
					Target: parseSingleTarget(map[string]int{"攻击力增加": 1}),
					Quota:  map[string]int{"攻击力增加": 4}, ValueTargets: valueTarget{"攻击力增加": 15}}
				parts := map[string]partScan{}
				for _, part := range cfg.valueScope() {
					scan := partScan{}
					if cfg.isValue() {
						scan.Slots[0] = valueTestSlot("攻击力增加", 1, LockNone)
					}
					parts[part] = scan
				}
				before := maps.Clone(parts)
				cost, err := initialModuleEstimate(parts, cfg)
				if err != nil || cost <= 0 || math.IsInf(cost, 0) || math.IsNaN(cost) {
					t.Fatalf("pending estimate = %v, %v", cost, err)
				}
				if !reflect.DeepEqual(parts, before) {
					t.Fatal("estimation changed the initial snapshot")
				}
				for part := range parts {
					parts[part] = partScan{Slots: [maxSlot]slotScanData{valueTestSlot("攻击力增加", 15, LockNone)}}
				}
				if cost, err = initialModuleEstimate(parts, cfg); err != nil || cost != 0 {
					t.Fatalf("satisfied estimate = %v, %v", cost, err)
				}
				delete(parts, "头部")
				if _, err = initialModuleEstimate(parts, cfg); err == nil {
					t.Fatal("missing snapshot must not report zero cost")
				}
			})
		}
	}
}

func TestInitialEstimateIncludesModuleLockCost(t *testing.T) {
	scan := partScan{Slots: [maxSlot]slotScanData{{}, {}, {Effect: "攻击力增加"}}}
	for _, mode := range []rerollMode{rerollModeSingle, rerollModeCharacter} {
		cfg := carrierConfig{Mode: mode, Part: "头部",
			Target: parseSingleTarget(map[string]int{"攻击力增加": 0, "优越代码伤害增加": 0}),
			Quota:  map[string]int{"攻击力增加": 4, "优越代码伤害增加": 4}}
		parts := map[string]partScan{}
		for _, part := range cfg.valueScope() {
			parts[part] = scan
		}
		cost, err := initialModuleEstimate(parts, cfg)
		keyCost := singleExpectedCost(scan, cfg.Target) * float64(len(parts))
		if err != nil || cost <= keyCost {
			t.Fatalf("%s: module estimate %v must exceed key-funded %v: %v", mode, cost, keyCost, err)
		}
	}
}

func TestInitialEstimateInvalidTargets(t *testing.T) {
	parts := map[string]partScan{"头部": {}}
	for _, cfg := range []carrierConfig{
		{Mode: rerollModeSingle, Part: "头部"},
		{Mode: rerollModeSingle, Part: "头部", Target: parseSingleTarget(map[string]int{"不存在": 0})},
		valueTestConfig(), // 数值重洗不能凭空生成目标效果。
		{Mode: rerollModeSingle, Part: "头部", Operation: "invalid"},
	} {
		if _, err := initialModuleEstimate(parts, cfg); err == nil {
			t.Fatalf("invalid target was estimated: %+v", cfg)
		}
	}
}

func TestInitialEstimateReportingBoundary(t *testing.T) {
	for _, operation := range []rerollOperation{rerollOperationEffect, rerollOperationValue} {
		for _, mode := range []rerollMode{rerollModeCharacter, rerollModeSingle} {
			cfg := carrierConfig{Operation: operation, Mode: mode, Part: "头部"}
			for _, part := range equipmentParts {
				want := part == equipmentParts[len(equipmentParts)-1]
				if cfg.isSingle() {
					want = part == cfg.Part
				}
				if shouldReportInitialEstimate(false, part, cfg) != want {
					t.Fatalf("unexpected reporting boundary: %s/%s/%s", operation, mode, part)
				}
				if shouldReportInitialEstimate(true, part, cfg) {
					t.Fatal("standalone scan must not report an estimate")
				}
			}
		}
	}
}
