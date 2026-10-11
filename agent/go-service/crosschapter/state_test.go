package crosschapter

import (
	"testing"
	"time"
)

func readyList(t *testing.T) (*Machine, time.Time) {
	t.Helper()
	m := New()
	now := time.Unix(100, 0)
	o := Observation{Complete: true, Source: 26, Target: 27, Difficulty: "hard"}
	m.Step(o, now, false)
	if d := m.Step(o, now.Add(time.Second), false); d.Command != "next" {
		t.Fatal(d)
	}
	return m, now.Add(2 * time.Second)
}
func TestNormalPathRequiresMapAndResumedBattle(t *testing.T) {
	m, now := readyList(t)
	o := Observation{List: true, NewCount: 1, NewNumber: 27}
	m.Step(o, now, false)
	if d := m.Step(o, now.Add(time.Second), false); d.Command != "new" {
		t.Fatal(d)
	}
	for i := 2; i < 5; i++ {
		m.Step(Observation{Map: true, Current: 27, Difficulty: "hard"}, now.Add(time.Duration(i)*time.Second), false)
	}
	if m.Stage != AwaitBattle {
		t.Fatal(m.Stage)
	}
	m.Step(Observation{Battle: true}, now.Add(6*time.Second), false)
	if m.Stage == Done {
		t.Fatal("battle without map interaction accepted")
	}
	m.MonsterClicked = true
	if d := m.Step(Observation{Battle: true}, now.Add(7*time.Second), false); d.Command != "resumed" {
		t.Fatal(d)
	}
}
func TestOrdinaryScreensNeverTrigger(t *testing.T) {
	for _, o := range []Observation{{}, {List: true, NewCount: 1, NewNumber: 27}, {Map: true, Current: 26}, {Battle: true}, {Source: 26, Target: 27}} {
		m := New()
		for i := 0; i < 5; i++ {
			m.Step(o, time.Unix(int64(i), 0), false)
		}
		if m.Stage != Idle {
			t.Fatal(o, m.Stage)
		}
	}
}
func TestUnsafeListStopsWithoutClick(t *testing.T) {
	for _, o := range []Observation{{List: true, NewCount: 2}, {List: true, NewCount: 1, NewNumber: 27, Locked: true}, {List: true, NewCount: 1, NewNumber: 26}, {List: true, NewCount: 0, TargetCardCount: 2}} {
		m, now := readyList(t)
		m.Step(o, now, false)
		d := m.Step(o, now.Add(time.Second), false)
		if d.Command != "stop" || m.Stage != Failed {
			t.Fatal(o, d)
		}
	}
	// 无 NEW 且目标卡暂不可见：等待重识别，由阶段超时兜底停止。
	m, now := readyList(t)
	o := Observation{List: true, NewCount: 0, TargetCardCount: 0}
	m.Step(o, now, false)
	if d := m.Step(o, now.Add(time.Second), false); d.Command != "wait" || m.Stage != AwaitList {
		t.Fatal(d, m.Stage)
	}
}
func TestRetriesRequireVisibleMatchingCompletion(t *testing.T) {
	m, now := readyList(t)
	if d := m.Step(Observation{}, now.Add(6*time.Second), false); d.Command != "wait" {
		t.Fatal(d)
	}
	o := Observation{Complete: true, Source: 26, Target: 27}
	if d := m.Step(o, now.Add(7*time.Second), false); d.Command != "next" {
		t.Fatal(d)
	}
	if d := m.Step(o, now.Add(13*time.Second), false); d.Command != "stop" {
		t.Fatal(d)
	}
}
func TestTimeoutAndCancellation(t *testing.T) {
	for _, stage := range []Stage{AwaitList, AwaitMap, AwaitBattle} {
		m := &Machine{Stage: stage, Since: time.Unix(10, 0)}
		if d := m.Step(Observation{}, time.Unix(200, 0), false); d.Command != "stop" {
			t.Fatal(stage, d)
		}
	}
	m, _ := readyList(t)
	if d := m.Step(Observation{}, time.Now(), true); d.Reason != "user_stopped" {
		t.Fatal(d)
	}
}
func TestHardCompletionCrossesViaListBadge(t *testing.T) {
	now := time.Unix(100, 0)
	m := New()
	// 困难模式通关画面：MISSION CLEAR + 锁定面板，无下一章提示、无章号。
	hard := Observation{HardComplete: true}
	if d := m.Step(hard, now, false); d.Command != "wait" {
		t.Fatal(d)
	}
	if d := m.Step(hard, now.Add(time.Second), false); d.Command != "next" || m.Stage != AwaitList {
		t.Fatal(d, m.Stage)
	}
	// 列表可见后 Target 尚为 0，应以唯一 NEW 章为准并记录难度。
	list := Observation{List: true, ListDifficulty: "hard", NewCount: 1, NewNumber: 28}
	if d := m.Step(list, now.Add(2*time.Second), false); d.Command != "wait" {
		t.Fatal(d)
	}
	if d := m.Step(list, now.Add(3*time.Second), false); d.Command != "new" {
		t.Fatal(d)
	}
	if m.Source != 27 || m.Target != 28 || m.Difficulty != "hard" {
		t.Fatal(m.Source, m.Target, m.Difficulty)
	}
	// 未读到难度时先等待，不能把另一难度的同章号当作目标地图。
	for i := 4; i < 7; i++ {
		m.Step(Observation{Map: true, Current: 28}, now.Add(time.Duration(i)*time.Second), false)
	}
	if m.Stage != AwaitMap {
		t.Fatal("unknown map difficulty accepted", m.Stage)
	}
	// 已知同难度稳定三帧后进入等待战斗。
	for i := 4; i < 8; i++ {
		m.Step(Observation{Map: true, Current: 28, Difficulty: "hard"}, now.Add(time.Duration(i+3)*time.Second), false)
	}
	if m.Stage != AwaitBattle {
		t.Fatal(m.Stage)
	}
}
func TestMissingNewSelectsCardByNumber(t *testing.T) {
	now := time.Unix(100, 0)
	m := New()
	hard := Observation{HardComplete: true, Source: 27}
	m.Step(hard, now, false)
	if d := m.Step(hard, now.Add(time.Second), false); d.Command != "next" {
		t.Fatal(d)
	}
	if m.Source != 27 || m.Target != 28 {
		t.Fatal(m.Source, m.Target)
	}
	list := Observation{List: true, ListDifficulty: "hard", NewCount: 0, TargetCardCount: 1}
	if d := m.Step(list, now.Add(2*time.Second), false); d.Command != "wait" {
		t.Fatal(d)
	}
	if d := m.Step(list, now.Add(3*time.Second), false); d.Command != "card" || m.Stage != AwaitMap {
		t.Fatal(d, m.Stage)
	}
	// 目标卡不在视野内：等待重识别，不立即失败。
	m2 := New()
	m2.Step(hard, now, false)
	m2.Step(hard, now.Add(time.Second), false)
	empty := Observation{List: true, NewCount: 0, TargetCardCount: 0}
	m2.Step(empty, now.Add(2*time.Second), false)
	if d := m2.Step(empty, now.Add(3*time.Second), false); d.Command != "wait" || m2.Stage != AwaitList {
		t.Fatal(d, m2.Stage)
	}
	// 同一章号出现两张：歧义停止。
	m3 := New()
	m3.Step(hard, now, false)
	m3.Step(hard, now.Add(time.Second), false)
	dup := Observation{List: true, NewCount: 0, TargetCardCount: 2}
	m3.Step(dup, now.Add(2*time.Second), false)
	if d := m3.Step(dup, now.Add(3*time.Second), false); d.Command != "stop" {
		t.Fatal(d)
	}
}
