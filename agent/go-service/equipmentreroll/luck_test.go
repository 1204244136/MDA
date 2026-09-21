package equipmentreroll

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/1204244136/MDA/agent/go-service/pkg/i18n"
)

func TestLuckBoundaries(t *testing.T) {
	for _, tc := range []struct {
		ratio float64
		want  string
	}{
		{0.01, "divine"}, {0.1, "divine"}, {0.1001, "lifespan"},
		{0.2, "lifespan"}, {0.2001, "insider"}, {0.35, "insider"},
		{0.3501, "vip"}, {0.5, "vip"}, {0.5001, "coupon"},
		{0.7, "coupon"}, {0.7001, "escape"}, {0.9, "escape"},
		{0.9001, "standard"}, {1.1, "standard"}, {1.1001, "detour"},
		{1.4, "detour"}, {1.4001, "donor"}, {2, "donor"},
		{2.0001, "shareholder"}, {3, "shareholder"}, {3.0001, "pity"},
		{5, "pity"}, {5.0001, "outlier"},
	} {
		if got := luckTier(tc.ratio); got != tc.want {
			t.Fatalf("%v: %s, want %s", tc.ratio, got, tc.want)
		}
	}
}

func TestLuckKeepsInitialEstimate(t *testing.T) {
	const id int64 = 987654322
	defer delete(states, id)
	cfg := carrierConfig{Mode: rerollModeSingle, Part: "头部", Target: parseSingleTarget(map[string]int{"攻击力增加": 1})}
	parts := map[string]partScan{"头部": {}}
	initialEstimateMessage(id, parts, cfg)
	baseline := states[id].InitialEstimate
	if baseline == nil || !baseline.Valid || baseline.Modules <= 0 {
		t.Fatal("initial estimate not stored")
	}
	if baseline.Flavor < 0 || baseline.Flavor >= luckFlavorCount {
		t.Fatalf("invalid flavor: %d", baseline.Flavor)
	}
	parts["头部"] = partScan{Slots: [maxSlot]slotScanData{{Effect: "攻击力增加"}}}
	initialEstimateMessage(id, parts, cfg)
	if states[id].InitialEstimate != baseline {
		t.Fatal("rescan replaced initial estimate")
	}
	state := states[id]
	state.Parts = parts
	state.Materials = MaterialUsage{CustomModules: 1}
	states[id] = state
	if !strings.Contains(buildFinalSummaryMessage(id), buildLuckMessage(id, parts, state.Materials)) {
		t.Fatal("final summary omits luck")
	}
}

func TestLuckFlavorsAndLocales(t *testing.T) {
	i18n.Init()
	const id int64 = 987654323
	defer delete(states, id)
	cfg := carrierConfig{Mode: rerollModeSingle, Part: "头部", Target: parseSingleTarget(map[string]int{"攻击力增加": 1})}
	parts := map[string]partScan{"头部": {Slots: [maxSlot]slotScanData{{Effect: "攻击力增加"}}}}
	locales := []map[string]string{}
	for _, lang := range []string{"zh_cn", "en_us"} {
		data, err := os.ReadFile("../../../assets/locales/go-service/" + lang + ".json")
		if err != nil {
			t.Fatal(err)
		}
		var messages map[string]string
		if err := json.Unmarshal(data, &messages); err != nil {
			t.Fatal(err)
		}
		locales = append(locales, messages)
	}
	for _, modules := range []int{5, 15, 30, 45, 60, 80, 100, 130, 180, 250, 400, 600} {
		tier := luckTier(float64(modules) / 100)
		seen := map[string]bool{}
		for flavor := 0; flavor < luckFlavorCount; flavor++ {
			key := "tasker.equipment_reroll.luck_" + tier
			quipKey := fmt.Sprintf("%s_%d", key, flavor+1)
			for _, messages := range locales {
				if messages[key] == "" || messages[quipKey] == "" {
					t.Fatalf("missing locale: %s", quipKey)
				}
				formatted := fmt.Sprintf(messages["tasker.equipment_reroll.luck_result"], messages[key], messages[quipKey], 100.0, modules, float64(modules))
				if strings.Contains(formatted, "%!") {
					t.Fatalf("invalid format: %s", formatted)
				}
			}
			states[id] = monitorState{InitialEstimate: &rerollEstimate{Modules: 100, Config: cfg, Valid: true, Flavor: flavor}}
			usage := MaterialUsage{CustomModules: modules}
			got := buildLuckMessage(id, parts, usage)
			if !strings.Contains(got, i18n.T(quipKey)) || seen[got] {
				t.Fatalf("missing or duplicated flavor: %s", got)
			}
			seen[got] = true
			if got != buildLuckMessage(id, parts, usage) {
				t.Fatal("summary rerolled flavor")
			}
		}
	}
}

func TestLuckSummaryFourModes(t *testing.T) {
	i18n.Init()
	const id int64 = 987654321
	defer delete(states, id)
	for _, operation := range []rerollOperation{rerollOperationEffect, rerollOperationValue} {
		for _, mode := range []rerollMode{rerollModeCharacter, rerollModeSingle} {
			cfg := carrierConfig{Operation: operation, Mode: mode, Part: "头部",
				Target: parseSingleTarget(map[string]int{"攻击力增加": 1}),
				Quota:  map[string]int{"攻击力增加": 4}, ValueTargets: valueTarget{"攻击力增加": 15}}
			parts := map[string]partScan{}
			for _, part := range cfg.valueScope() {
				parts[part] = partScan{}
			}
			states[id] = monitorState{InitialEstimate: &rerollEstimate{Modules: 100, Config: cfg, Valid: true}}
			usage := MaterialUsage{CustomModules: 50, CustomLockKeys: 2}
			if got := buildLuckMessage(id, parts, usage); got != "" {
				t.Fatalf("unfinished: %s", got)
			}
			for part := range parts {
				parts[part] = partScan{Slots: [maxSlot]slotScanData{valueTestSlot("攻击力增加", 15, LockNone)}}
			}
			got := buildLuckMessage(id, parts, usage)
			if !strings.Contains(got, i18n.T("tasker.equipment_reroll.luck_vip")) || !strings.Contains(got, i18n.T("tasker.equipment_reroll.luck_keys", 2)) {
				t.Fatalf("completed: %s", got)
			}
			if got := buildLuckMessage(id, parts, MaterialUsage{}); got != "" {
				t.Fatalf("no roll: %s", got)
			}
			states[id].InitialEstimate.Valid = false
			if got := buildLuckMessage(id, parts, usage); got != "" {
				t.Fatalf("unavailable: %s", got)
			}
		}
	}
	delete(states, id)
	if got := buildLuckMessage(id, nil, MaterialUsage{}); got != "" {
		t.Fatalf("missing baseline: %s", got)
	}
}
