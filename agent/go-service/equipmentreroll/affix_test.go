package equipmentreroll

import (
	"testing"
)

func TestAffixScanRequiresEvidence(t *testing.T) {
	for _, scan := range []slotScanResult{
		{},
		{RawEffect: "击率城", LockKnown: true},
		{Effect: "攻击力增加", LockKnown: true},
		{Effect: "攻击力增加", AffixKnown: true},
		{RawEffect: "未获得效果", AffixKnown: true, LockKnown: true, Value: "9.00%"},
		{RawEffect: "未获得效果", AffixKnown: true, LockKnown: true, Lock: LockPermanent},
	} {
		if affixScanReady(scan) {
			t.Fatalf("unknown or inconsistent slot accepted: %+v", scan)
		}
	}
	for _, scan := range []slotScanResult{
		{Effect: "攻击力增加", AffixKnown: true, LockKnown: true},
		{RawEffect: "未获得效果", AffixKnown: true, LockKnown: true},
	} {
		if !affixScanReady(scan) {
			t.Fatalf("verified slot rejected: %+v", scan)
		}
	}
}

func TestNormalizeEffectRejectsConflictingAndPartialNames(t *testing.T) {
	for _, raw := range []string{"攻", "防增加", "【代码伤害增", "击率城", "攻击力增加 暴击率增加", "蓄力伤害增力"} {
		if effect, ok := normalizeEffect(raw); ok {
			t.Errorf("%q guessed as %q", raw, effect)
		}
	}
}
