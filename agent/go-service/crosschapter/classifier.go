package crosschapter

import (
	"encoding/json"
	"fmt"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"image"
	"image/color"
	"sync"
	"time"
)

type classifyParam struct {
	Entry string `json:"entry"`
	Boss  string `json:"boss"`
	Ex    string `json:"ex"`
}
type ClassifiedMonsterRecognition struct{}

func containsSquadRing(img image.Image, b maa.Rect) bool {
	if img == nil || b[0] > 760 || b[0]+b[2] < 520 || b[1] > 470 || b[1]+b[3] < 220 {
		return false
	}
	count := 0
	for y := max(220, b[1]); y < min(470, b[1]+b[3]); y += 3 {
		for x := max(520, b[0]); x < min(760, b[0]+b[2]); x += 3 {
			r, g, bl, _ := img.At(x, y).RGBA()
			if isSquadOrange(color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(bl >> 8), 255}) {
				count++
			}
		}
	}
	return count >= 8
}
func isSquadOrange(c color.RGBA) bool {
	return c.R > 170 && c.G > 55 && c.G < 190 && c.B < 100 && int(c.R)-int(c.B) > 100
}

type sweepState struct {
	empty, direction, pans, edgePans int
	edge                             maa.Rect
	forceSweep                       bool
	retryPan                         bool
	exNeedsPan                       bool
	pendingUntil                     time.Time
	pendingLimit                     time.Time
	attempts                         int
	view, travelView                 *mapView
	panPending                       bool
	views                            []*mapView
	frame                            image.Image
	bossPhase, bossSeen              bool
	bossPans                         int
	bossPath                         [][2]int
	panMove                          [2]int
}

const maxSearchPans = 48
const maxBossSearchPans = maxSearchPans + 8
const maxUnconfirmedClicks = 3
const battleEntryWait = 8 * time.Second
const maxTravelWait = 30 * time.Second

var sweepMu sync.Mutex
var sweepByTask = map[int64]*sweepState{}

// The game image is normalized to 1280x720 by the controller. A target
// must fit wholly inside the map, away from HUD controls and clipped edges.
func safeMapTarget(b maa.Rect) bool {
	return b[2] > 0 && b[3] > 0 && b[0] >= 32 && b[1] >= 110 &&
		b[0]+b[2] <= 1248 && b[1]+b[3] <= 625 && !(b[0] < 185 && b[1] < 235)
}

func mapSurface(ctx *maa.Context, img image.Image) bool {
	title, err := ctx.RunRecognition("MapPushingMapTitle", img)
	if err != nil || title == nil || !title.Hit {
		return false
	}
	mail, err := ctx.RunRecognition("MapPushingMailVisible", img)
	if err != nil || mail == nil || mail.Hit {
		return false
	}
	minimap, err := ctx.RunRecognition("MapPushingExpandedMapVisible", img)
	return err == nil && minimap != nil && !minimap.Hit
}

func (s *sweepState) needsPan(now time.Time) bool {
	if !s.pendingUntil.IsZero() {
		if now.Before(s.pendingUntil) {
			return false
		}
		s.pendingUntil = time.Time{}
		s.pendingLimit = time.Time{}
		s.travelView = nil
		// Never rescan the same view after a click timeout. Force a camera
		// move first so the stale target cannot consume the retry budget.
		s.empty, s.forceSweep = 0, true
		s.retryPan = true
		s.edge = maa.Rect{}
	}
	return s.exNeedsPan || s.retryPan || s.forceSweep || s.empty > 0
}

func recordMapEvent(task int64, event string, detail any) {
	data, err := json.Marshal(map[string]any{"component": "map_navigation", "time": time.Now(), "task_id": task, "event": event, "detail": detail})
	if err == nil {
		fmt.Println(string(data))
	}
}

func (r *ClassifiedMonsterRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil || arg.Img == nil {
		return nil, false
	}
	var p classifyParam
	if json.Unmarshal([]byte(arg.CustomRecognitionParam), &p) != nil || p.Entry == "" {
		return nil, false
	}
	if !mapSurface(ctx, arg.Img) {
		return nil, false
	}
	sweepMu.Lock()
	st := sweepByTask[arg.TaskID]
	if st == nil {
		st = &sweepState{}
		sweepByTask[arg.TaskID] = st
	}
	st.observeMap(arg.TaskID, sampleMapView(arg.Img))
	st.frame = arg.Img
	st.needsPan(time.Now())
	waiting := !st.pendingUntil.IsZero()
	reposition := st.retryPan || st.exNeedsPan
	exhausted := st.attempts >= maxUnconfirmedClicks
	if exhausted {
		st.forceSweep = true
	}
	sweepMu.Unlock()
	if waiting || exhausted || reposition {
		return nil, false
	}
	d, err := ctx.RunRecognition(p.Entry, arg.Img)
	if err != nil || d == nil {
		return nil, false
	}
	var candidates []*maa.RecognitionResult
	if d.Hit && d.Results != nil {
		candidates = d.Results.Filtered
	}
	bossMarkers, err := excludedMarkers(ctx, arg.Img, p.Boss)
	if err != nil {
		recordMapEvent(arg.TaskID, "map_marker_recognition_failed", err.Error())
		return nil, false
	}
	exMarkers, err := excludedMarkers(ctx, arg.Img, p.Ex)
	if err != nil {
		recordMapEvent(arg.TaskID, "map_marker_recognition_failed", err.Error())
		return nil, false
	}
	markers := append(append([]maa.Rect{}, bossMarkers...), exMarkers...)
	nodeExcl := battleNodeExclusions(arg.TaskID)
	var best *maa.RecognitionResult
	var edge maa.Rect
	for _, candidate := range candidates {
		if candidate == nil {
			continue
		}
		nn, ok := candidate.AsNeuralNetworkDetect()
		if !ok {
			continue
		}
		box := nn.Box
		if containsSquadRing(arg.Img, box) {
			recordMapEvent(arg.TaskID, "normal_candidate_rejected", map[string]any{"reason": "squad_selection_ring", "box": box})
			continue
		}
		if hasExcludedMarker(box, markers) || hasExcludedMarker(box, nodeExcl) {
			continue
		}
		if !safeMapTarget(box) {
			if box[1]+box[3] <= 110 || box[1] >= 625 || box[0]+box[2] <= 32 || box[0] >= 1248 {
				continue
			}
			if edge == (maa.Rect{}) || distance(box) < distance(edge) {
				edge = box
			}
			continue
		}
		if best == nil {
			best = candidate
			continue
		}
		b, _ := best.AsNeuralNetworkDetect()
		if distance(box) < distance(b.Box) {
			best = candidate
		}
	}
	if best == nil {
		if bossBox := confirmedBossTarget(ctx, arg.Img, bossMarkers, exMarkers, nodeExcl); bossBox != (maa.Rect{}) {
			sweepMu.Lock()
			st.bossSeen, st.bossPath = true, nil
			st.empty, st.edge, st.forceSweep = 0, maa.Rect{}, false
			recordMonsterClick(arg.TaskID, bossBox)
			recordMapEvent(arg.TaskID, "boss_candidate", map[string]any{"box": bossBox, "no_visible_ordinary_target": true, "global_clear_proven": false})
			sweepMu.Unlock()
			return &maa.CustomRecognitionResult{Box: bossBox, Detail: `{"class":"boss","safe_map_target":true,"no_visible_ordinary_target":true,"global_clear_proven":false}`}, true
		}
	}
	sweepMu.Lock()
	defer sweepMu.Unlock()
	if best == nil {
		st.beginBossSearch(arg.TaskID)
		st.empty++
		st.edge = edge
		recordMapEvent(arg.TaskID, "map_pan_required", map[string]any{"empty_scans": st.empty, "edge_target": edge})
		return nil, false
	}
	st.empty = 0
	st.forceSweep = false
	st.edge = maa.Rect{}
	nn, _ := best.AsNeuralNetworkDetect()
	recordMonsterClick(arg.TaskID, nn.Box)
	recordMapEvent(arg.TaskID, "normal_candidate", map[string]any{"box": nn.Box})
	return &maa.CustomRecognitionResult{Box: nn.Box, Detail: `{"class":"normal_monster","safe_map_target":true}`}, true
}
func distance(b maa.Rect) int {
	dx := int(b[0]+b[2]/2) - 640
	dy := int(b[1]+b[3]/2) - 360
	return dx*dx + dy*dy
}
