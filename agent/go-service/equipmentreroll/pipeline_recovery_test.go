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
	Timeout int      `json:"timeout"`
	Action  struct {
		Param struct {
			CustomAction string `json:"custom_action"`
			CustomParam  struct {
				retryGateParam
				Reason string `json:"reason"`
			} `json:"custom_action_param"`
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

func TestResultRecognitionWaitCannotRepeatConsumption(t *testing.T) {
	nodes := loadRecoveryPipeline(t, "EquipmentReroll.json")
	wait := nodes["__EquipmentRerollResultButtonsVisible"]
	if wait.Timeout <= 0 || wait.Timeout > 5000 {
		t.Fatalf("result recognition wait is not bounded to five seconds: %d", wait.Timeout)
	}
	if len(wait.Next) != 1 || wait.Next[0] != "EquipmentRerollResultPage" {
		t.Fatalf("result recognition wait may repeat a consuming action: %v", wait.Next)
	}
	const failed = "EquipmentRerollResultUnreadableFailed"
	if len(wait.OnError) != 1 || wait.OnError[0] != failed {
		t.Fatalf("unreadable result must fail explicitly after polling: %v", wait.OnError)
	}
	failure := nodes[failed]
	if failure.Action.Param.CustomAction != "EquipmentRerollFailAction" || failure.Action.Param.CustomParam.Reason != "result_unreadable" || len(failure.Next) != 0 || len(failure.OnError) != 0 {
		t.Fatalf("result failure must stop without accepting or rerolling: %+v", failure)
	}
	if _, exists := nodes["__EquipmentRerollRetryGateResultPage"]; exists {
		t.Fatal("result polling must not use the sliding retry window that resets after each five-second wait")
	}
}

// TestRecordRerollCostHasResultUnreadableExit 守护结果页等待链路上的第二个缺口：
//
//	EquipmentRerollRecordRerollCost 是记账节点，本身没有识别兜底。它的 next 里若只剩
//	EquipmentRerollResultPage（数值模式 override 即如此），而该节点在 5 秒窗口内始终
//	识别不出，则 next 列表失败后就地裸失败——没有 on_error 就没有 result_unreadable
//	提示，也不会走 EquipmentRerollFailAction 清理投机状态。
//
// 参见 v1.11.8 用户日志 task 200000001：数值模式在 19:04:43.6~19:04:49.28 重复
// OCR 同一静止帧 6 次后 Tasker.Task.Failed，材料已扣而结果未选。
func TestRecordRerollCostHasResultUnreadableExit(t *testing.T) {
	nodes := loadRecoveryPipeline(t, "EquipmentReroll.json")
	const failed = "EquipmentRerollResultUnreadableFailed"
	node := nodes["EquipmentRerollRecordRerollCost"]
	// 基础流程经 __EquipmentRerollResultButtonsVisible 中转，该节点自带 on_error；
	// 数值模式 override 把 next 直连 EquipmentRerollResultPage，因此记账节点也必须自带出口。
	if len(node.OnError) != 1 || node.OnError[0] != failed {
		t.Fatalf("EquipmentRerollRecordRerollCost on_error = %v; unreadable result would fail the task without a user-visible reason", node.OnError)
	}
}
