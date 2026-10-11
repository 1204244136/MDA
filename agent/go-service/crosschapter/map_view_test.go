package crosschapter

import (
	"image"
	"image/draw"
	"image/color"
	"testing"
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func TestPanGestureKeepsAllDirectionsAndEndpoints(t *testing.T) {
	for _, delta := range [][2]int{{-480, 0}, {480, 0}, {0, -300}, {0, 300}, {-300, 240}, {300, -240}} {
		b, e := panGesture(delta[0], delta[1])
		if e[0]-b[0] != delta[0] || e[1]-b[1] != delta[1] {
			t.Fatalf("direction lost: %v -> %v for %v", b, e, delta)
		}
		for _, p := range [][2]int{b, e} {
			if !safeMapTarget(maa.Rect{p[0], p[1], 1, 1}) {
				t.Fatalf("point outside map: %v", p)
			}
		}
	}
}

func TestMapViewDetectsTranslationAndIgnoresHUD(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1280, 720))
	for y := 0; y < 720; y++ {
		for x := 0; x < 1280; x++ {
			shade := uint8(((x/53 + y/71) % 2) * 230)
			img.SetRGBA(x, y, color.RGBA{shade, shade, shade, 255})
		}
	}
	before := sampleMapView(img)
	if before == nil {
		t.Fatal("fixture must use controller coordinates")
	}
	copy := image.NewRGBA(img.Bounds())
	draw.Draw(copy, copy.Bounds(), img, img.Bounds().Min, draw.Src)
	draw.Draw(copy, image.Rect(0, 0, 1280, 100), image.White, image.Point{}, draw.Src)
	if !sameMapView(before, sampleMapView(copy)) {
		t.Fatal("HUD changed map signature")
	}
	draw.Draw(copy, copy.Bounds(), img, image.Pt(80, 0), draw.Src)
	if sameMapView(before, sampleMapView(copy)) {
		t.Fatal("translated map was treated as unchanged")
	}
}

func TestDragAvoidsDetectedTargetAtPressOrRelease(t *testing.T) {
	b, e, ok := clearPanGesture(-480, 0, []maa.Rect{{980, 490, 40, 40}})
	if !ok || b == ([2]int{1000, 510}) || e[0]-b[0] != -480 {
		t.Fatalf("unsafe or reversed drag: %v %v %v", b, e, ok)
	}
	if _, _, ok := clearPanGesture(0, -300, []maa.Rect{{0, 0, 1280, 720}}); ok {
		t.Fatal("accepted completely obstructed endpoints")
	}
}

func TestTravelActivityExtendsWaitButCannotWaitForever(t *testing.T) {
	now := time.Unix(100, 0)
	a, b := &mapView{}, &mapView{}
	for i := range b {
		b[i] = 100
	}
	s := sweepState{pendingUntil: now.Add(battleEntryWait), pendingLimit: now.Add(maxTravelWait), travelView: a, attempts: 1}
	if !s.waitForTravel(now.Add(7*time.Second), b) {
		t.Fatal("lost active travel")
	}
	if !s.waitForTravel(now.Add(10*time.Second), b) {
		t.Fatal("did not extend initial eight seconds")
	}
	if s.waitForTravel(now.Add(16*time.Second), b) || !s.needsPan(now.Add(16*time.Second)) || !s.forceSweep {
		t.Fatal("stalled travel must force a fresh camera view")
	}
	s.pendingUntil, s.pendingLimit, s.travelView = now.Add(maxTravelWait), now.Add(maxTravelWait), a
	if s.waitForTravel(now.Add(maxTravelWait), b) {
		t.Fatal("animation extended travel beyond hard limit")
	}
}

func TestUnchangedPanChangesLegAndPreservesBudget(t *testing.T) {
	s := sweepState{view: &mapView{}, direction: 3, pans: 3, panPending: true}
	s.observeMap(101, &mapView{})
	dx, dy := s.panDelta()
	if dx != 0 || dy != 300 || s.pans != 3 {
		t.Fatalf("did not turn at boundary: %+v", s)
	}
}

func TestViewChangeInvalidatesOldScreenExclusions(t *testing.T) {
	const task = 102
	exclByTask[task] = []maa.Rect{{500, 300, 50, 50}}
	failByTask[task] = &failState{fails: 1, hadClick: true, nodeFails: 1}
	defer delete(exclByTask, task)
	defer delete(failByTask, task)
	s := sweepState{view: &mapView{}}
	other := &mapView{}
	for i := range other {
		other[i] = 100
	}
	s.observeMap(task, other)
	if len(battleNodeExclusions(task)) != 0 || failByTask[task].hadClick || failByTask[task].fails != 1 {
		t.Fatal("view change must invalidate positions but retain total failure count")
	}
}

func TestTimedOutTargetRemainsBlockedUntilViewChanges(t *testing.T) {
	const task = 128
	defer delete(failByTask, task)
	defer delete(exclByTask, task)
	now := time.Now()
	old, next := &mapView{}, &mapView{}
	for i := range next {
		next[i] = 100
	}
	s := sweepState{view: old, views: []*mapView{old}, pendingUntil: now, attempts: 2, pans: 4}
	s.needsPan(now)
	s.panPending = true
	s.observeMap(task, old)
	if !s.retryPan || s.attempts != 2 || !s.needsPan(now) {
		t.Fatal("unchanged drag allowed old target again")
	}
	s.panPending = true
	s.observeMap(task, next)
	if s.retryPan || s.attempts != 0 || s.pans != 4 {
		t.Fatalf("new view did not restore retry context: %+v", s)
	}
	// An unsolicited EX animation also invalidates old coordinates, but is not
	// evidence that a controlled drag succeeded or that any battle was won.
	s.retryPan = true
	s.attempts = 2
	s.observeMap(task, old)
	if s.retryPan || s.attempts != 2 {
		t.Fatal("external camera change used wrong recovery budget")
	}
}
