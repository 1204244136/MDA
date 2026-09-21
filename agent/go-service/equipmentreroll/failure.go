package equipmentreroll

import (
	"encoding/json"

	"github.com/1204244136/MDA/agent/go-service/pkg/i18n"
	"github.com/1204244136/MDA/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type equipmentRerollFailParam struct {
	Reason string `json:"reason"`
}

func equipmentRerollFailureKey(reason string) string {
	switch reason {
	case "value_scan":
		return "tasker.equipment_reroll.value_scan_failed"
	case "value_lock_plan":
		return "tasker.equipment_reroll.value_lock_plan_failed"
	case "confirm_not_advanced":
		return "tasker.equipment_reroll.confirm_not_advanced"
	case "reload_locks_verify_failed":
		return "tasker.equipment_reroll.reload_locks_verify_failed"
	case "lock_abort_page_unknown":
		return "tasker.equipment_reroll.lock_abort_page_unknown"
	default:
		return "tasker.equipment_reroll.operation_failed"
	}
}

// EquipmentRerollFailAction clears speculative state, reports the reason, and fails the task.
type EquipmentRerollFailAction struct{}

var _ maa.CustomActionRunner = &EquipmentRerollFailAction{}

func (a *EquipmentRerollFailAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		return false
	}
	var params equipmentRerollFailParam
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
		log.Error().Err(err).Str("component", "EquipmentReroll").Msg("failed to parse failure reason")
		return false
	}
	clearPendingLock(arg.TaskID)
	clearPendingRerollCost(arg.TaskID)
	clearValuePlan(arg.TaskID)
	resetTaskRetryGates(arg.TaskID)
	message := i18n.T(equipmentRerollFailureKey(params.Reason))
	maafocus.PrintLargeContentTrimNewline(message)
	log.Error().
		Str("component", "EquipmentReroll").
		Int64("task_id", arg.TaskID).
		Str("reason", params.Reason).
		Msg("equipment reroll stopped after unrecoverable pipeline state")
	return false
}

// EquipmentRerollLockAbortAction clears the abandoned lock plan before Pipeline restores the page.
type EquipmentRerollLockAbortAction struct{}

var _ maa.CustomActionRunner = &EquipmentRerollLockAbortAction{}

func (a *EquipmentRerollLockAbortAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		return false
	}
	clearPendingLock(arg.TaskID)
	resetTaskRetryGates(arg.TaskID)
	return true
}
