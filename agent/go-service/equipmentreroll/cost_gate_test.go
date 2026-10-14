package equipmentreroll

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// 本文件守住「读确认页消耗」（EquipmentRerollPrepareRerollCost）各入口都挂了重试闸门：
// 只给单项 next 时识别失败会静默轮询到 reco_timeout(20s)，新增入口漏挂闸门即回归。

const prepareRerollCostNode = "EquipmentRerollPrepareRerollCost"

const costGateNode = "__EquipmentRerollRetryGateCostUnreadable"

type pipelineNode struct {
	Recognition struct {
		Type  string `json:"type"`
		Param struct {
			CustomRecognition string `json:"custom_recognition"`
		} `json:"param"`
	} `json:"recognition"`
	Action struct {
		Type  string `json:"type"`
		Param struct {
			CustomAction string `json:"custom_action"`
			Param        struct {
				Reason string   `json:"reason"`
				Key    string   `json:"key"`
				Retry  string   `json:"retry"`
				GiveUp string   `json:"give_up"`
				Next   []string `json:"next"`
			} `json:"custom_action_param"`
		} `json:"param"`
	} `json:"action"`
	Next    []string `json:"next"`
	OnError []string `json:"on_error"`
}

func loadPipelineNodes(t *testing.T, path string) map[string]pipelineNode {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var nodes map[string]pipelineNode
	if err := json.Unmarshal(raw, &nodes); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return nodes
}

func containsNode(list []string, want string) bool {
	for _, n := range list {
		if n == want {
			return true
		}
	}
	return false
}

// TestNextForRouteTargetAddsCostGate 守住 Go 侧两条程序化入口。
func TestNextForRouteTargetAddsCostGate(t *testing.T) {
	next := nextForRouteTarget(prepareRerollCostNode)
	if len(next) != 2 || next[0].Name != prepareRerollCostNode || next[1].Name != costGateNode {
		t.Fatalf("读消耗的 next 必须紧跟重试闸门, got %+v", next)
	}
	for _, target := range []string{
		"EquipmentRerollKeepClickSlot2",
		"EquipmentRerollValueVerifyReady",
		"EquipmentRerollReloadLocks",
	} {
		got := nextForRouteTarget(target)
		if len(got) != 1 || got[0].Name != target {
			t.Fatalf("nextForRouteTarget(%q) = %+v, want 单项", target, got)
		}
	}
}

// TestCostGatePipelineWiring 守住 Pipeline 侧三条入口与闸门/失败节点的连线。
func TestCostGatePipelineWiring(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "assets", "resource", "pipeline", "EquipmentReroll")

	main := loadPipelineNodes(t, filepath.Join(dir, "EquipmentReroll.json"))
	gate, ok := main[costGateNode]
	if !ok {
		t.Fatalf("%s 未定义", costGateNode)
	}
	if gate.Action.Param.CustomAction != "EquipmentRerollRetryGateAction" {
		t.Errorf("%s action = %q, want EquipmentRerollRetryGateAction",
			costGateNode, gate.Action.Param.CustomAction)
	}
	if gate.Action.Param.Param.Retry != prepareRerollCostNode {
		t.Errorf("%s retry = %q, want %s", costGateNode, gate.Action.Param.Param.Retry, prepareRerollCostNode)
	}
	if gate.Action.Param.Param.GiveUp != "EquipmentRerollCostUnreadableFailed" {
		t.Errorf("%s give_up = %q, want EquipmentRerollCostUnreadableFailed",
			costGateNode, gate.Action.Param.Param.GiveUp)
	}
	if !containsNode(gate.Action.Param.Param.Next, prepareRerollCostNode) ||
		!containsNode(gate.Action.Param.Param.Next, costGateNode) {
		t.Errorf("%s next = %v, 必须能重试读消耗并回到自身形成有界循环",
			costGateNode, gate.Action.Param.Param.Next)
	}

	failNode, ok := main["EquipmentRerollCostUnreadableFailed"]
	if !ok {
		t.Fatal("EquipmentRerollCostUnreadableFailed 未定义")
	}
	if failNode.Action.Param.CustomAction != "EquipmentRerollFailAction" {
		t.Errorf("失败节点 action = %q, want EquipmentRerollFailAction", failNode.Action.Param.CustomAction)
	}
	reason := failNode.Action.Param.Param.Reason
	if reason != "cost_unreadable" {
		t.Errorf("失败节点 reason = %q, want cost_unreadable", reason)
	}

	// 三条 Pipeline 入口都必须挂闸门（Go 侧两条见 TestNextForRouteTargetAddsCostGate）。
	entries := map[string][]string{
		"EquipmentReroll.json/EquipmentRerollKeepLockRoute": main["EquipmentRerollKeepLockRoute"].Next,
		"EquipmentRerollReloadLocks.json/EquipmentRerollReloadLocksDone": loadPipelineNodes(t,
			filepath.Join(dir, "EquipmentRerollReloadLocks.json"))["EquipmentRerollReloadLocksDone"].Next,
		"EquipmentRerollValueLocks.json/EquipmentRerollValueVerifyReady": loadPipelineNodes(t,
			filepath.Join(dir, "EquipmentRerollValueLocks.json"))["EquipmentRerollValueVerifyReady"].Next,
	}
	for name, next := range entries {
		if !containsNode(next, costGateNode) {
			t.Errorf("%s 的 next 缺少 %s：读消耗失败会静默空转到 reco_timeout", name, costGateNode)
		}
	}

	// 闸门不能挂在读消耗节点自己的 next 上：其 action 会 resetTaskRetryGates 清空计数。
	if containsNode(main[prepareRerollCostNode].Next, costGateNode) {
		t.Errorf("%s 的 next 不能包含 %s：其 action 会重置闸门计数，导致永久循环",
			prepareRerollCostNode, costGateNode)
	}

	// 缺文案时用户看到的是 i18n key。
	for _, lang := range []string{"zh_cn", "en_us"} {
		path := filepath.Join("..", "..", "..", "assets", "locales", "go-service", lang+".json")
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var messages map[string]string
		if err := json.Unmarshal(raw, &messages); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if messages["tasker.equipment_reroll."+reason] == "" {
			t.Errorf("%s 缺少 tasker.equipment_reroll.%s 文案", lang, reason)
		}
	}
}
