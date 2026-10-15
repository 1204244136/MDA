package main

import (
	"encoding/json"
	"image"
	"image/color"
	"image/draw"
	_ "image/png"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

// TestAndroidPipelineReplay 只在本机提供现场图时运行，不将用户截图纳入仓库。
// 使用空白控制器记录点击，验证真实识别及页面确认重试，不连接游戏。
func TestAndroidPipelineReplay(t *testing.T) {
	lib, battlePath, shopPath := os.Getenv("MDA_MAA_LIB"), os.Getenv("MDA_BATTLE_SCREENSHOT"), os.Getenv("MDA_SHOP_SCREENSHOT")
	if lib == "" || battlePath == "" || shopPath == "" {
		t.Skip("set MDA_MAA_LIB, MDA_BATTLE_SCREENSHOT and MDA_SHOP_SCREENSHOT for offline replay")
	}
	load := func(path string) image.Image {
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		img, _, err := image.Decode(f)
		if err != nil {
			t.Fatal(err)
		}
		return img
	}
	battle, shop := load(battlePath), load(shopPath)
	if err := maa.Init(maa.WithLibDir(lib), maa.WithJSONEncoder(json.Marshal), maa.WithJSONDecoder(json.Unmarshal)); err != nil {
		t.Fatal(err)
	}
	defer maa.Release()
	if err := maa.ConfigInitOption(t.TempDir(), "{}"); err != nil {
		t.Fatal(err)
	}
	res, err := maa.NewResource()
	if err != nil {
		t.Fatal(err)
	}
	defer res.Destroy()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	if !res.PostBundle(filepath.Join(root, "assets/resource")).Wait().Success() {
		t.Fatal("resource load failed")
	}
	ctrlState := &androidReplayController{img: shop}
	ctrl, err := maa.NewCustomController(ctrlState)
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.Destroy()
	if !ctrl.PostConnect().Wait().Success() {
		t.Fatal("replay controller connect failed")
	}
	tasker, err := maa.NewTasker()
	if err != nil {
		t.Fatal(err)
	}
	defer tasker.Destroy()
	if err := tasker.BindResource(res); err != nil {
		t.Fatal(err)
	}
	if err := tasker.BindController(ctrl); err != nil {
		t.Fatal(err)
	}
	if err := res.RegisterCustomRecognition("AndroidReplayProbe", &androidReplayRecognition{run: func(ctx *maa.Context, _ *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
		without := func(rect image.Rectangle) image.Image {
			out := image.NewRGBA(battle.Bounds())
			draw.Draw(out, out.Bounds(), battle, battle.Bounds().Min, draw.Src)
			draw.Draw(out, rect, image.NewUniform(color.Black), image.Point{}, draw.Src)
			return out
		}
		cases := []struct {
			name string
			img  image.Image
			hit  bool
		}{
			{"BattleDesktopCombatHudVisible", battle, false},
			{"BattleMobilePauseVisible", battle, true},
			{"BattleMobileTimerVisible", battle, true},
			{"BattleMobileCombatHudVisible", battle, true},
			{"BattleEntered", battle, true},
			{"BattleMobileCombatHudVisible", shop, false},
			{"BattleMobileCombatHudVisible", without(image.Rect(1230, 5, 1280, 60)), false},
			{"BattleMobileCombatHudVisible", without(image.Rect(1145, 10, 1235, 55)), false},
		}
		for _, test := range cases {
			detail, err := ctx.RunRecognition(test.name, test.img, nil)
			hit := err == nil && detail != nil && detail.Hit
			if err != nil || hit != test.hit {
				t.Errorf("%s: hit=%t want=%t error=%v detail=%+v", test.name, hit, test.hit, err, detail)
			}
		}
		return &maa.CustomRecognitionResult{Box: maa.Rect{0, 0, 1, 1}}, !t.Failed()
	}}); err != nil {
		t.Fatal(err)
	}
	if !tasker.PostRecognition(maa.RecognitionTypeCustom, maa.CustomRecognitionParam{CustomRecognition: "AndroidReplayProbe"}, battle).Wait().Success() {
		t.Fatal("battle replay failed")
	}

	if err := res.RegisterCustomRecognition("ShopReplayPageEntered", &androidReplayRecognition{run: func(_ *maa.Context, _ *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
		return &maa.CustomRecognitionResult{Box: maa.Rect{68, 264, 119, 20}}, ctrlState.acceptAfter.Load() > 0 && ctrlState.clicks.Load() >= ctrlState.acceptAfter.Load()
	}}); err != nil {
		t.Fatal(err)
	}
	// 仅缩短离线测试等待；识别入口、max_hit、next 和目标选择全部沿用正式节点。
	if err := res.OverridePipeline(map[string]any{
		"ShopEnterRecyclingShop":   map[string]any{"post_wait_freezes": 0, "timeout": 150, "pre_delay": 0, "post_delay": 0, "rate_limit": 0},
		"ShopRecyclingShopEntered": map[string]any{"recognition": map[string]any{"type": "Custom", "param": map[string]any{"custom_recognition": "ShopReplayPageEntered"}}},
	}); err != nil {
		t.Fatal(err)
	}
	ctrlState.acceptAfter.Store(2)
	if !tasker.PostTask("ShopEnterRecyclingShop").Wait().Success() || ctrlState.clicks.Load() != 2 {
		t.Fatalf("missed first click did not retry exactly once: clicks=%d", ctrlState.clicks.Load())
	}
	ctrlState.clicks.Store(0)
	ctrlState.acceptAfter.Store(-1)
	if tasker.PostTask("ShopEnterRecyclingShop").Wait().Success() || ctrlState.clicks.Load() != 3 {
		t.Fatalf("blocked page must fail after three clicks: clicks=%d", ctrlState.clicks.Load())
	}
}

type androidReplayRecognition struct {
	run func(*maa.Context, *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool)
}

func (r *androidReplayRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	return r.run(ctx, arg)
}

type androidReplayController struct {
	maa.BlankController
	img         image.Image
	clicks      atomic.Int32
	acceptAfter atomic.Int32
}

func (c *androidReplayController) Screencap() (image.Image, bool) { return c.img, true }
func (c *androidReplayController) Click(_, _ int32) bool          { c.clicks.Add(1); return true }
