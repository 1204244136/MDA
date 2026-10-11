package crosschapter

import (
	"encoding/json"
	"fmt"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type session struct {
	Machine     *Machine
	Observation Observation
	Box         maa.Rect
	Frame       image.Image
	Dir         string
	Sequence    int
}
type runtimeState struct {
	mu       sync.Mutex
	sessions map[int64]*session
}

var state = runtimeState{sessions: make(map[int64]*session)}

func jsonUnmarshal(raw string, v any) error { return json.Unmarshal([]byte(raw), v) }

type gate struct{}
type action struct{}
type observer struct{}
type lifecycle struct{}

func (*observer) OnNodeRecognitionNode(*maa.Context, maa.EventStatus, maa.NodeRecognitionNodeDetail) {
}
func (*observer) OnNodeActionNode(*maa.Context, maa.EventStatus, maa.NodeActionNodeDetail)   {}
func (*observer) OnNodeNextList(*maa.Context, maa.EventStatus, maa.NodeNextListDetail)       {}
func (*observer) OnNodeRecognition(*maa.Context, maa.EventStatus, maa.NodeRecognitionDetail) {}
func (*observer) OnNodeAction(_ *maa.Context, status maa.EventStatus, detail maa.NodeActionDetail) {
	if status != maa.EventStatusSucceeded || (detail.Name != "MapPushingClickMonster" && detail.Name != "MapPushingCloseExpandedMap") {
		return
	}
	sweepMu.Lock()
	s := sweepByTask[int64(detail.TaskID)]
	if s == nil {
		s = &sweepState{}
		sweepByTask[int64(detail.TaskID)] = s
	}
	if detail.Name == "MapPushingCloseExpandedMap" {
		s.pendingUntil = time.Time{}
		s.pendingLimit = time.Time{}
		s.travelView = nil
		s.forceSweep = true
		s.edge = maa.Rect{}
		recordMapEvent(int64(detail.TaskID), "map_minimap_closed_reposition_required", map[string]any{"pans": s.pans})
		sweepMu.Unlock()
		return
	}
	s.attempts++
	s.bossSeen, s.bossPath = false, nil
	s.pendingUntil = time.Now().Add(battleEntryWait)
	s.pendingLimit = time.Now().Add(maxTravelWait)
	s.travelView = s.view
	recordMapEvent(int64(detail.TaskID), "map_click_waiting_for_battle", map[string]any{"attempt": s.attempts, "deadline": s.pendingUntil})
	sweepMu.Unlock()
}

func Register() {
	if err := maa.AgentServerRegisterCustomRecognition("NormalFallbackChapter", &NormalFallbackChapterRecognition{}); err != nil {
		log.Fatal().Err(err).Msg("register NormalFallbackChapter")
	}
	if err := maa.AgentServerRegisterCustomRecognition("MapPushingCampaignReady", &CampaignReadyRecognition{}); err != nil {
		log.Fatal().Err(err).Msg("register MapPushingCampaignReady")
	}
	if err := maa.AgentServerRegisterCustomRecognition("MapPushingBossRecognition", &BossMarkerRecognition{}); err != nil {
		log.Fatal().Err(err).Msg("register MapPushingBossRecognition")
	}
	if err := maa.AgentServerRegisterCustomRecognition("CrossChapterGate", &gate{}); err != nil {
		log.Fatal().Err(err).Msg("register CrossChapterGate")
	}
	if err := maa.AgentServerRegisterCustomRecognition("ClassifiedMonsterRecognition", &ClassifiedMonsterRecognition{}); err != nil {
		log.Fatal().Err(err).Msg("register ClassifiedMonsterRecognition")
	}
	if err := maa.AgentServerRegisterCustomRecognition("MapPushingSweepReady", &SweepReadyRecognition{}); err != nil {
		log.Fatal().Err(err).Msg("register MapPushingSweepReady")
	}
	if err := maa.AgentServerRegisterCustomRecognition("MapPushingWaitingForBattle", &WaitingForBattleRecognition{}); err != nil {
		log.Fatal().Err(err).Msg("register MapPushingWaitingForBattle")
	}
	if err := maa.AgentServerRegisterCustomAction("MapPushingSweep", &SweepAction{}); err != nil {
		log.Fatal().Err(err).Msg("register MapPushingSweep")
	}
	if err := maa.AgentServerRegisterCustomAction("BattleExStageGuard", &ExStageGuardAction{}); err != nil {
		log.Fatal().Err(err).Msg("register BattleExStageGuard")
	}
	if err := maa.AgentServerRegisterCustomAction("BattleFailCount", &BattleFailCountAction{}); err != nil {
		log.Fatal().Err(err).Msg("register BattleFailCount")
	}
	if err := maa.AgentServerRegisterCustomRecognition("HardFallbackReady", &HardFallbackReady{}); err != nil {
		log.Fatal().Err(err).Msg("register HardFallbackReady")
	}
	if err := maa.AgentServerRegisterCustomAction("HardFallbackArm", &HardFallbackArmAction{}); err != nil {
		log.Fatal().Err(err).Msg("register HardFallbackArm")
	}
	if err := maa.AgentServerRegisterCustomAction("CrossChapterStep", &action{}); err != nil {
		log.Fatal().Err(err).Msg("register CrossChapterStep")
	}
	maa.AgentServerAddTaskerSink(&lifecycle{})
	maa.AgentServerAddContextSink(&observer{})
}
func (s *session) record(reason string, frame image.Image) {
	if s.Dir == "" {
		s.Dir = filepath.Join("evidence", "cross-chapter", time.Now().Format("20060102-150405.000000"))
		if err := os.MkdirAll(s.Dir, 0755); err != nil {
			log.Error().Err(err).Msg("cross-chapter evidence directory")
			return
		}
	}
	s.Sequence++
	shot := ""
	if frame != nil {
		shot = fmt.Sprintf("%03d-%s.png", s.Sequence, s.Machine.Stage)
		f, err := os.Create(filepath.Join(s.Dir, shot))
		if err == nil {
			err = png.Encode(f, frame)
			closeErr := f.Close()
			if err == nil {
				err = closeErr
			}
		}
		if err != nil {
			log.Error().Err(err).Msg("cross-chapter evidence screenshot")
			shot = ""
		}
	}
	data := map[string]any{"time": time.Now(), "state": s.Machine, "reason": reason, "observation": s.Observation, "screenshot": shot}
	f, err := os.OpenFile(filepath.Join(s.Dir, "events.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err == nil {
		err = json.NewEncoder(f).Encode(data)
		f.Close()
	}
	if err != nil {
		log.Error().Err(err).Msg("cross-chapter evidence log")
	}
	log.Info().Str("component", "CrossChapter").Str("stage", string(s.Machine.Stage)).Int("source", s.Machine.Source).Int("target", s.Machine.Target).Str("reason", reason).Str("evidence", s.Dir).Msg("cross-chapter transition")
}
func (g *gate) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil || arg.Img == nil {
		return nil, false
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	s := state.sessions[arg.TaskID]
	if s == nil {
		s = &session{Machine: New()}
		state.sessions[arg.TaskID] = s
	}
	var param struct {
		ResumeOnly bool `json:"resume_only"`
	}
	if arg.CustomRecognitionParam != "" {
		_ = jsonUnmarshal(arg.CustomRecognitionParam, &param)
	}
	if param.ResumeOnly && s.Machine.Stage != AwaitBattle {
		return nil, false
	}
	if !param.ResumeOnly && s.Machine.Stage == AwaitBattle {
		return nil, false
	}
	if s.Machine.Stage == Failed {
		return &maa.CustomRecognitionResult{Detail: "failed"}, true
	}
	o, box := ReadFrame(ctx, arg.Img, s.Machine.Stage, s.Machine.Target)
	if s.Machine.Stage == Done {
		if !o.Complete && !o.HardComplete && !unlockedNextList(o) {
			return nil, false
		}
		s = &session{Machine: New()}
		state.sessions[arg.TaskID] = s
		clearBattleNodeExclusions(arg.TaskID)
	}
	s.Observation, s.Box, s.Frame = o, box, arg.Img
	active := s.Machine.Active()
	if s.Machine.Stage == AwaitBattle && !o.Battle && time.Since(s.Machine.Since) < 120*time.Second {
		return nil, false
	}
	if !active && !o.Complete && !o.HardComplete && !unlockedNextList(o) {
		s.Machine.Stable = 0
		s.Machine.Since = time.Time{}
		return nil, false
	}
	return &maa.CustomRecognitionResult{Box: box, Detail: fmt.Sprintf("cross_chapter:%s", s.Machine.Stage)}, true
}
func (a *action) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	state.mu.Lock()
	defer state.mu.Unlock()
	s := state.sessions[arg.TaskID]
	if s == nil {
		return false
	}
	before := s.Machine.Stage
	d := s.Machine.Step(s.Observation, time.Now(), ctx.GetTasker().Stopping())
	if d.Command == "map" {
		sweepMu.Lock()
		delete(sweepByTask, arg.TaskID)
		sweepMu.Unlock()
		invalidateBattlePositions(arg.TaskID)
	}
	if before == Failed {
		return false
	}
	if d.Command == "next" || d.Command == "new" || d.Command == "card" {
		node := "CrossChapterClickNext"
		switch d.Command {
		case "new":
			node = "CrossChapterClickNew"
		case "card":
			node = "CrossChapterClickCard"
		}
		result, err := ctx.RunAction(node, s.Box, "")
		if err != nil || result == nil || !result.Success {
			d = s.Machine.fail("click_failed")
		}
	}
	if before != s.Machine.Stage || d.Command == "next" || d.Command == "new" || d.Command == "card" {
		s.record(d.Reason, s.Frame)
	}
	if d.Command == "stop" {
		return false
	}
	return true
}

// 关闭 EX 关卡面板并拒绝出战，回图后重新检查可见目标。
type ExStageGuardAction struct{}

func (*ExStageGuardAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		return false
	}
	d, err := ctx.RunAction("BattleCloseExStage", maa.Rect{824, 366, 20, 20}, "")
	closed := err == nil && d != nil && d.Success
	sweepMu.Lock()
	st := sweepByTask[arg.TaskID]
	if st == nil {
		st = &sweepState{}
		sweepByTask[arg.TaskID] = st
	}
	if closed {
		// A confirmed EX refusal is not a failed attempt to enter an ordinary
		// battle. Refund only the pending click, once, and avoid its position
		// until observeMap invalidates screen coordinates after camera motion.
		if !st.pendingUntil.IsZero() {
			if st.attempts > 0 {
				st.attempts--
			}
			failMu.Lock()
			if f := failByTask[arg.TaskID]; f != nil && f.hadClick && f.lastBox != (maa.Rect{}) {
				exclByTask[arg.TaskID] = append(exclByTask[arg.TaskID], f.lastBox)
				f.hadClick = false
			}
			failMu.Unlock()
		}
		st.pendingUntil, st.pendingLimit = time.Time{}, time.Time{}
		st.travelView = nil
		// Opening the panel can recenter the map. Unlike a stale screen
		// rectangle, this recovery requirement must survive that view change.
		st.exNeedsPan = true
	}
	sweepMu.Unlock()
	recordMapEvent(arg.TaskID, "ex_stage_refused", map[string]any{"closed": closed})
	return closed
}

func (*observer) OnNodePipelineNode(ctx *maa.Context, event maa.EventStatus, detail maa.NodePipelineNodeDetail) {
	if event != maa.EventStatusSucceeded {
		return
	}
	if ctx != nil {
		// MaaFramework reports the selecting parent in detail.Name. Resolve
		// the completed child by ID, including JumpBack and nested battle paths.
		node, err := ctx.GetTasker().GetNodeDetail(int64(detail.NodeID))
		if err != nil || node == nil || !node.RunCompleted {
			log.Warn().Err(err).Uint64("node_id", detail.NodeID).Msg("completed pipeline node unavailable")
			return
		}
		detail.Name = node.Name
	}
	// Quick battles go straight to settlement without passing BattleEntered.
	// Only a successful settlement node confirms that path, not its entry click.
	if event == maa.EventStatusSucceeded && (detail.Name == "BattleEntered" || detail.Name == "BattleConfirmOperationComplete") {
		sweepMu.Lock()
		if s := sweepByTask[int64(detail.TaskID)]; s != nil {
			s.pendingUntil, s.pendingLimit = time.Time{}, time.Time{}
			s.travelView = nil
			s.attempts, s.pans, s.empty, s.edgePans = 0, 0, 0, 0
			s.edge, s.forceSweep, s.panPending = maa.Rect{}, false, false
			s.retryPan = false
			s.exNeedsPan = false
			s.bossPhase, s.bossSeen, s.bossPans, s.bossPath = false, false, 0, nil
		}
		sweepMu.Unlock()
		recordMapEvent(int64(detail.TaskID), "map_battle_confirmed", map[string]any{"source": detail.Name})
		if detail.Name == "BattleConfirmOperationComplete" {
			failMu.Lock()
			if s := failByTask[int64(detail.TaskID)]; s != nil {
				s.fails, s.nodeFails, s.hadClick = 0, 0, false
				s.lastBox, s.failBox = maa.Rect{}, maa.Rect{}
				s.lastFail, s.lastClick = time.Time{}, time.Time{}
			}
			failMu.Unlock()
			state.mu.Lock()
			if s := state.sessions[int64(detail.TaskID)]; s != nil && s.Machine.Stage == AwaitBattle && s.Machine.MonsterClicked {
				s.Machine.Stage = Done
				s.record("new_map_settlement_verified", nil)
			}
			state.mu.Unlock()
		}
	}
	if event != maa.EventStatusSucceeded || detail.Name != "MapPushingClickMonster" {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	s := state.sessions[int64(detail.TaskID)]
	if s != nil && s.Machine.Stage == AwaitBattle && !s.Machine.MonsterClicked {
		s.Machine.MonsterClicked = true
		s.record("new_map_monster_clicked", nil)
	}
}
func (*lifecycle) OnTaskerTask(tasker *maa.Tasker, event maa.EventStatus, detail maa.TaskerTaskDetail) {
	if event == maa.EventStatusStarting {
		return
	}
	sweepMu.Lock()
	delete(sweepByTask, int64(detail.TaskID))
	sweepMu.Unlock()
	failMu.Lock()
	delete(failByTask, int64(detail.TaskID))
	delete(exclByTask, int64(detail.TaskID))
	failMu.Unlock()
	state.mu.Lock()
	defer state.mu.Unlock()
	s := state.sessions[int64(detail.TaskID)]
	if s != nil && s.Machine.Active() {
		reason := "task_ended_before_battle_resumed"
		if tasker.Stopping() {
			reason = "user_or_runner_stopped"
		}
		s.Machine.fail(reason)
		s.record(reason, s.Frame)
	}
	delete(state.sessions, int64(detail.TaskID))
}
