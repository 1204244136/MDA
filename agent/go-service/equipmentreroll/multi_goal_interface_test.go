package equipmentreroll

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// TestMultiGoalInterfaceCarrierContract 防止额外方案误覆盖方案1，或在关闭时仍应用子配置。
func TestMultiGoalInterfaceCarrierContract(t *testing.T) {
	type optionCase struct {
		Name             string          `json:"name"`
		Option           []string        `json:"option"`
		PipelineOverride carrierOverride `json:"pipeline_override"`
	}
	var cfg struct {
		Option map[string]struct {
			Type        string       `json:"type"`
			DefaultCase string       `json:"default_case"`
			Cases       []optionCase `json:"cases"`
		} `json:"option"`
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "assets", "tasks", "EquipmentReroll.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	findCase := func(name, caseName string) optionCase {
		t.Helper()
		for _, c := range cfg.Option[name].Cases {
			if c.Name == caseName {
				return c
			}
		}
		t.Fatalf("option %s missing case %s", name, caseName)
		return optionCase{}
	}
	affixes := []struct{ name, effect string }{
		{"ElementalDamage", "优越代码伤害增加"},
		{"AttackIncrease", "攻击力增加"},
		{"MaxAmmo", "最大装弹数增加"},
		{"CritDamage", "暴击伤害增加"},
		{"CritRate", "暴击率增加"},
		{"ChargeSpeed", "蓄力速度增加"},
		{"ChargeDamage", "蓄力伤害增加"},
		{"HitRate", "命中率增加"},
		{"Defense", "防御力增加"},
	}
	values := map[string]int{"Forbid": -1, "None": 0, "Need1": 1, "Need2": 2, "Need3": 3, "Need4": 4}
	for _, group := range []int{2, 3} {
		switchName := fmt.Sprintf("EquipmentRerollAlternative%d", group)
		option, ok := cfg.Option[switchName]
		if !ok || option.Type != "switch" || option.DefaultCase != "No" {
			t.Fatalf("%s must be a switch disabled by default", switchName)
		}
		if !slices.Contains(findCase("EquipmentRerollMode", "Character").Option, switchName) {
			t.Fatalf("character effect mode does not expose %s", switchName)
		}
		for _, modeCase := range [][2]string{{"EquipmentRerollMode", "Single"}, {"EquipmentRerollValueMode", "Character"}, {"EquipmentRerollValueMode", "Single"}} {
			if slices.Contains(findCase(modeCase[0], modeCase[1]).Option, switchName) {
				t.Fatalf("%s must not be exposed by %s/%s", switchName, modeCase[0], modeCase[1])
			}
		}
		yes, no := findCase(switchName, "Yes"), findCase(switchName, "No")
		if len(no.Option) != 0 || len(yes.Option) != len(affixes) {
			t.Fatalf("%s must expose nine effect conditions only when enabled", switchName)
		}
		key := fmt.Sprintf("enabled%d", group)
		for _, c := range []optionCase{yes, no} {
			attach := c.PipelineOverride["EquipmentRerollLockNeed"].Attach
			var enabled bool
			if len(attach) != 1 || json.Unmarshal(attach[key], &enabled) != nil || enabled != (c.Name == "Yes") {
				t.Fatalf("%s/%s must write only attach.%s with its boolean state", switchName, c.Name, key)
			}
		}
		for _, affix := range affixes {
			name := fmt.Sprintf("EquipmentRerollQuota%d%s", group, affix.name)
			child, ok := cfg.Option[name]
			if !ok || child.Type != "select" || child.DefaultCase != "None" || !slices.Contains(yes.Option, name) {
				t.Fatalf("%s must be an initially empty condition under its own switch", name)
			}
			if len(child.Cases) != len(values) {
				t.Fatalf("%s has %d choices, want %d", name, len(child.Cases), len(values))
			}
			for caseName, value := range values {
				c := findCase(name, caseName)
				attach := c.PipelineOverride["EquipmentRerollLockNeed"].Attach
				key := fmt.Sprintf("quota%d_%s", group, affix.effect)
				var got int
				if len(attach) != 1 || json.Unmarshal(attach[key], &got) != nil || got != value {
					t.Fatalf("%s/%s must write only attach.%s=%d", name, caseName, key, value)
				}
			}
		}
	}
}
