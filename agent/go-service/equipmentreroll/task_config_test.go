package equipmentreroll

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestTaskConfigAllSatisfiedKeepsPartAll 守护 EquipmentReroll 任务的配置结构。
//
// 基础 Pipeline 节点 EquipmentRerollAllSatisfied 必须保留 part:"all"，
// 否则 EquipmentRerollPartNeedRecognition 收到的 part 为空串，会走到单件判断分支并恒返回 false，
// 导致“四件均满足自定义配额”的全局完成判定永远不触发。
// 参见 docs/zh_cn/nikke/EquipmentReroll/洗词条策略与Agent逻辑.md §8.7。
func TestTaskConfigAllSatisfiedKeepsPartAll(t *testing.T) {
	pipelinePath := filepath.Join("..", "..", "..", "assets", "resource", "pipeline", "EquipmentReroll", "EquipmentReroll.json")
	data, err := os.ReadFile(pipelinePath)
	if err != nil {
		t.Skipf("skip: pipeline config not found: %v", err)
	}
	var pipeline struct {
		EquipmentRerollAllSatisfied struct {
			Recognition struct {
				Param struct {
					CustomRecognitionParam struct {
						Part string `json:"part"`
					} `json:"custom_recognition_param"`
				} `json:"param"`
			} `json:"recognition"`
		} `json:"EquipmentRerollAllSatisfied"`
	}
	if err := json.Unmarshal(data, &pipeline); err != nil {
		t.Fatalf("failed to parse pipeline config: %v", err)
	}
	if pipeline.EquipmentRerollAllSatisfied.Recognition.Param.CustomRecognitionParam.Part != "all" {
		t.Fatalf("EquipmentRerollAllSatisfied base node must keep part=\"all\", got %q; missing part makes the global completion check never pass", pipeline.EquipmentRerollAllSatisfied.Recognition.Param.CustomRecognitionParam.Part)
	}
}

// TestTaskConfigCarrierAttachOverrides 守护目标配置统一承载于 EquipmentRerollLockNeed 的 attach 顶层键。
//
// MaaFramework 对同一节点的多次 override 中 custom_recognition_param 是整体替换（会互相覆盖），
// 而 attach 顶层键按 key 合并（互不覆盖，MaaEnd 同款机制）。因此：
//   - 角色配额：9 个效果 select 的每个 case 写入 "attach.quota_<效果名>"（扁平顶层键，不用 global_quota）；
//   - 单件模式：需求词条 select 写入 "attach.wantN"，槽位 select 写入 "attach.slotN"。
//
// 参见 docs/zh_cn/nikke/EquipmentReroll/洗词条策略与Agent逻辑.md §9.2 与
// MaaFramework source/MaaFramework/Resource/PipelineParser.cpp（parse_custom_recognition_param / attach 合并）。
func TestTaskConfigCarrierAttachOverrides(t *testing.T) {
	taskPath := filepath.Join("..", "..", "..", "assets", "tasks", "EquipmentReroll.json")
	data, err := os.ReadFile(taskPath)
	if err != nil {
		t.Skipf("skip: task config not found: %v", err)
	}

	var cfg struct {
		Option map[string]struct {
			Type        string `json:"type"`
			DefaultCase string `json:"default_case"`
			Cases       []struct {
				Name             string `json:"name"`
				PipelineOverride map[string]struct {
					Attach map[string]json.RawMessage `json:"attach"`
				} `json:"pipeline_override"`
			} `json:"cases"`
		} `json:"option"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("failed to parse task config: %v", err)
	}

	// 角色配额：9 个效果 select，每个 case 必须写 attach.quota_<中文效果名>，且值等于该档位语义值。
	effectNames := []string{
		"ElementalDamage", "AttackIncrease", "MaxAmmo", "CritDamage", "CritRate",
		"ChargeSpeed", "ChargeDamage", "HitRate", "Defense",
	}
	effectZh := map[string]string{
		"ElementalDamage": "优越代码伤害增加",
		"AttackIncrease":  "攻击力增加",
		"MaxAmmo":         "最大装弹数增加",
		"CritDamage":      "暴击伤害增加",
		"CritRate":        "暴击率增加",
		"ChargeSpeed":     "蓄力速度增加",
		"ChargeDamage":    "蓄力伤害增加",
		"HitRate":         "命中率增加",
		"Defense":         "防御力增加",
	}
	// 档位 → 配额值。这份表是"选项 UI 语义"与"Go 端配额语义"之间的契约：
	// 这个 JSON 高度重复（9 效果 × 6 档），手写时把 Need3 写成 3 之外的值不会有任何报错，
	// 只会让用户选"需求 3 条"却按别的数量洗，因此这里逐个断言值。
	quotaCaseValue := map[string]int{
		"Forbid": -1, "None": 0, "Need1": 1, "Need2": 2, "Need3": 3, "Need4": 4,
	}
	for _, name := range effectNames {
		option, ok := cfg.Option["EquipmentRerollQuota"+name]
		if !ok {
			t.Fatalf("option EquipmentRerollQuota%s not found", name)
		}
		if option.Type != "select" {
			t.Fatalf("EquipmentRerollQuota%s must be a select, got %q", name, option.Type)
		}
		key := "quota_" + effectZh[name]
		seen := map[string]bool{}
		for _, c := range option.Cases {
			want, known := quotaCaseValue[c.Name]
			if !known {
				continue
			}
			seen[c.Name] = true
			checkCarrierAttach(t, "EquipmentRerollQuota"+name, c.Name, c.PipelineOverride, []string{key})
			assertAttachInt(t, "EquipmentRerollQuota"+name, c.Name, c.PipelineOverride, key, want)
		}
		for caseName := range quotaCaseValue {
			if !seen[caseName] {
				t.Fatalf("EquipmentRerollQuota%s missing case %s", name, caseName)
			}
		}
	}

	// 模式与部位也必须走 attach 承载：Go 侧只认 attach.mode 判定模式，
	// 只认 attach.part 决定洗哪一件。写到别的节点参数上会让模式判定失效。
	modeOpt, ok := cfg.Option["EquipmentRerollMode"]
	if !ok {
		t.Fatal("option EquipmentRerollMode not found")
	}
	if modeOpt.DefaultCase != "Character" {
		t.Fatalf("EquipmentRerollMode default_case = %q, want Character", modeOpt.DefaultCase)
	}
	modeValue := map[string]string{"Character": "character", "Single": "single"}
	seenMode := map[string]bool{}
	for _, c := range modeOpt.Cases {
		want, known := modeValue[c.Name]
		if !known {
			continue
		}
		seenMode[c.Name] = true
		checkCarrierAttach(t, "EquipmentRerollMode", c.Name, c.PipelineOverride, []string{"mode"})
		assertAttachString(t, "EquipmentRerollMode", c.Name, c.PipelineOverride, "mode", want)
	}
	for caseName := range modeValue {
		if !seenMode[caseName] {
			t.Fatalf("EquipmentRerollMode missing case %s", caseName)
		}
	}

	partOpt, ok := cfg.Option["EquipmentRerollSinglePart"]
	if !ok {
		t.Fatal("option EquipmentRerollSinglePart not found")
	}
	partValue := map[string]string{"Head": "头部", "Arms": "臂部", "Torso": "身躯", "Legs": "腿部"}
	seenPart := map[string]bool{}
	for _, c := range partOpt.Cases {
		want, known := partValue[c.Name]
		if !known {
			continue
		}
		seenPart[c.Name] = true
		checkCarrierAttach(t, "EquipmentRerollSinglePart", c.Name, c.PipelineOverride, []string{"part"})
		assertAttachString(t, "EquipmentRerollSinglePart", c.Name, c.PipelineOverride, "part", want)
	}
	for caseName := range partValue {
		if !seenPart[caseName] {
			t.Fatalf("EquipmentRerollSinglePart missing case %s", caseName)
		}
	}

	// 单件模式：3 组需求词条 select（wantN）+ 槽位 select（slotN）。
	slotValue := map[string]int{"Any": 0, "Slot1": 1, "Slot2": 2, "Slot3": 3}
	for n := 1; n <= 3; n++ {
		wantOpt, ok := cfg.Option[wantOptionName(n)]
		if !ok {
			t.Fatalf("option %s not found", wantOptionName(n))
		}
		slotOpt, ok := cfg.Option[slotOptionName(n)]
		if !ok {
			t.Fatalf("option %s not found", slotOptionName(n))
		}
		wantKey := "want" + strconv.Itoa(n)
		seenAffix := map[string]bool{}
		for _, c := range wantOpt.Cases {
			if c.Name == "None" {
				// "不需求" 不写 attach：该行留空即视为不需求。
				if len(c.PipelineOverride) != 0 {
					t.Fatalf("%s case None must not write any override", wantOptionName(n))
				}
				continue
			}
			zh, known := effectZh[c.Name]
			if !known {
				t.Fatalf("%s has unexpected case %s", wantOptionName(n), c.Name)
			}
			seenAffix[c.Name] = true
			checkCarrierAttach(t, wantOptionName(n), c.Name, c.PipelineOverride, []string{wantKey})
			// 写错效果名会让 Go 端 isOfficialEffect 判非法并直接结束任务，必须逐个核对。
			assertAttachString(t, wantOptionName(n), c.Name, c.PipelineOverride, wantKey, zh)
		}
		for _, name := range effectNames {
			if !seenAffix[name] {
				t.Fatalf("%s missing affix case %s", wantOptionName(n), name)
			}
		}

		slotKey := "slot" + strconv.Itoa(n)
		seenSlot := map[string]bool{}
		for _, c := range slotOpt.Cases {
			want, known := slotValue[c.Name]
			if !known {
				continue
			}
			seenSlot[c.Name] = true
			checkCarrierAttach(t, slotOptionName(n), c.Name, c.PipelineOverride, []string{slotKey})
			assertAttachInt(t, slotOptionName(n), c.Name, c.PipelineOverride, slotKey, want)
		}
		for caseName := range slotValue {
			if !seenSlot[caseName] {
				t.Fatalf("%s missing case %s", slotOptionName(n), caseName)
			}
		}
	}
}

// carrierOverride 是 pipeline_override 中承载点条目的解析形状（各断言辅助共用）。
type carrierOverride = map[string]struct {
	Attach map[string]json.RawMessage `json:"attach"`
}

func checkCarrierAttach(t *testing.T, option, caseName string, override carrierOverride, wantKeys []string) {
	t.Helper()
	ov, ok := override["EquipmentRerollLockNeed"]
	if !ok {
		t.Fatalf("%s case %s must override EquipmentRerollLockNeed carrier", option, caseName)
	}
	if len(ov.Attach) == 0 {
		t.Fatalf("%s case %s must write attach (top-level merge keys, see PipelineParser.cpp)", option, caseName)
	}
	for _, k := range wantKeys {
		if _, ok := ov.Attach[k]; !ok {
			t.Fatalf("%s case %s must write attach.%s", option, caseName, k)
		}
	}
}

func assertAttachString(t *testing.T, option, caseName string, override carrierOverride, key, want string) {
	t.Helper()
	var got string
	if err := json.Unmarshal(override["EquipmentRerollLockNeed"].Attach[key], &got); err != nil {
		t.Fatalf("%s case %s attach.%s is not a string: %v", option, caseName, key, err)
	}
	if got != want {
		t.Fatalf("%s case %s attach.%s = %q, want %q", option, caseName, key, got, want)
	}
}

func assertAttachInt(t *testing.T, option, caseName string, override carrierOverride, key string, want int) {
	t.Helper()
	var got int
	if err := json.Unmarshal(override["EquipmentRerollLockNeed"].Attach[key], &got); err != nil {
		t.Fatalf("%s case %s attach.%s is not an int: %v", option, caseName, key, err)
	}
	if got != want {
		t.Fatalf("%s case %s attach.%s = %d, want %d", option, caseName, key, got, want)
	}
}

// TestPipelineNodeReferencesResolve 守护 EquipmentReroll Pipeline 内部引用不悬空。
//
// 任务选项用 pipeline_override 把 next 指向 EquipmentRerollSingleScanRoute 这类节点，
// 而节点定义分散在 EquipmentReroll*.json 多个文件里。改名或删节点时若漏改引用方，
// MaaFramework 运行到那一步才会失败，日志只报"节点不存在"，很难定位到是哪个选项写错了。
func TestPipelineNodeReferencesResolve(t *testing.T) {
	pipelineDir := filepath.Join("..", "..", "..", "assets", "resource", "pipeline", "EquipmentReroll")
	entries, err := os.ReadDir(pipelineDir)
	if err != nil {
		t.Skipf("skip: pipeline dir not found: %v", err)
	}

	defined := map[string]bool{}
	nexts := map[string][]string{}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(pipelineDir, entry.Name()))
		if err != nil {
			t.Fatalf("failed to read %s: %v", entry.Name(), err)
		}
		var nodes map[string]struct {
			Next []string `json:"next"`
		}
		if err := json.Unmarshal(raw, &nodes); err != nil {
			t.Fatalf("failed to parse %s: %v", entry.Name(), err)
		}
		for name, node := range nodes {
			defined[name] = true
			nexts[name] = node.Next
		}
	}

	// 任务选项 override 里出现的 next 目标也要能解析。
	taskRaw, err := os.ReadFile(filepath.Join("..", "..", "..", "assets", "tasks", "EquipmentReroll.json"))
	if err != nil {
		t.Skipf("skip: task config not found: %v", err)
	}
	var task struct {
		Option map[string]struct {
			Cases []struct {
				Name             string `json:"name"`
				PipelineOverride map[string]struct {
					Next []string `json:"next"`
				} `json:"pipeline_override"`
			} `json:"cases"`
		} `json:"option"`
	}
	if err := json.Unmarshal(taskRaw, &task); err != nil {
		t.Fatalf("failed to parse task config: %v", err)
	}
	for optName, opt := range task.Option {
		for _, c := range opt.Cases {
			for node, ov := range c.PipelineOverride {
				if !defined[node] {
					t.Errorf("%s case %s overrides undefined node %q", optName, c.Name, node)
				}
				for _, target := range ov.Next {
					if !isResolvablePipelineTarget(target, defined) {
						t.Errorf("%s case %s next %q does not resolve to a defined node", optName, c.Name, target)
					}
				}
			}
		}
	}

	for name, next := range nexts {
		for _, target := range next {
			if !isResolvablePipelineTarget(target, defined) {
				t.Errorf("node %q next %q does not resolve to a defined node", name, target)
			}
		}
	}
}

// isResolvablePipelineTarget 判断 next 目标是否可解析。
// `[JumpBack]X` / `[Anchor]X` 等前缀语法的目标要么是节点名，要么是运行期锚点名——
// 锚点在别处以 anchor 声明，不在 next 的静态校验范围内，这里只校验裸节点名与
// 定义在本资源内的 CommonXxx 公共节点。
func isResolvablePipelineTarget(target string, defined map[string]bool) bool {
	if strings.HasPrefix(target, "[") {
		return true
	}
	if defined[target] {
		return true
	}
	// 公共节点（Common*/__* 内部节点）定义在其它资源目录，不在本次校验范围。
	return strings.HasPrefix(target, "Common") || strings.HasPrefix(target, "__")
}

func wantOptionName(n int) string {
	return "EquipmentRerollSingleWant" + strconv.Itoa(n)
}

func slotOptionName(n int) string {
	return "EquipmentRerollSingleWant" + strconv.Itoa(n) + "Slot"
}
