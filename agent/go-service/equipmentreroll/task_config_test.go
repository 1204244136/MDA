package equipmentreroll

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestTaskConfigAllSatisfiedKeepsPartAll 守护 EquipmentReroll 任务的 pipeline_override。
//
// EquipmentRerollAllSatisfied 的运行时 override 必须保留 part:"all"（与基础 Pipeline 节点一致），
// 否则 EquipmentRerollPartNeedRecognition 收到的 part 为空串，会走到单件判断分支并恒返回 false，
// 导致“四件均满足自定义配额”的全局完成判定永远不触发。
// 参见 docs/zh_cn/nikke/EquipmentReroll/洗词条策略与Agent逻辑.md §8.7。
func TestTaskConfigAllSatisfiedKeepsPartAll(t *testing.T) {
	path := filepath.Join("..", "..", "..", "assets", "tasks", "EquipmentReroll.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("skip: task config not found: %v", err)
	}

	var cfg struct {
		Option map[string]struct {
			PipelineOverride map[string]struct {
				Recognition struct {
					Param struct {
						CustomRecognitionParam struct {
							Part        string            `json:"part"`
							GlobalQuota map[string]string `json:"global_quota"`
						} `json:"custom_recognition_param"`
					} `json:"param"`
				} `json:"recognition"`
			} `json:"pipeline_override"`
		} `json:"option"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("failed to parse task config: %v", err)
	}

	option, ok := cfg.Option["EquipmentRerollQuota"]
	if !ok {
		t.Fatal("option EquipmentRerollQuota not found")
	}
	override, ok := option.PipelineOverride["EquipmentRerollAllSatisfied"]
	if !ok {
		t.Fatal("pipeline_override EquipmentRerollAllSatisfied not found")
	}
	param := override.Recognition.Param.CustomRecognitionParam
	if param.Part != "all" {
		t.Fatalf("EquipmentRerollAllSatisfied override must keep part=\"all\", got %q; missing part makes the global completion check never pass", param.Part)
	}
	if len(param.GlobalQuota) == 0 {
		t.Fatal("EquipmentRerollAllSatisfied override must carry non-empty global_quota")
	}
}
