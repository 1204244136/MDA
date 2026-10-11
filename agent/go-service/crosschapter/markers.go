package crosschapter

import (
	"fmt"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"image"
)

// Match at the normalized map scale once, not inside a detector box that
// may contain only the body/ring while its marker sits above it.
func excludedMarkers(ctx *maa.Context, img image.Image, nodes ...string) ([]maa.Rect, error) {
	var boxes []maa.Rect
	for _, node := range nodes {
		if node == "" {
			continue
		}
		d, err := ctx.RunRecognition(node, img)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", node, err)
		}
		if d == nil {
			return nil, fmt.Errorf("%s: missing result", node)
		}
		if !d.Hit {
			continue
		}
		if d.Results == nil {
			return nil, fmt.Errorf("%s: missing recognition results", node)
		}
		for _, result := range d.Results.Filtered {
			if result == nil {
				continue
			}
			if m, ok := result.AsTemplateMatch(); ok {
				boxes = append(boxes, m.Box)
				continue
			}
			if m, ok := result.AsCustom(); ok {
				boxes = append(boxes, m.Box)
				continue
			}
			return nil, fmt.Errorf("%s: unexpected result type", node)
		}
	}
	return boxes, nil
}

type BossMarkerRecognition struct{}

// Marker-only recognition remains useful for exclusion. Clicking additionally
// requires the model's body/ring box, with the target in its lower third.
func confirmedBossTarget(ctx *maa.Context, img image.Image, bosses, ex, blocked []maa.Rect) maa.Rect {
	if len(bosses) == 0 {
		return maa.Rect{}
	}
	d, err := ctx.RunRecognition("MapPushingBossRaw", img)
	if err != nil || d == nil || !d.Hit || d.Results == nil {
		return maa.Rect{}
	}
	var best maa.Rect
	for _, result := range d.Results.Filtered {
		if result == nil {
			continue
		}
		nn, ok := result.AsNeuralNetworkDetect()
		if !ok || nn.ClsIndex != 1 || !safeMapTarget(nn.Box) || !hasExcludedMarker(nn.Box, bosses) || hasExcludedMarker(nn.Box, ex) {
			continue
		}
		b := nn.Box
		target := maa.Rect{b[0] + b[2]/4, b[1] + 2*b[3]/3, max(1, b[2]/2), max(1, b[3]/3)}
		if hasExcludedMarker(target, blocked) {
			continue
		}
		if best == (maa.Rect{}) || distance(target) < distance(best) {
			best = target
		}
	}
	return best
}

func (*BossMarkerRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil || arg.Img == nil {
		return nil, false
	}
	d, err := ctx.RunRecognition("MapPushingBossRaw", arg.Img)
	if err != nil || d == nil {
		return nil, false
	}
	ex, err := excludedMarkers(ctx, arg.Img, "MapPushingExMarker")
	if err != nil {
		return nil, false
	}
	// A high-confidence icon can survive an animation frame where the
	// detector misses the ring. Lower icon scores still require model agreement.
	iconDetail, err := ctx.RunRecognition("MapPushingBossIcon", arg.Img)
	if err != nil || iconDetail == nil {
		return nil, false
	}
	var icons []maa.Rect
	if iconDetail.Hit && iconDetail.Results != nil {
		for _, r := range iconDetail.Results.Filtered {
			if r == nil {
				continue
			}
			icon, ok := r.AsTemplateMatch()
			if !ok {
				return nil, false
			}
			icons = append(icons, icon.Box)
			if ok && icon.Score >= 0.86 && !hasExcludedMarker(icon.Box, ex) {
				return &maa.CustomRecognitionResult{Box: icon.Box, Detail: fmt.Sprintf(`{"class":"boss","icon_score":%f,"ex_excluded":true}`, icon.Score)}, true
			}
		}
	}
	var best maa.Rect
	score := 0.0
	var candidates []*maa.RecognitionResult
	if d.Hit && d.Results != nil {
		candidates = d.Results.Filtered
	}
	for _, r := range candidates {
		if r == nil {
			continue
		}
		nn, ok := r.AsNeuralNetworkDetect()
		if !ok || nn.ClsIndex != 1 || hasExcludedMarker(nn.Box, ex) || !hasExcludedMarker(nn.Box, icons) {
			continue
		}
		if nn.Score > score {
			best, score = nn.Box, nn.Score
		}
	}
	if best == (maa.Rect{}) {
		return nil, false
	}
	return &maa.CustomRecognitionResult{Box: best, Detail: fmt.Sprintf(`{"class":"boss","model_score":%f,"icon_confirmed":true,"ex_excluded":true}`, score)}, true
}

func hasExcludedMarker(candidate maa.Rect, markers []maa.Rect) bool {
	for _, m := range markers {
		x, y := m[0]+m[2]/2, m[1]+m[3]/2
		if x >= candidate[0]-24 && x <= candidate[0]+candidate[2]+24 &&
			y >= candidate[1]-max(80, candidate[3]) && y <= candidate[1]+candidate[3] {
			return true
		}
	}
	return false
}
