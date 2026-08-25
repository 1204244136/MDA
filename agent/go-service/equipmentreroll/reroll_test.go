package equipmentreroll

import "testing"

func TestPartNeedParamNormalize(t *testing.T) {
	p := &partNeedParam{Part: "  头部  ", GlobalQuota: map[string]int{"最大装弹数增加": -1, "攻击力增加": 0}}
	p.normalize()
	if p.Part != "头部" {
		t.Fatalf("trimmed part = %q, want 头部", p.Part)
	}
	if quotaTotal(p.GlobalQuota) != 0 {
		t.Fatalf("quota total should ignore 0 and -1, got %d", quotaTotal(p.GlobalQuota))
	}
	if p.GlobalQuota["最大装弹数增加"] != -1 {
		t.Fatal("forbidden -1 should be kept")
	}
}

func TestResultDecideParamNormalize(t *testing.T) {
	p := &resultDecideParam{GlobalQuota: map[string]int{"优越代码伤害增加": 4, "命中率增加": 0}}
	p.normalize()
	if quotaTotal(p.GlobalQuota) != 4 {
		t.Fatalf("quota total = %d, want 4", quotaTotal(p.GlobalQuota))
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
