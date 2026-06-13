package centerpriority

import (
	"image"
	"testing"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func TestChooseNearest(t *testing.T) {
	center := point{X: 640, Y: 360}
	candidates := []resultCandidate{
		{Box: maa.Rect{0, 0, 100, 100}},
		{Box: maa.Rect{600, 320, 80, 80}},
		{Box: maa.Rect{900, 500, 100, 100}},
	}

	candidate, index, ok := chooseNearest(candidates, center)
	if !ok {
		t.Fatal("expected a candidate")
	}
	if index != 1 {
		t.Fatalf("expected index 1, got %d", index)
	}
	if candidate.Box != (maa.Rect{600, 320, 80, 80}) {
		t.Fatalf("unexpected box: %v", candidate.Box)
	}
}

func TestChooseNearestKeepsOrderOnTie(t *testing.T) {
	center := point{X: 50, Y: 50}
	candidates := []resultCandidate{
		{Box: maa.Rect{40, 0, 20, 20}},
		{Box: maa.Rect{40, 80, 20, 20}},
	}

	candidate, index, ok := chooseNearest(candidates, center)
	if !ok {
		t.Fatal("expected a candidate")
	}
	if index != 0 {
		t.Fatalf("expected first candidate to win tie, got index %d", index)
	}
	if candidate.Box != candidates[0].Box {
		t.Fatalf("unexpected box: %v", candidate.Box)
	}
}

func TestChooseNearestEmpty(t *testing.T) {
	_, _, ok := chooseNearest(nil, point{X: 640, Y: 360})
	if ok {
		t.Fatal("expected empty candidates to fail")
	}
}

func TestResolveCenterDefaultsToImageCenter(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1280, 720))
	center, err := resolveCenter(img, maa.Rect{}, recognitionParam{})
	if err != nil {
		t.Fatalf("resolve center failed: %v", err)
	}
	if center != (point{X: 640, Y: 360}) {
		t.Fatalf("unexpected center: %+v", center)
	}
}

func TestResolveCenterUsesExplicitCenter(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1280, 720))
	params := recognitionParam{Center: []byte(`[100,200]`)}

	center, err := resolveCenter(img, maa.Rect{}, params)
	if err != nil {
		t.Fatalf("resolve center failed: %v", err)
	}
	if center != (point{X: 100, Y: 200}) {
		t.Fatalf("unexpected center: %+v", center)
	}
}

func TestResolveCenterUsesROICenter(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1280, 720))
	params := recognitionParam{Center: []byte(`"roi"`)}

	center, err := resolveCenter(img, maa.Rect{100, 200, 300, 400}, params)
	if err != nil {
		t.Fatalf("resolve center failed: %v", err)
	}
	if center != (point{X: 250, Y: 400}) {
		t.Fatalf("unexpected center: %+v", center)
	}
}

func TestParseParamsDefaults(t *testing.T) {
	params, err := parseParams(`{"entry":"RawNode"}`)
	if err != nil {
		t.Fatalf("parse params failed: %v", err)
	}
	if params.Entry != "RawNode" {
		t.Fatalf("unexpected entry: %q", params.Entry)
	}
	if params.Source != sourceFiltered {
		t.Fatalf("unexpected source: %q", params.Source)
	}
	if params.Fallback != fallbackBest {
		t.Fatalf("unexpected fallback: %q", params.Fallback)
	}
}

func TestFallbackFail(t *testing.T) {
	detail := &maa.RecognitionDetail{Hit: true, Box: maa.Rect{1, 2, 3, 4}}
	_, _, ok := fallbackCandidate(detail, recognitionParam{Fallback: fallbackFail})
	if ok {
		t.Fatal("expected fallback fail to return false")
	}
}
