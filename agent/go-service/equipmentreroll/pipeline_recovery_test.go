package equipmentreroll

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

type recoveryPipelineNode struct {
	Next    []string `json:"next"`
	OnError []string `json:"on_error"`
	Action  struct {
		Param struct {
			CustomAction string         `json:"custom_action"`
			CustomParam  retryGateParam `json:"custom_action_param"`
		} `json:"param"`
	} `json:"action"`
}

func loadRecoveryPipeline(t *testing.T, name string) map[string]recoveryPipelineNode {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	data, err := os.ReadFile(filepath.Join(filepath.Dir(file), "../../../assets/resource/pipeline/EquipmentReroll", name))
	if err != nil {
		t.Fatal(err)
	}
	var nodes map[string]recoveryPipelineNode
	if err := json.Unmarshal(data, &nodes); err != nil {
		t.Fatal(err)
	}
	return nodes
}

func TestLockAbortRestoresByObservedPage(t *testing.T) {
	nodes := loadRecoveryPipeline(t, "EquipmentReroll.json")
	abort := nodes["EquipmentRerollLockAbort"]
	if abort.Action.Param.CustomAction != "EquipmentRerollLockAbortAction" {
		t.Fatalf("lock abort action = %q", abort.Action.Param.CustomAction)
	}
	if len(abort.Next) != 2 || abort.Next[0] != "[JumpBack]CommonClosePage" || abort.Next[1] != "EquipmentRerollLockAbortAfterClose" {
		t.Fatalf("lock abort next = %v", abort.Next)
	}
	afterClose := nodes["EquipmentRerollLockAbortAfterClose"]
	if len(afterClose.Next) != 2 || afterClose.Next[0] != "EquipmentRerollLockAbortOnConfirmPage" || afterClose.Next[1] != "EquipmentRerollLockAbortOnDetailsPage" {
		t.Fatalf("lock abort page routes = %v", afterClose.Next)
	}
	if len(afterClose.OnError) != 1 || afterClose.OnError[0] != "EquipmentRerollLockAbortFailed" {
		t.Fatalf("lock abort failure route = %v", afterClose.OnError)
	}
}

func TestDangerousClicksHaveBoundedFailureRoutes(t *testing.T) {
	mainNodes := loadRecoveryPipeline(t, "EquipmentReroll.json")
	confirm := mainNodes["EquipmentRerollConfirmChangeEffect"]
	if len(confirm.Next) != 1 || confirm.Next[0] != "EquipmentRerollRecordRerollCost" ||
		len(confirm.OnError) != 1 || confirm.OnError[0] != "__EquipmentRerollRetryGateConfirmChangeEffect" {
		t.Fatalf("confirm change effect next = %v", confirm.Next)
	}
	confirmGate := mainNodes["__EquipmentRerollRetryGateConfirmChangeEffect"].Action.Param.CustomParam
	if confirmGate.GiveUp != "EquipmentRerollConfirmChangeEffectFailed" {
		t.Fatalf("confirm gate give_up = %q", confirmGate.GiveUp)
	}

	reloadNodes := loadRecoveryPipeline(t, "EquipmentRerollReloadLocks.json")
	reload := reloadNodes["EquipmentRerollReloadLocks"]
	if len(reload.Next) != 2 || reload.Next[0] != "EquipmentRerollReloadLocksDone" || reload.Next[1] != "__EquipmentRerollRetryGateReloadLocksDone" {
		t.Fatalf("reload locks next = %v", reload.Next)
	}
	reloadGate := reloadNodes["__EquipmentRerollRetryGateReloadLocksDone"].Action.Param.CustomParam
	if reloadGate.GiveUp != "EquipmentRerollReloadLocksFailed" {
		t.Fatalf("reload gate give_up = %q", reloadGate.GiveUp)
	}
}
