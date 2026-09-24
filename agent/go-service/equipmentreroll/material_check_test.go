package equipmentreroll

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// 回归真实日志中的分块 OCR：持有标签和 5,867 分开，筛选后必须只剩库存数字。
func TestLockInventoryOCRFiltersSplitLabel(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "assets", "resource", "pipeline", "EquipmentReroll", "EquipmentReroll.json"))
	if err != nil {
		t.Fatal(err)
	}
	var nodes map[string]struct {
		Recognition struct {
			Param struct {
				Expected any         `json:"expected"`
				Replace  [][2]string `json:"replace"`
			} `json:"param"`
		} `json:"recognition"`
	}
	if err := json.Unmarshal(raw, &nodes); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ node, text, want string }{
		{"__EquipmentRerollLockModuleHeld", "1,360", "1360"},
		{"__EquipmentRerollLockKeyHeld", "5,867", "5867"},
		{"__EquipmentRerollLockKeyHeld", "5.867", "5867"},
		{"__EquipmentRerollLockKeyHeld", "0", "0"},
	} {
		t.Run(tc.node+tc.text, func(t *testing.T) {
			param := nodes[tc.node].Recognition.Param
			pattern, ok := param.Expected.(string)
			if !ok {
				t.Fatalf("inventory expected must be a numeric pattern, got %v", param.Expected)
			}
			expected, err := regexp.Compile(pattern)
			if err != nil {
				t.Fatal(err)
			}
			var matched []string
			for _, text := range []string{"持有", tc.text} {
				for _, replacement := range param.Replace {
					text = regexp.MustCompile(replacement[0]).ReplaceAllString(text, replacement[1])
				}
				if expected.MatchString(text) {
					matched = append(matched, text)
				}
			}
			if len(matched) != 1 || matched[0] != tc.want {
				t.Fatalf("filtered=%v want only %q", matched, tc.want)
			}
		})
	}
}

// TestSelectBestHeldCandidate 验证多候选择优逻辑，确保不会盲信左侧低分误识别。
func TestSelectBestHeldCandidate(t *testing.T) {
	tests := []struct {
		name       string
		candidates []heldCandidate
		wantNum    int
		wantOk     bool
	}{
		{
			name: "真实复现：左侧图标误识为0(0.568)与右侧真实1049(0.972)",
			candidates: []heldCandidate{
				{num: 0, text: "0", score: 0.568166, x: 652},
				{num: 1049, text: "1049", score: 0.971734, x: 667},
			},
			wantNum: 1049,
			wantOk:  true,
		},
		{
			name: "单个真实0",
			candidates: []heldCandidate{
				{num: 0, text: "0", score: 0.95, x: 667},
			},
			wantNum: 0,
			wantOk:  true,
		},
		{
			name: "score相近时更靠右优先",
			candidates: []heldCandidate{
				{num: 1, text: "1", score: 0.90, x: 650},
				{num: 200, text: "200", score: 0.91, x: 670},
			},
			wantNum: 200,
			wantOk:  true,
		},
		{
			name:       "空候选列表",
			candidates: nil,
			wantNum:    0,
			wantOk:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := selectBestHeldCandidate(tt.candidates)
			if ok != tt.wantOk {
				t.Fatalf("selectBestHeldCandidate() ok = %v, want %v", ok, tt.wantOk)
			}
			if ok && got.num != tt.wantNum {
				t.Fatalf("selectBestHeldCandidate() got.num = %v, want %v", got.num, tt.wantNum)
			}
		})
	}
}

// TestConfirmHeldModulePipelineROI 验证确认页持有模组节点的 offset 已调整避开图标。
func TestConfirmHeldModulePipelineROI(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "assets", "resource", "pipeline", "EquipmentReroll", "EquipmentRerollMaterials.json"))
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
	node, ok := nodes["__EquipmentRerollConfirmHeldModule"]
	if !ok {
		t.Fatal("__EquipmentRerollConfirmHeldModule not found in EquipmentRerollMaterials.json")
	}
	if len(node.Recognition.Param.RoiOffset) < 1 {
		t.Fatal("missing roi_offset")
	}
	xOffset := node.Recognition.Param.RoiOffset[0]
	if xOffset < 60 {
		t.Fatalf("x_offset = %d, expected >= 60 to bypass custom module icon", xOffset)
	}
}

// TestSummaryAbortReason 验证提前结束原因能正确输出到最终摘要。
func TestSummaryAbortReason(t *testing.T) {
	taskID := int64(999999)
	reason := "订制模块不足（持有 0，需消耗 1）"
	setAbortReason(taskID, reason)
	defer func() {
		stateMu.Lock()
		delete(states, taskID)
		stateMu.Unlock()
	}()

	msg := buildFinalSummaryMessage(taskID)
	if !strings.Contains(msg, "【提前结束】"+reason) {
		t.Fatalf("summary message does not contain abort reason, got: %s", msg)
	}
}
