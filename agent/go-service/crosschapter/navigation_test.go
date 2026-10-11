package crosschapter

import (
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"testing"
	"time"
)

func TestDangerousTargetsRequireRepositioning(t *testing.T) {
	for _, b := range []maa.Rect{{1186, 14, 45, 44}, {879, 0, 44, 6}, {168, 161, 45, 50}, {2, 330, 52, 56}, {1100, 640, 80, 50}} {
		if safeMapTarget(b) {
			t.Fatalf("HUD or clipped target accepted: %v", b)
		}
		s := sweepState{edge: b}
		dx, dy := s.panDelta()
		moved := maa.Rect{b[0] + dx, b[1] + dy, b[2], b[3]}
		if !safeMapTarget(moved) {
			t.Fatalf("pan did not bring %v inside map: %v", b, moved)
		}
		if !safeMapTarget(maa.Rect{640 + dx, 360 + dy, 1, 1}) {
			t.Fatalf("unsafe drag endpoint: %d,%d", 640+dx, 360+dy)
		}
	}
}

func TestSearchVisitsOverlappingNeighborViews(t *testing.T) {
	s := sweepState{}
	x, y := 0, 0
	seen := map[[2]int]bool{{0, 0}: true}
	for i := 0; i < maxSearchPans; i++ {
		dx, dy := s.panDelta()
		x += dx
		y += dy
		if seen[[2]int{x, y}] {
			t.Fatalf("step %d revisits %d,%d", i, x, y)
		}
		seen[[2]int{x, y}] = true
	}
}

func TestClickTimeoutRequiresFreshScanWithoutResettingSearch(t *testing.T) {
	now := time.Unix(100, 0)
	s := sweepState{pendingUntil: now.Add(battleEntryWait), attempts: 2, direction: 5, pans: 7}
	if s.needsPan(now) {
		t.Fatal("panned while waiting for battle")
	}
	if !s.needsPan(now.Add(battleEntryWait)) || !s.pendingUntil.IsZero() || !s.forceSweep {
		t.Fatal("timeout must force a fresh camera view before scanning")
	}
	if s.attempts != 2 || s.direction != 5 || s.pans != 7 {
		t.Fatal("unconfirmed click reset search progress")
	}
}

func TestOnlyBattleConfirmationResetsNavigation(t *testing.T) {
	t.Setenv("MDA_MAP_PUSHING_EVIDENCE", t.TempDir())
	sweepByTask[99] = &sweepState{attempts: 2, pans: 7, direction: 9}
	defer delete(sweepByTask, 99)
	o := &observer{}
	o.OnNodePipelineNode(nil, maa.EventStatusSucceeded, maa.NodePipelineNodeDetail{TaskID: 99, Name: "MapPushingClickMonster"})
	if sweepByTask[99].pans != 7 {
		t.Fatal("click reset progress before battle")
	}
	o.OnNodePipelineNode(nil, maa.EventStatusSucceeded, maa.NodePipelineNodeDetail{TaskID: 99, Name: "BattleEntered"})
	s := sweepByTask[99]
	if s.attempts != 0 || s.pans != 0 || s.direction != 9 {
		t.Fatal("battle must reset attempt budget and preserve exploration direction")
	}
}

func TestMinimapRecoveryRequiresPanWithoutResettingBudget(t *testing.T) {
	sweepByTask[100] = &sweepState{attempts: 2, pans: 7, pendingUntil: time.Now().Add(battleEntryWait)}
	defer delete(sweepByTask, 100)
	(&observer{}).OnNodeAction(nil, maa.EventStatusSucceeded, maa.NodeActionDetail{TaskID: 100, Name: "MapPushingCloseExpandedMap"})
	s := sweepByTask[100]
	if !s.needsPan(time.Now()) || s.pans != 7 || s.attempts != 2 {
		t.Fatalf("recovery lost navigation state: %+v", s)
	}
}

// Replay the failing run: quick wins bypass BattleEntered, then another map
// target is clicked. Successful settlements must not consume the retry budget.
func TestQuickBattleSettlementsResetNavigation(t *testing.T) {
	const task = 103
	view := &mapView{}
	s := &sweepState{direction: 9, views: []*mapView{view}, view: view}
	sweepByTask[task] = s
	defer delete(sweepByTask, task)
	o := &observer{}
	for i := 0; i < 2; i++ {
		o.OnNodeAction(nil, maa.EventStatusSucceeded, maa.NodeActionDetail{TaskID: task, Name: "MapPushingClickMonster"})
		s.pans, s.empty, s.edgePans = 5, 1, 2
		s.forceSweep, s.panPending = true, true
		s.edge = maa.Rect{1, 300, 10, 10}
		// Clicking the quick-battle button alone is not confirmation.
		o.OnNodePipelineNode(nil, maa.EventStatusSucceeded, maa.NodePipelineNodeDetail{TaskID: task, Name: "BattleEnterQuickBattle"})
		if s.attempts != 1 || s.pendingUntil.IsZero() {
			t.Fatal("button click prematurely reset navigation")
		}
		o.OnNodePipelineNode(nil, maa.EventStatusFailed, maa.NodePipelineNodeDetail{TaskID: task, Name: "BattleConfirmOperationComplete"})
		if s.attempts != 1 {
			t.Fatal("failed settlement reset navigation")
		}
		for repeat := 0; repeat < 2; repeat++ {
			o.OnNodePipelineNode(nil, maa.EventStatusSucceeded, maa.NodePipelineNodeDetail{TaskID: task, Name: "BattleConfirmOperationComplete"})
		}
		if s.attempts != 0 || s.pans != 0 || !s.pendingUntil.IsZero() || !s.pendingLimit.IsZero() || s.travelView != nil || s.needsPan(time.Now()) || s.panPending || s.edgePans != 0 || s.edge != (maa.Rect{}) {
			t.Fatalf("quick win left navigation pending: %+v", s)
		}
		if s.direction != 9 || len(s.views) != 1 || s.views[0] != view || s.view != view {
			t.Fatal("quick win erased exploration history")
		}
	}
	o.OnNodeAction(nil, maa.EventStatusSucceeded, maa.NodeActionDetail{TaskID: task, Name: "MapPushingClickMonster"})
	if s.attempts != 1 {
		t.Fatalf("next target counted as attempt %d, want 1", s.attempts)
	}
}

func TestQuickSettlementCompletesOnlyClickedNewMap(t *testing.T) {
	const task = 126
	defer delete(state.sessions, task)
	for _, tc := range []struct {
		name    string
		stage   Stage
		clicked bool
		event   maa.EventStatus
		node    string
		want    Stage
	}{
		{"quick_win", AwaitBattle, true, maa.EventStatusSucceeded, "BattleConfirmOperationComplete", Done},
		{"failed_settlement", AwaitBattle, true, maa.EventStatusFailed, "BattleConfirmOperationComplete", AwaitBattle},
		{"button_only", AwaitBattle, true, maa.EventStatusSucceeded, "BattleEnterQuickBattle", AwaitBattle},
		{"no_new_map_click", AwaitBattle, false, maa.EventStatusSucceeded, "BattleConfirmOperationComplete", AwaitBattle},
		{"old_map", AwaitMap, true, maa.EventStatusSucceeded, "BattleConfirmOperationComplete", AwaitMap},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &session{Machine: &Machine{Stage: tc.stage, MonsterClicked: tc.clicked, Since: time.Now()}, Dir: t.TempDir()}
			state.sessions[task] = s
			(&observer{}).OnNodePipelineNode(nil, tc.event, maa.NodePipelineNodeDetail{TaskID: task, Name: tc.node})
			if s.Machine.Stage != tc.want {
				t.Fatalf("stage=%s want %s", s.Machine.Stage, tc.want)
			}
		})
	}
}
