package equipmentreroll

import (
	"errors"
	"reflect"
	"testing"
)

func TestMultiGoalCarrierRetainsIndependentTargets(t *testing.T) {
	cfg := parseCarrierConfig(`{"attach":{
		"quota_攻击力增加":4,"quota_优越代码伤害增加":4,"quota_暴击率增加":4,"quota_最大装弹数增加":-1,
		"enabled2":true,"quota2_攻击力增加":4,"quota2_优越代码伤害增加":4,"quota2_暴击率增加":3,"quota2_最大装弹数增加":1,
		"enabled3":false,"quota3_攻击力增加":"stale"}}`)
	if err := cfg.validateGoalQuotas(nil); err != nil {
		t.Fatal(err)
	}
	goals := cfg.resolveGoalQuotas(nil)
	if len(goals) != 2 || goals[0]["最大装弹数增加"] != -1 || goals[1]["最大装弹数增加"] != 1 ||
		goals[0]["暴击率增加"] != 4 || goals[1]["暴击率增加"] != 3 {
		t.Fatalf("whole goals were lost or merged: %#v", goals)
	}
}

func TestMultiGoalCarrierLegacyFallbackOnlyWhenMissing(t *testing.T) {
	defaultQuota := map[string]int{"优越代码伤害增加": 4}
	for _, raw := range []string{
		`{}`,
		`{"recognition":{"param":{"custom_recognition_param":{"global_quota":{"优越代码伤害增加":4}}}}}`,
	} {
		cfg := parseCarrierConfig(raw)
		if err := cfg.validateGoalQuotas(defaultQuota); err != nil ||
			!reflect.DeepEqual(cfg.resolveGoalQuotas(defaultQuota), []map[string]int{defaultQuota}) {
			t.Fatalf("legacy fallback rejected: %s: %v", raw, err)
		}
	}
	cfg := parseCarrierConfig(`{"attach":{"quota_攻击力增加":0},"recognition":{"param":{"custom_recognition_param":{"global_quota":{"优越代码伤害增加":4}}}}}`)
	if len(cfg.resolveQuota(defaultQuota)) != 0 {
		t.Fatal("explicit empty goal silently became the default goal")
	}
	assertInvalidGoal(t, cfg.validateGoalQuotas(defaultQuota), 1)
}

func TestMultiGoalCarrierRejectsInvalidEnabledGoals(t *testing.T) {
	for _, tc := range []struct {
		name   string
		attach string
		goal   int
	}{
		{"empty second", `"quota_攻击力增加":1,"enabled2":true`, 2},
		{"forbidden only", `"quota_攻击力增加":1,"enabled2":true,"quota2_最大装弹数增加":-1`, 2},
		{"empty third", `"quota_攻击力增加":1,"enabled3":true,"quota3_攻击力增加":0`, 3},
		{"excessive count", `"quota_攻击力增加":1,"enabled2":true,"quota2_攻击力增加":5`, 2},
		{"negative count", `"quota_攻击力增加":1,"enabled2":true,"quota2_攻击力增加":-2`, 2},
		{"excessive total", `"quota_攻击力增加":1,"enabled2":true,"quota2_攻击力增加":4,"quota2_优越代码伤害增加":4,"quota2_暴击率增加":4,"quota2_最大装弹数增加":1`, 2},
		{"unknown effect", `"quota_攻击力增加":1,"enabled2":true,"quota2_不存在":1`, 2},
		{"unknown ignored effect", `"quota_攻击力增加":1,"enabled2":true,"quota2_攻击力增加":1,"quota2_不存在":0`, 2},
		{"wrong count type", `"quota_攻击力增加":1,"enabled2":true,"quota2_攻击力增加":"4"`, 2},
		{"null count", `"quota_攻击力增加":1,"enabled2":true,"quota2_攻击力增加":null`, 2},
		{"wrong switch type", `"quota_攻击力增加":1,"enabled2":"true"`, 2},
		{"null switch", `"quota_攻击力增加":1,"enabled2":null`, 2},
		{"invalid first", `"quota_攻击力增加":"4"`, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := parseCarrierConfig(`{"attach":{` + tc.attach + `}}`)
			assertInvalidGoal(t, cfg.validateOperation(), tc.goal)
			assertInvalidGoal(t, cfg.validateGoalQuotas(nil), tc.goal)
		})
	}
}

func TestMultiGoalCarrierIgnoresDisabledAndOtherModeResidue(t *testing.T) {
	for _, raw := range []string{
		`{"attach":{"quota_攻击力增加":1,"enabled2":false,"quota2_攻击力增加":99,"quota2_不存在":"bad"}}`,
		`{"attach":{"mode":"single","part":"头部","enabled2":"bad","quota2_不存在":99}}`,
		`{"attach":{"operation":"value","value_tier_攻击力增加":15,"enabled2":"bad","quota_攻击力增加":"bad","quota3_不存在":99}}`,
	} {
		cfg := parseCarrierConfig(raw)
		if err := cfg.validateGoalQuotas(map[string]int{"攻击力增加": 1}); err != nil {
			t.Fatalf("irrelevant residual options caused an error: %s: %v", raw, err)
		}
		if goals := cfg.resolveGoalQuotas(nil); len(goals) != 1 {
			t.Fatalf("irrelevant alternative goal activated: %#v", goals)
		}
	}
}

func TestMultiGoalCarrierKeepsThirdGoalNumber(t *testing.T) {
	cfg := carrierConfig{Mode: rerollModeCharacter, Quota: map[string]int{"攻击力增加": 1}, AlternativeEnabled: [2]bool{false, true}}
	assertInvalidGoal(t, cfg.validateGoalQuotas(nil), 3)
	cfg.AlternativeQuotas[1] = map[string]int{"优越代码伤害增加": 1}
	if err := cfg.validateGoalQuotas(nil); err != nil || len(cfg.resolveGoalQuotas(nil)) != 2 {
		t.Fatalf("third goal could not be enabled independently: %v", err)
	}
}

func assertInvalidGoal(t *testing.T, err error, number int) {
	t.Helper()
	var goalError *goalQuotaError
	if !errors.As(err, &goalError) || goalError.Number != number {
		t.Fatalf("invalid goal error = %v, expected goal %d", err, number)
	}
}
