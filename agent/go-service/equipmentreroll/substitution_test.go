package equipmentreroll

import (
	"reflect"
	"testing"
)

// critSubstitutionPair 是用户示例中「暴击=爆伤」的可替换词条对。
var critSubstitutionPair = [2]string{TargetEffectCritRate, TargetEffectCritDamage}

// substitutionExampleQuota 是用户示例：4 优越、4 攻击、3 暴击/爆伤、1 装弹。
func substitutionExampleQuota() map[string]int {
	return map[string]int{
		TargetEffectElementalDamage: 4,
		TargetEffectAttackIncrease:  4,
		TargetEffectCritDamage:      3,
		"最大装弹数增加":                   1,
	}
}

func TestSubstitutionExpandsCritQuotaIntoCompleteGoals(t *testing.T) {
	goals := substitutionGoalQuotas(substitutionExampleQuota(), critSubstitutionPair)
	if len(goals) != 4 {
		t.Fatalf("crit pool of 3 should expand into 4 goals, got %#v", goals)
	}
	distributions := make(map[[2]int]bool)
	for _, goal := range goals {
		if quotaTotal(goal) != 12 {
			t.Fatalf("expanded goal must keep the total requirement: %#v", goal)
		}
		for _, effect := range []string{TargetEffectElementalDamage, TargetEffectAttackIncrease, "最大装弹数增加"} {
			if goal[effect] != substitutionExampleQuota()[effect] {
				t.Fatalf("non-substituted effect %q changed: %#v", effect, goal)
			}
		}
		pair := [2]int{goal[TargetEffectCritRate], goal[TargetEffectCritDamage]}
		if pair[0] < 0 || pair[1] < 0 || pair[0]+pair[1] != 3 {
			t.Fatalf("crit pair must sum to 3, got %v", pair)
		}
		distributions[pair] = true
	}
	for _, want := range [][2]int{{3, 0}, {2, 1}, {1, 2}, {0, 3}} {
		if !distributions[want] {
			t.Fatalf("missing crit distribution %v in %#v", want, goals)
		}
	}
}

func TestSubstitutionWorksForAnyPair(t *testing.T) {
	// 「蓄力速度 = 蓄力伤害」：验证后端不再硬编码暴击对，任何官方效果都能组成替换组。
	pair := [2]string{"蓄力速度增加", "蓄力伤害增加"}
	goals := substitutionGoalQuotas(map[string]int{"蓄力速度增加": 3}, pair)
	if len(goals) != 4 {
		t.Fatalf("charge pair pool of 3 should expand into 4 goals, got %#v", goals)
	}
	parts := map[string]partScan{
		equipmentParts[0]: partScanOf("蓄力速度增加", "", ""),
		equipmentParts[1]: partScanOf("蓄力速度增加", "", ""),
		equipmentParts[2]: partScanOf("蓄力伤害增加", "", ""),
		equipmentParts[3]: partScanOf("", "", ""),
	}
	if !allPartsSatisfiedQuotaAlternatives(parts, goals) {
		t.Fatal("2 charge speed + 1 charge damage should satisfy a 3-total pair")
	}
	cfg := parseCarrierConfig(`{"attach":{"quota_蓄力速度增加":3,"allow_substitution":true,"substitution_affix1":"蓄力速度增加","substitution_affix2":"蓄力伤害增加"}}`)
	if cfg.Substitution != pair {
		t.Fatalf("charge pair was not parsed from attach: %+v", cfg)
	}
	if !rerollTargetSatisfied(parts, cfg) {
		t.Fatal("carrier config must accept a non-crit substitution pair")
	}
}

func TestSubstitutionSumsBothPositiveQuotas(t *testing.T) {
	goals := substitutionGoalQuotas(map[string]int{TargetEffectCritRate: 2, TargetEffectCritDamage: 2}, critSubstitutionPair)
	if len(goals) != 5 {
		t.Fatalf("pool of 4 across two affixes should have 5 distributions, got %#v", goals)
	}
	for _, goal := range goals {
		if goal[TargetEffectCritRate]+goal[TargetEffectCritDamage] != 4 {
			t.Fatalf("crit pair must sum to 4: %#v", goal)
		}
	}
}

func TestSubstitutionSatisfactionUsesSharedTotal(t *testing.T) {
	quota := substitutionExampleQuota()
	parts := map[string]partScan{
		equipmentParts[0]: partScanOf(TargetEffectElementalDamage, TargetEffectAttackIncrease, TargetEffectCritRate),
		equipmentParts[1]: partScanOf(TargetEffectElementalDamage, TargetEffectAttackIncrease, TargetEffectCritDamage),
		equipmentParts[2]: partScanOf(TargetEffectElementalDamage, TargetEffectAttackIncrease, TargetEffectCritRate),
		equipmentParts[3]: partScanOf(TargetEffectElementalDamage, TargetEffectAttackIncrease, "最大装弹数增加"),
	}
	// 2 条暴击率 + 1 条暴击伤害 = 合计 3 条，但未按暴击伤害分别达标。
	if AllPartsSatisfiedQuota(parts, quota) {
		t.Fatal("single crit-damage quota must not be satisfied by mixed crit affixes")
	}
	if !allPartsSatisfiedQuotaAlternatives(parts, substitutionGoalQuotas(quota, critSubstitutionPair)) {
		t.Fatal("shared crit total should accept mixed crit affixes")
	}
	enabled := carrierConfig{Mode: rerollModeCharacter, Quota: quota, AllowSubstitution: true, Substitution: critSubstitutionPair}
	if !rerollTargetSatisfied(parts, enabled) {
		t.Fatal("carrier config with substitution must treat the mixed crit result as complete")
	}
	if rerollTargetSatisfied(parts, carrierConfig{Mode: rerollModeCharacter, Quota: quota}) {
		t.Fatal("substitution must stay disabled by default")
	}
}

func TestSubstitutionKeepsForbiddenMemberUnavailable(t *testing.T) {
	pair := [2]string{TargetEffectCritRate, TargetEffectCritDamage}
	quota := map[string]int{TargetEffectCritRate: -1, TargetEffectCritDamage: 3}
	goals := substitutionGoalQuotas(quota, pair)
	if len(goals) != 1 {
		t.Fatalf("forbidden crit rate must not be used as a substitute, got %#v", goals)
	}
	if goals[0][TargetEffectCritRate] != -1 || goals[0][TargetEffectCritDamage] != 3 {
		t.Fatalf("forbidden entry changed while expanding: %#v", goals[0])
	}
	parts := multiGoalParts(partScanOf(TargetEffectCritRate, "", ""))
	if allPartsSatisfiedQuotaAlternatives(parts, goals) {
		t.Fatal("forbidden crit rate must still fail the goal")
	}
	complete := multiGoalParts(partScanOf(TargetEffectCritDamage, "", ""))
	if !allPartsSatisfiedQuotaAlternatives(complete, goals) {
		t.Fatal("three crit damage should satisfy the forbidden-crit configuration")
	}
}

func TestSubstitutionLeavesUnrelatedQuotaAlone(t *testing.T) {
	quota := map[string]int{TargetEffectAttackIncrease: 4, "防御力增加": 1}
	goals := substitutionGoalQuotas(quota, critSubstitutionPair)
	if len(goals) != 1 || !reflect.DeepEqual(goals[0], quota) {
		t.Fatalf("quota without a substitutable pair must not expand: %#v", goals)
	}
}

func TestSubstitutionCostNoWorseThanAnyDistribution(t *testing.T) {
	quota := substitutionExampleQuota()
	goals := substitutionGoalQuotas(quota, critSubstitutionPair)
	parts := multiGoalParts(partScan{})
	combined := expectedModulesForQuotaAlternativesWithMaterial(parts, goals, "订制模块")
	if combined <= 0 || combined >= costUnreachable {
		t.Fatalf("substitution plan should have a finite estimate, got %.9f", combined)
	}
	for _, goal := range goals {
		single := expectedModulesForQuotaWithMaterial(parts, goal, "订制模块")
		t.Logf("single distribution %v = %.6f", [2]int{goal[TargetEffectCritRate], goal[TargetEffectCritDamage]}, single)
		if combined > single+1e-7 {
			t.Fatalf("substitution plan %.9f is worse than single distribution %.9f (%#v)", combined, single, goal)
		}
	}
	t.Logf("substitution plan = %.6f", combined)
}

func TestSubstitutionConfigParsesPair(t *testing.T) {
	cfg := parseCarrierConfig(`{"attach":{"quota_暴击伤害增加":3,"allow_substitution":true,"substitution_affix1":"暴击率增加","substitution_affix2":"暴击伤害增加"}}`)
	if !cfg.AllowSubstitution || cfg.Substitution != critSubstitutionPair || cfg.ConfigProblem != "" {
		t.Fatalf("substitution pair was not parsed: %+v", cfg)
	}
	goals := cfg.resolveGoalQuotas(nil)
	if len(goals) != 4 {
		t.Fatalf("enabled substitution should expose four joint goals, got %#v", goals)
	}
	cfg = parseCarrierConfig(`{"attach":{"quota_暴击伤害增加":3,"allow_substitution":false,"substitution_affix1":"暴击率增加","substitution_affix2":"暴击伤害增加"}}`)
	if cfg.AllowSubstitution || len(cfg.resolveGoalQuotas(nil)) != 1 {
		t.Fatalf("disabled substitution must keep a single quota goal: %+v", cfg)
	}
	for _, raw := range []string{`"yes"`, `null`, `1`} {
		cfg = parseCarrierConfig(`{"attach":{"quota_暴击伤害增加":3,"allow_substitution":` + raw + `}}`)
		if cfg.ConfigProblem == "" {
			t.Fatalf("invalid allow_substitution value %s must be rejected", raw)
		}
	}
}

func TestSubstitutionConfigValidatesPair(t *testing.T) {
	base := `"quota_暴击伤害增加":3,"allow_substitution":true`
	for _, tc := range []struct {
		name  string
		extra string
		valid bool
	}{
		{"valid", `,"substitution_affix1":"暴击率增加","substitution_affix2":"暴击伤害增加"`, true},
		{"missing pair", ``, false},
		{"missing second", `,"substitution_affix1":"暴击率增加"`, false},
		{"same affix", `,"substitution_affix1":"暴击率增加","substitution_affix2":"暴击率增加"`, false},
		{"unknown affix", `,"substitution_affix1":"暴击率增加","substitution_affix2":"不存在"`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := parseCarrierConfig(`{"attach":{` + base + tc.extra + `}}`)
			err := cfg.validateGoalQuotas(nil)
			if tc.valid && err != nil {
				t.Fatalf("valid pair rejected: %v", err)
			}
			if !tc.valid && err == nil {
				t.Fatal("invalid pair must be rejected")
			}
		})
	}
}

func TestSubstitutionConfigIgnoresValueAndSingleModes(t *testing.T) {
	for _, attach := range []string{
		`"operation":"value","value_tier_暴击伤害增加":15,"allow_substitution":true`,
		`"mode":"single","part":"头部","want1":"暴击率增加","allow_substitution":true`,
	} {
		cfg := parseCarrierConfig(`{"attach":{` + attach + `}}`)
		if err := cfg.validateGoalQuotas(nil); err != nil {
			t.Fatalf("unrelated substitution flag caused an error: %s: %v", attach, err)
		}
		if goals := cfg.resolveGoalQuotas(nil); len(goals) != 1 {
			t.Fatalf("value/single mode must not expand quotas: %#v", goals)
		}
	}
}

func TestSubstitutionValidationRejectsExcessiveTotal(t *testing.T) {
	cfg := parseCarrierConfig(`{"attach":{"quota_暴击率增加":4,"quota_暴击伤害增加":4,"quota_攻击力增加":4,"quota_优越代码伤害增加":1,"allow_substitution":true,"substitution_affix1":"暴击率增加","substitution_affix2":"暴击伤害增加"}}`)
	if err := cfg.validateGoalQuotas(nil); err == nil {
		t.Fatal("total requirement above 12 affixes must be rejected even with substitution")
	}
}

func TestSubstitutionDistributionStaysWithinEquipmentCap(t *testing.T) {
	goals := substitutionGoalQuotas(map[string]int{TargetEffectCritRate: 4, TargetEffectCritDamage: 4}, critSubstitutionPair)
	if len(goals) != 1 || goals[0][TargetEffectCritRate] != 4 || goals[0][TargetEffectCritDamage] != 4 {
		t.Fatalf("an 8-affix pool only fits 4+4, got %#v", goals)
	}
}

func TestSubstitutionPreexistingLocksAcceptCompletedMixedCrit(t *testing.T) {
	cfg := carrierConfig{Mode: rerollModeCharacter, Quota: substitutionExampleQuota(), AllowSubstitution: true, Substitution: critSubstitutionPair}
	parts := map[string]partScan{
		equipmentParts[0]: partScanOf(TargetEffectElementalDamage, TargetEffectAttackIncrease, TargetEffectCritRate),
		equipmentParts[1]: partScanOf(TargetEffectElementalDamage, TargetEffectAttackIncrease, TargetEffectCritRate),
		equipmentParts[2]: partScanOf(TargetEffectElementalDamage, TargetEffectAttackIncrease, TargetEffectCritRate),
		equipmentParts[3]: partScanOf(TargetEffectElementalDamage, TargetEffectAttackIncrease, "最大装弹数增加"),
	}
	// 混合暴击已完成目标；已有锁只需通过联合策略核验，不应因不是暴击伤害而报错。
	head := parts[equipmentParts[0]]
	head.Slots[2].Lock = LockPermanent
	parts[equipmentParts[0]] = head
	if res := validateAlternativeGlobalPreexistingLocks(parts, cfg.resolveGoalQuotas(nil), "自订密钥"); !res.Passed {
		t.Fatalf("completed mixed crit state must pass preexisting lock check: %s", res.Message)
	}
}
