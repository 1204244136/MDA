package equipmentreroll

import (
	"reflect"
	"testing"
)

// TestCarrierQuotaLegacyFallbackOnlyWhenMissing 守护单一配额的兼容回退：
// 只有完全缺少 quota_* 键时才允许使用调用节点默认值；显式全为「不要求」必须报错。
func TestCarrierQuotaLegacyFallbackOnlyWhenMissing(t *testing.T) {
	defaultQuota := map[string]int{TargetEffectElementalDamage: 4}
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
		t.Fatal("explicit empty quota silently became the default goal")
	}
	if cfg.validateGoalQuotas(defaultQuota) == nil {
		t.Fatal("explicit empty quota must be rejected")
	}
}

func TestCarrierQuotaValidation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		attach string
		valid  bool
	}{
		{"valid", `"quota_攻击力增加":1,"quota_优越代码伤害增加":4`, true},
		{"forbidden only with another requirement", `"quota_攻击力增加":1,"quota_最大装弹数增加":-1`, true},
		{"empty", `"quota_攻击力增加":0`, false},
		{"forbidden only", `"quota_最大装弹数增加":-1`, false},
		{"excessive count", `"quota_攻击力增加":5`, false},
		{"negative count", `"quota_攻击力增加":-2`, false},
		{"excessive total", `"quota_攻击力增加":4,"quota_优越代码伤害增加":4,"quota_暴击率增加":4,"quota_最大装弹数增加":1`, false},
		{"unknown effect", `"quota_攻击力增加":1,"quota_不存在":1`, false},
		{"unknown ignored effect", `"quota_攻击力增加":1,"quota_不存在":0`, false},
		{"wrong count type", `"quota_攻击力增加":"4"`, false},
		{"null count", `"quota_攻击力增加":null`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := parseCarrierConfig(`{"attach":{` + tc.attach + `}}`)
			err := cfg.validateGoalQuotas(nil)
			if tc.valid && err != nil {
				t.Fatalf("valid quota rejected: %v", err)
			}
			if !tc.valid && err == nil {
				t.Fatal("invalid quota must be rejected")
			}
		})
	}
}
