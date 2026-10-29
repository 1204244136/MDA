package equipmentreroll

import (
	"encoding/json"
	"image"
	"image/color"
	"image/draw"
	_ "image/png"
	"os"
	"path/filepath"
	"testing"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

// TestHeldInventoryOffline 只回放本地图片，不绑定控制器或执行点击。
// 原始确认页验证真实库存；双候选布局从同一原图复制数字，单独验证框架选框。
func TestHeldInventoryOffline(t *testing.T) {
	lib := os.Getenv("MDA_MAA_LIB")
	if lib == "" {
		t.Skip("set MDA_MAA_LIB for offline inventory recognition")
	}
	if err := maa.Init(maa.WithLibDir(lib), maa.WithJSONEncoder(json.Marshal), maa.WithJSONDecoder(json.Unmarshal)); err != nil {
		t.Fatal(err)
	}
	defer maa.Release()
	f, err := os.Open("testdata/value/confirm.png")
	if err != nil {
		t.Fatal(err)
	}
	img, _, err := image.Decode(f)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
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
	if err := res.RegisterCustomRecognition("HeldInventoryOfflineProbe", &heldInventoryOfflineProbe{t: t}); err != nil {
		t.Fatal(err)
	}
	if !tasker.PostRecognition(maa.RecognitionTypeCustom, maa.CustomRecognitionParam{CustomRecognition: "HeldInventoryOfflineProbe"}, img).Wait().Success() {
		t.Fatal("offline inventory verification failed")
	}
}

type heldInventoryOfflineProbe struct{ t *testing.T }

func (p *heldInventoryOfflineProbe) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	t := p.t
	const confirmed = "__EquipmentRerollConfirmHeldModule"
	label, err := ctx.RunRecognition("__EquipmentRerollHeldLabel", arg.Img, nil)
	if err != nil || label == nil || !label.Hit {
		t.Errorf("original confirmation inventory label: %v %+v", err, label)
		return nil, false
	}
	detail, err := ctx.RunRecognition(confirmed, arg.Img, nil)
	if err != nil || detail == nil || !detail.Hit || detail.Results == nil || detail.Results.Best == nil {
		t.Errorf("original confirmation inventory: %v %+v", err, detail)
		return nil, false
	}
	best, ok := detail.Results.Best.AsOCR()
	if !ok || best.Text != "1545" {
		t.Errorf("original confirmation inventory best: %+v", best)
		return nil, false
	}
	if count, known := readHeldCount(ctx, arg.Img, confirmed); !known || count != 1545 {
		t.Errorf("original inventory read = %d/%v", count, known)
	}
	// 测试专用：复制真实数字到同一行两个相隔的框，不能把它当锁定页实机验收。
	box := image.Rect(best.Box[0], best.Box[1], best.Box[0]+best.Box[2], best.Box[1]+best.Box[3])
	patch := image.NewRGBA(image.Rect(0, 0, box.Dx(), box.Dy()))
	draw.Draw(patch, patch.Bounds(), arg.Img, box.Min, draw.Src)
	for _, node := range []string{"__EquipmentRerollLockModuleHeld", "__EquipmentRerollLockKeyHeld", confirmed} {
		canvas := image.NewRGBA(image.Rect(0, 0, 1280, 720))
		draw.Draw(canvas, canvas.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
		for _, x := range []int{510, 565} {
			draw.Draw(canvas, image.Rect(x, 445, x+box.Dx(), 445+box.Dy()), patch, image.Point{}, draw.Src)
		}
		// 仅替换测试画布的 ROI；排序、过滤和千分位归一化仍用生产节点定义。
		if err := ctx.OverridePipeline(map[string]any{node: map[string]any{"recognition": map[string]any{
			"type": "OCR", "param": map[string]any{"roi": []int{505, 440, 100, 30}, "roi_offset": []int{0, 0, 0, 0}},
		}}}); err != nil {
			t.Error(err)
			return nil, false
		}
		d, err := ctx.RunRecognition(node, canvas, nil)
		if err != nil || d == nil || !d.Hit || d.Results == nil || d.Results.Best == nil {
			t.Errorf("synthetic double-number %s: %v %+v", node, err, d)
			continue
		}
		got, ok := d.Results.Best.AsOCR()
		if !ok {
			t.Errorf("%s returned a non-OCR best result", node)
			continue
		}
		maxX := -1
		for _, result := range d.Results.Filtered {
			if candidate, valid := result.AsOCR(); valid && candidate.Box[0] > maxX {
				maxX = candidate.Box[0]
			}
		}
		if got.Text != "1545" || got.Box[0] != maxX || len(d.Results.Filtered) < 2 {
			t.Errorf("%s must select the rightmost filtered number: best=%+v filtered=%d", node, got, len(d.Results.Filtered))
		}
		if count, known := readHeldCount(ctx, canvas, node); !known || count != 1545 {
			t.Errorf("synthetic inventory %s = %d/%v", node, count, known)
		}
		// 结果解释必须尊重框架 Best，不得因其它候选更长或更高分而改选。
		originalText := got.Text
		got.Text = "0"
		if count, known := heldCountFromResult(d); !known || count != 0 {
			t.Errorf("%s must accept Best=0 despite other 1545 candidates: %d/%v", node, count, known)
		}
		got.Text = originalText
		originalBest := d.Results.Best
		d.Results.Best = nil
		if count, known := heldCountFromResult(d); known || count != 0 {
			t.Errorf("%s must reject absent Best despite All/Filtered candidates: %d/%v", node, count, known)
		}
		d.Results.Best = originalBest
	}
	return &maa.CustomRecognitionResult{Box: arg.Roi}, !t.Failed()
}
