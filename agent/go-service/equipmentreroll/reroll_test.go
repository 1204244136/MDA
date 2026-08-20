package equipmentreroll

import (
	"testing"
)

func TestPartNeedParamNormalize(t *testing.T) {
	p := &partNeedParam{}
	p.normalize()
	if p.TargetEffect != TargetEffectElementalDamage {
		t.Fatalf("default target effect = %q, want %q", p.TargetEffect, TargetEffectElementalDamage)
	}

	p = &partNeedParam{TargetEffect: "  攻击力增加  "}
	p.normalize()
	if p.TargetEffect != "攻击力增加" {
		t.Fatalf("trimmed target effect = %q", p.TargetEffect)
	}
}

func TestResultDecideParamNormalize(t *testing.T) {
	p := &resultDecideParam{}
	p.normalize()
	if p.TargetEffect != TargetEffectElementalDamage {
		t.Fatalf("default target effect = %q, want %q", p.TargetEffect, TargetEffectElementalDamage)
	}
	p = &resultDecideParam{TargetEffect: " 最大装弹数增加 "}
	p.normalize()
	if p.TargetEffect != "最大装弹数增加" {
		t.Fatalf("trimmed target effect = %q", p.TargetEffect)
	}
}
