package equipmentreroll

import (
	"fmt"
	"strings"

	"github.com/1204244136/MDA/agent/go-service/pkg/i18n"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

// PrecheckLockResult 记录已有锁前置检查结果。
type PrecheckLockResult struct {
	Passed  bool
	Message string
	IsInfo  bool
}

// isLocksSubsequenceOrEqual 校验实际已有锁定是否与策略模拟的期望加锁序列完全匹配。
// 用户实际锁定了 K 个槽位（K ∈ {1, 2}）：
// 模拟的前 K 步加锁集合必须与实际锁定集合完全一致。
// 这保证了：如果装备原本无锁，要洗该装备第一件事（或前两步）正是去锁上这些槽位。
func isLocksSubsequenceOrEqual(actualLocks, simLocks []int) bool {
	if len(actualLocks) == 0 {
		return true
	}
	if len(simLocks) < len(actualLocks) {
		return false
	}
	simSet := make(map[int]bool, len(actualLocks))
	for i := 0; i < len(actualLocks); i++ {
		simSet[simLocks[i]] = true
	}
	for _, l := range actualLocks {
		if !simSet[l] {
			return false
		}
	}
	return true
}

// validatePreexistingLocks 执行细粒度的已有锁前置检查：
//   - 独立扫描入口（EquipmentRerollScanMain）：不限制锁定，由调用方直接放行；
//   - 单件模式：按单件目标（Want/AllowSlots）及单件锁定策略验证；
//   - 角色模式：按全局配额（GlobalQuota）先做部位初筛，四件齐全后再做全局策略核验。
func validatePreexistingLocks(ctx *maa.Context, taskID int64, part string, scan partScan) PrecheckLockResult {
	return validatePreexistingLocksForConfig(taskID, part, scan, loadCarrierConfig(ctx))
}

// validatePreexistingLocksForConfig 保留完整扫描的校验入口，末件无锁也须核对前面各件的已有锁。
func validatePreexistingLocksForConfig(taskID int64, part string, scan partScan, cfg carrierConfig) PrecheckLockResult {
	if cfg.isValue() {
		// 数值任务在完整范围扫描后按自身锁定计划统一校验。
		return PrecheckLockResult{Passed: true}
	}
	if cfg.isSingle() {
		return validateSinglePreexistingLocks(part, scan, cfg.Target, lockMaterialForTask(taskID, part))
	}

	return validateCharacterPreexistingLocks(taskID, part, scan, cfg)
}

// validateSinglePreexistingLocks 单件模式已有锁前置检查。
func validateSinglePreexistingLocks(part string, scan partScan, target singleTarget, material string) PrecheckLockResult {
	if countLocks(scan) == 0 {
		return PrecheckLockResult{Passed: true}
	}

	// 1. 3 槽全锁检查：无法进行效果变更
	if countLocks(scan) >= maxSlot {
		return PrecheckLockResult{
			Passed:  false,
			Message: fmt.Sprintf(i18n.T("tasker.equipment_reroll.preexisting_lock_all_locked"), part),
		}
	}

	// 2. 检查是否已经完全达成单件目标：达标装备无需洗练，已有锁完全合法
	if singlePartSatisfied(scan, target) {
		return PrecheckLockResult{
			Passed:  true,
			IsInfo:  true,
			Message: fmt.Sprintf(i18n.T("tasker.equipment_reroll.preexisting_lock_satisfied"), part),
		}
	}

	// 3. 检查各锁定槽位词条是否在 target.Want 中，以及是否满足槽位限定
	for i, slot := range scan.Slots {
		if slot.Lock == LockNone {
			continue
		}
		eff := strings.TrimSpace(slot.Effect)
		if eff == "" || eff == "未获得效果" {
			return PrecheckLockResult{
				Passed:  false,
				Message: fmt.Sprintf(i18n.T("tasker.equipment_reroll.preexisting_lock_empty"), part, i+1),
			}
		}
		slotLimit, want := target.Want[eff]
		if !want {
			return PrecheckLockResult{
				Passed:  false,
				Message: fmt.Sprintf(i18n.T("tasker.equipment_reroll.preexisting_lock_not_target"), part, i+1, eff, lockDisplayLabel(slot.Lock)),
			}
		}
		if !effectInAllowedSlot(scan.Effects(), eff, slotLimit) {
			return PrecheckLockResult{
				Passed:  false,
				Message: fmt.Sprintf(i18n.T("tasker.equipment_reroll.preexisting_lock_slot_conflict"), part, i+1, eff, lockDisplayLabel(slot.Lock)),
			}
		}
	}

	// 4. 检查单件目标是否不可达（如已有锁定占满槽位导致剩余槽位不足）
	if singleTargetUnreachable(scan, target) {
		return PrecheckLockResult{
			Passed:  false,
			Message: fmt.Sprintf(i18n.T("tasker.equipment_reroll.single_target_unreachable"), part),
		}
	}

	// 5. 策略一致性检查：如果该装备未加锁，按单件策略如果要洗它，期望加锁的槽位是否包含所有已有锁定
	unlockedScan := scan
	actualLocks := make([]int, 0, maxSlot)
	for i, s := range unlockedScan.Slots {
		if s.Lock != LockNone {
			actualLocks = append(actualLocks, i+1)
			unlockedScan.Slots[i].Lock = LockNone
		}
	}

	mat := lockMaterialOrDefault(material)
	simScan := unlockedScan
	simLocks := make([]int, 0, len(actualLocks))
	for step := 0; step < len(actualLocks); step++ {
		desiredSlot, need := singleDesiredLockSlot(simScan, target, mat)
		if !need || desiredSlot == 0 {
			break
		}
		simLocks = append(simLocks, desiredSlot)
		simScan.Slots[desiredSlot-1].Lock = LockPermanent
	}

	if !isLocksSubsequenceOrEqual(actualLocks, simLocks) {
		firstActual := actualLocks[0]
		return PrecheckLockResult{
			Passed: false,
			Message: fmt.Sprintf(
				i18n.T("tasker.equipment_reroll.preexisting_lock_strategy_mismatch"),
				part,
				firstActual,
				scan.Slots[firstActual-1].Effect,
				lockDisplayLabel(scan.Slots[firstActual-1].Lock),
			),
		}
	}

	firstActual := actualLocks[0]
	return PrecheckLockResult{
		Passed: true,
		IsInfo: true,
		Message: fmt.Sprintf(
			i18n.T("tasker.equipment_reroll.preexisting_lock_accepted"),
			part,
			firstActual,
			scan.Slots[firstActual-1].Effect,
			lockDisplayLabel(scan.Slots[firstActual-1].Lock),
		),
	}
}

// validateCharacterPreexistingLocks 角色模式已有锁前置检查。
func validateCharacterPreexistingLocks(taskID int64, part string, scan partScan, cfg carrierConfig) PrecheckLockResult {
	if countLocks(scan) == 0 && part != equipmentParts[len(equipmentParts)-1] {
		return PrecheckLockResult{Passed: true}
	}

	quota := cfg.resolveQuota(nil)

	// 1. 3 槽全锁检查：无法进行效果变更
	if countLocks(scan) >= maxSlot {
		return PrecheckLockResult{
			Passed:  false,
			Message: fmt.Sprintf(i18n.T("tasker.equipment_reroll.preexisting_lock_all_locked"), part),
		}
	}

	// 2. 检查每个已锁槽位的词条有效性
	for i, slot := range scan.Slots {
		if slot.Lock == LockNone {
			continue
		}
		eff := strings.TrimSpace(slot.Effect)
		if eff == "" || eff == "未获得效果" {
			return PrecheckLockResult{
				Passed:  false,
				Message: fmt.Sprintf(i18n.T("tasker.equipment_reroll.preexisting_lock_empty"), part, i+1),
			}
		}
		if quota[eff] < 0 {
			return PrecheckLockResult{
				Passed:  false,
				Message: fmt.Sprintf(i18n.T("tasker.equipment_reroll.preexisting_lock_forbidden"), part, i+1, eff, lockDisplayLabel(slot.Lock)),
			}
		}
		if quota[eff] == 0 {
			return PrecheckLockResult{
				Passed:  false,
				Message: fmt.Sprintf(i18n.T("tasker.equipment_reroll.preexisting_lock_unwanted"), part, i+1, eff, lockDisplayLabel(slot.Lock)),
			}
		}
	}

	// 本件是否还需要洗练取决于四件的目标分配，不能把空槽或不足三条当作未达标。
	// 1号槽已有锁延后到完整快照中判断，此处通过只表示继续扫描，不宣称策略已认可。
	if part != equipmentParts[len(equipmentParts)-1] {
		if scan.Slots[0].Lock != LockNone {
			return PrecheckLockResult{Passed: true}
		}
		firstLocked := 0
		for i, s := range scan.Slots {
			if s.Lock != LockNone {
				firstLocked = i + 1
				break
			}
		}
		return PrecheckLockResult{
			Passed: true,
			IsInfo: true,
			Message: fmt.Sprintf(
				i18n.T("tasker.equipment_reroll.preexisting_lock_accepted"),
				part,
				firstLocked,
				scan.Slots[firstLocked-1].Effect,
				lockDisplayLabel(scan.Slots[firstLocked-1].Lock),
			),
		}
	}

	// 扫描到腿部时，四件部位快照已全部写入
	parts, ok := GetEquipmentSlotScans(taskID)
	if !ok || len(parts) < len(equipmentParts) {
		return PrecheckLockResult{Passed: true}
	}

	return validateCharacterGlobalPreexistingLocks(parts, quota, lockMaterialForTask(taskID, ""))
}

// validateCharacterGlobalPreexistingLocks 角色模式四件全量扫描完成后的全局已有锁策略校验。
func validateCharacterGlobalPreexistingLocks(parts map[string]partScan, quota map[string]int, material string) PrecheckLockResult {
	mat := lockMaterialOrDefault(material)
	// 全局可达性检查
	if expectedModulesForQuotaWithMaterial(parts, quota, mat) >= costUnreachable {
		return PrecheckLockResult{
			Passed:  false,
			Message: i18n.T("tasker.equipment_reroll.quota_unreachable"),
		}
	}

	assigned := allocateQuotaRequiredWithMaterial(parts, quota, mat)

	// 逐部位核对已有锁是否符合策略
	for _, p := range equipmentParts {
		scan, ok := parts[p]
		if !ok || countLocks(scan) == 0 {
			continue
		}
		// 若该部位已不需要洗练（已达标），已有锁有效放行
		if partCompleteForQuota(scan, assigned[p], quota) {
			continue
		}
		// 该部位需要洗练：三个槽位均按同一策略核对，包含保护需求后清除禁止项的锁定方案。
		unlockedParts := make(map[string]partScan, len(parts))
		for k, v := range parts {
			unlockedParts[k] = v
		}
		unlockedScan := scan
		actualLocks := make([]int, 0, maxSlot)
		for i, s := range unlockedScan.Slots {
			if s.Lock != LockNone {
				actualLocks = append(actualLocks, i+1)
				unlockedScan.Slots[i].Lock = LockNone
			}
		}
		unlockedParts[p] = unlockedScan

		simParts := make(map[string]partScan, len(unlockedParts))
		for k, v := range unlockedParts {
			simParts[k] = v
		}
		simScan := unlockedScan
		simLocks := make([]int, 0, len(actualLocks))
		for step := 0; step < len(actualLocks); step++ {
			desiredSlot, need := DesiredLockSlotForQuota(simParts, p, quota, mat)
			if !need || desiredSlot == 0 {
				break
			}
			simLocks = append(simLocks, desiredSlot)
			simScan.Slots[desiredSlot-1].Lock = LockPermanent
			simParts[p] = simScan
		}

		if !isLocksSubsequenceOrEqual(actualLocks, simLocks) {
			firstActual := actualLocks[0]
			return PrecheckLockResult{
				Passed: false,
				Message: fmt.Sprintf(
					i18n.T("tasker.equipment_reroll.preexisting_lock_strategy_mismatch"),
					p,
					firstActual,
					scan.Slots[firstActual-1].Effect,
					lockDisplayLabel(scan.Slots[firstActual-1].Lock),
				),
			}
		}
	}

	return PrecheckLockResult{Passed: true}
}
