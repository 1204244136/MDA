package equipmentreroll

import (
	"encoding/json"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type previousLockSettings struct {
	Part  string
	Locks [maxSlot]SlotLock
}

// reusableLockPlan 在副本上复用现有决策，比较完整锁计划，不因某一槽相同就恢复全部历史。
// 当装备已存在固定锁（LockPermanent）时，若计划只需通过历史单次锁恢复剩余锁位且与历史一致，同样支持复用。
func reusableLockPlan(cfg carrierConfig, parts map[string]partScan, part string, inv Inventory, previous previousLockSettings) ([maxSlot]SlotLock, bool) {
	var empty [maxSlot]SlotLock
	scan, ok := parts[part]
	if !ok || part != previous.Part {
		return empty, false
	}
	if cfg.isValue() {
		if cfg.validateOperation() != nil || (cfg.isSingle() && cfg.Part != part) {
			return empty, false
		}
	} else if cfg.isSingle() {
		if cfg.Part != part || !cfg.singleTargetOK() || len(cfg.Target.Want) == 0 {
			return empty, false
		}
	} else if cfg.Mode != rerollModeCharacter || !quotaIsValid(cfg.Quota) || len(parts) != len(equipmentParts) {
		return empty, false
	}
	hasKey := false
	for _, lock := range previous.Locks {
		hasKey = hasKey || lock == LockOneTime
	}
	if !hasKey {
		return empty, false
	}
	for i, slot := range scan.Slots {
		if slot.Lock == LockOneTime {
			return empty, false
		}
		if slot.Lock == LockPermanent && previous.Locks[i] != LockPermanent {
			return empty, false
		}
	}
	copyParts := make(map[string]partScan, len(parts))
	if cfg.isValue() {
		plan, err := planValueRerollWithInventory(parts, cfg, inv, part)
		return plan.Locks, err == nil && !plan.Satisfied && len(plan.Release) == 0 && plan.Locks == previous.Locks
	}
	for p, s := range parts {
		copyParts[p] = s
	}
	var planned [maxSlot]SlotLock
	for i, slot := range scan.Slots {
		planned[i] = slot.Lock
	}
	for countLocks(scan) < 2 {
		material, affordable := inv.ChooseLockMaterial(countLocks(scan))
		if !affordable {
			break
		}
		var slot int
		var need bool
		if cfg.isSingle() {
			slot, need = singleDesiredLockSlot(scan, cfg.Target, material)
		} else {
			slot, need = DesiredLockSlotForQuota(copyParts, part, cfg.Quota, material)
		}
		if !need {
			break
		}
		if slot < minSlot || slot > maxSlot || (!cfg.isValue() && slot == 1) || scan.Slots[slot-1].Lock != LockNone || material != "自订密钥" {
			return empty, false
		}
		inv.CustomLockKeys -= LockCost(material, countLocks(scan))
		scan.Slots[slot-1].Lock = LockOneTime
		planned[slot-1] = LockOneTime
		copyParts[part] = scan
	}
	return planned, planned == previous.Locks && inv.CanAffordReroll(countLocks(scan))
}

func reusableLocksForTask(taskID int64, cfg carrierConfig) ([maxSlot]SlotLock, bool) {
	stateMu.Lock()
	state, exists := states[taskID]
	parts := make(map[string]partScan, len(state.Parts))
	for p, s := range state.Parts {
		parts[p] = s
	}
	stateMu.Unlock()
	if !exists || !state.InventoryInitialized {
		return [maxSlot]SlotLock{}, false
	}
	return reusableLockPlan(cfg, parts, state.Part, state.Inventory, state.PreviousLocks)
}

// EquipmentRerollReloadLocksPlanRecognition 在点击前检查完整历史计划和材料余额。
type EquipmentRerollReloadLocksPlanRecognition struct{}

var _ maa.CustomRecognitionRunner = &EquipmentRerollReloadLocksPlanRecognition{}

func (r *EquipmentRerollReloadLocksPlanRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil {
		return nil, false
	}
	_, ok := reusableLocksForTask(arg.TaskID, loadCarrierConfig(ctx))
	if !ok {
		return nil, false
	}
	return &maa.CustomRecognitionResult{Box: arg.Roi}, true
}

// EquipmentRerollReloadLocksVerifyRecognition 等待实际锁位与计划一致，不在过渡帧写快照。
type EquipmentRerollReloadLocksVerifyRecognition struct{}

var _ maa.CustomRecognitionRunner = &EquipmentRerollReloadLocksVerifyRecognition{}

func (r *EquipmentRerollReloadLocksVerifyRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil || arg.Img == nil {
		return nil, false
	}
	planned, ok := reusableLocksForTask(arg.TaskID, loadCarrierConfig(ctx))
	if !ok {
		return nil, false
	}
	title, err := ctx.RunRecognition("__EquipmentRerollChangeEffectTitle", arg.Img, nil)
	if err != nil || title == nil || !title.Hit {
		return nil, false
	}
	actual, valid := readConfirmLocks(ctx, arg.Img)
	if !valid {
		return nil, false
	}

	if actual != planned {
		log.Debug().Int64("task_id", arg.TaskID).Interface("expected", planned).Interface("actual", actual).Msg("waiting for previous locks to be restored")
		return nil, false
	}
	part, ok := currentEffectPart(arg.TaskID)
	if !ok {
		return nil, false
	}
	data, _ := json.Marshal(previousLockSettings{Part: part, Locks: actual})
	return &maa.CustomRecognitionResult{Box: title.Box, Detail: string(data)}, true
}

// EquipmentRerollReloadLocksDoneAction 只提交已验证的锁状态；加载设置本身不扣库存。
type EquipmentRerollReloadLocksDoneAction struct{}

var _ maa.CustomActionRunner = &EquipmentRerollReloadLocksDoneAction{}

func (a *EquipmentRerollReloadLocksDoneAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		return false
	}
	planned, ok := reusableLocksForTask(arg.TaskID, loadCarrierConfig(ctx))
	if !ok {
		log.Error().Int64("task_id", arg.TaskID).Msg("reload lock plan became unavailable before commit")
		return false
	}
	part, ok := currentEffectPart(arg.TaskID)
	if !ok {
		return false
	}
	var verified previousLockSettings
	if err := json.Unmarshal([]byte(customRecognitionDetail(arg)), &verified); err != nil || verified.Part != part || verified.Locks != planned {
		log.Error().Err(err).Int64("task_id", arg.TaskID).Msg("reload locks lack matching visual verification")
		return false
	}
	for i, lock := range planned {
		if lock == LockOneTime {
			applyLockToSnapshot(arg.TaskID, part, i+1, "自订密钥")
		}
	}
	clearPendingLock(arg.TaskID)
	log.Info().Str("part", part).Interface("locks", planned).Msg("previous one-time locks restored to snapshot")
	return true
}
