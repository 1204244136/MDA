package equipmentreroll

import (
	"strings"
	"testing"
)

// 真实现场取自 v1.11.8 用户日志（task 200000001，19:04:44~19:04:49）：
// 结果页第 2 槽把「【暴击伤害增加】 14.48%」读成「[击伤害增加】 14.48%」，第 3 槽把
// 「【攻击力增加】 8.29%」读成「[攻击增加】 8.29%」。丢字后 normalizeEffect 匹配不上官方
// 词条名，整帧被判「识别不完整」，5 秒等待窗口内重复 OCR 同一静止帧必然得到同样结果。
var (
	liveResultRaws = [maxSlot]string{
		"【优越代码伤害增加】 10.94%",
		"[击伤害增加】 14.48%",
		"[攻击增加】 8.29%",
	}
	liveResultValues = [maxSlot]string{"10.94%", "14.48%", "8.29%"}
	// 数值重洗保持效果种类不变，因此同一时刻的快照词条名就是结果页词条名的唯一正解。
	liveSnapshotEffects = [maxSlot]string{"优越代码伤害增加", "暴击伤害增加", "攻击力增加"}
)

func liveSnapshot() partScan {
	var source partScan
	for i, effect := range liveSnapshotEffects {
		source.Slots[i] = slotScanData{Effect: effect, Value: "1.00%"}
	}
	return source
}

// TestValueModeFillsUnreadableEffectFromSnapshot 守护数值重洗的关键兜底：
// 词条名丢字时用本轮变更前快照补齐，避免整帧作废后空转到任务失败。
func TestValueModeFillsUnreadableEffectFromSnapshot(t *testing.T) {
	effects := [maxSlot]string{"优越代码伤害增加", "", ""}
	recognized := [maxSlot]bool{true, false, false}

	gotEffects, gotRaws, gotRecognized := fillUnreadableEffectsFromSource(effects, liveResultValues, liveResultRaws, recognized, liveSnapshot())

	if !gotRecognized[0] || !gotRecognized[1] || !gotRecognized[2] {
		t.Fatalf("unreadable slots were not filled: %v", gotRecognized)
	}
	for i, want := range liveSnapshotEffects {
		if gotEffects[i] != want {
			t.Fatalf("slot %d effect = %q want %q", i+1, gotEffects[i], want)
		}
	}
	// 补齐后必须通过完整性校验，否则兜底等于没做。
	if !validateSlotRecognitionCompleteness(gotEffects, liveResultValues, gotRaws, gotRecognized) {
		t.Fatal("filled result still rejected by completeness check")
	}
	// 原始外观文字必须保留在 raw 中，便于日志与失败报告定位 OCR 丢字。
	if gotRaws[1] != "[击伤害增加】 14.48%（词条名沿用本轮快照）" {
		t.Fatalf("raw text lost original evidence: %q", gotRaws[1])
	}
	// 已正确识别的槽位不得被快照覆盖。
	if gotEffects[0] != "优越代码伤害增加" {
		t.Fatalf("recognized slot overwritten: %q", gotEffects[0])
	}
}

// TestValueModeFallbackRefusesUnsafeFrames 确认兜底不会掩盖真正的异常帧。
// 三组用例都只让第 2 槽处于“未识别”，第 1、3 槽已正常识别，便于断言兜底结果。
func TestValueModeFallbackRefusesUnsafeFrames(t *testing.T) {
	effectsBase := [maxSlot]string{"优越代码伤害增加", "", "攻击力增加"}
	recognizedBase := [maxSlot]bool{true, false, true}

	emptyRaw := liveResultRaws
	emptyRaw[1] = "" // 未渲染或定位失败：必须继续重试，不能猜名称。

	missingValue := liveResultValues
	missingValue[1] = "" // 缺数值：补齐名称也无法评估，继续重试。

	unknownSnapshot := liveSnapshot()
	unknownSnapshot.Slots[1].Effect = "不是官方词条" // 快照本身不可信时不补。

	dupSnapshot := liveSnapshot()
	dupSnapshot.Slots[1].Effect = "优越代码伤害增加" // 补齐后与第 1 槽重复，说明槽位可能错位。

	for _, tc := range []struct {
		name   string
		values [maxSlot]string
		raws   [maxSlot]string
		source partScan
	}{
		{"empty raw", liveResultValues, emptyRaw, liveSnapshot()},
		{"missing value", missingValue, liveResultRaws, liveSnapshot()},
		{"invalid snapshot", liveResultValues, liveResultRaws, unknownSnapshot},
		{"empty source", liveResultValues, liveResultRaws, partScan{}},
		{"duplicate after fill", liveResultValues, liveResultRaws, dupSnapshot},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gotEffects, _, gotRecognized := fillUnreadableEffectsFromSource(effectsBase, tc.values, tc.raws, recognizedBase, tc.source)
			if gotRecognized != recognizedBase {
				t.Fatalf("fallback changed recognition flags: %v want %v", gotRecognized, recognizedBase)
			}
			if gotEffects != effectsBase {
				t.Fatalf("fallback changed effects: %v want %v", gotEffects, effectsBase)
			}
		})
	}
}

// TestValueModeFallbackKeepsStrictnessForRealNameChange 确认补齐只处理“读不出”，
// 真正读出了另一个官方词条的槽位仍会被 evaluateValueResult 拒绝并重试。
func TestValueModeFallbackKeepsStrictnessForRealNameChange(t *testing.T) {
	// 快照：优越代码伤害增加 / 暴击伤害增加 / 攻击力增加
	base := liveSnapshot()
	for i, effect := range liveSnapshotEffects {
		base.Slots[i] = valueTestSlot(effect, 1, LockNone)
	}
	// 结果页第 2 槽被读成另一个官方词条（不是丢字，是真的读到别的名字）。
	effects := [maxSlot]string{"优越代码伤害增加", "攻击力增加", "防御力增加"}
	recognized := [maxSlot]bool{true, true, true}

	gotEffects, _, gotRecognized := fillUnreadableEffectsFromSource(effects, liveResultValues, liveResultRaws, recognized, base)
	if gotEffects != effects || gotRecognized != recognized {
		t.Fatal("fallback must not touch slots that were read as a different official affix")
	}

	cfg := carrierConfig{Operation: rerollOperationValue, Mode: rerollModeSingle, Part: "腿部", ValueTargets: valueTarget{"攻击力增加": 11}}
	_, _, _, err := evaluateValueResult(map[string]partScan{"腿部": base}, "腿部", gotEffects, liveResultValues, cfg)
	if err == nil || !strings.Contains(err.Error(), "effect changed during value reroll") {
		t.Fatalf("value reroll must reject an effect the player did not have before the reroll, got err=%v", err)
	}
}
