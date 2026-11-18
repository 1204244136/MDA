package equipmentreroll

import (
	"encoding/json"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"slices"
	"testing"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

type ocrReplacePipelineNode struct {
	Recognition struct {
		Param struct {
			Replace [][]string `json:"replace"`
		} `json:"param"`
	} `json:"recognition"`
}

// TestEquipmentScanPercentDegreeOffline 回放真实故障面板（腿部 1 号槽「13.75%」被 OCR
// 识别为「13.75°」），验证详情页扫描与结果页均能按 Pipeline 替换规则恢复并通过档位校验。
func TestEquipmentScanPercentDegreeOffline(t *testing.T) {
	lib := os.Getenv("MDA_MAA_LIB")
	if lib == "" {
		t.Skip("set MDA_MAA_LIB for offline percent OCR regression")
	}
	if err := maa.Init(maa.WithLibDir(lib), maa.WithJSONEncoder(json.Marshal), maa.WithJSONDecoder(json.Unmarshal)); err != nil {
		t.Fatal(err)
	}
	defer maa.Release()

	res, err := maa.NewResource()
	if err != nil {
		t.Fatal(err)
	}
	defer res.Destroy()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	if !res.PostBundle(filepath.Join(root, "assets/resource")).Wait().Success() {
		t.Fatal("resource load failed")
	}

	tasker, err := maa.NewTasker()
	if err != nil {
		t.Fatal(err)
	}
	defer tasker.Destroy()
	if err := tasker.BindResource(res); err != nil {
		t.Fatal(err)
	}
	if err := res.RegisterCustomRecognition("PercentDegreeOfflineProbe", &percentDegreeOfflineProbe{t: t}); err != nil {
		t.Fatal(err)
	}

	img := loadAffixPanel(t, "affix-legs-degree-panel.png", image.Pt(460, 39))
	if !tasker.PostRecognition(maa.RecognitionTypeCustom, maa.CustomRecognitionParam{CustomRecognition: "PercentDegreeOfflineProbe"}, img).Wait().Success() {
		t.Fatal("offline percent degree verification failed")
	}
}

type percentDegreeOfflineProbe struct{ t *testing.T }

func (p *percentDegreeOfflineProbe) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	p.verifyLegsPanelRealOCR(ctx, arg)
	p.verifyScanPercentMisreadRules(ctx, arg)
	p.verifyResultPercentMisreadRules(ctx)
	return &maa.CustomRecognitionResult{Box: arg.Roi}, !p.t.Failed()
}

func (p *percentDegreeOfflineProbe) verifyLegsPanelRealOCR(ctx *maa.Context, arg *maa.CustomRecognitionArg) {
	t := p.t
	t.Helper()

	flag, err := ctx.RunRecognition("__EquipmentRerollLocateFlag", arg.Img, nil)
	if err != nil || flag == nil || !flag.Hit {
		t.Errorf("legs flag: %v %+v", err, flag)
		return
	}
	if err := ctx.SetAnchor("EquipmentRerollInspectFlag", "__EquipmentRerollLocateFlag"); err != nil {
		t.Error(err)
		return
	}

	clearMonitorState(arg.TaskID)
	defer clearMonitorState(arg.TaskID)
	if err := beginScan(arg.TaskID, "腿部"); err != nil {
		t.Error(err)
		return
	}
	if err := ctx.OverridePipeline(map[string]any{
		carrierNode: map[string]any{"attach": map[string]any{"operation": "value", "mode": "character"}},
	}); err != nil {
		t.Error(err)
		return
	}

	wants := []struct {
		effect, value string
		tier          int
	}{
		{"优越代码伤害增加", "13.75%", 4},
		{"暴击伤害增加", "17.42%", 12},
		{"", "", 0},
	}
	for i, want := range wants {
		slot := i + 1
		copyArg := *arg
		copyArg.CustomRecognitionParam = fmt.Sprintf(`{"slot":%d,"is_last":%v}`, slot, slot == maxSlot)
		if _, hit := (&EquipmentRerollScanSlotRecognition{}).Run(ctx, &copyArg); !hit {
			t.Errorf("legs slot %d failed under real OCR", slot)
		}
		scan := scanSlotByPipeline(ctx, arg.Img, slot)
		if scan.Effect != want.effect || scan.Value != want.value || scan.Tier != want.tier {
			t.Errorf("legs slot %d = %+v, want %+v", slot, scan, want)
		}
		if slot == 1 && !slices.Contains(scan.ValueCandidates, "13.75°") {
			t.Errorf("expected real OCR candidate 13.75° on slot 1, got %v", scan.ValueCandidates)
		}
	}
}

func (p *percentDegreeOfflineProbe) verifyScanPercentMisreadRules(ctx *maa.Context, arg *maa.CustomRecognitionArg) {
	t := p.t
	t.Helper()

	scanNodes := loadOCRReplaceNodes(t, "../../../assets/resource/pipeline/EquipmentReroll/EquipmentRerollScan.json")
	detailsImg := loadAffixPanel(t, "affix-details-panel.png", image.Pt(460, 39))
	flag, err := ctx.RunRecognition("__EquipmentRerollLocateFlag", detailsImg, nil)
	if err != nil || flag == nil || !flag.Hit {
		t.Errorf("details flag: %v %+v", err, flag)
		return
	}
	if err := ctx.SetAnchor("EquipmentRerollInspectFlag", "__EquipmentRerollLocateFlag"); err != nil {
		t.Error(err)
		return
	}

	const primary = "__EquipmentRerollSlot1ValueOCR"
	const fallback = "__EquipmentRerollSlot1ValuePercentOCR"
	origPrimary := saveNodeJSON(t, ctx, primary)
	origFallback := saveNodeJSON(t, ctx, fallback)
	defer func() {
		if err := ctx.OverridePipeline(map[string]any{primary: origPrimary, fallback: origFallback}); err != nil {
			t.Error(err)
		}
	}()

	for _, tc := range []struct {
		raw, want string
		tier      int
	}{
		{"13.75°", "13.75%", 4},
		{"13.75º", "13.75%", 4},
		{"13.75‰", "13.75%", 4},
		{"12.349", "12.34%", 3},
		{"13.758", "", 0},
	} {
		inject := []string{`^23\.56[%％]?$`, tc.raw}
		if err := ctx.OverridePipeline(map[string]any{
			primary:  map[string]any{"recognition": map[string]any{"param": map[string]any{"replace": [][]string{inject}}}},
			fallback: map[string]any{"recognition": map[string]any{"param": map[string]any{"replace": append([][]string{inject}, scanNodes[fallback].Recognition.Param.Replace...)}}},
		}); err != nil {
			t.Error(err)
			return
		}
		scan := scanSlotByPipeline(ctx, detailsImg, 1)
		if scan.Value != tc.want || scan.Tier != tc.tier {
			t.Errorf("scan misread %q: got (%q, T%d), want (%q, T%d)", tc.raw, scan.Value, scan.Tier, tc.want, tc.tier)
		}
	}
}

func (p *percentDegreeOfflineProbe) verifyResultPercentMisreadRules(ctx *maa.Context) {
	t := p.t
	t.Helper()

	mainNodes := loadOCRReplaceNodes(t, "../../../assets/resource/pipeline/EquipmentReroll/EquipmentReroll.json")
	resultImg := loadAffixPanel(t, "affix-result-panel.png", image.Pt(460, 110))
	const directNode = "__EquipmentRerollResultChangedEffectSlot1"
	const detectedNode = "__EquipmentRerollResultChangedEffectSlot1DetectedOCR"
	origDirect := saveNodeJSON(t, ctx, directNode)
	origDetected := saveNodeJSON(t, ctx, detectedNode)
	defer func() {
		if err := ctx.OverridePipeline(map[string]any{directNode: origDirect, detectedNode: origDetected}); err != nil {
			t.Error(err)
		}
	}()

	for _, tc := range []struct {
		misread, want string
	}{
		{"13.75°", "13.75%"},
		{"13.75º", "13.75%"},
		{"13.75‰", "13.75%"},
		{"10.949", "10.94%"},
	} {
		inject := []string{`23\.56[%％]?`, tc.misread}
		if err := ctx.OverridePipeline(map[string]any{
			directNode:   map[string]any{"recognition": map[string]any{"param": map[string]any{"replace": append([][]string{inject}, mainNodes[directNode].Recognition.Param.Replace...)}}},
			detectedNode: map[string]any{"recognition": map[string]any{"param": map[string]any{"replace": append([][]string{inject}, mainNodes[detectedNode].Recognition.Param.Replace...)}}},
		}); err != nil {
			t.Error(err)
			return
		}
		effects, values, raws, known := recognizeChangedEffects(ctx, resultImg, partScan{}, true)
		if !known || effects[0] != "优越代码伤害增加" || values[0] != tc.want {
			t.Errorf("result misread %q: known=%v effects=%v values=%v raw=%v", tc.misread, known, effects, values, raws)
		}
	}
}

func loadOCRReplaceNodes(t *testing.T, path string) map[string]ocrReplacePipelineNode {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var nodes map[string]ocrReplacePipelineNode
	if err := unmarshalJSONC(raw, &nodes); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return nodes
}

func saveNodeJSON(t *testing.T, ctx *maa.Context, name string) map[string]any {
	t.Helper()
	raw, err := ctx.GetNodeJSON(name)
	if err != nil {
		t.Fatal(err)
	}
	var node map[string]any
	if err := json.Unmarshal([]byte(raw), &node); err != nil {
		t.Fatal(err)
	}
	return node
}
