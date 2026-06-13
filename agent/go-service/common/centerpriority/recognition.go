package centerpriority

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	componentName = "CenterPriorityRecognition"

	sourceFiltered = "filtered"
	sourceAll      = "all"
	sourceBest     = "best"

	fallbackBest = "best"
	fallbackBox  = "box"
	fallbackFail = "fail"

	centerImage = "image"
	centerROI   = "roi"
)

var _ maa.CustomRecognitionRunner = &CenterPriorityRecognition{}

type recognitionParam struct {
	Entry    string          `json:"entry"`
	Source   string          `json:"source"`
	Center   json.RawMessage `json:"center"`
	Fallback string          `json:"fallback"`
}

type point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type resultCandidate struct {
	Box      maa.Rect `json:"box"`
	Type     string   `json:"type,omitempty"`
	Score    *float64 `json:"score,omitempty"`
	Label    string   `json:"label,omitempty"`
	ClsIndex *uint64  `json:"cls_index,omitempty"`
}

type recognitionDetail struct {
	Entry         string          `json:"entry"`
	Source        string          `json:"source"`
	Fallback      string          `json:"fallback,omitempty"`
	SelectedIndex int             `json:"selected_index"`
	Selected      resultCandidate `json:"selected"`
	Center        point           `json:"center"`
	DistanceSq    float64         `json:"distance_sq"`
}

// CenterPriorityRecognition runs another recognition and returns the result nearest to the image center.
type CenterPriorityRecognition struct{}

func (r *CenterPriorityRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil || arg.Img == nil {
		log.Error().
			Str("component", componentName).
			Msg("invalid recognition context")
		return nil, false
	}

	params, err := parseParams(arg.CustomRecognitionParam)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentName).
			Str("param", arg.CustomRecognitionParam).
			Msg("failed to parse custom recognition param")
		return nil, false
	}
	if params.Entry == "" {
		log.Error().
			Str("component", componentName).
			Str("task", arg.CurrentTaskName).
			Msg("recognition entry is required")
		return nil, false
	}
	if params.Entry == arg.CurrentTaskName {
		log.Error().
			Str("component", componentName).
			Str("task", arg.CurrentTaskName).
			Str("entry", params.Entry).
			Msg("recognition entry cannot reference itself")
		return nil, false
	}

	center, err := resolveCenter(arg.Img, arg.Roi, params)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentName).
			Str("entry", params.Entry).
			Msg("failed to resolve center point")
		return nil, false
	}

	detail, err := ctx.RunRecognition(params.Entry, arg.Img)
	if err != nil {
		log.Warn().
			Err(err).
			Str("component", componentName).
			Str("entry", params.Entry).
			Msg("failed to run wrapped recognition")
		return nil, false
	}
	if detail == nil || !detail.Hit {
		log.Debug().
			Str("component", componentName).
			Str("entry", params.Entry).
			Msg("wrapped recognition missed")
		return nil, false
	}

	selected, index, ok := selectCandidate(detail, params, center)
	if !ok {
		log.Warn().
			Str("component", componentName).
			Str("entry", params.Entry).
			Str("source", params.Source).
			Str("fallback", params.Fallback).
			Msg("no recognition candidate available")
		return nil, false
	}

	distance := distanceSq(selected.Box, center)
	payload := recognitionDetail{
		Entry:         params.Entry,
		Source:        params.Source,
		Fallback:      params.Fallback,
		SelectedIndex: index,
		Selected:      selected,
		Center:        center,
		DistanceSq:    distance,
	}
	detailJSON, err := json.Marshal(payload)
	if err != nil {
		log.Warn().
			Err(err).
			Str("component", componentName).
			Str("entry", params.Entry).
			Msg("failed to marshal recognition detail")
		detailJSON = []byte(detail.DetailJson)
	}

	log.Debug().
		Str("component", componentName).
		Str("entry", params.Entry).
		Str("source", params.Source).
		Int("selected_index", index).
		Ints("box", selected.Box[:]).
		Float64("distance_sq", distance).
		Msg("selected center-priority recognition result")

	return &maa.CustomRecognitionResult{
		Box:    selected.Box,
		Detail: string(detailJSON),
	}, true
}

func parseParams(raw string) (recognitionParam, error) {
	params := recognitionParam{
		Source:   sourceFiltered,
		Fallback: fallbackBest,
	}

	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "null" {
		return params, nil
	}
	if err := json.Unmarshal([]byte(trimmed), &params); err != nil {
		return params, err
	}

	params.Entry = strings.TrimSpace(params.Entry)
	params.Source = normalizeOption(params.Source, sourceFiltered)
	params.Fallback = normalizeOption(params.Fallback, fallbackBest)
	return params, nil
}

func normalizeOption(value, fallback string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return fallback
	}
	return value
}

func resolveCenter(img image.Image, roi maa.Rect, params recognitionParam) (point, error) {
	centerRaw := bytes.TrimSpace(params.Center)
	if len(centerRaw) == 0 || bytes.Equal(centerRaw, []byte("null")) {
		return imageCenter(img), nil
	}

	if centerRaw[0] == '"' {
		var centerType string
		if err := json.Unmarshal(centerRaw, &centerType); err != nil {
			return point{}, err
		}
		switch normalizeOption(centerType, centerImage) {
		case centerImage:
			return imageCenter(img), nil
		case centerROI:
			return rectCenter(roi), nil
		default:
			return point{}, fmt.Errorf("unsupported center type: %s", centerType)
		}
	}

	var coords []float64
	if err := json.Unmarshal(centerRaw, &coords); err != nil {
		return point{}, err
	}
	if len(coords) != 2 {
		return point{}, errors.New("center must contain exactly two numbers")
	}
	return point{X: coords[0], Y: coords[1]}, nil
}

func imageCenter(img image.Image) point {
	bounds := img.Bounds()
	return point{
		X: float64(bounds.Min.X) + float64(bounds.Dx())/2,
		Y: float64(bounds.Min.Y) + float64(bounds.Dy())/2,
	}
}

func rectCenter(rect maa.Rect) point {
	return point{
		X: float64(rect.X()) + float64(rect.Width())/2,
		Y: float64(rect.Y()) + float64(rect.Height())/2,
	}
}

func selectCandidate(detail *maa.RecognitionDetail, params recognitionParam, center point) (resultCandidate, int, bool) {
	if detail == nil {
		return resultCandidate{}, -1, false
	}

	if detail.Results != nil {
		switch params.Source {
		case sourceBest:
			if candidate, ok := candidateFromResult(detail.Results.Best); ok {
				return candidate, -1, true
			}
		case sourceAll:
			if candidate, index, ok := chooseNearest(candidatesFromResults(detail.Results.All), center); ok {
				return candidate, index, true
			}
		default:
			if candidate, index, ok := chooseNearest(candidatesFromResults(detail.Results.Filtered), center); ok {
				return candidate, index, true
			}
		}
	}

	return fallbackCandidate(detail, params)
}

func fallbackCandidate(detail *maa.RecognitionDetail, params recognitionParam) (resultCandidate, int, bool) {
	switch params.Fallback {
	case fallbackFail:
		return resultCandidate{}, -1, false
	case fallbackBox:
		return resultCandidate{Box: detail.Box, Type: "detail_box"}, -1, true
	default:
		if detail.Results != nil {
			if candidate, ok := candidateFromResult(detail.Results.Best); ok {
				return candidate, -1, true
			}
		}
		return resultCandidate{Box: detail.Box, Type: "detail_box"}, -1, true
	}
}

func candidatesFromResults(results []*maa.RecognitionResult) []resultCandidate {
	candidates := make([]resultCandidate, 0, len(results))
	for _, result := range results {
		candidate, ok := candidateFromResult(result)
		if ok {
			candidates = append(candidates, candidate)
		}
	}
	return candidates
}

func candidateFromResult(result *maa.RecognitionResult) (resultCandidate, bool) {
	if result == nil {
		return resultCandidate{}, false
	}

	if value, ok := result.AsNeuralNetworkDetect(); ok {
		return resultCandidate{
			Box:      value.Box,
			Type:     string(result.Type()),
			Score:    float64Ptr(value.Score),
			Label:    value.Label,
			ClsIndex: uint64Ptr(value.ClsIndex),
		}, true
	}
	if value, ok := result.AsTemplateMatch(); ok {
		return resultCandidate{Box: value.Box, Type: string(result.Type()), Score: float64Ptr(value.Score)}, true
	}
	if value, ok := result.AsFeatureMatch(); ok {
		return resultCandidate{Box: value.Box, Type: string(result.Type())}, true
	}
	if value, ok := result.AsColorMatch(); ok {
		return resultCandidate{Box: value.Box, Type: string(result.Type())}, true
	}
	if value, ok := result.AsOCR(); ok {
		return resultCandidate{Box: value.Box, Type: string(result.Type()), Score: float64Ptr(value.Score), Label: value.Text}, true
	}
	if value, ok := result.AsCustom(); ok {
		return resultCandidate{Box: value.Box, Type: string(result.Type())}, true
	}

	return resultCandidate{}, false
}

func chooseNearest(candidates []resultCandidate, center point) (resultCandidate, int, bool) {
	if len(candidates) == 0 {
		return resultCandidate{}, -1, false
	}

	best := candidates[0]
	bestIndex := 0
	bestDistance := distanceSq(best.Box, center)
	for i := 1; i < len(candidates); i++ {
		distance := distanceSq(candidates[i].Box, center)
		if distance < bestDistance {
			best = candidates[i]
			bestIndex = i
			bestDistance = distance
		}
	}
	return best, bestIndex, true
}

func distanceSq(box maa.Rect, center point) float64 {
	boxCenter := rectCenter(box)
	dx := boxCenter.X - center.X
	dy := boxCenter.Y - center.Y
	return dx*dx + dy*dy
}

func float64Ptr(value float64) *float64 {
	return &value
}

func uint64Ptr(value uint64) *uint64 {
	return &value
}
