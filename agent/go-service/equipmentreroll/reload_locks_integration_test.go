package equipmentreroll

import (
	"encoding/json"
	"fmt"
	"image"
	_ "image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

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
	if os.Getenv("MDA_VALUE_SAMPLE") == "unlock" {
		// 用户原图保留无损；只在离线识别输入中按项目 1280×720 基准缩放。
		scaled := image.NewRGBA(image.Rect(0, 0, 1280, 720))
		for y := 0; y < 720; y++ {
			for x := 0; x < 1280; x++ {
				scaled.Set(x, y, img.At(x*img.Bounds().Dx()/1280, y*img.Bounds().Dy()/720))
			}
		}
		img = scaled
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
	if err := res.RegisterCustomRecognition("EquipmentRerollValueLocksVerifyRecognition", &EquipmentRerollValueLocksVerifyRecognition{}); err != nil {
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
	if os.Getenv("MDA_VALUE_SAMPLE") == "unlock" {
		for _, node := range []string{"__EquipmentRerollUnlockTitle", "__EquipmentRerollUnlockMessage", "__EquipmentRerollUnlockConfirmText", "EquipmentRerollValueUnlockConfirm"} {
			d, err := ctx.RunRecognition(node, p.img, nil)
			if err != nil || d == nil || !d.Hit {
				t.Errorf("unlock sample did not match %s: %v", node, err)
				return false
			}
		}
		// 只识别，不执行确认或解锁点击。
		return true
	}
	if sample := os.Getenv("MDA_VALUE_SAMPLE"); sample != "" {
		return p.verifyValueSample(ctx, arg, sample)
	}
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

// verifyValueSample 复用已有离线回放流程，只替换样本和断言，不创建控制器或点击。
func (p *reloadOfflineProbe) verifyValueSample(ctx *maa.Context, arg *maa.CustomActionArg, sample string) bool {
	t := p.t
	// 离线测试没有 PI 客户端，直接应用实际 Value case 的覆盖来模拟选项生效。
	if err := ctx.OverridePipeline(loadValueOptionOverrides(t)); err != nil {
		t.Errorf("value option override failed: %v", err)
		return false
	}
	if sample == "prepare" {
		return p.verifyValuePrepare(ctx, arg.TaskID)
	}
	if sample == "locks-bright" || sample == "locks-dark" {
		actual, ok := readConfirmLocks(ctx, p.img)
		if sample == "locks-bright" {
			if !ok || actual != ([maxSlot]SlotLock{LockNone, LockOneTime, LockPermanent}) {
				t.Errorf("bright locks: %v %v", actual, ok)
				return false
			}
		} else if ok {
			t.Errorf("dark colored locks misread as unlocked: %v", actual)
			return false
		}
		for i := 1; i <= 3; i++ {
			d, err := ctx.RunRecognition(fmt.Sprintf("__EquipmentRerollConfirmSlot%dLockGray", i), p.img, nil)
			if err != nil || d == nil || d.Hit != (i == 1) {
				t.Errorf("gray icon %d: %+v %v", i, d, err)
				return false
			}
		}
		return true
	}
	checkNode := func(name string, want bool) bool {
		detail, err := ctx.RunRecognition(name, p.img, nil)
		if err != nil || detail == nil || detail.Hit != want {
			t.Errorf("%s hit=%+v error=%v", name, detail, err)
			return false
		}
		if want {
			t.Logf("%s: box=%v", name, detail.Box)
		}
		return true
	}
	if !checkNode("EquipmentRerollClickChangeEffect", false) {
		return false
	}
	if !checkNode("EquipmentRerollValueUnlockConfirm", false) {
		return false
	}
	if sample == "confirm" {
		for _, name := range []string{"__EquipmentRerollChangeEffectTitle", "__EquipmentRerollPreviousLockSettings", "__EquipmentRerollChangeEffectButton", "EquipmentRerollConfirmChangeEffect", "EquipmentRerollKeepClickSlot1", "EquipmentRerollKeepClickSlot2", "EquipmentRerollKeepClickSlot3"} {
			if !checkNode(name, true) {
				return false
			}
		}
		if !checkNode("__EquipmentRerollResultPageTitle", false) {
			return false
		}
		cost, ok := readRerollCost(ctx, p.img, partScan{})
		if !ok || cost.CustomModules != 1 || cost.CustomLockKeys != 0 {
			t.Errorf("cost=%+v ok=%v", cost, ok)
			return false
		}
		for slot := 1; slot <= 3; slot++ {
			if !checkNode(fmt.Sprintf("__EquipmentRerollConfirmSlot%dLockGray", slot), true) {
				return false
			}
		}
		id := arg.TaskID
		defer clearMonitorState(id)
		_ = setCurrentPart(id, "头部")
		updatePartEffects(id, "头部", [maxSlot]string{"攻击力增加"}, [maxSlot]string{"7.59%"})
		applyLockToSnapshot(id, "头部", 1, "订制模块")
		setInventory(id, Inventory{100, 200})
		storeValuePlan(id, valuePlan{Part: "头部", FirstModules: 1})
		setPendingLock(id, 1, valueReleaseMaterial)
		verified, err := ctx.RunRecognition("EquipmentRerollValueVerifyChange", p.img, nil)
		if err != nil || verified == nil || !verified.Hit {
			t.Errorf("gray locks did not verify release: %v", err)
			return false
		}
		doneArg := &maa.CustomActionArg{TaskID: id, RecognitionDetail: verified}
		if !(&EquipmentRerollValueLocksDoneAction{}).Run(ctx, doneArg) {
			t.Error("verified release commit failed")
			return false
		}
		if (&EquipmentRerollValueLocksDoneAction{}).Run(ctx, doneArg) {
			t.Error("release committed twice")
			return false
		}
		scan, _ := GetPartScan(id, "头部")
		inv, _ := getInventory(id)
		if scan.Slots[0].Lock != LockNone || inv != (Inventory{100, 200}) {
			t.Error("release changed inventory or failed to clear lock")
			return false
		}
		if !checkNode("EquipmentRerollValueVerifyReady", true) {
			return false
		}
		storeValuePlan(id, valuePlan{Part: "头部", Locks: [maxSlot]SlotLock{LockPermanent}})
		if !checkNode("EquipmentRerollValueVerifyReady", false) {
			return false
		}
		return true
	}
	if sample != "result" {
		t.Errorf("unknown value sample %q", sample)
		return false
	}
	for _, name := range []string{"__EquipmentRerollResultPageTitle", "EquipmentRerollResultClickKeep", "EquipmentRerollResultClickAccept"} {
		if !checkNode(name, true) {
			return false
		}
	}
	if !checkNode("__EquipmentRerollChangeEffectTitle", false) {
		return false
	}
	effects, values, _, ok := recognizeChangedEffects(ctx, p.img, partScan{})
	if !ok || effects != ([maxSlot]string{"优越代码伤害增加", "暴击伤害增加", "攻击力增加"}) || values != ([maxSlot]string{"23.56%", "6.64%", "11.11%"}) {
		t.Errorf("result effects=%v values=%v ok=%v", effects, values, ok)
		return false
	}
	id := arg.TaskID
	defer clearMonitorState(id)
	_ = setCurrentPart(id, "头部")
	updatePartEffects(id, "头部", effects, [maxSlot]string{"22.15%", "12.52%", "9.70%"})
	if err := ctx.OverridePipeline(map[string]any{carrierNode: map[string]any{"attach": map[string]any{"operation": "value", "mode": "single", "part": "头部", "value_tier_优越代码伤害增加": 12, "value_tier_攻击力增加": 11}}}); err != nil {
		t.Error(err)
		return false
	}
	result, hit := (&EquipmentRerollResultDecideRecognition{}).Run(ctx, &maa.CustomRecognitionArg{TaskID: id, Img: p.img, CustomRecognitionParam: "{}"})
	if !hit || result.Detail != resultDecisionDetail(ResultDecisionAccept) {
		t.Errorf("value decision=%+v hit=%v", result, hit)
		return false
	}
	before, _ := GetPartScan(id, "头部")
	if before.Slots[0].Value != "22.15%" {
		t.Error("candidate committed before confirmation")
		return false
	}
	if !(&EquipmentRerollAfterAcceptRouteAction{}).Run(ctx, &maa.CustomActionArg{TaskID: id, CurrentTaskName: "EquipmentRerollAfterAccept"}) {
		t.Error("after accept route failed")
		return false
	}
	after, _ := GetPartScan(id, "头部")
	if after.Slots[0].Value != values[0] {
		t.Error("accepted values not committed")
		return false
	}
	raw, err := ctx.GetNodeJSON("EquipmentRerollAfterAccept")
	var node struct {
		Next []struct {
			Name string `json:"name"`
		} `json:"next"`
	}
	if err != nil || json.Unmarshal([]byte(raw), &node) != nil || len(node.Next) != 1 || node.Next[0].Name != "EquipmentRerollKeepLockGate" {
		t.Errorf("unexpected continue route: %s error=%v", raw, err)
		return false
	}
	return true
}

// verifyValuePrepare 回放故障确认页，验证资源动作接线及角色/单件的初始化链路。
// 仅调用无点击的规划动作；不执行 next 中的锁变更或重洗。
func (p *reloadOfflineProbe) verifyValuePrepare(ctx *maa.Context, id int64) bool {
	t := p.t
	defer clearMonitorState(id)
	for _, mode := range []string{"character", "single"} {
		clearMonitorState(id)
		parts, _ := valueLogTestParts()
		stateMu.Lock()
		states[id] = monitorState{Part: "身躯", Parts: parts}
		stateMu.Unlock()
		// 限制为一轮无锁预算，避免测试依赖期望策略对新增锁的选择。
		setInventory(id, Inventory{CustomModules: 1})
		target := 15
		if mode == "character" {
			target = 60
		}
		if err := ctx.OverridePipeline(map[string]any{carrierNode: map[string]any{"attach": map[string]any{
			"operation": "value", "mode": mode, "part": "身躯", "value_tier_攻击力增加": target,
		}}}); err != nil {
			t.Error(err)
			return false
		}
		if _, ok := currentValuePlan(id); ok {
			t.Error("plan exists before prepare")
			return false
		}
		first, err := ctx.RunRecognition("EquipmentRerollValuePrepare", p.img, nil)
		if err != nil || first == nil || first.Hit {
			t.Errorf("%s accepted initial single frame: %+v %v", mode, first, err)
			return false
		}
		time.Sleep(220 * time.Millisecond)
		verified, err := ctx.RunRecognition("EquipmentRerollValuePrepare", p.img, nil)
		if err != nil || verified == nil || !verified.Hit {
			t.Errorf("%s initial locks did not stabilize: %+v %v", mode, verified, err)
			return false
		}
		raw, err := ctx.GetNodeJSON("EquipmentRerollValuePrepare")
		var node struct {
			Action struct {
				Type  string `json:"type"`
				Param struct {
					CustomAction string `json:"custom_action"`
				} `json:"param"`
			} `json:"action"`
		}
		if err != nil || json.Unmarshal([]byte(raw), &node) != nil || node.Action.Type != "Custom" || node.Action.Param.CustomAction != "EquipmentRerollValuePrepareAction" {
			t.Errorf("loaded prepare node lost its action: %s (%v)", raw, err)
			return false
		}
		if !(&EquipmentRerollValuePrepareAction{}).Run(ctx, &maa.CustomActionArg{TaskID: id, CurrentTaskName: "EquipmentRerollValuePrepare", RecognitionDetail: verified}) {
			t.Errorf("%s prepare action failed", mode)
			return false
		}
		plan, ok := currentValuePlan(id)
		if !ok || plan.Part != "身躯" || plan.Locks != ([maxSlot]SlotLock{}) {
			t.Errorf("%s did not freeze unlocked plan: %+v", mode, plan)
			return false
		}
		ready, err := ctx.RunRecognition("EquipmentRerollValueVerifyReady", p.img, nil)
		// 新方案与两帧视觉证据一致时不再重复锁识别；已有方案不能复用旧证据。
		for _, want := range []string{"EquipmentRerollPrepareRerollCost", "EquipmentRerollValueVerifyReady"} {
			raw, e := ctx.GetNodeJSON("EquipmentRerollValuePrepare")
			var routed struct {
				Next []struct {
					Name string `json:"name"`
				} `json:"next"`
			}
			if e != nil || json.Unmarshal([]byte(raw), &routed) != nil || len(routed.Next) != 1 || routed.Next[0].Name != want {
				t.Errorf("%s prepare route want %s: %s (%v)", mode, want, raw, e)
				return false
			}
			if !(&EquipmentRerollValuePrepareAction{}).Run(ctx, &maa.CustomActionArg{TaskID: id, CurrentTaskName: "EquipmentRerollValuePrepare"}) {
				t.Error("existing plan preparation failed")
				return false
			}
		}
		if err != nil || ready == nil || !ready.Hit {
			t.Errorf("%s ready verification failed: %+v %v", mode, ready, err)
			return false
		}
		if inv, _ := getInventory(id); inv != (Inventory{CustomModules: 1}) {
			t.Error("preparation consumed inventory")
			return false
		}
		t.Logf("%s: initial recognition -> prepare action -> frozen plan -> ready passed", mode)
	}
	return true
}

func (r *reloadOfflineRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	return &maa.CustomRecognitionResult{Box: arg.Roi}, r.probe.Run(ctx, &maa.CustomActionArg{TaskID: arg.TaskID})
}
