package equipmentreroll

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestScanSlotLockPipelineROI 验证详情页扫描的锁检测 ROI 只覆盖锁定按钮，
// 不会把词条行的数值文本纳入检测区域。
//
// 背景：roi_offset 的语义是「锚点框 + 该增量」，而不是「最终 ROI 尺寸」。
// 旧配置 [135, 45, 61, 7] 在 14x15 的 InspectFlag 锚点下实际得到
// (715, 484, 75, 22)：横向侵入数值文本，纵向又放大到整行高。当数值较长
// 且被游戏渲染为蓝色时（如 26.36%），其蓝色像素会被 LockBlue 的 ColorMatch
// 命中（24 ≥ count 20），触发「第 N 号槽已有锁定」误报并中断任务。
func TestScanSlotLockPipelineROI(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(
		"..", "..", "..", "assets", "resource", "pipeline", "EquipmentReroll", "EquipmentRerollScan.json"))
	if err != nil {
		t.Fatal(err)
	}
	var nodes map[string]struct {
		Recognition struct {
			Param struct {
				Roi       any   `json:"roi"`
				RoiOffset []int `json:"roi_offset"`
			} `json:"param"`
		} `json:"recognition"`
	}
	if err := json.Unmarshal(raw, &nodes); err != nil {
		t.Fatal(err)
	}

	const (
		anchorW = 14 // InspectFlag.png 模板尺寸即锚点框尺寸
		anchorH = 15
		// 数值文本从锚点 +88 左右开始、最长到 +148 结束（8 字词条 + 5 字符数值），
		// 锁定按钮左边缘位于锚点 +191，因此 ROI 左边界取 +188 可同时避开数值并覆盖按钮。
		minXOffset = 188
		maxRoiW    = 32
		maxRoiH    = 26
	)

	for slot := minSlot; slot <= maxSlot; slot++ {
		for _, color := range []string{"Blue", "Orange"} {
			name := fmt.Sprintf("__EquipmentRerollSlot%dLock%s", slot, color)
			node, ok := nodes[name]
			if !ok {
				t.Fatalf("%s not found in EquipmentRerollScan.json", name)
			}
			if roi, _ := node.Recognition.Param.Roi.(string); roi != "[Anchor]EquipmentRerollInspectFlag" {
				t.Errorf("%s roi = %v, want [Anchor]EquipmentRerollInspectFlag", name, node.Recognition.Param.Roi)
			}
			off := node.Recognition.Param.RoiOffset
			if len(off) != 4 {
				t.Fatalf("%s roi_offset = %v, want 4 elements", name, off)
			}
			if off[0] < minXOffset {
				t.Errorf("%s roi_offset[0] = %d, want >= %d：ROI 左边界会侵入数值文本，蓝色数值将被误判为永久锁",
					name, off[0], minXOffset)
			}
			if w := anchorW + off[2]; w > maxRoiW {
				t.Errorf("%s 实际 ROI 宽 = %d, want <= %d", name, w, maxRoiW)
			}
			if h := anchorH + off[3]; h > maxRoiH {
				t.Errorf("%s 实际 ROI 高 = %d, want <= %d", name, h, maxRoiH)
			}
		}
	}
}
