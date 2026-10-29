package equipmentreroll

import (
	"testing"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

// substitutionRuntimeConfig 是运行时联调用的「允许词条替换」配置：
// 1 条攻击 + 暴击率/暴击伤害合计 4 条。
func substitutionRuntimeConfig() carrierConfig {
	return parseCarrierConfig(`{"attach":{"quota_攻击力增加":1,"quota_暴击伤害增加":4,"allow_substitution":true,"substitution_affix1":"暴击率增加","substitution_affix2":"暴击伤害增加"}}`)
}

func seedGoalRuntime(t *testing.T, taskID int64, parts map[string]partScan, part string) {
	t.Helper()
	clearMonitorState(taskID)
	t.Cleanup(func() { clearMonitorState(taskID) })
	stateMu.Lock()
	states[taskID] = monitorState{Part: part, Parts: parts}
	stateMu.Unlock()
	setInventory(taskID, Inventory{CustomModules: 1000, CustomLockKeys: 1000})
}

func TestGoalRuntimeSubstitutedCritCommitsOnceAndStops(t *testing.T) {
	const taskID int64 = 993301
	cfg := substitutionRuntimeConfig()
	parts := multiGoalParts(partScanOf(TargetEffectCritRate, "防御力增加", ""))
	parts[equipmentParts[3]] = partScanOf("防御力增加", "", "")
	before := partScanOf("防御力增加", TargetEffectCritDamage, TargetEffectAttackIncrease)
	before.Slots[1].Lock, before.Slots[2].Lock = LockOneTime, LockPermanent
	parts[equipmentParts[0]] = before
	seedGoalRuntime(t, taskID, parts, equipmentParts[0])

	// 暴击率 2 条 + 暴击伤害 1 条 = 合计 3，仍差 1 条；把头部防御换成暴击率后合计 4 条达标。
	changed := [maxSlot]string{TargetEffectCritRate, TargetEffectCritDamage, TargetEffectAttackIncrease}
	result, ok := (&EquipmentRerollResultDecideRecognition{}).decideQuota(
		&maa.CustomRecognitionArg{TaskID: taskID}, resultDecideParam{Goals: cfg.resolveGoalQuotas(nil)},
		equipmentParts[0], changed, [maxSlot]string{}, [maxSlot]string{},
	)
	if !ok || result.Detail != resultDecisionDetail(ResultDecisionAccept) {
		t.Fatalf("mixed crit completion must be accepted: %+v / %v", result, ok)
	}
	staged, _ := GetPartScan(taskID, equipmentParts[0])
	if staged.Effects() != before.Effects() {
		t.Fatal("result decision committed effects before visual confirmation")
	}
	if staged.Slots[1].Lock != LockNone || staged.Slots[2].Lock != LockPermanent {
		t.Fatalf("temporary protection must expire without changing permanent lock: %+v", staged)
	}
	if !commitAcceptedResult(taskID) || commitAcceptedResult(taskID) {
		t.Fatal("confirmed candidate must commit exactly once")
	}
	committed, _ := GetEquipmentSlotScans(taskID)
	if AllPartsSatisfiedQuota(committed, cfg.Quota) || !rerollTargetSatisfied(committed, cfg) {
		t.Fatal("the single crit-damage quota must only pass through substitution")
	}
	if shouldContinueCurrentPartAfterAcceptWithMaterial(equipmentParts[0], cfg, committed, "自订密钥") {
		t.Fatal("completed substituted target must stop rerolling")
	}
	if _, need := desiredLockSlotForConfig(cfg, taskID, equipmentParts[0]); need {
		t.Fatal("completed substituted target must not acquire another lock")
	}
	if result, ok := (&EquipmentRerollPartNeedRecognition{}).runQuota(
		&maa.CustomRecognitionArg{TaskID: taskID}, partNeedParam{Part: partAll, Goals: cfg.resolveGoalQuotas(nil)},
	); !ok || result == nil {
		t.Fatal("completion recognition must accept a substituted target")
	}
}

func TestGoalRuntimeRequiresCompleteSnapshotAndDoesNotMixTargets(t *testing.T) {
	const taskID int64 = 993302
	cfg := parseCarrierConfig(`{"attach":{"quota_攻击力增加":4,"quota_优越代码伤害增加":4,"quota_暴击伤害增加":4,"allow_substitution":true,"substitution_affix1":"暴击率增加","substitution_affix2":"暴击伤害增加"}}`)
	parts := multiGoalParts(partScanOf(TargetEffectAttackIncrease, TargetEffectElementalDamage, TargetEffectCritRate))
	parts[equipmentParts[0]] = partScanOf(TargetEffectAttackIncrease, TargetEffectElementalDamage, "防御力增加")
	delete(parts, equipmentParts[3])
	seedGoalRuntime(t, taskID, parts, equipmentParts[0])
	changed := [maxSlot]string{TargetEffectAttackIncrease, TargetEffectElementalDamage, "防御力增加"}
	if result, ok := (&EquipmentRerollResultDecideRecognition{}).decideQuota(
		&maa.CustomRecognitionArg{TaskID: taskID}, resultDecideParam{Goals: cfg.resolveGoalQuotas(nil)},
		equipmentParts[0], changed, [maxSlot]string{}, [maxSlot]string{},
	); ok || result != nil || commitAcceptedResult(taskID) {
		t.Fatal("incomplete snapshot must not accept or stage a substituted result")
	}
	parts[equipmentParts[3]] = partScanOf(TargetEffectAttackIncrease, TargetEffectElementalDamage, TargetEffectCritRate)
	if rerollTargetSatisfied(parts, cfg) {
		t.Fatal("4 attack + 4 elemental + 3 mixed critical is not a complete 4-critical target")
	}
}

func TestGoalRuntimeReloadsOnlyMatchingJointLockPlan(t *testing.T) {
	cfg := parseCarrierConfig(`{"attach":{"quota_攻击力增加":4,"quota_优越代码伤害增加":4,"quota_暴击伤害增加":4,"allow_substitution":true,"substitution_affix1":"暴击率增加","substitution_affix2":"暴击伤害增加"}}`)
	parts := multiGoalParts(partScanOf(TargetEffectAttackIncrease, TargetEffectElementalDamage, TargetEffectCritRate))
	parts[equipmentParts[0]] = partScanOf(TargetEffectAttackIncrease, TargetEffectElementalDamage, "防御力增加")

	// 以同一套联合规划推导头部应有的锁位，历史计划必须与它完全一致才能复用。
	scan := parts[equipmentParts[0]]
	planned := [maxSlot]SlotLock{}
	planParts := make(map[string]partScan, len(parts))
	for part, value := range parts {
		planParts[part] = value
	}
	for countLocks(scan) < maxSlot-1 {
		slot, need := desiredLockSlotForQuotaAlternatives(planParts, equipmentParts[0], cfg.resolveGoalQuotas(nil), "自订密钥")
		if !need || slot == 0 {
			break
		}
		scan.Slots[slot-1].Lock = LockOneTime
		planned[slot-1] = LockOneTime
		planParts[equipmentParts[0]] = scan
	}
	if countLocks(scan) == 0 {
		t.Fatal("test scenario must require at least one lock on the incomplete piece")
	}
	history := previousLockSettings{Part: equipmentParts[0], Locks: planned}
	inv := Inventory{CustomModules: 100, CustomLockKeys: 1000}
	if got, ok := reusableLockPlan(cfg, parts, equipmentParts[0], inv, history); !ok || got != planned {
		t.Fatalf("shared effects should reuse matching joint lock plan: %v / %v", got, ok)
	}
	wrong := history
	if planned[0] == LockOneTime {
		wrong.Locks[0] = LockNone
	} else {
		wrong.Locks[0] = LockOneTime
	}
	if _, ok := reusableLockPlan(cfg, parts, equipmentParts[0], inv, wrong); ok {
		t.Fatal("joint targets must not restore a lock plan that differs from the current joint plan")
	}
	keyBudget, lockIndex := 0, 0
	for _, lock := range planned {
		if lock == LockOneTime {
			keyBudget += LockCost("自订密钥", lockIndex)
			lockIndex++
		}
	}
	inv.CustomLockKeys = keyBudget - 1
	if _, ok := reusableLockPlan(cfg, parts, equipmentParts[0], inv, history); ok {
		t.Fatalf("joint history reuse still requires enough keys for %d locks", lockIndex)
	}
	inv.CustomLockKeys = 1000
	parts[equipmentParts[0]] = partScanOf(TargetEffectAttackIncrease, TargetEffectElementalDamage, TargetEffectCritDamage)
	parts[equipmentParts[3]] = partScanOf(TargetEffectAttackIncrease, TargetEffectElementalDamage, TargetEffectCritRate)
	if _, ok := reusableLockPlan(cfg, parts, equipmentParts[0], inv, history); ok {
		t.Fatal("a completed substituted target must not restore old locks")
	}
}
