package equipmentreroll

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// 本文件守住「打开装备详情页」四个入口的失败处理。
//
// 这四个节点带 Click，任何形式的重试都等于再点一次：v1.10.16 曾给它们挂重试闸门，
// 结果弹窗打开后又被第二次点击关掉（弹窗淡出使锚点判据读不到蓝色图标），反而卡死。
// 因此这里只允许用 on_error 报错，不允许把闸门挂回 next。

func TestDetailsPageFailureWiring(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "assets", "resource", "pipeline", "EquipmentReroll")
	nodes := loadPipelineNodes(t, filepath.Join(dir, "EquipmentRerollOpen.json"))

	for _, part := range []string{"Head", "Arms", "Torso", "Legs"} {
		openNode := "EquipmentRerollOpen" + part + "Details"
		enteredNode := "EquipmentReroll" + part + "DetailsEntered"

		node, ok := nodes[openNode]
		if !ok {
			t.Fatalf("%s 未定义", openNode)
		}
		if len(node.Next) != 1 || node.Next[0] != enteredNode {
			t.Errorf("%s next = %v, want 仅 %s：带 Click 的节点重试会多点一次",
				openNode, node.Next, enteredNode)
		}
		if len(node.OnError) != 1 || node.OnError[0] != "EquipmentRerollDetailsPageFailed" {
			t.Errorf("%s on_error = %v, want [EquipmentRerollDetailsPageFailed]",
				openNode, node.OnError)
		}
		if gate := "__EquipmentRerollRetryGate" + part + "DetailsOpened"; gate != "" {
			if _, exists := nodes[gate]; exists {
				t.Errorf("%s 不应存在：重试点击会关掉已打开的详情页", gate)
			}
		}
	}

	failNode, ok := nodes["EquipmentRerollDetailsPageFailed"]
	if !ok {
		t.Fatal("EquipmentRerollDetailsPageFailed 未定义")
	}
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
