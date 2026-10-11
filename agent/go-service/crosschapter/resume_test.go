package crosschapter

import (
	"testing"
	"time"
)

func TestResumeOnlyAdjacentUnlockedNewChapter(t *testing.T) {
	valid := Observation{List: true, NewCount: 1, NewNumber: 46, Current: 45, Difficulty: "normal", ListDifficulty: "normal"}
	m := New()
	if d := m.Step(valid, time.Now(), false); d.Reason != "unlocked_next_chapter_resume" || m.Source != 45 || m.Target != 46 || m.Stage != AwaitList {
		t.Fatalf("resume=%+v machine=%+v", d, m)
	}
	for name, change := range map[string]func(*Observation){
		"locked":           func(o *Observation) { o.Locked = true },
		"multiple":         func(o *Observation) { o.NewCount = 2 },
		"nonadjacent":      func(o *Observation) { o.NewNumber = 47 },
		"unknown_current":  func(o *Observation) { o.Current = 0 },
		"wrong_difficulty": func(o *Observation) { o.ListDifficulty = "hard" },
		"no_list":          func(o *Observation) { o.List = false },
	} {
		t.Run(name, func(t *testing.T) {
			o := valid
			change(&o)
			m := New()
			if d := m.Step(o, time.Now(), false); d.Command != "" || m.Stage != Idle {
				t.Fatalf("unsafe resume=%+v machine=%+v", d, m)
			}
		})
	}
}

func TestFallbackChapterRequiresUnlockBoundary(t *testing.T) {
	if c, ok := latestUnlockedCard([]chapterCard{{number: 45}, {number: 46}, {number: 47, locked: true}}); !ok || c.number != 46 {
		t.Fatal("latest chapter missed")
	}
	for _, cards := range [][]chapterCard{
		{{number: 46}},
		{{number: 46}, {number: 46}, {number: 47, locked: true}},
		{{number: 46}, {number: 48, locked: true}},
		{{number: 46, locked: true}, {number: 47, locked: true}},
	} {
		if _, ok := latestUnlockedCard(cards); ok {
			t.Errorf("ambiguous boundary accepted: %+v", cards)
		}
	}
}
