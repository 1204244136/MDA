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

func TestIsTransientUnlockText(t *testing.T) {
	cases := []struct {
		raw  string
		want bool
	}{
		{raw: " 已解除效果", want: true},
		{raw: "已解除效果锁定。", want: true},
		{raw: "解除", want: true},
		{raw: "【攻击力增加】", want: false},
		{raw: "未获得效果", want: false},
		{raw: "", want: false},
	}

	for _, tc := range cases {
		if got := isTransientUnlockText(tc.raw); got != tc.want {
			t.Fatalf("isTransientUnlockText(%q) = %v, want %v", tc.raw, got, tc.want)
		}
	}
}
