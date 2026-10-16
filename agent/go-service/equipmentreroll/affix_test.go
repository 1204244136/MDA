package equipmentreroll

import (
	"testing"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
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

func TestRecognizeAffixOCRPairsOnlyUnambiguousSameRowValues(t *testing.T) {
	ocr := func(text string, box maa.Rect) *maa.OCRResult {
		return &maa.OCRResult{Text: text, Box: box, Score: 0.99}
	}
	name := ocr("【防御力增加】", maa.Rect{575, 409, 62, 13})
	value := ocr("9.00%", maa.Rect{650, 406, 50, 15})
	inline := ocr("【防御力增加】9.00%", maa.Rect{498, 406, 295, 20})
	tests := []struct {
		name        string
		results     []*maa.OCRResult
		effect      string
		value       string
		known       bool
		conflicting bool
	}{
		{"recorded split boxes", []*maa.OCRResult{name, value}, "防御力增加", "9.00%", true, false},
		{"value first", []*maa.OCRResult{value, name}, "防御力增加", "9.00%", true, false},
		{"best duplicated in filtered", []*maa.OCRResult{name, name, value}, "防御力增加", "9.00%", true, false},
		{"inline survives name-only duplicate", []*maa.OCRResult{inline, name}, "防御力增加", "9.00%", true, false},
		{"same inline and split value", []*maa.OCRResult{inline, name, value}, "防御力增加", "9.00%", true, false},
		{"missing value", []*maa.OCRResult{name}, "防御力增加", "", true, false},
		{"partial name cannot supply value", []*maa.OCRResult{ocr("【防御增加】9.00%", inline.Box), name}, "防御力增加", "", true, false},
		{"partial name alone", []*maa.OCRResult{ocr("【防御增加】9.00%", inline.Box)}, "", "9.00%", false, false},
		{"adjacent row", []*maa.OCRResult{name, ocr("9.00%", maa.Rect{650, 430, 50, 15})}, "防御力增加", "", true, false},
		{"slight vertical overlap", []*maa.OCRResult{name, ocr("9.00%", maa.Rect{650, 420, 50, 15})}, "防御力增加", "", true, false},
		{"value left of name", []*maa.OCRResult{name, ocr("9.00%", maa.Rect{500, 406, 50, 15})}, "防御力增加", "", true, false},
		{"missing box", []*maa.OCRResult{name, ocr("9.00%", maa.Rect{})}, "防御力增加", "", true, false},
		{"conflicting percentages", []*maa.OCRResult{name, value, ocr("10.00%", value.Box)}, "", "", false, true},
		{"conflicting inline percentage", []*maa.OCRResult{inline, ocr("【防御力增加】10.00%", inline.Box)}, "", "9.00%", false, true},
		{"multiple inline percentages", []*maa.OCRResult{ocr("【防御力增加】9.00% 10.00%", inline.Box)}, "", "10.00%", false, true},
		{"conflicting names", []*maa.OCRResult{name, ocr("【攻击力增加】", name.Box), value}, "", "", false, true},
		{"empty slot with percentage", []*maa.OCRResult{ocr("未获得效果", name.Box), value}, "", "", false, true},
		{"transient unlock", []*maa.OCRResult{name, value, ocr("已解除效果锁定", inline.Box)}, "", "", false, true},
		{"empty slot", []*maa.OCRResult{ocr("未获得效果", inline.Box)}, "", "", true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			effect, raw, known, conflicting := recognizeAffixOCR(tt.results)
			if effect != tt.effect || known != tt.known || conflicting != tt.conflicting {
				t.Fatalf("effect=%q known=%v conflicting=%v raw=%q", effect, known, conflicting, raw)
			}
			if known && extractPercentValue(raw) != tt.value {
				t.Fatalf("value=%q raw=%q, want %q", extractPercentValue(raw), raw, tt.value)
			}
		})
	}
}
