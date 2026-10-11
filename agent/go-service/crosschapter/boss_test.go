package crosschapter

import (
	"testing"
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func TestBossReturnTrailAndExternalCameraChange(t *testing.T) {
	const task = 2701
	defer delete(failByTask, task)
	defer delete(exclByTask, task)
	a, b, c := &mapView{}, &mapView{}, &mapView{}
	for i := range b {
		b[i], c[i] = 80, 160
	}
	s := sweepState{view: a, bossSeen: true, panPending: true, panMove: [2]int{-480, 0}}
	s.observeMap(task, b)
	s.panPending, s.panMove = true, [2]int{0, -300}
	s.observeMap(task, c)
	s.pans = maxSearchPans
	s.beginBossSearch(task)
	if dx, dy := s.panDelta(); dx != 0 || dy != 300 {
		t.Fatalf("expected reverse last drag, got %d,%d", dx, dy)
	}
	s.panPending = true
	s.observeMap(task, b)
	if dx, dy := s.panDelta(); dx != 480 || dy != 0 {
		t.Fatalf("expected reverse first drag, got %d,%d", dx, dy)
	}
	s.observeMap(task, a)
	if s.bossSeen || len(s.bossPath) != 0 || !s.bossPhase {
		t.Fatal("external camera change must discard trail without resetting search phase")
	}
	s.bossSeen, s.bossPath, s.panPending = true, [][2]int{{-480, 0}}, true
	s.observeMap(task, a)
	if len(s.bossPath) != 0 {
		t.Fatal("boundary must invalidate return trail")
	}
}

func TestBossBattleConfirmationResetsSearch(t *testing.T) {
	for _, name := range []string{"BattleEntered", "BattleConfirmOperationComplete"} {
		const task = 2702
		s := &sweepState{bossPhase: true, bossSeen: true, bossPans: 7, bossPath: [][2]int{{480, 0}}, pans: 55, pendingUntil: time.Now().Add(time.Second)}
		sweepByTask[task] = s
		(&observer{}).OnNodePipelineNode(nil, maa.EventStatusSucceeded, maa.NodePipelineNodeDetail{Name: name, TaskID: task})
		delete(sweepByTask, task)
		if s.bossPhase || s.bossSeen || s.bossPans != 0 || len(s.bossPath) != 0 || s.pans != 0 || !s.pendingUntil.IsZero() {
			t.Fatalf("%s left old boss state", name)
		}
	}
}
