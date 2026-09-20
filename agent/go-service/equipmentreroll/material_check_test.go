package equipmentreroll

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
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
