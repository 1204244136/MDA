package equipmentreroll

import (
	"encoding/json"
	"fmt"
	"image"
	_ "image/png"
	"os"
	"path/filepath"
	"testing"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

// 可选的真实 MaaFramework 离线回放，不连接游戏，不产生实际点击。
// MDA_RELOAD_SCREENSHOT 指向故障现场 PNG，MDA_MAA_LIB 指向框架 DLL 目录。
// MDA_RELOAD_EMPTY_SLOT=1 选择 22:47 空槽正例，否则为 22:18 锁状态不匹配负例。
func TestReloadLocksOfflineIntegration(t *testing.T) {
	lib, screenshot := os.Getenv("MDA_MAA_LIB"), os.Getenv("MDA_RELOAD_SCREENSHOT")
	if lib == "" || screenshot == "" {
		t.Skip("set MDA_MAA_LIB and MDA_RELOAD_SCREENSHOT for offline replay")
	}
	if err := maa.Init(maa.WithLibDir(lib), maa.WithJSONEncoder(json.Marshal), maa.WithJSONDecoder(json.Unmarshal)); err != nil {
		t.Fatal(err)
	}
	defer maa.Release()
	f, err := os.Open(screenshot)
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
	root, _ := filepath.Abs("../../..")
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
	if err := res.RegisterCustomRecognition("EquipmentRerollReloadLocksPlanRecognition", &EquipmentRerollReloadLocksPlanRecognition{}); err != nil {
		t.Fatal(err)
	}
	if err := res.RegisterCustomRecognition("EquipmentRerollReloadLocksVerifyRecognition", &EquipmentRerollReloadLocksVerifyRecognition{}); err != nil {
		t.Fatal(err)
	}
	if err := res.RegisterCustomRecognition("ReloadOfflineProbe", &reloadOfflineRecognition{probe: &reloadOfflineProbe{t: t, img: img}}); err != nil {
		t.Fatal(err)
	}
	if !tasker.PostRecognition(maa.RecognitionTypeCustom, maa.CustomRecognitionParam{CustomRecognition: "ReloadOfflineProbe"}, img).Wait().Success() {
		t.Fatal("offline probe failed")
	}

}

type reloadOfflineProbe struct {
	t   *testing.T
	img image.Image
}

func (p *reloadOfflineProbe) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	t := p.t
	if os.Getenv("MDA_RESULT_LOCKED_SOURCE") == "1" {
		// 23:12 故障结果页第三槽被解除锁定提示遮挡；本轮锁定快照可直接补全。
		var source partScan
		source.Slots[2] = slotScanData{Effect: "优越代码伤害增加", Value: "15.15%", Lock: LockOneTime}
		effects, values, _, ok := recognizeChangedEffects(ctx, p.img, source)
		if !ok || effects != ([maxSlot]string{"暴击伤害增加", "蓄力伤害增加", "优越代码伤害增加"}) || values[2] != "15.15%" {
			t.Errorf("locked result replay failed: effects=%v values=%v ok=%v", effects, values, ok)
			return false
		}
		if _, _, _, ok := recognizeChangedEffects(ctx, p.img, partScan{}); ok {
			t.Error("transient unlocked slot must still reject the frame without a trusted source")
			return false
		}
		return true
	}
	check := func(ok bool, message string) bool {
		if !ok {
			t.Error(message)
		}
		return ok
	}
	id := arg.TaskID
	defer clearMonitorState(id)
	cfg, parts, history := reloadFixture()
	stateMu.Lock()
	states[id] = monitorState{Part: "头部", Parts: parts, PreviousLocks: history}
	stateMu.Unlock()
	if err := ctx.OverridePipeline(map[string]any{carrierNode: map[string]any{"attach": map[string]any{"mode": "single", "part": "头部", "want1": "攻击力增加", "want2": "优越代码伤害增加", "want3": ""}}}); err != nil {
		t.Error(err)
		return false
	}
	a := &maa.CustomRecognitionArg{TaskID: id, Img: p.img}
	_, ok := (&EquipmentRerollReloadLocksPlanRecognition{}).Run(ctx, a)
	if !check(!ok, "unknown inventory allowed reload") {
		return false
	}
	setInventory(id, Inventory{CustomModules: 1357, CustomLockKeys: 5867})
	_, ok = (&EquipmentRerollReloadLocksPlanRecognition{}).Run(ctx, a)
	if !check(ok, "valid plan denied reload") {
		return false
	}
	var detail *maa.RecognitionDetail
	var err error
	if os.Getenv("MDA_RELOAD_EMPTY_SLOT") == "1" {
		// 22:47:41 实际截图：第1槽灰锁、第2槽未获得效果、第3槽橙色单次锁。
		for _, node := range []string{"__EquipmentRerollConfirmSlot1LockGray", "__EquipmentRerollConfirmSlot2Empty", "__EquipmentRerollConfirmSlot3LockOrange"} {
			r, e := ctx.RunRecognition(node, p.img, nil)
			if !check(e == nil && r != nil && r.Hit, "actual screenshot recognition failed: "+node) {
				return false
			}
		}
		detail, err = ctx.RunRecognition("EquipmentRerollReloadLocksDone", p.img, nil)
		if !check(err == nil && detail != nil && detail.Hit, "restored locks with empty slot did not pass") {
			return false
		}
		// 空槽文字也识别失败时仍应拒绝，防止放宽为“未识别到锁 = 未锁”。
		if e := ctx.OverridePipeline(map[string]any{"__EquipmentRerollConfirmSlot2Empty": map[string]any{"recognition": map[string]any{"type": "OCR", "param": map[string]any{"expected": "^THIS_MUST_NOT_MATCH$"}}}}); e != nil {
			t.Error(e)
			return false
		}
		_, hit := (&EquipmentRerollReloadLocksVerifyRecognition{}).Run(ctx, a)
		if !check(!hit, "unknown empty slot was accepted") {
			return false
		}
	} else {
		// 实际故障截图：第1/2槽灰锁，第3槽蓝色固定锁，尚未恢复单次锁。
		for i, color := range []string{"Gray", "Gray", "Blue"} {
			r, err := ctx.RunRecognition(fmt.Sprintf("__EquipmentRerollConfirmSlot%dLock%s", i+1, color), p.img, nil)
			if !check(err == nil && r != nil && r.Hit, "actual screenshot lock not recognized: "+color) {
				return false
			}
		}
		_, ok = (&EquipmentRerollReloadLocksVerifyRecognition{}).Run(ctx, a)
		if !check(!ok, "unrestored screenshot passed verification") {
			return false
		}
		if !check(!(&EquipmentRerollReloadLocksDoneAction{}).Run(ctx, &maa.CustomActionArg{TaskID: id}), "commit accepted without visual evidence") {
			return false
		}
		// 仅替换基础识别命中结果，验证真实 Custom 识别到动作之间的 detail 接线。
		override := map[string]any{}
		for i := 0; i < maxSlot; i++ {
			for _, color := range []string{"Blue", "Orange", "Gray"} {
				hit := color == "Gray" || i == 2 && color == "Orange"
				reco := map[string]any{"type": "DirectHit"}
				if !hit {
					reco = map[string]any{"type": "ColorMatch", "param": map[string]any{"roi": []int{0, 0, 1, 1}, "count": 2}}
				}
				override[fmt.Sprintf("__EquipmentRerollConfirmSlot%dLock%s", i+1, color)] = map[string]any{"recognition": reco}
			}
		}
		detail, err = ctx.RunRecognition("EquipmentRerollReloadLocksDone", p.img, override)
		if !check(err == nil && detail != nil && detail.Hit, "verified result did not hit") {
			return false
		}
	}
	if !check((&EquipmentRerollReloadLocksDoneAction{}).Run(ctx, &maa.CustomActionArg{TaskID: id, RecognitionDetail: detail}), "verified lock commit failed") {
		return false
	}
	scan, _ := GetPartScan(id, "头部")
	inv, _ := getInventory(id)
	if !check(scan.Slots[2].Lock == LockOneTime && inv.CustomModules == 1357 && inv.CustomLockKeys == 5867, "commit changed inventory or lost lock") {
		return false
	}
	_, ok = reusableLocksForTask(id, cfg)
	return check(!ok, "already restored locks permitted another reload")
}

type reloadOfflineRecognition struct{ probe *reloadOfflineProbe }

func (r *reloadOfflineRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	return &maa.CustomRecognitionResult{Box: arg.Roi}, r.probe.Run(ctx, &maa.CustomActionArg{TaskID: arg.TaskID})
}
