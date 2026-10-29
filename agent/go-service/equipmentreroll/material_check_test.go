package equipmentreroll

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
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
				OrderBy  string      `json:"order_by"`
				Index    int         `json:"index"`
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
			if param.OrderBy != "Horizontal" || param.Index != -1 {
				t.Fatal("inventory candidate selection must be declared by Pipeline")
			}
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

// TestHeldCountTextRequiresCompleteNumber 不在 Go 中截取、纠正或重排 OCR 候选。
func TestHeldCountTextRequiresCompleteNumber(t *testing.T) {
	tests := []struct {
		text string
		want int
		ok   bool
	}{
		{"1049", 1049, true},
		{"0", 0, true},
		{" 5867 ", 5867, true},
		{"", 0, false},
		{"持有1049", 0, false},
		{"0 1049", 0, false},
		{"1,049", 0, false},
		{"-1", 0, false},
		{"+1", 0, false},
		{"999999999999999999999999999999", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			got, ok := parseHeldCountText(tt.text)
			if ok != tt.ok || (ok && got != tt.want) {
				t.Fatalf("count = %d/%v, want %d/%v", got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestHeldCountRequiresPipelineBest(t *testing.T) {
	for _, detail := range []*maa.RecognitionDetail{
		nil,
		{},
		{Hit: true},
		{Hit: true, Results: &maa.RecognitionResults{
			All: []*maa.RecognitionResult{{}}, Filtered: []*maa.RecognitionResult{{}},
		}},
		{Hit: true, Results: &maa.RecognitionResults{Best: &maa.RecognitionResult{}}},
	} {
		if _, ok := heldCountFromResult(detail); ok {
			t.Fatal("missing or non-OCR Best must not fall back to raw candidates")
		}
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
