package crosschapter

import (
	"testing"
	"time"
)

func TestNoNewCardRetry(t *testing.T) {
	for _, difficulty := range []string{"normal", "hard"} {
		t.Run(difficulty, func(t *testing.T) {
			now := time.Unix(100, 0)
			m := New()
			m.Stage, m.Source, m.Target, m.Since = AwaitList, 27, 28, now
			list := Observation{List: true, TargetCardCount: 1, ListDifficulty: difficulty}
			m.Step(list, now, false)
			if d := m.Step(list, now.Add(time.Second), false); d.Command != "card" {
				t.Fatal(d)
			}
			if d := m.Step(list, now.Add(7*time.Second), false); d.Command != "card" || m.Stage != AwaitMap {
				t.Fatalf("safe matching card retry rejected: %+v %s", d, m.Stage)
			}
			if d := m.Step(list, now.Add(13*time.Second), false); d.Command != "stop" || d.Reason != "chapter_click_no_progress" {
				t.Fatal(d)
			}
		})
	}
}

func TestNoNewCardRetryRejectsChangedList(t *testing.T) {
	for _, list := range []Observation{
		{List: true, TargetCardCount: 0},
		{List: true, TargetCardCount: 2},
		{List: true, TargetCardCount: 1, Locked: true},
		{List: true, TargetCardCount: 1, ListDifficulty: "normal"},
		{List: true, TargetCardCount: 1, NewCount: 1, NewNumber: 29},
	} {
		now := time.Unix(100, 0)
		m := New()
		m.Stage, m.Source, m.Target, m.Since = AwaitList, 27, 28, now
		original := Observation{List: true, TargetCardCount: 1, ListDifficulty: "hard"}
		m.Step(original, now, false)
		m.Step(original, now.Add(time.Second), false)
		if d := m.Step(list, now.Add(7*time.Second), false); d.Command != "stop" {
			t.Fatalf("unsafe retry accepted: %+v %+v", list, d)
		}
	}
}

func TestNoNewCardRejectsLockedInitialSelection(t *testing.T) {
	now := time.Unix(100, 0)
	m := New()
	m.Stage, m.Source, m.Target, m.Since = AwaitList, 27, 28, now
	list := Observation{List: true, TargetCardCount: 1, Locked: true, ListDifficulty: "hard"}
	m.Step(list, now, false)
	if d := m.Step(list, now.Add(time.Second), false); d.Command != "stop" || d.Reason != "target_locked" {
		t.Fatal(d)
	}
}
