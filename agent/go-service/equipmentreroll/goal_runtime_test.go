package equipmentreroll

import (
	"testing"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func runtimeAlternativeConfig() carrierConfig {
	goals := multiGoalExample()
	return carrierConfig{
		Mode: rerollModeCharacter, Quota: goals[0],
		AlternativeEnabled: [2]bool{true},
		AlternativeQuotas:  [2]map[string]int{goals[1]},
	}
}

func seedAlternativeRuntime(t *testing.T, taskID int64, parts map[string]partScan, part string) {
	t.Helper()
	clearMonitorState(taskID)
	t.Cleanup(func() { clearMonitorState(taskID) })
	stateMu.Lock()
	states[taskID] = monitorState{Part: part, Parts: parts}
	stateMu.Unlock()
	setInventory(taskID, Inventory{CustomModules: 1000, CustomLockKeys: 1000})
}

func TestGoalRuntimeSecondTargetCommitsAfterConfirmationAndStops(t *testing.T) {
	const taskID int64 = 993301
	cfg := runtimeAlternativeConfig()
	parts := multiGoalParts(partScanOf(TargetEffectAttackIncrease, TargetEffectElementalDamage, "暴击率增加"))
	before := partScanOf("防御力增加", TargetEffectElementalDamage, TargetEffectAttackIncrease)
	before.Slots[1].Lock, before.Slots[2].Lock = LockOneTime, LockPermanent
	parts["头部"] = before
	seedAlternativeRuntime(t, taskID, parts, "头部")
	changed := [maxSlot]string{"最大装弹数增加", TargetEffectElementalDamage, TargetEffectAttackIncrease}
	result, ok := (&EquipmentRerollResultDecideRecognition{}).decideQuota(
		&maa.CustomRecognitionArg{TaskID: taskID}, resultDecideParam{Goals: cfg.resolveGoalQuotas(nil)},
		"头部", changed, [maxSlot]string{}, [maxSlot]string{},
	)
	if !ok || result.Detail != resultDecisionDetail(ResultDecisionAccept) {
		t.Fatalf("second target result must be accepted: %+v / %v", result, ok)
	}
	staged, _ := GetPartScan(taskID, "头部")
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
		t.Fatal("second target alone must satisfy the configured OR target")
	}
	if shouldContinueCurrentPartAfterAcceptWithMaterial("头部", cfg, committed, "自订密钥") {
		t.Fatal("accepted complete second target must stop rerolling")
	}
	if _, need := desiredLockSlotForConfig(cfg, taskID, "头部"); need {
		t.Fatal("completed second target must not acquire another lock")
	}
	if result, ok := (&EquipmentRerollPartNeedRecognition{}).runQuota(
		&maa.CustomRecognitionArg{TaskID: taskID}, partNeedParam{Part: partAll, Goals: cfg.resolveGoalQuotas(nil)},
	); !ok || result == nil {
		t.Fatal("completion recognition must accept a complete second target")
	}
}

func TestGoalRuntimeRequiresCompleteSnapshotAndDoesNotMixTargets(t *testing.T) {
	const taskID int64 = 993302
	cfg := runtimeAlternativeConfig()
	parts := multiGoalParts(partScanOf(TargetEffectAttackIncrease, TargetEffectElementalDamage, "暴击率增加"))
	parts["头部"] = partScanOf(TargetEffectAttackIncrease, TargetEffectElementalDamage, "防御力增加")
	delete(parts, "腿部")
	seedAlternativeRuntime(t, taskID, parts, "头部")
	changed := [maxSlot]string{TargetEffectAttackIncrease, TargetEffectElementalDamage, "最大装弹数增加"}
	if result, ok := (&EquipmentRerollResultDecideRecognition{}).decideQuota(
		&maa.CustomRecognitionArg{TaskID: taskID}, resultDecideParam{Goals: cfg.resolveGoalQuotas(nil)},
		"头部", changed, [maxSlot]string{}, [maxSlot]string{},
	); ok || result != nil || commitAcceptedResult(taskID) {
		t.Fatal("incomplete snapshot must not accept or stage a multi-goal result")
	}
	parts["腿部"] = partScanOf(TargetEffectAttackIncrease, TargetEffectElementalDamage, "暴击率增加")
	if rerollTargetSatisfied(parts, cfg) {
		t.Fatal("4 attack + 4 elemental + 3 critical + garbage is not a complete target")
	}
}

func TestGoalRuntimeReloadsOnlyMatchingJointLockPlan(t *testing.T) {
	cfg := runtimeAlternativeConfig()
	parts := multiGoalParts(partScanOf(TargetEffectAttackIncrease, TargetEffectElementalDamage, "暴击率增加"))
	parts["头部"] = partScanOf("防御力增加", TargetEffectElementalDamage, TargetEffectAttackIncrease)
	history := previousLockSettings{Part: "头部", Locks: [maxSlot]SlotLock{LockNone, LockOneTime, LockOneTime}}
	inv := Inventory{CustomModules: 100, CustomLockKeys: 1000}
	if planned, ok := reusableLockPlan(cfg, parts, "头部", inv, history); !ok || planned != history.Locks {
		t.Fatalf("shared effects should reuse matching joint lock plan: %v / %v", planned, ok)
	}
	wrong := previousLockSettings{Part: "头部", Locks: [maxSlot]SlotLock{LockOneTime, LockNone, LockOneTime}}
	if _, ok := reusableLockPlan(cfg, parts, "头部", inv, wrong); ok {
		t.Fatal("joint targets must not restore a lock on an irrelevant effect")
	}
	inv.CustomLockKeys = 49
	if _, ok := reusableLockPlan(cfg, parts, "头部", inv, history); ok {
		t.Fatal("joint history reuse still requires enough keys for both locks")
	}
	inv.CustomLockKeys = 1000
	parts["头部"] = partScanOf("最大装弹数增加", TargetEffectElementalDamage, TargetEffectAttackIncrease)
	if _, ok := reusableLockPlan(cfg, parts, "头部", inv, history); ok {
		t.Fatal("a completed second target must not restore old locks")
	}
}
