package equipmentreroll

import (
	"encoding/json"
	"image"
	"os"
	"path/filepath"
	"testing"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

// 原始故障面板按原像素保存；只识别本地图片，不绑定控制器或点击。
func TestEquipmentBlackResultOffline(t *testing.T) {
	lib := os.Getenv("MDA_MAA_LIB")
	if lib == "" {
		t.Skip("set MDA_MAA_LIB for offline black result recognition")
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
	if err := res.RegisterCustomRecognition("BlackResultOfflineProbe", &blackResultOfflineProbe{t}); err != nil {
		t.Fatal(err)
	}
	img := loadAffixPanel(t, "affix-result-black-panel.png", image.Pt(460, 110))
	if !tasker.PostRecognition(maa.RecognitionTypeCustom, maa.CustomRecognitionParam{CustomRecognition: "BlackResultOfflineProbe"}, img).Wait().Success() {
		t.Fatal("offline black result verification failed")
	}
}

type blackResultOfflineProbe struct{ t *testing.T }

func (p *blackResultOfflineProbe) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	t := p.t
	effects, values, raws, known := recognizeChangedEffects(ctx, arg.Img, partScan{}, false)
	if !known || effects != [3]string{"暴击率增加", "", "攻击力增加"} || values != [3]string{"7.07%", "", "13.22%"} {
		t.Errorf("black result: known=%v effects=%v values=%v raw=%v", known, effects, values, raws)
		return nil, false
	}
	// 重建本次反馈的角色配额和变更前快照；最高档“不需求”仍走正常成本比较。
	parts := map[string]partScan{}
	for part, slots := range map[string][3]slotScanData{
		"头部": {{Effect: "暴击率增加", Value: "2.30%"}, {Effect: "优越代码伤害增加", Value: "15.15%"}, {Effect: "攻击力增加", Value: "10.40%"}},
		"臂部": {{Effect: "攻击力增加", Value: "8.29%"}, {Effect: "优越代码伤害增加", Value: "16.55%"}, {Effect: "最大装弹数增加", Value: "40.17%"}},
		"身躯": {{Effect: "蓄力速度增加", Value: "3.75%"}, {}, {Effect: "攻击力增加", Value: "13.22%"}},
		"腿部": {{Effect: "优越代码伤害增加", Value: "12.34%"}, {Effect: "攻击力增加", Value: "11.11%"}, {}},
	} {
		parts[part] = partScan{Slots: slots}
	}
	clearMonitorState(arg.TaskID)
	defer clearMonitorState(arg.TaskID)
	source := parts["身躯"]
	source.Slots[2].Lock = LockOneTime
	stateMu.Lock()
	states[arg.TaskID] = monitorState{Part: "身躯", Parts: parts, ResultSource: &rerollResultSource{Part: "身躯", Scan: source}}
	stateMu.Unlock()
	if err := ctx.OverridePipeline(map[string]any{carrierNode: map[string]any{"attach": map[string]any{
		"operation": "effect", "mode": "character",
		"quota_优越代码伤害增加": 4, "quota_攻击力增加": 4, "quota_最大装弹数增加": 1,
		"quota_暴击伤害增加": 0, "quota_暴击率增加": 0,
		"quota_蓄力速度增加": -1, "quota_蓄力伤害增加": -1, "quota_命中率增加": -1, "quota_防御力增加": -1,
	}}}); err != nil {
		t.Error(err)
		return nil, false
	}
	copyArg := *arg
	copyArg.CustomRecognitionParam = `{}`
	decision, hit := (&EquipmentRerollResultDecideRecognition{}).Run(ctx, &copyArg)
	if !hit || decision == nil || resultRouteTarget(decision.Detail) != "EquipmentRerollResultClickKeep" {
		t.Errorf("unneeded black result: hit=%v decision=%+v", hit, decision)
		return nil, false
	}
	if got, ok := GetPartScan(arg.TaskID, "身躯"); !ok || got != parts["身躯"] || commitAcceptedResult(arg.TaskID) {
		t.Errorf("keep decision changed the equipment snapshot: ok=%v scan=%+v", ok, got)
	}
	return &maa.CustomRecognitionResult{Box: arg.Roi}, !t.Failed()
}
