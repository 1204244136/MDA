package equipmentreroll

import (
	"testing"
)

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
		{name: "full", scan: slotScanData{Effect: "蓄力伤害增加", Value: "11.81%", Lock: LockPermanent}, want: "蓄力伤害增加 11.81% （永久锁）"},
		{name: "no value", scan: slotScanData{Effect: "防御力增加", Lock: LockNone}, want: "防御力增加"},
		{name: "one-time lock", scan: slotScanData{Effect: "最大装弹数增加", Value: "68.93%", Lock: LockOneTime}, want: "最大装弹数增加 68.93% （一次性锁）"},
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
