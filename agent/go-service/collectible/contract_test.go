package collectible

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/1204244136/MDA/agent/go-service/pkg/jsonclean"
)

// actionNode 是契约测试关心的那一小部分节点字段。
type actionNode struct {
	Action struct {
		Param struct {
			CustomAction      string          `json:"custom_action"`
			CustomActionParam json.RawMessage `json:"custom_action_param"`
		} `json:"param"`
	} `json:"action"`
	MaxHit int      `json:"max_hit"`
	Next   []string `json:"next"`
}

// Pipeline 与 Go 之间的参数契约：三个动作节点的 custom_action_param 必须能被各自的
// 参数结构解析出来，且关键节点名不为空。
// Go 侧不硬编码节点名，一旦 Pipeline 改了字段名或忘了 max_hit 预算，
// 只会在实机跑到那一步时才炸，所以这里直接读 Pipeline 文件核对。
func TestPipelineParamContract(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "assets", "resource", "pipeline", "CharacterPrep")
	files, err := filepath.Glob(filepath.Join(dir, "CharacterPrep*.json"))
	if err != nil || len(files) == 0 {
		t.Skipf("读不到 Pipeline（%v），跳过契约检查", err)
	}
	// 整目录一起读：CharacterPrep 下的流水线文件都可能被契约检查引用
	nodes := map[string]json.RawMessage{}
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Skipf("读不到 Pipeline（%v），跳过契约检查", err)
		}
		var part map[string]json.RawMessage
		if err := json.Unmarshal(jsonclean.Clean(raw), &part); err != nil { // Pipeline 是 JSONC
			t.Fatalf("Pipeline 解析失败（%s）: %v", filepath.Base(file), err)
		}
		for name, node := range part {
			nodes[name] = node
		}
	}
	read := func(name string) actionNode {
		var n actionNode
		if err := json.Unmarshal(nodes[name], &n); err != nil {
			t.Fatalf("解析节点 %s 失败: %v", name, err)
		}
		return n
	}
	if _, ok := nodes["CharacterPrepCollectibleStep"]; !ok {
		t.Fatal("Pipeline 里没有 CharacterPrepCollectibleStep")
	}

	// 决策动作
	step := read("CharacterPrepCollectibleStep")
	if step.Action.Param.CustomAction != "CharacterPrepCollectibleDecide" {
		t.Fatalf("决策节点的动作名变了: %q", step.Action.Param.CustomAction)
	}
	var decide DecideParam
	if err := json.Unmarshal(step.Action.Param.CustomActionParam, &decide); err != nil {
		t.Fatalf("DecideParam 解析失败: %v", err)
	}
	for name, got := range map[string]string{
		"stage_node": decide.Nodes.StageNode, "exp_node": decide.Nodes.ExpNode,
		"held.R": decide.Nodes.HeldNodes["R"], "held.SR": decide.Nodes.HeldNodes["SR"],
		"held.SSR": decide.Nodes.HeldNodes["SSR"],
		"sel.R":    decide.Nodes.SelectedNodes["R"], "pick.R": decide.Nodes.PickNodes["R"],
	} {
		if got == "" {
			t.Errorf("DecideParam 缺少 %s", name)
		}
	}
	if decide.MaxedNode == "" || decide.NoMaterialNode == "" {
		t.Error("DecideParam 缺少满级／工具箱不足出口")
	}

	// 档位核对动作
	var tier TierCheckParam
	check := read("CharacterPrepMaintainTierCheck")
	if err := json.Unmarshal(check.Action.Param.CustomActionParam, &tier); err != nil {
		t.Fatalf("TierCheckParam 解析失败: %v", err)
	}
	if tier.OkNode == "" || tier.MismatchNode == "" || tier.Nodes.DialogNodes["SSR"] == "" {
		t.Errorf("TierCheckParam 字段不全: %+v", tier)
	}

	// 次数设置动作
	var repeat SetRepeatParam
	set := read("CharacterPrepSetRepeat")
	if err := json.Unmarshal(set.Action.Param.CustomActionParam, &repeat); err != nil {
		t.Fatalf("SetRepeatParam 解析失败: %v", err)
	}
	if repeat.Nodes.RepeatNode == "" || repeat.PlusNode == "" || repeat.MaxNode == "" || len(repeat.Next) == 0 {
		t.Errorf("SetRepeatParam 字段不全: %+v", repeat)
	}

	for _, name := range []string{
		"CharacterPrepCollectiblePageReread",
		"CharacterPrepCollectibleEntryReread",
	} {
		if got := read(name).MaxHit; got <= 0 {
			t.Errorf("%s 必须配 max_hit 给重试设预算, got %d", name, got)
		}
	}

	// 保养完成后必须先给面板哨兵机会，再认定回到收藏品页：
	// 「收藏品」标签在面板上同样可见，抢在面板哨兵之前就会把面板当页面
	maintainDone := read("CharacterPrepMaintainDone").Next
	if panelIdx, pageIdx := indexOf(maintainDone, "CharacterPrepMaintainPanelVisible"), indexOf(maintainDone, "CharacterPrepCollectiblePageAfterMaintain"); panelIdx < 0 || pageIdx < 0 || panelIdx > pageIdx {
		t.Errorf("MaintainDone.next 里面板可见必须排在回收藏品页之前, got %v", maintainDone)
	}

	// 回收藏品页后的分派：先判面板（游戏会自己把面板开回来），面板已在就不能去点保养
	pageNext := read("CharacterPrepCollectiblePageAfterMaintain").Next
	if indexOf(pageNext, "CharacterPrepMaintainPanelVisible") != 0 {
		t.Errorf("PageAfterMaintain.next 必须先判面板可见, got %v", pageNext)
	}

	// 点保养之后要能识别出「计划外对话框」并点取消，而不是盲目再点一次保养
	clickPage := read("CharacterPrepClickMaintainPage").Next
	if indexOf(clickPage, "CharacterPrepMaintainPanelVisible") != 0 || indexOf(clickPage, "CharacterPrepCancelStaleDialog") != 1 {
		t.Errorf("ClickMaintainPage.next 必须先是面板可见、再是计划外对话框兜底, got %v", clickPage)
	}
	if next := read("CharacterPrepCancelStaleDialog").Next; indexOf(next, "CharacterPrepMaintainPanelVisible") < 0 {
		t.Errorf("取消计划外对话框后应回到面板判定, got %v", next)
	}
}

func indexOf(items []string, want string) int {
	for i, v := range items {
		if v == want {
			return i
		}
	}
	return -1
}
