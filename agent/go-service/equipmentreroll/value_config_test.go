package equipmentreroll

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValueOptionsReachCarrier(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("../../../assets/tasks/EquipmentReroll.json"))
	if err != nil {
		t.Fatal(err)
	}
	type optionCase struct {
		Name     string                     `json:"name"`
		Option   []string                   `json:"option"`
		Override map[string]json.RawMessage `json:"pipeline_override"`
	}
	var task struct {
		Task []struct {
			Option []string `json:"option"`
		} `json:"task"`
		Option map[string]struct {
			Default string       `json:"default_case"`
			Cases   []optionCase `json:"cases"`
			Inputs  []struct {
				Name    string `json:"name"`
				Default string `json:"default"`
				Type    string `json:"pipeline_type"`
			} `json:"inputs"`
			Override map[string]json.RawMessage `json:"pipeline_override"`
		} `json:"option"`
	}
	if err := json.Unmarshal(data, &task); err != nil {
		t.Fatal(err)
	}
	if len(task.Task) != 1 || len(task.Task[0].Option) != 1 || task.Task[0].Option[0] != "EquipmentRerollOperation" {
		t.Fatal("task must expose the operation category")
	}
	operation := task.Option["EquipmentRerollOperation"]
	if operation.Default != "Effect" || len(operation.Cases) != 2 {
		t.Fatal("default must preserve effect reroll")
	}
	if strings.Join(operation.Cases[0].Option, ",") != "EquipmentRerollMode" {
		t.Fatal("old effect mode option must remain reachable")
	}
	if strings.Join(operation.Cases[1].Option, ",") != "EquipmentRerollValueMode" {
		t.Fatal("value mode and targets must be reachable")
	}
	input := task.Option["EquipmentRerollValueTargets"]
	if len(input.Inputs) != len(officialEffects) {
		t.Fatal("all nine effects need tier inputs")
	}
	raw := string(input.Override[carrierNode])
	for _, field := range input.Inputs {
		if field.Type != "int" {
			t.Fatalf("%s must be an integer", field.Name)
		}
		placeholder := `"{` + field.Name + `}"`
		if !strings.Contains(raw, placeholder) {
			t.Fatalf("missing placeholder %s", placeholder)
		}
		raw = strings.ReplaceAll(raw, placeholder, field.Default)
	}
	var carrier struct {
		Attach map[string]any `json:"attach"`
	}
	if err := json.Unmarshal([]byte(raw), &carrier); err != nil {
		t.Fatal(err)
	}
	mode := task.Option["EquipmentRerollValueMode"]
	if mode.Default != "Character" || len(mode.Cases) != 2 {
		t.Fatal("value mode must support character and single")
	}
	for _, choice := range mode.Cases {
		inputName, wantTier := "EquipmentRerollValueTargets", 11
		if choice.Name == "Character" {
			inputName, wantTier = "EquipmentRerollValueTotalTargets", 44
		}
		found := false
		for _, name := range choice.Option {
			found = found || name == inputName
		}
		if !found {
			t.Fatalf("%s does not expose %s", choice.Name, inputName)
		}
		modeInput := task.Option[inputName]
		modeRaw := string(modeInput.Override[carrierNode])
		for _, field := range modeInput.Inputs {
			modeRaw = strings.ReplaceAll(modeRaw, `"{`+field.Name+`}"`, field.Default)
		}
		if err := json.Unmarshal([]byte(modeRaw), &carrier); err != nil {
			t.Fatal(err)
		}
		var modeCarrier struct {
			Attach map[string]any `json:"attach"`
		}
		if err := json.Unmarshal(choice.Override[carrierNode], &modeCarrier); err != nil {
			t.Fatal(err)
		}
		carrier.Attach["operation"] = "value"
		carrier.Attach["mode"] = modeCarrier.Attach["mode"]
		carrier.Attach["part"] = "头部"
		encoded, _ := json.Marshal(carrier)
		cfg := parseCarrierConfig(string(encoded))
		if err := cfg.validateOperation(); err != nil || cfg.ValueTargets["优越代码伤害增加"] != wantTier {
			t.Fatalf("UI -> carrier failed: %+v %v", cfg, err)
		}
		if cfg.isSingle() != (choice.Name == "Single") || rerollDecisionTarget(cfg) != "EquipmentRerollValueDecide" {
			t.Fatalf("UI mode mismatch: %+v", cfg)
		}
	}
}

// loadValueOptionOverrides 读取真实选项，供静态接线检查和框架离线回放共用。
func loadValueOptionOverrides(t *testing.T) map[string]json.RawMessage {
	t.Helper()
	data, err := os.ReadFile("../../../assets/tasks/EquipmentReroll.json")
	if err != nil {
		t.Fatal(err)
	}
	var task struct {
		Option map[string]struct {
			Cases []struct {
				Name     string                     `json:"name"`
				Override map[string]json.RawMessage `json:"pipeline_override"`
			} `json:"cases"`
		} `json:"option"`
	}
	if err := json.Unmarshal(data, &task); err != nil {
		t.Fatal(err)
	}
	for _, choice := range task.Option["EquipmentRerollOperation"].Cases {
		if choice.Name == "Value" {
			return choice.Override
		}
	}
	t.Fatal("value operation option is missing")
	return nil
}

func TestValuePipelineUsesSharedFlow(t *testing.T) {
	overrides := loadValueOptionOverrides(t)
	// 选项必须直接接通数值确认页与锁变更核验，不能依赖 Go 初始化覆盖。
	for name, want := range map[string]string{
		"EquipmentRerollConfirmPageBranch": "EquipmentRerollValuePrepare",
		"EquipmentRerollLockConfirm":       "EquipmentRerollValueVerifyChange",
	} {
		var node recoveryPipelineNode
		if err := json.Unmarshal(overrides[name], &node); err != nil {
			t.Fatal(err)
		}
		if len(node.Next) != 1 || node.Next[0] != want || len(node.OnError) != 1 || node.OnError[0] != "EquipmentRerollValuePlanFailed" {
			t.Fatalf("value option %s lost its verified route: %+v", name, node)
		}
	}
	files, err := filepath.Glob("../../../assets/resource/pipeline/EquipmentReroll/*.json")
	if err != nil {
		t.Fatal(err)
	}
	defined := map[string]bool{}
	for _, file := range files {
		for name := range loadRecoveryPipeline(t, filepath.Base(file)) {
			defined[name] = true
		}
	}
	for name := range overrides {
		if !defined[name] {
			t.Errorf("value option overrides undefined node %s", name)
		}
	}
	locks := loadRecoveryPipeline(t, "EquipmentRerollValueLocks.json")
	if locks["EquipmentRerollValuePrepare"].Action.Param.CustomAction != "EquipmentRerollValuePrepareAction" {
		t.Fatal("initial lock recognition must execute the plan preparation action before ready verification")
	}
	nodes := loadRecoveryPipeline(t, "EquipmentRerollChangeValue.json")
	decision := nodes["EquipmentRerollValueDecide"]
	if decision.Action.Param.CustomAction != "EquipmentRerollValueDecideAction" ||
		len(decision.Next) != 1 || decision.Next[0] != "EquipmentRerollFinalSummary" || len(decision.OnError) != 0 {
		t.Fatal("value decision must choose the part in Go and retain a safe summary fallback")
	}
	back := nodes["EquipmentRerollValueReturnToDecide"]
	if len(back.Next) != 2 || back.Next[1] != "EquipmentRerollValueDecide" {
		t.Fatal("value result must not return to effect strategy")
	}
	first := nodes["EquipmentRerollKeepClickSlot1"]
	if len(first.Next) != 2 || first.Next[0] != "EquipmentRerollLockPageEntered" {
		t.Fatal("first slot must use shared locking flow")
	}
	main := loadRecoveryPipeline(t, "EquipmentReroll.json")
	if main["EquipmentRerollFlow"].Action.Param.CustomAction != "EquipmentRerollConfigCheckAction" {
		t.Fatal("operation must be validated before scanning")
	}
}

func TestSharedValueResultStaging(t *testing.T) {
	const id int64 = 9210
	t.Cleanup(func() { clearMonitorState(id) })
	_ = setCurrentPart(id, "头部")
	effects := [maxSlot]string{"攻击力增加", "最大装弹数增加"}
	before := [maxSlot]string{"7.59%", "73.04%"}
	after := [maxSlot]string{"8.29%", "73.04%"}
	updatePartEffects(id, "头部", effects, before)
	applyLockToSnapshot(id, "头部", 2, "自订密钥")
	stageResultDecision(id, "头部", ResultDecisionAccept, effects, after)
	scan, _ := GetPartScan(id, "头部")
	if scan.Slots[0].Value != before[0] || scan.Slots[1].Lock != LockNone {
		t.Fatal("staging must expire single-use locks without applying candidate values")
	}
	if !commitAcceptedResult(id) {
		t.Fatal("confirmed value result should commit through shared primitive")
	}
	scan, _ = GetPartScan(id, "头部")
	if scan.Effects() != effects || scan.Slots[0].Value != after[0] || commitAcceptedResult(id) {
		t.Fatal("value commit must retain effects and be idempotent")
	}
}
