package crosschapter

import (
	"testing"
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func TestBattleFailCountArmsFallback(t *testing.T) {
	failMu.Lock()
	delete(failByTask, 77)
	delete(exclByTask, 77)
	failMu.Unlock()
	a := &BattleFailCountAction{}
	arg := &maa.CustomActionArg{TaskID: 77}
	if !a.Run(nil, arg) {
		t.Fatal("first count failed")
	}
	failMu.Lock()
	armed := failByTask[77].fails >= fallbackFailThreshold
	failMu.Unlock()
	if armed {
		t.Fatal("armed after one failure")
	}
	time.Sleep(5 * time.Second)
	if !a.Run(nil, arg) {
		t.Fatal("second count failed")
	}
	failMu.Lock()
	f := failByTask[77].fails
	failMu.Unlock()
	if f != 2 {
		t.Fatal("fails:", f)
	}
}

func TestBattleFailCountDedupesBurst(t *testing.T) {
	failMu.Lock()
	delete(failByTask, 78)
	failMu.Unlock()
	a := &BattleFailCountAction{}
	arg := &maa.CustomActionArg{TaskID: 78}
	for i := 0; i < 4; i++ {
		a.Run(nil, arg)
	}
	failMu.Lock()
	f := failByTask[78].fails
	failMu.Unlock()
	if f != 1 {
		t.Fatal("burst dedup failed, fails:", f)
	}
}

func TestBattleNodeExclusionAfterTwoFails(t *testing.T) {
	failMu.Lock()
	delete(failByTask, 79)
	delete(exclByTask, 79)
	failMu.Unlock()
	recordMonsterClick(79, maa.Rect{500, 300, 50, 50})
	a := &BattleFailCountAction{}
	arg := &maa.CustomActionArg{TaskID: 79}
	a.Run(nil, arg) // 第一次战败
	time.Sleep(5 * time.Second)
	recordMonsterClick(79, maa.Rect{505, 305, 50, 50})
	a.Run(nil, arg) // 同一节点第二次战败 → 拉黑
	failMu.Lock()
	excl := append([]maa.Rect(nil), exclByTask[79]...)
	failMu.Unlock()
	if len(excl) != 1 {
		t.Fatal("node not excluded:", excl)
	}
	if n := len(battleNodeExclusions(79)); n != 1 {
		t.Fatal("exclusion readback:", n)
	}
	clearBattleNodeExclusions(79)
	if n := len(battleNodeExclusions(79)); n != 0 {
		t.Fatal("clear failed")
	}
}

func TestFailureClickAndDuplicateAccounting(t *testing.T) {
	const task = 127
	defer delete(failByTask, task)
	defer delete(exclByTask, task)
	recordMonsterClick(task, maa.Rect{500, 300, 50, 50})
	a := &BattleFailCountAction{}
	a.Run(nil, &maa.CustomActionArg{TaskID: task})
	a.Run(nil, &maa.CustomActionArg{TaskID: task})
	s := failByTask[task]
	if s.fails != 1 || s.nodeFails != 1 || len(exclByTask[task]) != 0 {
		t.Fatalf("one defeat was missed or double-counted: %+v exclusions=%v", s, exclByTask[task])
	}
	(&observer{}).OnNodePipelineNode(nil, maa.EventStatusSucceeded, maa.NodePipelineNodeDetail{TaskID: task, Name: "BattleEntered"})
	if s.fails != 1 {
		t.Fatal("battle entry cleared defeats")
	}
	(&observer{}).OnNodePipelineNode(nil, maa.EventStatusSucceeded, maa.NodePipelineNodeDetail{TaskID: task, Name: "BattleConfirmOperationComplete"})
	if s.fails != 0 || s.nodeFails != 0 || s.hadClick {
		t.Fatalf("win retained consecutive failure attribution: %+v", s)
	}
}
