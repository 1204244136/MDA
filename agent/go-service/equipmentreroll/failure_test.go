package equipmentreroll

import (
	"testing"
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func TestEquipmentRerollFailureKey(t *testing.T) {
	tests := map[string]string{
		"confirm_not_advanced":       "tasker.equipment_reroll.confirm_not_advanced",
		"reload_locks_verify_failed": "tasker.equipment_reroll.reload_locks_verify_failed",
		"lock_abort_page_unknown":    "tasker.equipment_reroll.lock_abort_page_unknown",
		"unknown":                    "tasker.equipment_reroll.operation_failed",
	}
	for reason, want := range tests {
		if got := equipmentRerollFailureKey(reason); got != want {
			t.Fatalf("equipmentRerollFailureKey(%q) = %q, want %q", reason, got, want)
		}
	}
}

func TestEquipmentRerollLockAbortActionClearsPendingState(t *testing.T) {
	const taskID int64 = 901
	clearMonitorState(taskID)
	clearPendingLock(taskID)
	clearRetryGates()
	t.Cleanup(func() {
		clearMonitorState(taskID)
		clearPendingLock(taskID)
		clearRetryGates()
	})

	setPendingLock(taskID, 2, "自订密钥")
	setPendingRerollCost(taskID, MaterialUsage{CustomModules: 3, RerollModules: 3})
	retryGate["901|lock_confirm"] = retryGateState{count: 2, last: time.Now()}

	action := &EquipmentRerollLockAbortAction{}
	if !action.Run(&maa.Context{}, &maa.CustomActionArg{TaskID: taskID}) {
		t.Fatal("lock abort action failed")
	}
	if _, _, ok := getPendingLock(taskID); ok {
		t.Fatal("pending lock was not cleared")
	}
	if _, ok := retryGate["901|lock_confirm"]; ok {
		t.Fatal("retry gates were not cleared")
	}
	stateMu.Lock()
	pendingCost := states[taskID].PendingRerollCost
	stateMu.Unlock()
	if pendingCost.CustomModules != 3 {
		t.Fatal("recoverable lock abort must not clear the prepared reroll cost")
	}
}
