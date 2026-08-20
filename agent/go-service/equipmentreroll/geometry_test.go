package equipmentreroll

import (
	"testing"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func TestSlotRectFromFlag(t *testing.T) {
	// 用户实测：Flag 框 [580,439,14,15] 时第 1 槽左上角为 (490,484)。
	flag := maa.Rect{580, 439, 14, 15}

	cases := []struct {
		name string
		slot int
		want maa.Rect
	}{
		{name: "slot 1", slot: 1, want: maa.Rect{490, 484, 300, 22}},
		{name: "slot 2", slot: 2, want: maa.Rect{490, 508, 300, 22}},
		{name: "slot 3", slot: 3, want: maa.Rect{490, 532, 300, 22}},
		{name: "below min clamps to slot 1", slot: 0, want: maa.Rect{490, 484, 300, 22}},
		{name: "above max clamps to slot 3", slot: 9, want: maa.Rect{490, 532, 300, 22}},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := slotRectFromFlag(flag, tt.slot)
			if got != tt.want {
				t.Fatalf("slotRectFromFlag() = %v, want %v", got, tt.want)
			}
		})
	}
}
