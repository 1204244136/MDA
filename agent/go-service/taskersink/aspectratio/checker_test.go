package aspectratio

import "testing"

func TestIsLandscapeAspectRatio16x9AcceptsLandscape16x9(t *testing.T) {
	if !isLandscapeAspectRatio16x9(1280, 720) {
		t.Fatal("1280x720 should satisfy landscape 16:9")
	}
}

func TestIsLandscapeAspectRatio16x9RejectsPortrait9x16(t *testing.T) {
	if isLandscapeAspectRatio16x9(720, 1280) {
		t.Fatal("720x1280 should not satisfy landscape 16:9")
	}
}

func TestIsAtLeastTargetResolutionRequiresLandscapeMinimum(t *testing.T) {
	tests := []struct {
		name   string
		width  int
		height int
		want   bool
	}{
		{name: "target", width: 1280, height: 720, want: true},
		{name: "larger landscape", width: 1920, height: 1080, want: true},
		{name: "portrait target sides", width: 720, height: 1280, want: false},
		{name: "too narrow", width: 1279, height: 720, want: false},
		{name: "too short", width: 1280, height: 719, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isAtLeastTargetResolution(tt.width, tt.height); got != tt.want {
				t.Fatalf("isAtLeastTargetResolution(%d, %d) = %v, want %v", tt.width, tt.height, got, tt.want)
			}
		})
	}
}
