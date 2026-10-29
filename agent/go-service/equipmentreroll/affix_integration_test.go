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
		return readEquipmentAffixName(ctx, arg.Img, direct, detected)
	}
	// 保留原节点，避免注入场景污染后续真实数值回归。
	original := map[string]any{}
	const valueNode = "__EquipmentRerollSlot1ValueOCR"
	const valueFallback = "__EquipmentRerollSlot1ValuePercentOCR"
	for _, name := range []string{direct, detected, valueNode, valueFallback, carrierNode} {
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
	// 专用数值节点本身无有效档位时，数值模式仍不得提交初始快照。
	override(map[string]any{
		valueNode:     map[string]any{"recognition": map[string]any{"param": map[string]any{"replace": [][]string{{`23\.56[%％]?`, "99.99%"}}}}},
		valueFallback: map[string]any{"recognition": map[string]any{"param": map[string]any{"expected": []string{"^MUST_NOT_MATCH$"}}}},
	})
	for _, mode := range []string{"character", "single"} {
		clearMonitorState(arg.TaskID)
		if err := beginScan(arg.TaskID, "头部"); err != nil {
			t.Fatal(err)
		}
		override(map[string]any{carrierNode: map[string]any{"attach": map[string]any{"mode": mode, "operation": "value"}}})
		copyArg := *arg
		copyArg.CustomRecognitionParam = `{"slot":1}`
		_, hit := (&EquipmentRerollScanSlotRecognition{}).Run(ctx, &copyArg)
		stateMu.Lock()
		state := states[arg.TaskID]
		stateMu.Unlock()
		if hit || state.Effects != [3]string{} || state.Values != [3]string{} || state.NextSlot != 1 || len(state.Parts) != 0 {
			t.Errorf("%s committed invalid value evidence: hit=%v state=%+v", mode, hit, state)
		}
	}
	override(original)
	clearMonitorState(arg.TaskID)
	// 名称一致、整行数字漏位时，初始扫描仍应使用专用数值节点并校准档位。
	override(map[string]any{
		direct: map[string]any{"recognition": map[string]any{"param": map[string]any{"replace": [][]string{
			{"优越代码伤害增加", "攻击力增加"}, {`23\.56[%％]?`, "1.11%"},
		}}}},
		detected: map[string]any{"recognition": map[string]any{"param": map[string]any{"replace": [][]string{
			{"优越代码伤害增加", "攻击力增加"}, {`23\.56[%％]?`, "11.11%"},
		}}}},
		valueNode: map[string]any{"recognition": map[string]any{"param": map[string]any{"replace": [][]string{{`23\.56[%％]?`, "11.11%"}}}}},
	})
	for _, mode := range []string{"character", "single"} {
		for _, operation := range []string{"effect", "value"} {
			clearMonitorState(arg.TaskID)
			if err := beginScan(arg.TaskID, "头部"); err != nil {
				t.Fatal(err)
			}
			override(map[string]any{carrierNode: map[string]any{"attach": map[string]any{"mode": mode, "operation": operation}}})
			copyArg := *arg
			copyArg.CustomRecognitionParam = `{"slot":1}`
			_, hit := (&EquipmentRerollScanSlotRecognition{}).Run(ctx, &copyArg)
			stateMu.Lock()
			state := states[arg.TaskID]
			stateMu.Unlock()
			if !hit || state.Effects[0] != "攻击力增加" || state.Values[0] != "11.11%" || state.NextSlot != 2 {
				t.Errorf("%s/%s rejected independent scan evidence: hit=%v state=%+v", mode, operation, hit, state)
			}
		}
	}
	override(original)
	clearMonitorState(arg.TaskID)
	// 结果页回放验证完整词条和数值可以通过 OCR 读取。
	img := loadAffixPanel(t, "affix-result-panel.png", image.Pt(460, 110))
	effects, values, raws, known := recognizeChangedEffects(ctx, img, partScan{}, false)
	t.Logf("result affixes: %v %v raw=%v", effects, values, raws)
	if !known || effects != [3]string{"优越代码伤害增加", "暴击伤害增加", "攻击力增加"} || values != [3]string{"23.56%", "6.64%", "11.11%"} {
		t.Errorf("result OCR: known=%v effects=%v values=%v", known, effects, values)
	}
	// 直接识别漏字时，检测识别仍可从同槽的名称框与数值框恢复完整结果。
	resultSlot := "__EquipmentRerollResultChangedEffectSlot1"
	override(map[string]any{resultSlot: map[string]any{"recognition": map[string]any{"param": map[string]any{"replace": [][]string{{"优越代码伤害增加", "优越代码伤害增"}}}}}})
	effects, values, raws, known = recognizeChangedEffects(ctx, img, partScan{}, false)
	if !known || effects[0] != "优越代码伤害增加" || values[0] != "23.56%" {
		t.Errorf("split result OCR: known=%v effects=%v values=%v raw=%v", known, effects, values, raws)
	}
	// 检测路径的数值被过滤后，不能借用残缺名称路径中的百分比。
	resultDetected := resultSlot + "DetectedOCR"
	override(map[string]any{resultDetected: map[string]any{"recognition": map[string]any{"param": map[string]any{"expected": []string{"优越代码伤害增加"}}}}})
	if _, _, _, known := recognizeChangedEffects(ctx, img, partScan{}, false); known {
		t.Error("unfiltered split percentage or partial-name percentage was accepted")
	}
	override(map[string]any{resultDetected: map[string]any{"recognition": map[string]any{"param": map[string]any{"expected": []string{".*"}}}}})
	// 名称相同但两条路径给出不同百分比时，也不能进入结果取舍。
	override(map[string]any{resultSlot: map[string]any{"recognition": map[string]any{"param": map[string]any{"replace": [][]string{{"23.56", "24.00"}}}}}})
	if _, _, _, known := recognizeChangedEffects(ctx, img, partScan{}, false); known {
		t.Error("conflicting result percentages were accepted")
	}
	override(map[string]any{resultSlot: map[string]any{"recognition": map[string]any{"param": map[string]any{"replace": [][]string{}}}}})
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
			effect, raw, known := readEquipmentAffixName(ctx, crop, direct, detected)
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
	override(original)
	// 名称入口也必须拒绝“空槽”同时带数字的矛盾识别。
	for _, node := range []string{direct, detected} {
		override(map[string]any{node: map[string]any{"recognition": map[string]any{"param": map[string]any{"replace": [][]string{{"优越代码伤害增加", "未获得效果"}}}}}})
	}
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
	// v1.11.8 / task 200001415 现场：装弹 77.15% 被读成 17.15%，
	// 整行名称中的数值又漏成 7.15%。扫描必须依据官方档位恢复 T13，而不是保存
	// tier=0 的错值。这里复用真实框架 OCR，再把结果替换成日志中的三路文本。
	override(original)
	override(map[string]any{
		direct: map[string]any{"recognition": map[string]any{"param": map[string]any{"replace": [][]string{
			{`【优越代码伤害增加】23\.56`, "【最大装弹数增加】7.15%"},
		}}}},
		detected: map[string]any{"recognition": map[string]any{"param": map[string]any{"replace": [][]string{
			{"优越代码伤害增加", "最大装弹数增加"}, {`23\.56`, "17.15%"},
		}}}},
		valueNode: map[string]any{"recognition": map[string]any{"param": map[string]any{"replace": [][]string{
			{`^23\.56[%％]?$`, "17.15%"},
		}}}},
		valueFallback: map[string]any{"recognition": map[string]any{"param": map[string]any{"replace": [][]string{
			{`^23\.56[%％]?$`, "17.15%"},
		}}}},
	})
	scan := scanSlotByPipeline(ctx, arg.Img, 1)
	if scan.Effect != "最大装弹数增加" || scan.RawValue != "17.15%" || scan.Value != "77.15%" || scan.Tier != 13 {
		t.Errorf("blue ammo OCR should recover T13: %+v", scan)
	}
	override(original)
}
