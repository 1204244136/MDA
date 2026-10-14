package equipmentreroll

import (
	"encoding/json"
	"image"
	_ "image/png"
	"os"
	"path/filepath"
	"testing"
)

// TestConsumableCostCountOnlyRec 守住两个消耗数量节点必须 only_rec：
// 走 det 时小 ROI 的数字只被框住上半部，会读错并静默失配到 reco_timeout。
func TestConsumableCostCountOnlyRec(t *testing.T) {
	const pipelineDir = "../../../assets/resource/pipeline/EquipmentReroll"
	raw, err := os.ReadFile(filepath.Join(pipelineDir, "EquipmentRerollMaterials.json"))
	if err != nil {
		t.Fatal(err)
	}
	var nodes map[string]struct {
		Recognition struct {
			Param struct {
				Roi       any   `json:"roi"`
				RoiOffset []int `json:"roi_offset"`
				Expected  any   `json:"expected"`
				OnlyRec   *bool `json:"only_rec"`
			} `json:"param"`
		} `json:"recognition"`
	}
	if err := json.Unmarshal(raw, &nodes); err != nil {
		t.Fatal(err)
	}

	// 数量节点 -> 它引用的锚点节点（锚点框尺寸即模板图尺寸）
	cases := map[string]string{
		"__EquipmentRerollCostModuleCount": "__EquipmentRerollCostModule",
		"__EquipmentRerollCostKeyCount":    "__EquipmentRerollCostKey",
	}
	// 实测数字字形约 5x15（模块）与 12x15（两位密钥），ROI 需留出余量。
	const minRoiW, minRoiH = 18, 18

	for name, anchor := range cases {
		node, ok := nodes[name]
		if !ok {
			t.Fatalf("%s not found in EquipmentRerollMaterials.json", name)
		}
		if roi, _ := node.Recognition.Param.Roi.(string); roi != anchor {
			t.Errorf("%s roi = %v, want %s", name, node.Recognition.Param.Roi, anchor)
		}
		if node.Recognition.Param.OnlyRec == nil || !*node.Recognition.Param.OnlyRec {
			t.Errorf("%s 必须 only_rec: true：走 det 时小字只被捞回上半部", name)
		}
		if node.Recognition.Param.Expected == nil {
			t.Errorf("%s 缺少 expected 数字约束", name)
		}

		off := node.Recognition.Param.RoiOffset
		if len(off) != 4 {
			t.Fatalf("%s roi_offset = %v, want 4 elements", name, off)
		}
		// roi_offset 的语义是「锚点框 + 该增量」，宽高同样是增量。
		tw, th := templateSize(t, filepath.Join("..", "..", "..",
			"assets", "resource", "image", "EquipmentReroll", templateFor(anchor)))
		if w := tw + off[2]; w < minRoiW {
			t.Errorf("%s 实际 ROI 宽 = %d, want >= %d", name, w, minRoiW)
		}
		if h := th + off[3]; h < minRoiH {
			t.Errorf("%s 实际 ROI 高 = %d, want >= %d", name, h, minRoiH)
		}
	}
}

func templateFor(anchor string) string {
	if anchor == "__EquipmentRerollCostKey" {
		return "CostKey.png"
	}
	return "CostModule.png"
}

func templateSize(t *testing.T, path string) (int, int) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		t.Fatal(err)
	}
	return cfg.Width, cfg.Height
}
