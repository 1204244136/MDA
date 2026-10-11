package crosschapter

import (
	"image"
	"time"
)

// A sampled background change is evidence of camera activity, not a squad
// locator or a measured world-space translation. Exclude the fixed HUD.
type mapView [64 * 32]uint8

func sampleMapView(img image.Image) *mapView {
	if img == nil || img.Bounds().Dx() != 1280 || img.Bounds().Dy() != 720 {
		return nil
	}
	v := &mapView{}
	b := img.Bounds().Min
	for y := 0; y < 32; y++ {
		for x := 0; x < 64; x++ {
			sum := uint32(0)
			for _, offset := range [][2]int{{0, 0}, {4, 0}, {0, 4}, {4, 4}} {
				r, g, blue, _ := img.At(b.X+200+x*14+offset[0], b.Y+140+y*14+offset[1]).RGBA()
				sum += (299*r + 587*g + 114*blue) / 1000 >> 8
			}
			v[y*64+x] = uint8(sum / 4)
		}
	}
	return v
}

func sameMapView(a, b *mapView) bool {
	if a == nil || b == nil {
		return false
	}
	changed := 0
	for i, value := range a {
		d := int(value) - int(b[i])
		if d > 18 || d < -18 {
			changed++
		}
	}
	return changed*5 < len(a)
}

func (s *sweepState) observeMap(task int64, view *mapView) {
	if view == nil {
		return
	}
	if s.view != nil && !sameMapView(s.view, view) {
		if !s.panPending {
			s.bossSeen, s.bossPath = false, nil
		}
		invalidateBattlePositions(task)
		// A forced EX camera move also invalidates the old click position.
		s.retryPan = false
	}
	if s.panPending {
		status := "changed"
		if s.view == nil {
			status = "unknown"
		} else if sameMapView(s.view, view) {
			status = "unchanged"
		} else {
			for _, seen := range s.views {
				if sameMapView(seen, view) {
					status = "revisited"
					break
				}
			}
		}
		if status == "unchanged" || status == "revisited" {
			// Skip the rest of this leg when it fails to reveal a new view.
			_, _, remaining := searchStep(max(0, s.direction-1))
			s.direction += remaining - 1
			s.edgePans = 2
		} else if status == "changed" {
			// A new camera view is a new retry context. Do not carry a
			// failed screen coordinate into a different map view.
			s.attempts = 0
			s.pendingUntil = time.Time{}
			s.pendingLimit = time.Time{}
			s.travelView = nil
		}
		if s.bossPhase && len(s.bossPath) > 0 {
			if status == "changed" || status == "revisited" {
				s.bossPath = s.bossPath[:len(s.bossPath)-1]
			} else {
				// A boundary prevents reliable reversal of the remaining trail.
				s.bossSeen, s.bossPath = false, nil
			}
		} else if !s.bossPhase && s.bossSeen && (status == "changed" || status == "revisited") {
			s.bossPath = append(s.bossPath, s.panMove)
			if len(s.bossPath) > maxSearchPans {
				s.bossSeen, s.bossPath = false, nil
			}
		}
		recordMapEvent(task, "map_pan_observed", map[string]any{"result": status, "pan": s.pans})
		s.panPending = false
	}
	if s.view == nil || !sameMapView(s.view, view) {
		s.views = append(s.views, view)
		if len(s.views) > maxSearchPans {
			s.views = s.views[1:]
		}
	}
	s.view = view
}

func (s *sweepState) waitForTravel(now time.Time, view *mapView) bool {
	if s.pendingUntil.IsZero() {
		return false
	}
	if !s.pendingLimit.IsZero() && !now.Before(s.pendingLimit) {
		s.pendingUntil = now
		s.needsPan(now)
		return false
	}
	if view != nil {
		if s.travelView != nil && !sameMapView(s.travelView, view) {
			// Camera following can justify extra time, bounded even for animation.
			if !s.pendingLimit.IsZero() {
				s.pendingUntil = minTime(now.Add(battleEntryWait), s.pendingLimit)
			}
		}
		s.travelView = view
	}
	s.needsPan(now)
	return !s.pendingUntil.IsZero()
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
