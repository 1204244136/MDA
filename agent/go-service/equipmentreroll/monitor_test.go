package equipmentreroll

import (
	"testing"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func TestNormalizeEffect(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
		ok   bool
	}{
		{name: "effect with brackets and value", raw: "[蓄力伤害增加] 11.81%", want: "蓄力伤害增加", ok: true},
		{name: "effect with range suffix", raw: "【优越代码伤害增加】（9.54% ~ 29.16%）10%", want: "优越代码伤害增加", ok: true},
		{name: "runtime truncated number", raw: "【蓄力伤害增加】 1]", want: "蓄力伤害增加", ok: true},
		{name: "runtime punctuation suffix", raw: "【蓄力速度增加】.", want: "蓄力速度增加", ok: true},
		{name: "runtime missing opening bracket", raw: "优越代码伤害增加】", want: "优越代码伤害增加", ok: true},
		{name: "effect with spaces", raw: "最大 装弹数 增加 68.93%", want: "最大装弹数增加", ok: true},
		{name: "unobtained effect is empty", raw: "未获得效果", want: "", ok: false},
		{name: "one OCR character error", raw: "蓄力伤害增力", want: "蓄力伤害增加", ok: true},
		{name: "unknown effect", raw: "效果变更", want: "", ok: false},
		{name: "empty effect", raw: "", want: "", ok: false},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := normalizeEffect(tt.raw)
			if got != tt.want || ok != tt.ok {
				t.Fatalf("normalizeEffect(%q) = (%q, %v), want (%q, %v)", tt.raw, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestIsUnobtainedEffect(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{name: "plain label", raw: "未获得效果", want: true},
		{name: "label with spaces", raw: "未 获得 效果", want: true},
		{name: "effect text", raw: "蓄力速度增加 4.92%", want: false},
		{name: "unrecognized description", raw: "效果数值将在进入战斗时生效", want: false},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := isUnobtainedEffect(tt.raw); got != tt.want {
				t.Fatalf("isUnobtainedEffect(%q) = %v, want %v", tt.raw, got, tt.want)
			}
		})
	}
}

func TestDisplayEffectSlotsFillsEmptyEntries(t *testing.T) {
	effects := displayEffectSlots([maxSlot]string{"蓄力速度增加", "", "命中率增加"}, "（空槽位）")
	want := [maxSlot]string{"蓄力速度增加", "（空槽位）", "命中率增加"}
	if effects != want {
		t.Fatalf("displayEffectSlots() = %v, want %v", effects, want)
	}
}

func TestNormalizeEffectRecognizesAllOfficialEffects(t *testing.T) {
	for _, effect := range officialEffects {
		got, ok := normalizeEffect("【" + effect + "】 11.81%")
		if !ok || got != effect {
			t.Fatalf("normalizeEffect(%q) = (%q, %v)", effect, got, ok)
		}
	}
}

func TestRecordEffectRequiresOrderedSlots(t *testing.T) {
	const taskID int64 = 1001
	clearMonitorState(taskID)
	t.Cleanup(func() { clearMonitorState(taskID) })

	_, err := recordEffect(taskID, recordEffectParam{Slot: 2, Part: "头部"}, "攻击力增加")
	if err == nil {
		t.Fatal("recordEffect() accepted slot 2 without slot 1")
	}
}

func TestScanBeginInitializesGenericPartState(t *testing.T) {
	const taskID int64 = 1004
	clearMonitorState(taskID)
	t.Cleanup(func() { clearMonitorState(taskID) })

	if err := beginScan(taskID, "身躯"); err != nil {
		t.Fatalf("beginScan() failed: %v", err)
	}
	part, ok := currentEffectPart(taskID)
	if !ok || part != "身躯" {
		t.Fatalf("currentEffectPart() = (%q, %v), want (身躯, true)", part, ok)
	}
}

func TestScanNextItems(t *testing.T) {
	tests := []struct {
		name          string
		part          string
		stopAfterScan bool
		wantCount     int
		wantFirst     string
		wantSecond    string
	}{
		{name: "head", part: "头部", wantCount: 2, wantFirst: "[JumpBack]EquipmentRerollScanCloseDetails", wantSecond: "EquipmentRerollOpenArmsDetails"},
		{name: "arm", part: "臂部", wantCount: 2, wantFirst: "[JumpBack]EquipmentRerollScanCloseDetails", wantSecond: "EquipmentRerollOpenTorsoDetails"},
		{name: "body", part: "身躯", wantCount: 2, wantFirst: "[JumpBack]EquipmentRerollScanCloseDetails", wantSecond: "EquipmentRerollOpenLegsDetails"},
		{name: "leg full flow", part: "腿部", wantCount: 2, wantFirst: "[JumpBack]EquipmentRerollScanCloseDetails", wantSecond: "EquipmentRerollDecide"},
		{name: "leg standalone stops", part: "腿部", stopAfterScan: true, wantCount: 1, wantFirst: "EquipmentRerollScanCloseDetails"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			next, ok := scanNextItems(tt.part, tt.stopAfterScan)
			if !ok || len(next) != tt.wantCount || next[0].Name != tt.wantFirst {
				t.Fatalf("scanNextItems(%q, %v) = (%v, %v)", tt.part, tt.stopAfterScan, next, ok)
			}
			if tt.wantCount == 2 && next[1].Name != tt.wantSecond {
				t.Fatalf("scanNextItems(%q) second = %q, want %q", tt.part, next[1].Name, tt.wantSecond)
			}
		})
	}
}

func TestRecordEffectCompletesAndRetainsTaskSnapshot(t *testing.T) {
	const taskID int64 = 1002
	clearMonitorState(taskID)
	t.Cleanup(func() { clearMonitorState(taskID) })

	inputs := []struct {
		params recordEffectParam
		effect string
	}{
		{params: recordEffectParam{Slot: 1, Part: "臂部"}, effect: "蓄力伤害增加"},
		{params: recordEffectParam{Slot: 2, Part: "臂部"}, effect: "蓄力速度增加"},
		{params: recordEffectParam{Slot: 3, Part: "臂部", IsLast: true}, effect: ""},
	}

	var state monitorState
	for _, input := range inputs {
		var err error
		state, err = recordEffect(taskID, input.params, input.effect)
		if err != nil {
			t.Fatalf("recordEffect(slot %d) failed: %v", input.params.Slot, err)
		}
	}
	want := [maxSlot]string{"蓄力伤害增加", "蓄力速度增加", ""}
	if state.Effects != want {
		t.Fatalf("effects = %v, want %v", state.Effects, want)
	}

	parts, ok := GetEquipmentEffects(taskID)
	if ok {
		t.Fatal("partial task snapshot should not be complete")
	}

	for _, part := range equipmentParts {
		for slot := minSlot; slot <= maxSlot; slot++ {
			params := recordEffectParam{Slot: slot, Part: part, IsLast: slot == maxSlot}
			if _, err := recordEffect(taskID, params, "攻击力增加"); err != nil {
				t.Fatalf("recordEffect(%s, slot %d) failed: %v", part, slot, err)
			}
		}
	}
	parts, ok = GetEquipmentEffects(taskID)
	if !ok {
		t.Fatal("complete task snapshot was not available")
	}
	for _, part := range equipmentParts {
		if parts[part] != [maxSlot]string{"攻击力增加", "攻击力增加", "攻击力增加"} {
			t.Fatalf("snapshot[%s] = %v", part, parts[part])
		}
	}
}

func TestTaskLifecycleClearsSnapshot(t *testing.T) {
	const taskID int64 = 1003
	clearMonitorState(taskID)
	t.Cleanup(func() { clearMonitorState(taskID) })

	for _, part := range equipmentParts {
		for slot := minSlot; slot <= maxSlot; slot++ {
			if _, err := recordEffect(taskID, recordEffectParam{Slot: slot, Part: part, IsLast: slot == maxSlot}, "攻击力增加"); err != nil {
				t.Fatalf("recordEffect(%s, slot %d) failed: %v", part, slot, err)
			}
		}
	}
	if _, ok := GetEquipmentEffects(taskID); !ok {
		t.Fatal("snapshot was not complete before lifecycle cleanup")
	}

	(&taskLifecycle{}).OnTaskerTask(nil, maa.EventStatusSucceeded, maa.TaskerTaskDetail{TaskID: uint64(taskID), Entry: "EquipmentRerollMain"})
	if _, ok := GetEquipmentEffects(taskID); ok {
		t.Fatal("snapshot survived task completion")
	}
}

func TestUpdatePartEffectsRefreshesSnapshot(t *testing.T) {
	const taskID int64 = 2001
	clearMonitorState(taskID)
	t.Cleanup(func() { clearMonitorState(taskID) })

	// 模拟首次全量扫描记录四件装备
	for _, part := range equipmentParts {
		for slot := minSlot; slot <= maxSlot; slot++ {
			if _, err := recordEffect(taskID, recordEffectParam{Slot: slot, Part: part, IsLast: slot == maxSlot}, "攻击力增加"); err != nil {
				t.Fatalf("recordEffect(%s, slot %d) failed: %v", part, slot, err)
			}
		}
	}

	// 决策刷新：标记洗头部，接受变更 → 头部新词条含优越代码伤害增加
	if err := setCurrentPart(taskID, "头部"); err != nil {
		t.Fatalf("setCurrentPart failed: %v", err)
	}
	updatePartEffects(taskID, "头部", [maxSlot]string{"优越代码伤害增加", "", ""}, [maxSlot]string{"10.00%", "", ""})

	parts, ok := GetEquipmentEffects(taskID)
	if !ok {
		t.Fatal("snapshot was not complete after update")
	}
	if !PartHasEffect(parts["头部"], "优越代码伤害增加") {
		t.Fatal("头部 should be satisfied after accepted change")
	}
	if PartHasEffect(parts["臂部"], "优越代码伤害增加") {
		t.Fatal("臂部 should still be unsatisfied")
	}

	// 完整快照应包含数值与锁定关系（供后续任务使用）。
	scans, ok := GetEquipmentSlotScans(taskID)
	if !ok {
		t.Fatal("full slot scans were not available after update")
	}
	head := scans["头部"]
	if head.Slots[0].Value != "10.00%" {
		t.Fatalf("头部 slot1 value = %q, want 10.00%%", head.Slots[0].Value)
	}
	if head.Slots[0].Lock != LockNone {
		t.Fatalf("头部 slot1 lock = %v, want LockNone", head.Slots[0].Lock)
	}
}
