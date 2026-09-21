package equipmentreroll

import (
	"encoding/json"
	"fmt"
	"image"
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const valueReleaseMaterial = "__release__"

type valueLockObservation struct {
	Settings previousLockSettings
	At       time.Time
	Stable   bool
}

// observeInitialValueLocks 只积累明确识别的间隔帧；暗相位未知或状态改变时重新计时。
func observeInitialValueLocks(taskID int64, part string, locks [maxSlot]SlotLock, valid bool, now time.Time) bool {
	stateMu.Lock()
	defer stateMu.Unlock()
	s := states[taskID]
	if s.Part != part || s.ValuePlan != nil {
		return false
	}
	settings := previousLockSettings{Part: part, Locks: locks}
	old := s.ValueLockObservation
	if !valid {
		s.ValueLockObservation = nil
		states[taskID] = s
		return false
	}
	if old == nil || old.Settings != settings || now.Sub(old.At) > 2*time.Second {
		s.ValueLockObservation = &valueLockObservation{Settings: settings, At: now}
		states[taskID] = s
		return false
	}
	old.Stable = now.Sub(old.At) >= 200*time.Millisecond
	return old.Stable
}

func syncValueInitialLocks(taskID int64, verified previousLockSettings) bool {
	stateMu.Lock()
	defer stateMu.Unlock()
	s := states[taskID]
	scan, ok := s.Parts[verified.Part]
	if !ok || s.Part != verified.Part || s.ValuePlan != nil || s.ValueLockObservation == nil || !s.ValueLockObservation.Stable || s.ValueLockObservation.Settings != verified {
		return false
	}
	var before [maxSlot]SlotLock
	for i, slot := range scan.Slots {
		lock := verified.Locks[i]
		if lock < LockNone || lock > LockOneTime || (slot.Effect == "" && lock != LockNone) {
			return false
		}
		before[i] = slot.Lock
		scan.Slots[i].Lock = lock
	}
	s.Parts[verified.Part] = scan
	s.ValueLockObservation = nil
	states[taskID] = s
	if before != verified.Locks {
		log.Info().Int64("task_id", taskID).Str("part", verified.Part).Interface("snapshot_locks", before).Interface("confirmed_locks", verified.Locks).Msg("value initial locks reconciled after stable observations")
	}
	return true
}

func valueInventory(taskID int64) Inventory {
	inv := optimisticLockInventory
	if v, ok := getModulesHeld(taskID); ok {
		inv.CustomModules = v
	}
	if v, ok := getKeysHeld(taskID); ok {
		inv.CustomLockKeys = v
	}
	return inv
}

func clearValuePlan(taskID int64) {
	stateMu.Lock()
	defer stateMu.Unlock()
	s := states[taskID]
	s.ValuePlan = nil
	s.ValueLockObservation = nil
	states[taskID] = s
}

func currentValuePlan(taskID int64) (valuePlan, bool) {
	stateMu.Lock()
	defer stateMu.Unlock()
	s := states[taskID]
	if s.ValuePlan == nil || s.ValuePlan.Part != s.Part {
		return valuePlan{}, false
	}
	return *s.ValuePlan, true
}

func storeValuePlan(taskID int64, p valuePlan) {
	stateMu.Lock()
	defer stateMu.Unlock()
	s := states[taskID]
	s.ValuePlan = &p
	s.ValueLockObservation = nil
	states[taskID] = s
}

func nextValueLockChange(scan partScan, plan valuePlan) (int, string) {
	// 先解除所有不需要的锁，再按已评估的顺序上锁，费用顺序不可变化。
	for i, slot := range scan.Slots {
		if slot.Lock != LockNone && slot.Lock != plan.Locks[i] {
			return i + 1, valueReleaseMaterial
		}
	}
	for _, step := range plan.Steps {
		if scan.Slots[step.Slot-1].Lock != plan.Locks[step.Slot-1] {
			return step.Slot, step.Material
		}
	}
	return 0, ""
}

// EquipmentRerollValuePrepareAction 在确认页冻结完整方案，逐个执行并验证锁变更。
type EquipmentRerollValuePrepareAction struct{}

var _ maa.CustomActionRunner = &EquipmentRerollValuePrepareAction{}

func (*EquipmentRerollValuePrepareAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		return false
	}
	cfg := loadCarrierConfig(ctx)
	if !cfg.isValue() {
		return false
	}
	part, ok := currentEffectPart(arg.TaskID)
	if !ok {
		return false
	}
	plan, exists := currentValuePlan(arg.TaskID)
	initialVerified := false
	if !exists {
		var verified previousLockSettings
		if json.Unmarshal([]byte(customRecognitionDetail(arg)), &verified) != nil || !syncValueInitialLocks(arg.TaskID, verified) {
			return false
		}
		started := time.Now()
		var err error
		plan, err = planValueRerollWithInventory(getScannedParts(arg.TaskID), cfg, valueInventory(arg.TaskID), part)
		if err != nil {
			log.Error().Err(err).Msg("cannot prepare value locks")
			return false
		}
		if plan.Satisfied {
			return routeEquipmentRerollEnd(ctx, arg.CurrentTaskName) == nil
		}
		storeValuePlan(arg.TaskID, plan)
		initialVerified = true
		log.Info().Int64("task_id", arg.TaskID).Dur("elapsed_ms", time.Since(started)).Interface("plan", plan).Msg("value round plan frozen")
	}
	scan, ok := GetPartScan(arg.TaskID, part)
	if !ok {
		return false
	}
	slot, material := nextValueLockChange(scan, plan)
	target := "EquipmentRerollValueVerifyReady"
	// 本次动作刚消费了两帧完整锁证据，且规划不改变任何锁时，直接读费用。
	// 已有方案、加锁、解锁和历史锁恢复仍须走独立就绪核验。
	if initialVerified && valueLocksMatchPlan(scan, plan) {
		target = "EquipmentRerollPrepareRerollCost"
	}
	if slot != 0 {
		setPendingLock(arg.TaskID, slot, material)
		if material == valueReleaseMaterial {
			target = fmt.Sprintf("EquipmentRerollValueReleaseSlot%d", slot)
		} else {
			target = keepLockRouteTarget(slot)
		}
	}
	// 历史加载也必须服从已冻结的完整方案；按钮不可用才逐槽落实。
	next := []maa.NextItem{{Name: target}}
	if locks, reusable := reusableLocksForTask(arg.TaskID, cfg); reusable && locks == plan.Locks {
		next = append([]maa.NextItem{{Name: "EquipmentRerollReloadLocks"}}, next...)
	}
	return ctx.OverrideNext(arg.CurrentTaskName, next) == nil
}

func valueLocksMatchPlan(scan partScan, plan valuePlan) bool {
	for i, slot := range scan.Slots {
		if slot.Lock != plan.Locks[i] {
			return false
		}
	}
	return true
}

// readConfirmLocks 只读取 Pipeline 定义的锁颜色/空槽，不把识别失败解释成未锁。
func readConfirmLocks(ctx *maa.Context, img image.Image) ([maxSlot]SlotLock, bool) {
	var actual [maxSlot]SlotLock
	title, err := ctx.RunRecognition("__EquipmentRerollChangeEffectTitle", img, nil)
	if err != nil || title == nil || !title.Hit {
		return actual, false
	}
	for i := range actual {
		var hits [3]bool
		for j, color := range []string{"Blue", "Orange", "Gray"} {
			d, err := ctx.RunRecognition(fmt.Sprintf("__EquipmentRerollConfirmSlot%dLock%s", i+1, color), img, nil)
			if err != nil || d == nil {
				return actual, false
			}
			hits[j] = d.Hit
		}
		if hits[0] && hits[1] {
			return actual, false
		}
		if !hits[0] && !hits[1] && !hits[2] {
			d, err := ctx.RunRecognition(fmt.Sprintf("__EquipmentRerollConfirmSlot%dEmpty", i+1), img, nil)
			if err != nil || d == nil || !d.Hit {
				return actual, false
			}
		}
		if hits[0] {
			actual[i] = LockPermanent
		} else if hits[1] {
			actual[i] = LockOneTime
		}
	}
	return actual, true
}

// EquipmentRerollValueLocksVerifyRecognition 同时验证单步锁变更和耗材前完整锁方案。
type EquipmentRerollValueLocksVerifyRecognition struct{}

var _ maa.CustomRecognitionRunner = &EquipmentRerollValueLocksVerifyRecognition{}

func (*EquipmentRerollValueLocksVerifyRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil || arg.Img == nil {
		return nil, false
	}
	var params struct {
		Ready   bool `json:"ready"`
		Initial bool `json:"initial"`
	}
	if json.Unmarshal([]byte(arg.CustomRecognitionParam), &params) != nil {
		return nil, false
	}
	plan, ok := currentValuePlan(arg.TaskID)
	if params.Initial {
		if ok {
			return &maa.CustomRecognitionResult{Box: arg.Roi}, true
		}
		part, exists := currentEffectPart(arg.TaskID)
		if !exists {
			return nil, false
		}
		actual, valid := readConfirmLocks(ctx, arg.Img)
		if !observeInitialValueLocks(arg.TaskID, part, actual, valid, time.Now()) {
			return nil, false
		}
		b, _ := json.Marshal(previousLockSettings{Part: part, Locks: actual})
		return &maa.CustomRecognitionResult{Box: arg.Roi, Detail: string(b)}, true
	}
	if !ok {
		return nil, false
	}
	scan, ok := GetPartScan(arg.TaskID, plan.Part)
	if !ok {
		return nil, false
	}
	expected := plan.Locks
	if !params.Ready {
		slot, material, has := getPendingLock(arg.TaskID)
		if !has || slot < 1 || slot > maxSlot {
			return nil, false
		}
		for i, s := range scan.Slots {
			expected[i] = s.Lock
		}
		expected[slot-1] = plan.Locks[slot-1]
		if material == valueReleaseMaterial {
			expected[slot-1] = LockNone
		}
	}
	actual, ok := readConfirmLocks(ctx, arg.Img)
	if !ok || actual != expected {
		log.Debug().Int64("task_id", arg.TaskID).Str("part", plan.Part).
			Bool("recognized", ok).Interface("expected_locks", expected).Interface("actual_locks", actual).
			Msg("value lock verification pending")
		return nil, false
	}
	b, _ := json.Marshal(previousLockSettings{Part: plan.Part, Locks: actual})
	return &maa.CustomRecognitionResult{Box: arg.Roi, Detail: string(b)}, true
}

// EquipmentRerollValueLocksDoneAction 只提交带视觉证据的锁变化，不扣费、不返还沉没成本。
type EquipmentRerollValueLocksDoneAction struct{}

var _ maa.CustomActionRunner = &EquipmentRerollValueLocksDoneAction{}

func (*EquipmentRerollValueLocksDoneAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		return false
	}
	var verified previousLockSettings
	if json.Unmarshal([]byte(customRecognitionDetail(arg)), &verified) != nil {
		return false
	}
	plan, ok := currentValuePlan(arg.TaskID)
	if !ok || verified.Part != plan.Part {
		return false
	}
	scan, ok := GetPartScan(arg.TaskID, plan.Part)
	if !ok {
		return false
	}
	slot, material, has := getPendingLock(arg.TaskID)
	if !has || slot < 1 || slot > maxSlot {
		return false
	}
	expected := scan
	expected.Slots[slot-1].Lock = plan.Locks[slot-1]
	if material == valueReleaseMaterial {
		expected.Slots[slot-1].Lock = LockNone
	}
	for i, s := range expected.Slots {
		if s.Lock != verified.Locks[i] {
			return false
		}
	}
	if material == valueReleaseMaterial {
		material = ""
	}
	applyLockToSnapshot(arg.TaskID, plan.Part, slot, material)
	clearPendingLock(arg.TaskID)
	resetTaskRetryGates(arg.TaskID)
	return true
}

// valueLockSelection 发现真实库存不足时停止，不把密钥方案偷偷替换为模组锁。
func valueLockSelection(taskID int64) (int, string, bool) {
	plan, ok := currentValuePlan(taskID)
	if !ok {
		return 0, "", false
	}
	inv := valueInventory(taskID)
	if inv.CustomModules < plan.FirstModules || inv.CustomLockKeys < plan.FirstKeys {
		return 0, "", false
	}
	scan, ok := GetPartScan(taskID, plan.Part)
	if !ok {
		return 0, "", false
	}
	slot, material := nextValueLockChange(scan, plan)
	return slot, material, slot > 0 && material != valueReleaseMaterial
}
