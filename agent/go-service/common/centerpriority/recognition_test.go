package centerpriority

import (
	"encoding/json"
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

func TestDetectionCandidatesUseAllMatchingMonsters(t *testing.T) {
	// 2026-10-13 19:54:56 maafw.log 的原始检测；框架 Filtered 当时只留下第一项。
	var candidates []resultCandidate
	if err := json.Unmarshal([]byte(`[
		{"box":[125,190,74,62],"cls_index":0,"score":0.805367,"label":"monster"},
		{"box":[194,0,38,15],"cls_index":0,"score":0.634457,"label":"monster"},
		{"box":[202,182,73,72],"cls_index":0,"score":0.751505,"label":"monster"},
		{"box":[256,0,35,23],"cls_index":0,"score":0.415664,"label":"monster"},
		{"box":[265,2,32,28],"cls_index":0,"score":0.359841,"label":"monster"},
		{"box":[501,43,56,90],"cls_index":0,"score":0.765478,"label":"monster"},
		{"box":[568,57,53,62],"cls_index":0,"score":0.771212,"label":"monster"},
		{"box":[841,100,44,67],"cls_index":0,"score":0.361778,"label":"monster"},
		{"box":[855,148,70,82],"cls_index":0,"score":0.522037,"label":"monster"},
		{"box":[856,165,62,64],"cls_index":0,"score":0.616782,"label":"monster"}
	]`), &candidates); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`[0,1]`, `["monster","boss"]`} {
		t.Run(expected, func(t *testing.T) {
			node := `{"recognition":{"type":"NeuralNetworkDetect","param":{"expected":` + expected + `,"threshold":[0.3,0.3]}}}`
			selected, _, ok, err := selectDetectionCandidate(candidates, node, point{X: 640, Y: 360})
			if err != nil || !ok || selected.Box != (maa.Rect{568, 57, 53, 62}) {
				t.Fatalf("nearest lower-confidence monster must remain selectable: selected=%+v ok=%v err=%v", selected, ok, err)
			}
		})
	}
	// 提高 Pipeline 的生效阈值后，较近的低分框必须被排除。
	node := `{"recognition":{"type":"NeuralNetworkDetect","param":{"expected":[0],"threshold":[0.8]}}}`
	selected, _, ok, err := selectDetectionCandidate(candidates, node, point{X: 640, Y: 360})
	if err != nil || !ok || selected.Box != candidates[0].Box {
		t.Fatalf("effective threshold was ignored: selected=%+v ok=%v err=%v", selected, ok, err)
	}
}

func TestDetectionCandidatesRespectPlateStateAndOverrides(t *testing.T) {
	// 来自本次会话的怪物、未开启踏板与已开启踏板实测结果。
	var candidates []resultCandidate
	if err := json.Unmarshal([]byte(`[
		{"box":[570,367,76,76],"cls_index":0,"score":0.6786448,"label":"monster"},
		{"box":[517,348,62,46],"cls_index":2,"score":0.680214,"label":"plate0"},
		{"box":[559,286,56,74],"cls_index":3,"score":0.936960,"label":"plate1"}
	]`), &candidates); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		expected string
		box      maa.Rect
	}{
		{`[2]`, maa.Rect{517, 348, 62, 46}},
		{`[3]`, maa.Rect{559, 286, 56, 74}},
		{`[]`, maa.Rect{570, 367, 76, 76}},
	} {
		node := `{"recognition":{"type":"NeuralNetworkDetect","param":{"expected":` + tc.expected + `,"threshold":[0.3]}}}`
		selected, _, ok, err := selectDetectionCandidate(candidates, node, point{X: 640, Y: 360})
		if err != nil || !ok || selected.Box != tc.box {
			t.Fatalf("filter %s: selected=%+v ok=%v err=%v", tc.expected, selected, ok, err)
		}
	}
	node := `{"recognition":{"type":"NeuralNetworkDetect","param":{"expected":[2],"threshold":[0.7]}}}`
	if _, _, ok, err := selectDetectionCandidate(candidates, node, point{}); err != nil || ok {
		t.Fatalf("empty filtered candidates must not fall back to monster/active plate: ok=%v err=%v", ok, err)
	}
}

func TestDetectionCandidatesRejectInvalidEffectiveFilter(t *testing.T) {
	for _, raw := range []string{
		`{}`,
		`{"recognition":{"type":"TemplateMatch","param":{"expected":[0],"threshold":[0.3]}}}`,
		`{"recognition":{"type":"NeuralNetworkDetect","param":{"expected":[0,1],"threshold":[0.3]}}}`,
		`{"recognition":{"type":"NeuralNetworkDetect","param":{"expected":[null],"threshold":[0.3]}}}`,
		`{"recognition":{"type":"NeuralNetworkDetect","param":{"expected":[-1],"threshold":[0.3]}}}`,
	} {
		if _, _, ok, err := selectDetectionCandidate(nil, raw, point{}); err == nil || ok {
			t.Fatalf("invalid filter must fail: ok=%v err=%v", ok, err)
		}
	}
}
