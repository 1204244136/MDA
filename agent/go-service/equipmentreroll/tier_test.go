package equipmentreroll

import (
	"strings"
	"testing"
)

func TestResolveEffectTier(t *testing.T) {
	// 对照 mapping（docs/装备系统与洗词条研究.md）：蓄力伤害/最大装弹/暴击伤害 11.81%/68.93%/16.44% 均为 T11。
	cases := []struct {
		effect, ocr string
		wantTier    int
		wantCalibr  string
		wantOK      bool
	}{
		{"蓄力伤害增加", "11.81%", 11, "11.81%", true},
		{"最大装弹数增加", "68.93%", 11, "68.93%", true},
		{"暴击伤害增加", "16.44%", 11, "16.44%", true},
		{"优越代码伤害增加", "9.54%", 1, "9.54%", true},
		{"暴击率增加", "3.32%", 4, "3.32%", true},
		// OCR 抖动校准：11.80% 就近归到 11.81%(T11)，输出精确档位值。
		{"蓄力伤害增加", "11.80%", 11, "11.81%", true},
		// 未知效果 / 空值 / 无法确认档位 → 不校准。
		{"未知效果", "11.81%", 0, "", false},
		{"蓄力伤害增加", "", 0, "", false},
		{"蓄力伤害增加", "未获得效果", 0, "", false},
	}
	for _, c := range cases {
		tier, calibrated, ok := resolveEffectTier(c.effect, c.ocr)
		if ok != c.wantOK {
			t.Fatalf("resolveEffectTier(%q,%q) ok=%v want %v", c.effect, c.ocr, ok, c.wantOK)
		}
		if tier != c.wantTier || calibrated != c.wantCalibr {
			t.Fatalf("resolveEffectTier(%q,%q)=(tier=%d,val=%q) want (tier=%d,val=%q)", c.effect, c.ocr, tier, calibrated, c.wantTier, c.wantCalibr)
		}
	}
}

func TestValueTierDisplay(t *testing.T) {
	if got := valueTierDisplay("11.81%", 11); got != "11.81%（T11）" {
		t.Fatalf("valueTierDisplay = %q, want 11.81%%（T11）", got)
	}
	if got := valueTierDisplay("11.81%", 0); got != "11.81%" {
		t.Fatalf("valueTierDisplay(tier=0) = %q, want 11.81%%", got)
	}
}

func TestBuildScanSlotDetail(t *testing.T) {
	scan := slotScanResult{
		Effect:   "蓄力伤害增加",
		Value:    "11.81%",
		RawValue: "11.81%",
		Tier:     11,
		Lock:     LockNone,
	}
	got := buildScanSlotDetail("头部", 1, scan)
	// 诊断 detail 应含 value(带档位) / raw_value(OCR) / tier / value_tier / lock / message 字段。
	for _, want := range []string{`"part":"头部"`, `"slot":1`, `"effect":"蓄力伤害增加"`, `"value":"11.81%（T11）"`, `"raw_value":"11.81%"`, `"tier":11`, `"value_tier":"11.81%（T11）"`, `"lock":"无锁"`, `"message":"蓄力伤害增加 11.81%（T11）"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("detail %s missing %s", got, want)
		}
	}
	// 空槽：value_tier 直接为 "效果"，message 为 "空槽位"。
	empty := slotScanResult{Effect: "", Value: "效果", Tier: 0, Lock: LockNone}
	gotEmpty := buildScanSlotDetail("腿部", 2, empty)
	if !strings.Contains(gotEmpty, `"value_tier":"效果"`) || !strings.Contains(gotEmpty, `"message":"空槽位"`) {
		t.Fatalf("empty detail should carry value_tier=效果 and message=空槽位, got %s", gotEmpty)
	}
}

func TestBuildMaterialCheckDetail(t *testing.T) {
	got := buildMaterialCheckDetail(154, 1920)
	for _, want := range []string{`"custom_modules":154`, `"custom_lock_keys":1920`, `"message":"订制模块 154 / 自订密钥 1920"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("material detail %s missing %s", got, want)
		}
	}
}

func TestBuildFinalSummaryMessage(t *testing.T) {
	const taskID int64 = 9999
	clearMonitorState(taskID)
	t.Cleanup(func() { clearMonitorState(taskID) })
	// 设置四件装备；头部 slot1 带档位效果。
	for _, part := range equipmentParts {
		for i := 0; i < maxSlot; i++ {
			eff, val := "", "效果"
			if part == "头部" && i == 0 {
				eff, val = "蓄力伤害增加", "11.81%"
			}
			if _, err := recordEffect(taskID, recordEffectParam{Slot: i + 1, Part: part, IsLast: i == maxSlot-1, Value: val}, eff); err != nil {
				t.Fatalf("recordEffect(%s slot %d) failed: %v", part, i+1, err)
			}
		}
	}
	setInventory(taskID, Inventory{CustomModules: 154, CustomLockKeys: 1920})
	recordRerollModuleCost(taskID, 6)
	recordLockMaterialCost(taskID, "自订密钥", 0)
	msg := buildFinalSummaryMessage(taskID)
	for _, want := range []string{"【装备详情】", "头部:\n蓄力伤害增加 11.81%（T11）", "（空槽位）", "【消耗材料】", "订制模块 6", "自订密钥 2"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("summary message %q missing %q", msg, want)
		}
	}
	if strings.Contains(msg, "\n空槽位") {
		t.Fatalf("summary message should use localized empty-slot label, got %q", msg)
	}
}

func TestBuildStandaloneSummaryMessage(t *testing.T) {
	const taskID int64 = 10000
	clearMonitorState(taskID)
	t.Cleanup(func() { clearMonitorState(taskID) })
	setInventory(taskID, Inventory{CustomModules: 1679, CustomLockKeys: 5876})

	if got := buildStandaloneSummaryMessage(taskID); got != "【库存】订制模块 1679 / 自订密钥 5876" {
		t.Fatalf("standalone summary = %q", got)
	}
	if got := buildStandaloneSummaryMessage(taskID + 1); got != "" {
		t.Fatalf("standalone summary without inventory = %q", got)
	}
}
