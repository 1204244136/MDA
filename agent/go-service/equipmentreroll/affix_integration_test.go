package equipmentreroll

import (
	"encoding/json"
	"image"
	"image/draw"
	_ "image/png"
	"os"
	"path/filepath"
	"testing"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func TestEquipmentAffixOffline(t *testing.T) {
	lib := os.Getenv("MDA_MAA_LIB")
	if lib == "" {
		t.Skip("set MDA_MAA_LIB for offline recognition")
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
	if err := res.RegisterCustomRecognition("AffixOfflineProbe", &affixOfflineProbe{t: t}); err != nil {
		t.Fatal(err)
	}
	img := loadAffixPanel(t, "affix-details-panel.png", image.Pt(460, 39))
	if !tasker.PostRecognition(maa.RecognitionTypeCustom, maa.CustomRecognitionParam{CustomRecognition: "AffixOfflineProbe"}, img).Wait().Success() {
		t.Fatal("offline affix verification failed")
	}
}

func loadAffixPanel(t *testing.T, name string, origin image.Point) image.Image {
	t.Helper()
	f, err := os.Open("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	panel, _, err := image.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 1280, 720))
	draw.Draw(img, panel.Bounds().Add(origin), panel, image.Point{}, draw.Src)
	return img
}

type affixOfflineProbe struct{ t *testing.T }

func (p *affixOfflineProbe) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	flag, err := ctx.RunRecognition("__EquipmentRerollLocateFlag", arg.Img, nil)
	if err != nil || flag == nil || !flag.Hit {
		p.t.Errorf("flag: %v %+v", err, flag)
		return nil, false
	}
	if err := ctx.SetAnchor("EquipmentRerollInspectFlag", "__EquipmentRerollLocateFlag"); err != nil {
		p.t.Error(err)
		return nil, false
	}
	p.checkAffixSafety(ctx, arg)
	return &maa.CustomRecognitionResult{Box: arg.Roi}, !p.t.Failed()
}

// 真实框架只识别本地面板，不连接控制器，不执行点击。
func (p *affixOfflineProbe) checkAffixSafety(ctx *maa.Context, arg *maa.CustomRecognitionArg) {
	t := p.t
	const direct, detected = "__EquipmentRerollSlot1AffixOCR", "__EquipmentRerollSlot1AffixDetectedOCR"
	override := func(nodes map[string]any) {
		t.Helper()
		if err := ctx.OverridePipeline(nodes); err != nil {
			t.Fatal(err)
		}
	}
	read := func() (string, string, bool) {
		return readEquipmentAffix(ctx, arg.Img, direct, detected)
	}
	// 保留原节点，避免注入场景污染后续真实数值回归。
	original := map[string]any{}
	for _, name := range []string{direct, detected, carrierNode} {
		raw, err := ctx.GetNodeJSON(name)
		if err != nil {
			t.Fatal(err)
		}
		var node map[string]any
		if err := json.Unmarshal([]byte(raw), &node); err != nil {
			t.Fatal(err)
		}
		original[name] = node
	}
	defer func() { override(original); clearMonitorState(arg.TaskID) }()
	for _, operation := range []string{"effect", "value"} {
		clearMonitorState(arg.TaskID)
		if err := beginScan(arg.TaskID, "头部"); err != nil {
			t.Fatal(err)
		}
		override(map[string]any{carrierNode: map[string]any{"attach": map[string]any{"operation": operation}}})
		copyArg := *arg
		copyArg.CustomRecognitionParam = `{"slot":1}`
		if _, hit := (&EquipmentRerollScanSlotRecognition{}).Run(ctx, &copyArg); !hit {
			t.Errorf("%s rejected verified slot", operation)
		}
	}
	override(original)
	clearMonitorState(arg.TaskID)
	// 结果页回放验证完整词条和数值可以通过 OCR 读取。
	img := loadAffixPanel(t, "affix-result-panel.png", image.Pt(460, 110))
	effects, values, raws, known := recognizeChangedEffects(ctx, img, partScan{})
	t.Logf("result affixes: %v %v raw=%v", effects, values, raws)
	if !known || effects != [3]string{"优越代码伤害增加", "暴击伤害增加", "攻击力增加"} || values != [3]string{"23.56%", "6.64%", "11.11%"} {
		t.Errorf("result OCR: known=%v effects=%v values=%v", known, effects, values)
	}
	// 不同现场的无损行截图用于检查类别混淆，保留原始字号和像素。
	for _, sample := range []struct {
		file  string
		wants [3]string
	}{
		{"affix-details-rows.png", [3]string{"攻击力增加", "优越代码伤害增加", "最大装弹数增加"}},
		{"affix-speed-empty-rows.png", [3]string{"蓄力速度增加", "", ""}},
		{"affix-confirm-rows.png", [3]string{"暴击率增加", "优越代码伤害增加", ""}},
	} {
		f, err := os.Open("testdata/" + sample.file)
		if err != nil {
			t.Fatal(err)
		}
		crop, _, err := image.Decode(f)
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
		for i, want := range sample.wants {
			y := 24 * i
			if sample.file == "affix-confirm-rows.png" {
				y = 26 * i
			}
			roi := []int{0, y, crop.Bounds().Dx(), 24}
			override(map[string]any{
				direct:   map[string]any{"recognition": map[string]any{"param": map[string]any{"roi": roi, "roi_offset": []int{0, 0, 0, 0}}}},
				detected: map[string]any{"recognition": map[string]any{"param": map[string]any{"roi": roi, "roi_offset": []int{0, 0, 0, 0}}}},
			})
			effect, raw, known := readEquipmentAffix(ctx, crop, direct, detected)
			if !known || effect != want {
				t.Errorf("%s slot %d: %q %v raw=%q, want %q", sample.file, i+1, effect, known, raw, want)
			}

		}
	}
	override(original)
	// 框架已过滤的 OCR 不得从 All 中复活。
	blocked := map[string]any{"recognition": map[string]any{"param": map[string]any{"expected": "^MUST_NOT_MATCH$"}}}
	override(map[string]any{direct: blocked, detected: blocked})
	if _, _, known := read(); known {
		t.Error("unfiltered OCR was accepted")
	}
	input := arg.Img
	assertNoSnapshot := func(operation string) {
		t.Helper()
		clearMonitorState(arg.TaskID)
		if err := beginScan(arg.TaskID, "头部"); err != nil {
			t.Fatal(err)
		}
		override(map[string]any{carrierNode: map[string]any{"attach": map[string]any{"operation": operation}}})
		copyArg := *arg
		copyArg.Img = input
		copyArg.CustomRecognitionParam = `{"slot":1}`
		if _, hit := (&EquipmentRerollScanSlotRecognition{}).Run(ctx, &copyArg); hit {
			t.Errorf("%s accepted unknown/conflicting slot", operation)
		}
		stateMu.Lock()
		state := states[arg.TaskID]
		stateMu.Unlock()
		if state.NextSlot != minSlot || len(state.Parts) != 0 || state.Effects != [3]string{} {
			t.Errorf("%s mutated scan state: %+v", operation, state)
		}
	}
	for _, operation := range []string{"effect", "value"} {
		assertNoSnapshot(operation)
	}
	override(original)
	// 两条 OCR 路径给出不同完整名称时，两种模式都不得提交快照。
	override(map[string]any{direct: map[string]any{"recognition": map[string]any{"param": map[string]any{"replace": [][]string{{"优越代码伤害增加", "命中率增加"}}}}}})
	for _, operation := range []string{"effect", "value"} {
		assertNoSnapshot(operation)
	}
	override(original)
	override(map[string]any{direct: map[string]any{"recognition": map[string]any{"param": map[string]any{"replace": [][]string{{"优越代码伤害增加", "攻击力增加暴击率增加"}}}}}})
	for _, operation := range []string{"effect", "value"} {
		assertNoSnapshot(operation)
	}
	// 页面消失时即使已有旧锚点，也不能在背景上继续读取和提交槽位。
	override(original)
	input = image.NewRGBA(arg.Img.Bounds())
	for _, operation := range []string{"effect", "value"} {
		assertNoSnapshot(operation)
	}
	input = arg.Img
	// 所有词条统一由完整名称 OCR 识别。
	override(original)
	for _, node := range []string{direct, detected} {
		override(map[string]any{node: map[string]any{"recognition": map[string]any{"param": map[string]any{"replace": [][]string{{"优越代码伤害增加", "防御力增加"}}}}}})
	}
	if effect, _, known := read(); !known || effect != "防御力增加" {
		t.Errorf("complete-name OCR failed: %q %v", effect, known)
	}
}
