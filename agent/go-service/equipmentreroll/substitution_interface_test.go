package equipmentreroll

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestSubstitutionInterfaceCarrierContract 守护「允许词条替换」的选项契约：
// 开关位于角色效果模式选项列表最下方；开启后用户用两个 select 选择 X/X 词条对，
// 选项只写 attach 的 allow_substitution 与 substitution_affix1/2。
// 方案 2/3 及其重复配额选项已移除。
func TestSubstitutionInterfaceCarrierContract(t *testing.T) {
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

	const switchName = "EquipmentRerollAllowSubstitution"
	option, ok := cfg.Option[switchName]
	if !ok || option.Type != "switch" || option.DefaultCase != "No" {
		t.Fatalf("%s must be a switch disabled by default", switchName)
	}
	character := findCase("EquipmentRerollMode", "Character")
	if !slices.Contains(character.Option, switchName) {
		t.Fatalf("character effect mode does not expose %s", switchName)
	}
	if character.Option[len(character.Option)-1] != switchName {
		t.Fatalf("%s must be the last option under character mode, got %v", switchName, character.Option)
	}
	for _, modeCase := range [][2]string{{"EquipmentRerollMode", "Single"}, {"EquipmentRerollValueMode", "Character"}, {"EquipmentRerollValueMode", "Single"}} {
		if slices.Contains(findCase(modeCase[0], modeCase[1]).Option, switchName) {
			t.Fatalf("%s must not be exposed by %s/%s", switchName, modeCase[0], modeCase[1])
		}
	}
	yes, no := findCase(switchName, "Yes"), findCase(switchName, "No")
	for _, c := range []optionCase{yes, no} {
		attach := c.PipelineOverride["EquipmentRerollLockNeed"].Attach
		var enabled bool
		if len(attach) != 1 || json.Unmarshal(attach["allow_substitution"], &enabled) != nil || enabled != (c.Name == "Yes") {
			t.Fatalf("%s/%s must write only attach.allow_substitution with its boolean state", switchName, c.Name)
		}
	}
	childNames := []string{"EquipmentRerollSubstitutionAffix1", "EquipmentRerollSubstitutionAffix2"}
	if !slices.Equal(yes.Option, childNames) {
		t.Fatalf("%s/Yes must expose exactly the two affix selectors, got %v", switchName, yes.Option)
	}
	if len(no.Option) != 0 {
		t.Fatalf("%s/No must not expose affix selectors, got %v", switchName, no.Option)
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
	attach := map[string]json.RawMessage{}
	for i, childName := range childNames {
		child, exists := cfg.Option[childName]
		if !exists || child.Type != "select" || child.DefaultCase != "None" {
			t.Fatalf("%s must be an initially empty select", childName)
		}
		if len(child.Cases) != len(affixes)+1 {
			t.Fatalf("%s has %d choices, want %d", childName, len(child.Cases), len(affixes)+1)
		}
		none := findCase(childName, "None")
		if len(none.PipelineOverride) != 0 {
			t.Fatalf("%s/None must not write attach", childName)
		}
		key := fmt.Sprintf("substitution_affix%d", i+1)
		for _, affix := range affixes {
			c := findCase(childName, affix.name)
			got := c.PipelineOverride["EquipmentRerollLockNeed"].Attach
			var value string
			if len(got) != 1 || json.Unmarshal(got[key], &value) != nil || value != affix.effect {
				t.Fatalf("%s/%s must write only attach.%s=%q", childName, affix.name, key, affix.effect)
			}
		}
	}
	// 取 UI 里实际选择「暴击率增加」和「暴击伤害增加」时应用的 attach，交给 Go 端解析。
	for _, choice := range []struct{ child, caseName string }{{childNames[0], "CritRate"}, {childNames[1], "CritDamage"}} {
		for key, value := range findCase(choice.child, choice.caseName).PipelineOverride["EquipmentRerollLockNeed"].Attach {
			attach[key] = value
		}
	}
	attach["allow_substitution"] = json.RawMessage("true")
	attach["quota_暴击伤害增加"] = json.RawMessage("3")
	payload, err := json.Marshal(map[string]any{"attach": attach})
	if err != nil {
		t.Fatal(err)
	}
	parsed := parseCarrierConfig(string(payload))
	if !parsed.AllowSubstitution || parsed.Substitution != critSubstitutionPair {
		t.Fatalf("UI attach is not readable by carrier: %+v", parsed)
	}
	if goals := parsed.resolveGoalQuotas(nil); len(goals) != 4 {
		t.Fatalf("UI pair should expand into four goals, got %#v", goals)
	}

	for name := range cfg.Option {
		if strings.HasPrefix(name, "EquipmentRerollQuota2") || strings.HasPrefix(name, "EquipmentRerollQuota3") ||
			name == "EquipmentRerollAlternative2" || name == "EquipmentRerollAlternative3" {
			t.Fatalf("removed multi-plan option %s still exists", name)
		}
	}
}
