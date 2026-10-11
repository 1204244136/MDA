package crosschapter

import "time"

type Stage string

const (
	Idle        Stage = "idle"
	AwaitList   Stage = "await_list"
	AwaitMap    Stage = "await_map"
	AwaitBattle Stage = "await_battle"
	Done        Stage = "battle_resumed"
	Failed      Stage = "failed"
)

// Scenario counts non-EX Campaign battle nodes for this difficulty/chapter.
// None of the Scenario, Lost Relics, or EX counters is a chapter-clear gate.
type Observation struct {
	Complete                   bool
	HardComplete               bool
	Source, Target             int
	List                       bool
	NewCount, NewNumber        int
	Locked                     bool
	TargetCardCount            int
	Map                        bool
	Current                    int
	Battle                     bool
	Difficulty, ListDifficulty string
}
type Decision struct{ Command, Reason string }
type Machine struct {
	Stage            Stage
	Source, Target   int
	Since, LastClick time.Time
	Clicks, Stable   int
	MonsterClicked   bool
	Reason           string
	Difficulty       string
	CardSelected     bool
}

func New() *Machine { return &Machine{Stage: Idle} }
func (m *Machine) fail(reason string) Decision {
	m.Stage, m.Reason = Failed, reason
	return Decision{"stop", reason}
}
func (m *Machine) Active() bool { return m.Stage != Idle && m.Stage != Done && m.Stage != Failed }

func unlockedNextList(o Observation) bool {
	return o.List && !o.Locked && o.NewCount == 1 && o.Current >= 1 && o.NewNumber == o.Current+1 && o.ListDifficulty != "" && o.ListDifficulty == o.Difficulty
}

// Only controls revalidated in the current frame may be retried.
func (m *Machine) Step(o Observation, now time.Time, stopping bool) Decision {
	if stopping {
		return m.fail("user_stopped")
	}
	if m.Stage == Done || m.Stage == Failed {
		return Decision{}
	}
	if m.Stage == Idle {
		if unlockedNextList(o) {
			m.Source, m.Target, m.Difficulty = o.Current, o.NewNumber, o.ListDifficulty
			m.Stage, m.Since, m.Stable = AwaitList, now, 0
			return Decision{"wait", "unlocked_next_chapter_resume"}
		}
		if !o.Complete && !o.HardComplete {
			m.Stable = 0
			m.Since = time.Time{}
			return Decision{}
		}
		// 困难模式通关：画面只有 MISSION CLEAR 加锁定面板，没有"开启下一章"
		// 提示，点击任意处推进到章节列表；当前章号可直接读到，目标章=当前+1。
		if o.HardComplete && !o.Complete {
			if m.Since.IsZero() {
				m.Since = now
			}
			m.Stable++
			if m.Stable < 2 {
				return Decision{"wait", "hard_completion_stabilizing"}
			}
			m.Stage, m.Since, m.LastClick, m.Clicks, m.Stable = AwaitList, now, now, 1, 0
			if o.Source >= 1 {
				m.Source, m.Target = o.Source, o.Source+1
			}
			if o.Difficulty != "" {
				m.Difficulty = o.Difficulty
			}
			return Decision{"next", "hard_completion_dismiss"}
		}
		if m.Since.IsZero() {
			m.Since = now
		}
		if now.Sub(m.Since) > 10*time.Second {
			return m.fail("completion_identity_unreadable")
		}
		if o.Source < 1 || o.Target != o.Source+1 {
			m.Stable = 0
			return Decision{"wait", "completion_identity_pending"}
		}
		if m.Source != o.Source || m.Target != o.Target {
			m.Source, m.Target, m.Stable = o.Source, o.Target, 0
		}
		m.Stable++
		if m.Stable < 2 {
			return Decision{"wait", "completion_stabilizing"}
		}
		m.Stage, m.Since, m.LastClick, m.Clicks, m.Stable = AwaitList, now, now, 1, 0
		if o.Difficulty != "" {
			m.Difficulty = o.Difficulty
		}
		return Decision{"next", "completion_confirmed"}
	}
	limit := 20 * time.Second
	if m.Stage == AwaitMap {
		limit = 60 * time.Second
	}
	if m.Stage == AwaitBattle {
		limit = 120 * time.Second
	}
	if now.Sub(m.Since) >= limit {
		return m.fail(string(m.Stage) + "_timeout")
	}
	switch m.Stage {
	case AwaitList:
		if o.List {
			m.Stable++
			if m.Stable < 2 {
				return Decision{"wait", "list_stabilizing"}
			}
			if o.ListDifficulty != "" {
				m.Difficulty = o.ListDifficulty
			}
			switch {
			case o.NewCount == 1:
				if o.Locked {
					return m.fail("target_locked")
				}
				// 困难模式通关画面没有下一章章号，Target 以列表里唯一的 NEW 章为准。
				if m.Target == 0 {
					m.Source, m.Target = o.NewNumber-1, o.NewNumber
				}
				if o.NewNumber != m.Target {
					return m.fail("target_number_mismatch")
				}
				m.Stage, m.Since, m.LastClick, m.Clicks, m.Stable = AwaitMap, now, now, 1, 0
				return Decision{"new", "chapter_list_verified"}
			case o.NewCount == 0:
				// 没有 NEW 徽章（进过一次就不再显示）：按章号直接找下一章的卡。
				if m.Target <= 0 {
					return m.fail("target_unknown")
				}
				if o.TargetCardCount == 0 {
					return Decision{"wait", "target_card_not_visible"}
				}
				if o.TargetCardCount > 1 {
					return m.fail("target_card_ambiguous")
				}
				if o.Locked {
					return m.fail("target_locked")
				}
				m.Source = m.Target - 1
				m.CardSelected = true
				m.Stage, m.Since, m.LastClick, m.Clicks, m.Stable = AwaitMap, now, now, 1, 0
				return Decision{"card", "target_card_selected"}
			default:
				return m.fail("new_chapter_ambiguous")
			}
		}
		m.Stable = 0
		if o.Complete && o.Source == m.Source && o.Target == m.Target && now.Sub(m.LastClick) >= 5*time.Second {
			if m.Clicks >= 2 {
				return m.fail("completion_click_no_progress")
			}
			m.LastClick, m.Clicks = now, m.Clicks+1
			return Decision{"next", "retry_visible_completion"}
		}
		return Decision{"wait", "await_list"}
	case AwaitMap:
		// A known list difficulty must be confirmed again on the target map.
		if o.Map && !o.List && !o.Complete && o.Current == m.Target && (m.Difficulty == "" || o.Difficulty == m.Difficulty) {
			m.Stable++
			if m.Stable >= 3 {
				m.Stage, m.Since, m.Stable = AwaitBattle, now, 0
				return Decision{"map", "target_map_verified"}
			}
		} else {
			m.Stable = 0
		}
		if o.List && now.Sub(m.LastClick) >= 5*time.Second {
			matchingDifficulty := o.ListDifficulty == "" || m.Difficulty == "" || o.ListDifficulty == m.Difficulty
			matchingNew := o.NewCount == 1 && o.NewNumber == m.Target
			matchingCard := m.CardSelected && o.NewCount == 0 && o.TargetCardCount == 1
			if o.Locked || !matchingDifficulty || (!matchingNew && !matchingCard) {
				return m.fail("target_changed_before_retry")
			}
			if m.Clicks >= 2 {
				return m.fail("chapter_click_no_progress")
			}
			m.LastClick, m.Clicks = now, m.Clicks+1
			if matchingCard {
				return Decision{"card", "retry_visible_target_card"}
			}
			return Decision{"new", "retry_visible_new_chapter"}
		}
		if o.Map && !o.List && o.Current > 0 && o.Current != m.Source && o.Current != m.Target {
			return m.fail("wrong_map")
		}
		return Decision{"wait", "await_map"}
	case AwaitBattle:
		if o.Battle && m.MonsterClicked {
			m.Stage = Done
			return Decision{"resumed", "battle_hud_verified"}
		}
	}
	return Decision{}
}
