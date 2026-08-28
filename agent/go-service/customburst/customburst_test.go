package customburst

import (
	"testing"
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func TestStageFromCombinedDetailUsesChildBox(t *testing.T) {
	detail := &maa.RecognitionDetail{
		CombinedResult: []*maa.RecognitionDetail{
			{Name: "FastBurstHexGreen", Hit: false, Box: maa.Rect{0, 0, 0, 0}},
			{Name: "FastBurstHexOrange", Hit: false, Box: maa.Rect{1150, 278, 60, 34}},
		},
	}
	if got := stageFromDetail(detail); got != 2 {
		t.Fatalf("stageFromDetail()=%d, want 2", got)
	}
}

func TestSlotFromCombinedDetailUsesFirstMatchedSlot(t *testing.T) {
	detail := &maa.RecognitionDetail{
		CombinedResult: []*maa.RecognitionDetail{
			{Name: "FastBurstSlot1", Box: maa.Rect{0, 0, 0, 0}},
			{Name: "FastBurstSlot2", Box: maa.Rect{1262, 381, 10, 56}},
		},
	}
	if got := slotFromCombinedDetail(detail); got != 2 {
		t.Fatalf("slotFromCombinedDetail()=%d, want 2", got)
	}
}

// neverPressed 返回"从未释放"的状态函数。
func neverPressed(int) bool { return false }

// allPressed 返回"全部已释放"的状态函数。
func allPressed(int) bool { return true }

// axisConfig 构造一个 2 轮测试配置：round0=AAA, round1=AAS。
func axisConfig() burstConfig {
	cfg := defaultBurstConfig()
	cfg.RoundCount = 2
	cfg.Rounds[0] = [maxStages]string{"A", "A", "A"}
	cfg.Rounds[1] = [maxStages]string{"A", "A", "S"}
	return cfg
}

// TestRouteActionRoundAxis 验证多轮轴按检测阶段跟轴的决策。
func TestRouteActionRoundAxis(t *testing.T) {
	cfg := axisConfig()

	cases := []struct {
		name     string
		res      *FastBurstResult
		round    int
		pressed  func(int) bool
		wantAct  string
		wantKey  string
		wantSlot int
	}{
		{
			name:  "第0轮(AAA)Ⅰ阶段就绪A -> 释放A",
			res:   &FastBurstResult{Present: true, Stage: 1, PresentSlots: []int{1}, ReadySlots: []int{1}, ReadyKeys: []string{"A"}},
			round: 0, pressed: neverPressed, wantAct: "release", wantKey: "A", wantSlot: 1,
		},
		{
			name:  "第0轮(AAA)Ⅱ阶段就绪A -> 释放A",
			res:   &FastBurstResult{Present: true, Stage: 2, PresentSlots: []int{1, 2}, ReadySlots: []int{1, 2}, ReadyKeys: []string{"A", "S"}},
			round: 0, pressed: neverPressed, wantAct: "release", wantKey: "A", wantSlot: 1,
		},
		{
			name:  "第1轮(AAS)Ⅲ阶段就绪S -> 释放S",
			res:   &FastBurstResult{Present: true, Stage: 3, PresentSlots: []int{1, 2, 3}, ReadySlots: []int{1, 2, 3}, ReadyKeys: []string{"A", "S", "D"}},
			round: 1, pressed: neverPressed, wantAct: "release", wantKey: "S", wantSlot: 2,
		},
		{
			name:  "第1轮(AAS)Ⅲ阶段S冷却 -> 等待S",
			res:   &FastBurstResult{Present: true, Stage: 3, PresentSlots: []int{1, 2, 3}, CDSlots: []int{2}, ReadySlots: []int{1, 3}, ReadyKeys: []string{"A", "D"}},
			round: 1, pressed: neverPressed, wantAct: "wait", wantKey: "S", wantSlot: 2,
		},
		{
			name:  "第1轮(AAS)Ⅲ阶段S就绪但刚释放过 -> done(不重复释放)",
			res:   &FastBurstResult{Present: true, Stage: 3, PresentSlots: []int{1, 2, 3}, ReadySlots: []int{1, 2, 3}, ReadyKeys: []string{"A", "S", "D"}},
			round: 1, pressed: allPressed, wantAct: "done", wantKey: "S", wantSlot: 0,
		},
	}

	for _, c := range cases {
		act, key, slot := routeAction(c.res, cfg, c.round, c.pressed)
		if act != c.wantAct || key != c.wantKey || slot != c.wantSlot {
			t.Errorf("%s: routeAction=(%q,%q,%d) want=(%q,%q,%d)", c.name, act, key, slot, c.wantAct, c.wantKey, c.wantSlot)
		}
	}
}

// TestDefaultConfig 验证默认配置：单轮、各阶段"不指定"（空串），RoundCount=1。
func TestDefaultConfig(t *testing.T) {
	cfg := defaultBurstConfig()
	if cfg.RoundCount != 1 {
		t.Errorf("default RoundCount=%d want 1", cfg.RoundCount)
	}
	for r := 0; r < maxRounds; r++ {
		for s := 0; s < maxStages; s++ {
			if cfg.Rounds[r][s] != "" {
				t.Errorf("default Rounds[%d][%d]=%q want empty", r, s, cfg.Rounds[r][s])
			}
		}
	}
	for key, wantSlot := range map[string]int{"A": 1, "S": 2, "D": 3} {
		if got := keyToSlot(key); got != wantSlot {
			t.Errorf("keyToSlot(%q)=%d want=%d", key, got, wantSlot)
		}
	}
	if !isFallbackLoopRound(cfg, 0) {
		t.Fatal("default round should use the unlimited ASD fallback loop")
	}
}

func TestFallbackLoopRoundRequiresAllStagesUnspecified(t *testing.T) {
	cfg := defaultBurstConfig()
	for i, key := range []string{"A", "S", "D"} {
		cfg.Rounds[0][i] = key
		if isFallbackLoopRound(cfg, 0) {
			t.Fatalf("round with configured stage %d should not use the ASD fallback loop", i+1)
		}
		cfg.Rounds[0][i] = ""
	}

	cfg.Rounds[0][1] = "S"
	if isFallbackLoopRound(cfg, 0) {
		t.Fatal("mixed round should keep the stage-driven route")
	}
}

func TestFallbackLoopCyclesASDWithoutDetectionState(t *testing.T) {
	tracker := newRoundTracker()
	const taskID int64 = 101
	want := []struct {
		key  string
		slot int
	}{
		{key: "A", slot: 1},
		{key: "S", slot: 2},
		{key: "D", slot: 3},
		{key: "A", slot: 1},
	}

	for i, expected := range want {
		slot := tracker.fallbackSlot(taskID)
		act, key, gotSlot := fallbackLoopAction(slot)
		if act != "release" || key != expected.key || gotSlot != expected.slot {
			t.Fatalf("fallback action %d=(%q,%q,%d), want=(release,%q,%d)", i, act, key, gotSlot, expected.key, expected.slot)
		}
		tracker.advanceFallbackSlot(taskID)
	}
}

// TestRouteActionRespectsRoundCount 验证按 RoundCount 取模后跟轴。
func TestRouteActionRespectsRoundCount(t *testing.T) {
	cfg := defaultBurstConfig()
	cfg.RoundCount = 2
	cfg.Rounds[1][2] = "S"
	res := &FastBurstResult{Present: true, Stage: 3, PresentSlots: []int{1, 2, 3}, ReadySlots: []int{1, 2, 3}, ReadyKeys: []string{"A", "S", "D"}}
	// 第2轮(round=1) Ⅲ阶段 → cfg.Rounds[1][2]=S → 释放 S(槽2)
	act, key, slot := routeAction(res, cfg, 1, neverPressed)
	if act != "release" || key != "S" || slot != 2 {
		t.Errorf("round1 stage3 got=(%q,%q,%d) want=(release,S,2)", act, key, slot)
	}
}

func TestFallbackStageConsumesAllSlots(t *testing.T) {
	state := newSlotState()
	const taskID int64 = 99
	state.setAll(taskID)

	cfg := defaultBurstConfig()
	res := &FastBurstResult{
		Present:      true,
		Stage:        3,
		PresentSlots: []int{2},
		ReadySlots:   []int{2},
		ReadyKeys:    []string{"S"},
	}
	act, key, slot := routeAction(res, cfg, 0, func(slot int) bool {
		return state.get(taskID, slot)
	})
	if act != "none" || key != "" || slot != 0 {
		t.Fatalf("fallback route after stage consumption=(%q,%q,%d), want=(none,\"\",0)", act, key, slot)
	}
	if !state.all(taskID) {
		t.Fatal("setAll should make the fallback state complete")
	}
}

func TestRoundTrackerPredictsAndConfirmsTransition(t *testing.T) {
	tracker := newRoundTracker()
	const taskID int64 = 7

	changed, matched, cancelled := tracker.observeStage(taskID, 1)
	if !changed || matched || cancelled {
		t.Fatalf("first stage observation=(changed:%v matched:%v cancelled:%v), want=(true,false,false)", changed, matched, cancelled)
	}
	if got := tracker.nextStage(taskID); got != 2 {
		t.Fatalf("nextStage after stage1=%d, want 2", got)
	}
	if !tracker.beginTransition(taskID, 2) {
		t.Fatal("beginTransition(2)=false, want true")
	}
	changed, matched, cancelled = tracker.observeStage(taskID, 2)
	if changed || !matched || cancelled {
		t.Fatalf("confirmed predicted stage=(changed:%v matched:%v cancelled:%v), want=(false,true,false)", changed, matched, cancelled)
	}
}

func TestRoundTrackerRetriesTransitionUntilAttempt(t *testing.T) {
	tracker := newRoundTracker()
	const taskID int64 = 11

	if !tracker.beginTransition(taskID, 2) {
		t.Fatal("first transition attempt should be allowed")
	}
	if !tracker.beginTransition(taskID, 2) {
		t.Fatal("transition without a successful key dispatch should be retried")
	}
	tracker.recordAttempt(taskID, 2, 2)
	if tracker.beginTransition(taskID, 2) {
		t.Fatal("transition should be locked after a successful key dispatch")
	}
}

func TestRoundTrackerRetriesRejectedTransitionAndUnchangedStage(t *testing.T) {
	tracker := newRoundTracker()
	const taskID int64 = 8

	tracker.observeStage(taskID, 1)
	if !tracker.beginTransition(taskID, 2) {
		t.Fatal("beginTransition(2)=false, want true")
	}
	changed, matched, cancelled := tracker.observeStage(taskID, 1)
	if changed || matched || !cancelled {
		t.Fatalf("reappeared stage=(changed:%v matched:%v cancelled:%v), want=(false,false,true)", changed, matched, cancelled)
	}

	tracker.lastAttempt[taskID] = stageAttempt{stage: 1, at: time.Now().Add(-phaseRetryInterval)}
	if !tracker.shouldRetry(taskID, 1) {
		t.Fatal("shouldRetry(stage1)=false after retry interval, want true")
	}
	if tracker.shouldRetry(taskID, 2) {
		t.Fatal("shouldRetry(stage2)=true for a stage1 attempt, want false")
	}
}

func TestShouldResetStageDoesNotRetryFinalStage(t *testing.T) {
	if shouldResetStage(maxStages, false, false, false, true) {
		t.Fatal("stage 3 retry should not reset burst state")
	}
	if !shouldResetStage(maxStages, true, false, false, false) {
		t.Fatal("first observation of stage 3 should reset burst state")
	}
	if !shouldResetStage(2, false, true, false, false) {
		t.Fatal("matched prediction of stage 2 should reset burst state")
	}
}

func TestTransitionActionUsesConfiguredOrVerifiedCandidate(t *testing.T) {
	base := &FastBurstResult{Present: true, Stage: 2}

	// 未指定整轮：过渡帧不扫描槽位，也不在阶段色消失窗口盲按。
	cfg := defaultBurstConfig()
	ready := &FastBurstResult{Present: true, Stage: 2, ReadySlots: []int{2}}
	act, key, slot := transitionAction(ready, cfg, 0, 0, 1)
	if act != "none" || key != "" || slot != 0 {
		t.Fatalf("fallback transition=(%q,%q,%d), want=(none,\"\",0)", act, key, slot)
	}

	withCooldown := &FastBurstResult{Present: true, Stage: 2, CDSlots: []int{1}}
	act, key, slot = transitionAction(withCooldown, cfg, 0, 0, 1)
	if act != "none" || key != "" || slot != 0 {
		t.Fatalf("fallback transition without candidate=(%q,%q,%d), want=(none,\"\",0)", act, key, slot)
	}

	cfg.Rounds[0][1] = "D"
	missing := &FastBurstResult{Present: true, Stage: 2}
	act, key, slot = transitionAction(missing, cfg, 0, 0, 1)
	if act != "none" || key != "" || slot != 0 {
		t.Fatalf("configured transition without visible target=(%q,%q,%d), want=(none,\"\",0)", act, key, slot)
	}

	base.ReadySlots = []int{3}
	act, key, slot = transitionAction(base, cfg, 0, 0, 1)
	if act != "release" || key != "D" || slot != 3 {
		t.Fatalf("configured transition=(%q,%q,%d), want=(release,D,3)", act, key, slot)
	}

	act, key, slot = transitionAction(&FastBurstResult{Present: true, Stage: 2, ReadySlots: []int{3}, CDSlots: []int{3}}, cfg, 0, 0, 1)
	if act != "wait" || key != "D" || slot != 3 {
		t.Fatalf("configured cooldown=(%q,%q,%d), want=(wait,D,3)", act, key, slot)
	}
}

func TestRoundTrackerSafetyProbeInterval(t *testing.T) {
	tracker := newRoundTracker()
	const taskID int64 = 9
	if tracker.shouldProbeSafety(taskID) {
		t.Fatal("first safety probe should wait for the interval")
	}
	if tracker.shouldProbeSafety(taskID) {
		t.Fatal("immediate safety probe should be throttled")
	}
	tracker.lastSafetyProbe[taskID] = time.Now().Add(-safetyProbeInterval)
	if !tracker.shouldProbeSafety(taskID) {
		t.Fatal("safety probe after interval should be allowed")
	}
}

func TestRoundTrackerReturnToLowFrequencyIsOneShot(t *testing.T) {
	tracker := newRoundTracker()
	const taskID int64 = 10

	if tracker.takeReturnToLow(taskID) {
		t.Fatal("unmarked low-frequency return should not hit")
	}
	tracker.markReturnToLow(taskID)
	if !tracker.takeReturnToLow(taskID) {
		t.Fatal("marked low-frequency return should hit once")
	}
	if tracker.takeReturnToLow(taskID) {
		t.Fatal("low-frequency return marker should be consumed")
	}
}

func TestRoundTrackerStageConsumedSurvivesTransientAbsence(t *testing.T) {
	tracker := newRoundTracker()
	const taskID int64 = 12

	tracker.setWas(taskID, true)
	tracker.markStageConsumed(taskID, 3)
	if !tracker.stageWasConsumed(taskID, 3) {
		t.Fatal("stage 3 should be marked consumed")
	}
	if tracker.shouldEndAfterAbsent(taskID) {
		t.Fatal("first absent frame should not end the burst")
	}
	tracker.setWas(taskID, true)
	if !tracker.stageWasConsumed(taskID, 3) {
		t.Fatal("transient absence must not clear stage consumption")
	}
	tracker.absentSince[taskID] = time.Now().Add(-panelAbsentGrace)
	if !tracker.shouldEndAfterAbsent(taskID) {
		t.Fatal("absence beyond grace period should end the burst")
	}
}

func TestRoundTrackerResetClearsStageConsumption(t *testing.T) {
	tracker := newRoundTracker()
	const taskID int64 = 13
	tracker.markStageConsumed(taskID, 3)
	tracker.resetLastStage(taskID)
	if tracker.stageWasConsumed(taskID, 3) {
		t.Fatal("new burst cycle must allow stage 3 again")
	}
	if tracker.was(taskID) {
		t.Fatal("reset burst cycle must clear presence state")
	}
}
