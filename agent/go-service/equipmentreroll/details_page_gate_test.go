package equipmentreroll

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// 本文件守住「打开装备详情页」四个入口的重试闸门：后台输入方式下点击会静默失效，
// 单项 next 只能空转到 reco_timeout；挂闸门后超限即显式失败并提示改用前台输入方式。
func TestDetailsPageGateWiring(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "assets", "resource", "pipeline", "EquipmentReroll")
	nodes := loadPipelineNodes(t, filepath.Join(dir, "EquipmentRerollOpen.json"))

	parts := map[string]string{
		"Head":  "EquipmentRerollOpenHeadDetails",
		"Arms":  "EquipmentRerollOpenArmsDetails",
		"Torso": "EquipmentRerollOpenTorsoDetails",
		"Legs":  "EquipmentRerollOpenLegsDetails",
	}
	for part, openNode := range parts {
		gateName := "__EquipmentRerollRetryGate" + part + "DetailsOpened"
		gate, ok := nodes[gateName]
		if !ok {
			t.Fatalf("%s 未定义", gateName)
		}
		if !containsNode(nodes[openNode].Next, gateName) {
			t.Errorf("%s 的 next 缺少 %s：详情页没打开会静默空转到 reco_timeout(20s)",
				openNode, gateName)
		}
		if got := gate.Action.Param.CustomAction; got != "EquipmentRerollRetryGateAction" {
			t.Errorf("%s action = %q, want EquipmentRerollRetryGateAction", gateName, got)
		}
		p := gate.Action.Param.Param
		if p.Retry != openNode {
			t.Errorf("%s retry = %q, want %q", gateName, p.Retry, openNode)
		}
		if p.GiveUp != "EquipmentRerollDetailsPageFailed" {
			t.Errorf("%s give_up = %q, want EquipmentRerollDetailsPageFailed", gateName, p.GiveUp)
		}
		if !containsNode(p.Next, openNode) || !containsNode(p.Next, gateName) {
			t.Errorf("%s next = %v, 必须能重试点击并回到自身形成有界循环", gateName, p.Next)
		}
	}

	failNode := nodes["EquipmentRerollDetailsPageFailed"]
	if got := failNode.Action.Param.CustomAction; got != "EquipmentRerollFailAction" {
		t.Errorf("失败节点 action = %q, want EquipmentRerollFailAction", got)
	}
	if reason := failNode.Action.Param.Param.Reason; reason != "details_page_not_opened" {
		t.Errorf("失败节点 reason = %q, want details_page_not_opened", reason)
	}
}

// TestFailureReasonsHaveMessages 守住每个失败原因在两种语言里都有文案。
func TestFailureReasonsHaveMessages(t *testing.T) {
	src, err := os.ReadFile("failure.go")
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`"(tasker\.equipment_reroll\.[a-z_]+)"`)
	seen := map[string]bool{}
	var keys []string
	for _, m := range re.FindAllStringSubmatch(string(src), -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			keys = append(keys, m[1])
		}
	}
	if len(keys) < 5 {
		t.Fatalf("failure.go 里只找到 %d 个文案 key，解析可能已经失效", len(keys))
	}
	for _, lang := range []string{"zh_cn", "en_us"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "..",
			"assets", "locales", "go-service", lang+".json"))
		if err != nil {
			t.Fatal(err)
		}
		var messages map[string]string
		if err := json.Unmarshal(raw, &messages); err != nil {
			t.Fatalf("%s: %v", lang, err)
		}
		for _, k := range keys {
			if messages[k] == "" {
				t.Errorf("%s 缺少 %s", lang, k)
			}
		}
	}
}
