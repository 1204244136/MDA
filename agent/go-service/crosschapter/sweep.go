package crosschapter

import (
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"time"
)

// Generate an expanding spiral without wrapping a fixed 24-step table.
func searchStep(index int) (dx, dy, remaining int) {
	directions := [4][2]int{{-1, 0}, {0, -1}, {1, 0}, {0, 1}}
	for leg := 0; ; leg++ {
		length := leg/2 + 1
		if index < length {
			d := directions[leg%4]
			return d[0] * 480, d[1] * 300, length - index
		}
		index -= length
	}
}

func (s *sweepState) panDelta() (int, int) {
	if s.bossPhase && len(s.bossPath) > 0 {
		last := s.bossPath[len(s.bossPath)-1]
		return -last[0], -last[1]
	}
	if s.edge != (maa.Rect{}) && s.edgePans < 2 {
		s.edgePans++
		return max(-300, min(300, 640-s.edge[0]-s.edge[2]/2)), max(-240, min(240, 360-s.edge[1]-s.edge[3]/2))
	}
	dx, dy, _ := searchStep(s.direction)
	s.direction++
	return dx, dy
}

// Delta always means map-content movement, identical to pointer movement.
func panGesture(dx, dy int) (begin, end [2]int) {
	begin = [2]int{1000, 510}
	if dx > 0 {
		begin[0] -= dx
	}
	if dy > 0 {
		begin[1] -= dy
	}
	end = [2]int{begin[0] + dx, begin[1] + dy}
	return
}

func clearPanGesture(dx, dy int, targets []maa.Rect) (begin, end [2]int, ok bool) {
	b, e := panGesture(dx, dy)
	for _, offset := range [][2]int{{0, 0}, {-160, 0}, {100, 0}, {0, -60}, {-160, -60}, {100, -60}} {
		begin, end = [2]int{b[0] + offset[0], b[1] + offset[1]}, [2]int{e[0] + offset[0], e[1] + offset[1]}
		if !safeMapTarget(maa.Rect{begin[0], begin[1], 1, 1}) || !safeMapTarget(maa.Rect{end[0], end[1], 1, 1}) {
			continue
		}
		ok = true
		for _, box := range targets {
			for _, p := range [][2]int{begin, end} {
				if p[0] >= box[0]-32 && p[0] <= box[0]+box[2]+32 && p[1] >= box[1]-32 && p[1] <= box[1]+box[3]+32 {
					ok = false
				}
			}
		}
		if ok {
			return
		}
	}
	return begin, end, false
}

type SweepReadyRecognition struct{}

type WaitingForBattleRecognition struct{}

func (*WaitingForBattleRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if arg == nil {
		return nil, false
	}
	// Modal/battle nodes have priority; only map frames may extend the wait.
	var view *mapView
	if ctx != nil && arg.Img != nil && mapSurface(ctx, arg.Img) {
		view = sampleMapView(arg.Img)
	}
	sweepMu.Lock()
	defer sweepMu.Unlock()
	s := sweepByTask[arg.TaskID]
	if s == nil || !s.waitForTravel(time.Now(), view) {
		return nil, false
	}
	return &maa.CustomRecognitionResult{Box: maa.Rect{}, Detail: "waiting_for_battle"}, true
}

func (*SweepReadyRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil || arg.Img == nil || !mapSurface(ctx, arg.Img) {
		return nil, false
	}
	sweepMu.Lock()
	defer sweepMu.Unlock()
	s := sweepByTask[arg.TaskID]
	if s == nil || !s.needsPan(time.Now()) {
		return nil, false
	}
	s.frame = arg.Img
	return &maa.CustomRecognitionResult{Box: maa.Rect{0, 0, 1280, 720}, Detail: "sweep_ready"}, true
}

type SweepAction struct{}

func (s *sweepState) beginBossSearch(task int64) {
	if s.bossPhase {
		return
	}
	s.bossPhase = true
	s.edge, s.edgePans = maa.Rect{}, 0
	recordMapEvent(task, "no_visible_ordinary_target", map[string]any{"pans": s.pans, "global_clear_proven": false, "boss_return_steps": len(s.bossPath)})
}

func (*SweepAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		return false
	}
	sweepMu.Lock()
	s := sweepByTask[arg.TaskID]
	if s == nil {
		s = &sweepState{}
		sweepByTask[arg.TaskID] = s
	}
	s.beginBossSearch(arg.TaskID)
	if s.bossPans >= maxBossSearchPans || s.attempts >= maxUnconfirmedClicks {
		recordMapEvent(arg.TaskID, "map_search_failed", map[string]any{"pans": s.pans, "boss_pans": s.bossPans, "unconfirmed_clicks": s.attempts, "global_clear_proven": false})
		sweepMu.Unlock()
		return false
	}
	dx, dy := s.panDelta()
	s.pans++
	if s.bossPhase {
		s.bossPans++
	}
	s.empty, s.edge, s.forceSweep = 0, maa.Rect{}, false
	pan := s.pans
	frame := s.frame
	sweepMu.Unlock()
	// Keep press/release away from detected monsters, bosses and mechanisms.
	if frame == nil {
		return false
	}
	raw, err := ctx.RunRecognition("MapPushingClickMonsterRaw", frame, map[string]any{"MapPushingClickMonsterRaw": map[string]any{"recognition": map[string]any{"param": map[string]any{"expected": []int{0, 1, 2, 3}}}}})
	if err != nil || raw == nil {
		return false
	}
	var boxes []maa.Rect
	if raw.Results != nil {
		for _, result := range raw.Results.Filtered {
			if result != nil {
				if nn, ok := result.AsNeuralNetworkDetect(); ok {
					boxes = append(boxes, nn.Box)
				}
			}
		}
	}
	begin, end, clear := clearPanGesture(dx, dy, boxes)
	if !clear {
		recordMapEvent(arg.TaskID, "map_search_failed", map[string]any{"reason": "no_clear_drag_endpoints"})
		return false
	}
	override := map[string]any{"MapPushingPan": map[string]any{"action": map[string]any{"param": map[string]any{"begin": begin, "end": end, "duration": 400, "end_hold": 0}}}}
	d, err := ctx.RunAction("MapPushingPan", maa.Rect{185, 110, 910, 515}, "", override)
	ok := err == nil && d != nil && d.Success
	sweepMu.Lock()
	s.panPending = ok
	if ok {
		s.exNeedsPan = false
	}
	s.panMove = [2]int{dx, dy}
	sweepMu.Unlock()
	recordMapEvent(arg.TaskID, "map_pan", map[string]any{"pan": pan, "delta": []int{dx, dy}, "begin": begin, "end": end, "success": ok})
	return ok
}
