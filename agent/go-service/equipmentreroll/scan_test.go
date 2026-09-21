package equipmentreroll

import (
	"testing"
)

func TestValueScanReady(t *testing.T) {
	for _, tc := range []struct {
		name string
		scan slotScanResult
		want bool
	}{
		{"valid", slotScanResult{Effect: "优越代码伤害增加", Value: "12.34%", Tier: 3}, true},
		{"missing value", slotScanResult{Effect: "优越代码伤害增加"}, false},
		{"invalid tier", slotScanResult{Effect: "优越代码伤害增加", Value: "99.99%"}, false},
		{"OCR missing is not empty slot", slotScanResult{}, false},
		{"confirmed empty", slotScanResult{RawEffect: "未获得效果"}, true},
		{"inconsistent empty", slotScanResult{RawEffect: "未获得效果", Value: "12.34%"}, false},
		{"locked empty", slotScanResult{RawEffect: "未获得效果", Lock: LockPermanent}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := valueScanReady(tc.scan); got != tc.want {
				t.Fatalf("valueScanReady(%+v)=%v", tc.scan, got)
			}
		})
	}
}

func TestRecordEffectStoresValueAndLock(t *testing.T) {
	const taskID int64 = 3001
	clearMonitorState(taskID)
	t.Cleanup(func() { clearMonitorState(taskID) })

	inputs := []struct {
		params recordEffectParam
		effect string
	}{
		{params: recordEffectParam{Slot: 1, Part: "头部", Value: "11.81%", Lock: LockPermanent}, effect: "蓄力伤害增加"},
		{params: recordEffectParam{Slot: 2, Part: "头部", Value: "6.88%", Lock: LockNone}, effect: "蓄力速度增加"},
		{params: recordEffectParam{Slot: 3, Part: "头部", IsLast: true, Value: "68.93%", Lock: LockOneTime}, effect: "最大装弹数增加"},
	}

	for _, input := range inputs {
		if _, err := recordEffect(taskID, input.params, input.effect); err != nil {
			t.Fatalf("recordEffect(slot %d) failed: %v", input.params.Slot, err)
		}
	}

	scan, ok := currentPartScan(taskID)
	if !ok {
		t.Fatal("currentPartScan() was not available after slot 3")
	}
	want := partScan{Slots: [maxSlot]slotScanData{
		{Effect: "蓄力伤害增加", Value: "11.81%", Lock: LockPermanent},
		{Effect: "蓄力速度增加", Value: "6.88%", Lock: LockNone},
		{Effect: "最大装弹数增加", Value: "68.93%", Lock: LockOneTime},
	}}
	if scan != want {
		t.Fatalf("currentPartScan() = %+v, want %+v", scan, want)
	}
}

func TestFirstExistingLock(t *testing.T) {
	scan := partScan{Slots: [maxSlot]slotScanData{
		{Effect: "攻击力增加", Lock: LockNone},
		{Effect: "蓄力速度增加", Lock: LockOneTime},
		{Effect: "最大装弹数增加", Lock: LockPermanent},
	}}

	slot, lock, found := firstExistingLock(scan)
	if !found || slot != 2 || lock != LockOneTime {
		t.Fatalf("firstExistingLock() = (%d, %v, %v), want (2, %v, true)", slot, lock, found, LockOneTime)
	}

	slot, lock, found = firstExistingLock(partScan{})
	if found || slot != 0 || lock != LockNone {
		t.Fatalf("firstExistingLock(empty) = (%d, %v, %v), want (0, %v, false)", slot, lock, found, LockNone)
	}
}

func TestExtractPercentValue(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{name: "plain percent", raw: "【蓄力伤害增加】11.81%", want: "11.81%"},
		{name: "range then value", raw: "【优越代码伤害增加】（9.54% ~ 29.16%）10%", want: "10%"},
		{name: "empty", raw: "未获得效果", want: ""},
		{name: "no percent", raw: "效果变更", want: ""},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractPercentValue(tt.raw); got != tt.want {
				t.Fatalf("extractPercentValue(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestFormatSlotLine(t *testing.T) {
	lockLabels := map[SlotLock]string{
		LockPermanent: "（永久锁）",
		LockOneTime:   "（一次性锁）",
	}
	cases := []struct {
		name string
		scan slotScanData
		want string
	}{
		{name: "full", scan: slotScanData{Effect: "蓄力伤害增加", Value: "11.81%", Lock: LockPermanent}, want: "蓄力伤害增加 11.81%（T11） （永久锁）"},
		{name: "calibrated", scan: slotScanData{Effect: "蓄力伤害增加", Value: "11.80%", Lock: LockNone}, want: "蓄力伤害增加 11.81%（T11）"},
		{name: "no value", scan: slotScanData{Effect: "防御力增加", Lock: LockNone}, want: "防御力增加"},
		{name: "one-time lock", scan: slotScanData{Effect: "最大装弹数增加", Value: "68.93%", Lock: LockOneTime}, want: "最大装弹数增加 68.93%（T11） （一次性锁）"},
		{name: "empty slot", scan: slotScanData{Effect: "", Value: "10%", Lock: LockNone}, want: "（空槽位）"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatSlotLine(tt.scan, "（空槽位）", lockLabels); got != tt.want {
				t.Fatalf("formatSlotLine() = %q, want %q", got, tt.want)
			}
		})
	}
}
