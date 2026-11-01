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

func TestRecognizeAffixNameOCRSeparatesNameFromValueEvidence(t *testing.T) {
	ocr := func(text string) *maa.OCRResult { return &maa.OCRResult{Text: text} }
	for _, tt := range []struct {
		name        string
		results     []*maa.OCRResult
		effect      string
		known       bool
		conflicting bool
	}{
		{"inline digit missing", []*maa.OCRResult{ocr("【攻击力增加]1.11%")}, "攻击力增加", true, false},
		{"same names with different values", []*maa.OCRResult{ocr("【攻击力增加】1.11%"), ocr("【攻击力增加】11.11%")}, "攻击力增加", true, false},
		{"name and separate percentage", []*maa.OCRResult{ocr("【攻击力增加】"), ocr("11.11%")}, "攻击力增加", true, false},
		{"name without value", []*maa.OCRResult{ocr("【攻击力增加】")}, "攻击力增加", true, false},
		{"unknown name with value", []*maa.OCRResult{ocr("【攻击增加】11.11%")}, "", false, false},
		{"percentage alone", []*maa.OCRResult{ocr("11.11%")}, "", false, false},
		{"no OCR", nil, "", false, false},
		{"conflicting names", []*maa.OCRResult{ocr("攻击力增加"), ocr("防御力增加")}, "", false, true},
		{"multiple names in one box", []*maa.OCRResult{ocr("攻击力增加 防御力增加")}, "", false, true},
		{"empty conflicts with name", []*maa.OCRResult{ocr("未获得效果"), ocr("攻击力增加")}, "", false, true},
		{"unlock overlay", []*maa.OCRResult{ocr("攻击力增加"), ocr("已解除效果锁定")}, "", false, true},
		{"confirmed empty", []*maa.OCRResult{ocr("未获得效果")}, "", true, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			effect, raw, names, conflicting := recognizeAffixNameOCR(tt.results)
			if effect != tt.effect || (len(names) > 0) != tt.known || conflicting != tt.conflicting {
				t.Fatalf("effect=%q known=%v conflicting=%v raw=%q", effect, len(names) > 0, conflicting, raw)
			}
		})
	}
}

// 结果页同一槽位的两条 OCR 路径对数值精度不同：整行识别（only_rec）会吞掉小字号的
// 小数位。以下读数全部取自 MDA-logs-v1.12.2-20261101-120023 的真实失败帧，修复前这些帧
// 会被判成「数值冲突」而反复重试，直到 result_unreadable 让任务停在结果页。
func TestAffixReadingMergeToleratesTruncatedPercent(t *testing.T) {
	inline := func(text string) []*maa.OCRResult {
		return []*maa.OCRResult{{Text: text, Box: maa.Rect{535, 404, 258, 24}, Score: 0.838}}
	}
	split := func(name, value string) []*maa.OCRResult {
		return []*maa.OCRResult{
			{Text: name, Box: maa.Rect{571, 409, 69, 13}, Score: 0.994},
			{Text: value, Box: maa.Rect{656, 408, 49, 14}, Score: 0.964},
		}
	}

	tests := []struct {
		name   string
		direct []*maa.OCRResult
		detect []*maa.OCRResult
		value  string
		ok     bool
	}{
		{"6.64 dropped last digit", inline("【暴击伤害增加】 6.6%"), split("【暴击伤害增加】", "6.64%"), "6.64%", true},
		{"11.11 dropped decimals", inline("【命中率增加】11%"), split("【命中率增加】", "11.11%"), "11.11%", true},
		{"11.11 dropped leading digit", inline("【蓄力伤害增加】1.1%"), split("【蓄力伤害增加】", "11.11%"), "11.11%", true},
		{"10.94 dropped last digit", inline("【优越代码伤害增加】 10.9%"), split("【优越代码伤害增加】", "10.94%"), "10.94%", true},
		{"another official tier stays a conflict", inline("【暴击伤害增加】 6.64%"), split("【暴击伤害增加】", "8.60%"), "", false},
		{"name conflict stays a conflict", inline("【暴击伤害增加】 6.64%"), split("【防御力增加】", "6.64%"), "", false},
	}

	for _, tt := range tests {
		for _, order := range []string{"direct first", "detected first"} {
			t.Run(tt.name+"/"+order, func(t *testing.T) {
				readings := [][]*maa.OCRResult{tt.direct, tt.detect}
				if order == "detected first" {
					readings[0], readings[1] = readings[1], readings[0]
				}
				var state affixReading
				ok := true
				for _, results := range readings {
					effect, raw, recognized, conflicting := recognizeAffixOCR(results)
					state, ok = state.merge(effect, raw, recognized, conflicting)
					if !ok {
						break
					}
				}
				if ok != tt.ok {
					t.Fatalf("ok=%v want %v (raw=%q)", ok, tt.ok, state.raw)
				}
				if tt.ok && extractPercentValue(state.raw) != tt.value {
					t.Fatalf("value=%q want %q (raw=%q)", extractPercentValue(state.raw), tt.value, state.raw)
				}
			})
		}
	}
}

// 空槽（未获得效果）不能靠档位表把任何数值洗白，含糊的数值仍要判冲突。
func TestReconcileAffixValueRequiresKnownEffect(t *testing.T) {
	if value, ok := reconcileAffixValue("", "6.6%", "6.64%"); ok {
		t.Fatalf("empty effect reconciled to %q", value)
	}
	if value, ok := reconcileAffixValue("未知效果", "6.6%", "6.64%"); ok {
		t.Fatalf("unknown effect reconciled to %q", value)
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
