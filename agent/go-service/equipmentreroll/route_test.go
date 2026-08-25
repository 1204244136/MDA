package equipmentreroll

import (
	"strings"
	"testing"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func TestResultRouteTarget(t *testing.T) {
	tests := []struct {
		name   string
		detail string
		want   string
	}{
		{name: "keep", detail: `{"decision":"keep"}`, want: "EquipmentRerollResultClickKeep"},
		{name: "accept", detail: `{"decision":"accept"}`, want: "EquipmentRerollResultClickAccept"},
		{name: "missing decision", detail: `{}`, want: "EquipmentRerollResultClickAccept"},
		{name: "invalid detail", detail: `{`, want: "EquipmentRerollResultClickAccept"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resultRouteTarget(tt.detail); got != tt.want {
				t.Fatalf("resultRouteTarget(%q) = %q, want %q", tt.detail, got, tt.want)
			}
		})
	}
}

func TestLockRouteTargets(t *testing.T) {
	tests := []struct {
		name string
		slot int
		want string
	}{
		{name: "slot 2", slot: 2, want: "EquipmentRerollLockClickSlot2"},
		{name: "slot 3", slot: 3, want: "EquipmentRerollLockClickSlot3"},
		{name: "no slot", slot: 0, want: "EquipmentRerollClickChangeEffect"},
		{name: "invalid slot", slot: 1, want: "EquipmentRerollClickChangeEffect"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := lockRouteTarget(tt.slot); got != tt.want {
				t.Fatalf("lockRouteTarget(%d) = %q, want %q", tt.slot, got, tt.want)
			}
		})
	}
}

func TestKeepLockRouteTargets(t *testing.T) {
	tests := []struct {
		name string
		slot int
		want string
	}{
		{name: "slot 2", slot: 2, want: "EquipmentRerollKeepClickSlot2"},
		{name: "slot 3", slot: 3, want: "EquipmentRerollKeepClickSlot3"},
		{name: "no slot", slot: 0, want: "EquipmentRerollPrepareRerollCost"},
		{name: "invalid slot", slot: 1, want: "EquipmentRerollPrepareRerollCost"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := keepLockRouteTarget(tt.slot); got != tt.want {
				t.Fatalf("keepLockRouteTarget(%d) = %q, want %q", tt.slot, got, tt.want)
			}
		})
	}
}

func TestLockSelectRouteTarget(t *testing.T) {
	if got := lockSelectRouteTarget(1); got != "EquipmentRerollLockSelectModule" {
		t.Fatalf("module route = %q", got)
	}
	for _, materialCode := range []int{0, 2, 99} {
		if got := lockSelectRouteTarget(materialCode); got != "EquipmentRerollLockSelectKey" {
			t.Fatalf("lockSelectRouteTarget(%d) = %q, want key route", materialCode, got)
		}
	}
}

func TestMaterialCheckAndChoosePartRouteTargets(t *testing.T) {
	if got := materialCheckRouteTarget(true, false); got != "EquipmentRerollFinalSummary" {
		t.Fatalf("standalone material check route = %q", got)
	}
	if got := materialCheckRouteTarget(false, false); got != "EquipmentRerollDecide" {
		t.Fatalf("full material check route = %q", got)
	}
	if got := materialCheckRouteTarget(false, true); got != "EquipmentRerollSingleDecide" {
		t.Fatalf("single equipment material check route = %q", got)
	}

	tests := map[string]string{
		"头部": "EquipmentRerollOpenHeadDetails",
		"臂部": "EquipmentRerollOpenArmsDetails",
		"身躯": "EquipmentRerollOpenTorsoDetails",
		"腿部": "EquipmentRerollOpenLegsDetails",
	}
	for part, want := range tests {
		got, ok := choosePartRouteTarget(part)
		if !ok || got != want {
			t.Fatalf("choosePartRouteTarget(%q) = (%q, %v), want (%q, true)", part, got, ok, want)
		}
	}
	if got, ok := choosePartRouteTarget("未知"); ok || got != "" {
		t.Fatalf("unknown part target = (%q, %v), want (\"\", false)", got, ok)
	}
}

func TestScanNextItemsSingleModeStopsAfterSelectedPart(t *testing.T) {
	// 角色模式：非腿部链到下一部位，腿部才做物资检测。
	for part, wantNext := range map[string]string{
		"头部": "EquipmentRerollOpenArmsDetails",
		"臂部": "EquipmentRerollOpenTorsoDetails",
		"身躯": "EquipmentRerollOpenLegsDetails",
	} {
		items, ok := scanNextItems(part, false)
		if !ok {
			t.Fatalf("scanNextItems(%q, false) not ok", part)
		}
		if len(items) != 2 || items[0].Name != "[JumpBack]EquipmentRerollScanCloseDetails" || items[1].Name != wantNext {
			t.Fatalf("scanNextItems(%q, false) = %+v, want close + %s", part, items, wantNext)
		}
	}
	items, ok := scanNextItems("腿部", false)
	if !ok || len(items) != 1 || items[0].Name != "EquipmentRerollMaterialCheckEnter" {
		t.Fatalf("character mode legs route = %+v", items)
	}

	// 单件模式：任意部位扫完都直接进物资检测，不再链到下一件。
	for _, part := range equipmentParts {
		items, ok := scanNextItems(part, true)
		if !ok {
			t.Fatalf("scanNextItems(%q, true) not ok", part)
		}
		if len(items) != 1 || items[0].Name != "EquipmentRerollMaterialCheckEnter" {
			t.Fatalf("single mode %q route = %+v, want material check only", part, items)
		}
	}

	if _, ok := scanNextItems("未知", false); ok {
		t.Fatal("unknown part should not produce a route")
	}
}

func TestRouteActionsRejectNilArguments(t *testing.T) {
	tests := []struct {
		name string
		run  func(*maa.Context, *maa.CustomActionArg) bool
	}{
		{name: "result", run: (&EquipmentRerollResultRouteAction{}).Run},
		{name: "after accept", run: (&EquipmentRerollAfterAcceptRouteAction{}).Run},
		{name: "scan", run: (&EquipmentRerollScanRouteAction{}).Run},
		{name: "lock slot", run: (&EquipmentRerollLockRouteSlotAction{}).Run},
		{name: "lock select", run: (&EquipmentRerollLockSelectRouteAction{}).Run},
		{name: "keep lock", run: (&EquipmentRerollKeepLockRouteSlotAction{}).Run},
		{name: "choose part", run: (&EquipmentRerollChoosePartAction{}).Run},
		{name: "material check", run: (&EquipmentRerollAfterMaterialCheckAction{}).Run},
		{name: "single decide", run: (&EquipmentRerollSingleDecideAction{}).Run},
		{name: "single scan route", run: (&EquipmentRerollSingleScanRouteAction{}).Run},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.run(nil, nil) {
				t.Fatal("Run(nil, nil) = true, want false")
			}
		})
	}
	if err := routeEquipmentRerollEnd(nil, "EquipmentRerollChoosePart"); err == nil {
		t.Fatal("routeEquipmentRerollEnd(nil, ...) returned nil error")
	}
}

func TestBuildFinalSummaryMessageForRouting(t *testing.T) {
	const completeTaskID int64 = 991001
	clearMonitorState(completeTaskID)
	t.Cleanup(func() { clearMonitorState(completeTaskID) })

	for _, part := range equipmentParts {
		for slot := minSlot; slot <= maxSlot; slot++ {
			if _, err := recordEffect(completeTaskID, recordEffectParam{Slot: slot, Part: part, IsLast: slot == maxSlot}, "攻击力增加"); err != nil {
				t.Fatalf("recordEffect(%s, %d): %v", part, slot, err)
			}
		}
	}
	setInventory(completeTaskID, Inventory{CustomModules: 7, CustomLockKeys: 40})
	recordRerollModuleCost(completeTaskID, 3)
	recordLockMaterialCost(completeTaskID, "自订密钥", 0)

	got := buildFinalSummaryMessage(completeTaskID)
	for _, want := range []string{"【装备详情】", "头部:", "腿部:", "攻击力增加", "【消耗材料】", "订制模块 3", "自订密钥 2"} {
		if !strings.Contains(got, want) {
			t.Fatalf("complete summary missing %q: %s", want, got)
		}
	}

	incomplete := buildFinalSummaryMessage(completeTaskID + 1)
	if !strings.Contains(incomplete, "（快照不完整）") {
		t.Fatalf("incomplete summary = %q", incomplete)
	}
}
