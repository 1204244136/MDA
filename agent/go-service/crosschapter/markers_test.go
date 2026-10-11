package crosschapter

import (
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"testing"
)

func TestMarkerAboveRingIsAssociatedWithoutSuppressingNeighbors(t *testing.T) {
	marker := []maa.Rect{{573, 368, 62, 54}}
	if !hasExcludedMarker(maa.Rect{550, 430, 120, 90}, marker) {
		t.Fatal("EX marker above its ring was ignored")
	}
	for _, normal := range []maa.Rect{{400, 430, 50, 60}, {710, 430, 50, 60}, {570, 540, 50, 60}} {
		if hasExcludedMarker(normal, marker) {
			t.Fatalf("unrelated ordinary target suppressed: %v", normal)
		}
	}
}
