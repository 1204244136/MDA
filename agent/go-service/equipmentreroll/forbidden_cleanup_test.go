package equipmentreroll

import (
	"fmt"
	"math"
	"testing"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func TestForbiddenCleanupRetainsBothTargets(t *testing.T) {
	quota := map[string]int{TargetEffectAttackIncrease: 4, TargetEffectElementalDamage: 4, "最大装弹数增加": -1}
	required := []string{TargetEffectAttackIncrease, TargetEffectElementalDamage}
	current := partScanOf("防御力增加", "命中率增加", "暴击率增加")
	for _, material := range []string{"订制模块", "自订密钥"} {
		t.Run(material, func(t *testing.T) {
			candidate := partScanOf(TargetEffectAttackIncrease, TargetEffectElementalDamage, "最大装弹数增加")
			if got := decideResultPageQuotaWithMaterial(current.Effects(), candidate.Effects(), current, quota, material); got != ResultDecisionAccept {
				t.Fatalf("useful candidate with removable forbidden effect was discarded: %v", got)
			}
			parts := map[string]partScan{}
			for _, part := range equipmentParts {
				parts[part] = partScanOf(TargetEffectAttackIncrease, TargetEffectElementalDamage, "")
			}
			parts["头部"] = candidate
			if AllPartsSatisfiedQuota(parts, quota) {
				t.Fatal("forbidden effect must still prevent completion")
			}
			for i := 0; i < 2; i++ {
				slot, need := DesiredLockSlotForQuota(parts, "头部", quota, material)
				if !need || slot < 1 || slot > 2 || candidate.Slots[slot-1].Lock != LockNone {
					t.Fatalf("cleanup step %d must protect each target before reroll: slot=%d need=%v scan=%+v", i, slot, need, candidate)
				}
				candidate.Slots[slot-1].Lock = LockPermanent
				parts["头部"] = candidate
			}
			// 只洗第3槽，装弹条件概率为 0.3 * 0.12 / (1 - 0.1 - 0.1)。
			_, cost := bestLockSlotAndCostForRequired(candidate, quota, required, material)
			want := 3 / (1 - .3*.12/.8)
			if math.Abs(cost-want) > 1e-7 {
				t.Fatalf("two protected targets cleanup cost=%.9f want %.9f", cost, want)
			}
		})
	}
}

func TestForbiddenCleanupAllSlotOrdersThroughResultFlow(t *testing.T) {
	quota := map[string]int{TargetEffectAttackIncrease: 4, TargetEffectElementalDamage: 4, "最大装弹数增加": -1}
	cfg := carrierConfig{Mode: rerollModeCharacter, Quota: quota}
	orders := [][maxSlot]string{
		{TargetEffectAttackIncrease, TargetEffectElementalDamage, "最大装弹数增加"},
		{TargetEffectElementalDamage, TargetEffectAttackIncrease, "最大装弹数增加"},
		{TargetEffectAttackIncrease, "最大装弹数增加", TargetEffectElementalDamage},
		{TargetEffectElementalDamage, "最大装弹数增加", TargetEffectAttackIncrease},
		{"最大装弹数增加", TargetEffectAttackIncrease, TargetEffectElementalDamage},
		{"最大装弹数增加", TargetEffectElementalDamage, TargetEffectAttackIncrease},
	}
	for index, effects := range orders {
		for _, material := range []string{"订制模块", "自订密钥"} {
			t.Run(fmt.Sprintf("order%d/%s", index, material), func(t *testing.T) {
				const taskID int64 = 992201
				t.Cleanup(func() { clearMonitorState(taskID) })
				parts := map[string]partScan{}
				for _, part := range equipmentParts {
					parts[part] = partScanOf(TargetEffectAttackIncrease, TargetEffectElementalDamage, "")
				}
				parts["头部"] = partScanOf("防御力增加", "命中率增加", "暴击率增加")
				stateMu.Lock()
				states[taskID] = monitorState{Part: "头部", Parts: parts}
				stateMu.Unlock()
				inv := Inventory{CustomModules: 1000}
				if material == "自订密钥" {
					inv.CustomLockKeys = 1000
				}
				setInventory(taskID, inv)
				arg := &maa.CustomRecognitionArg{TaskID: taskID}
				r := EquipmentRerollResultDecideRecognition{}
				result, ok := r.decideQuota(arg, resultDecideParam{GlobalQuota: quota}, "头部", effects, [maxSlot]string{}, [maxSlot]string{})
				if !ok || result.Detail != resultDecisionDetail(ResultDecisionAccept) || !commitAcceptedResult(taskID) {
					t.Fatalf("useful mixed candidate must pass real global result flow: result=%+v ok=%v", result, ok)
				}
				parts, _ = GetEquipmentSlotScans(taskID)
				if AllPartsSatisfiedQuota(parts, quota) || !shouldContinueCurrentPartAfterAcceptWithMaterial("头部", cfg, parts, material) {
					t.Fatal("mixed candidate must remain selected for cleanup")
				}
				for step := 0; step < 2; step++ {
					slot, need := desiredLockSlotForConfig(cfg, taskID, "头部")
					if !need || slot < minSlot || slot > maxSlot || quota[effects[slot-1]] <= 0 {
						t.Fatalf("step %d must protect a target: slot=%d need=%v", step, slot, need)
					}
					applyLockToSnapshot(taskID, "头部", slot, material)
					parts, _ = GetEquipmentSlotScans(taskID)
					if countLocks(parts["头部"]) != step+1 {
						t.Fatal("lock progression did not protect a new slot")
					}
					if result := validateCharacterGlobalPreexistingLocks(parts, quota, material); !result.Passed {
						t.Fatalf("restarting after a planned lock must pass precheck: %s", result.Message)
					}
				}
				if _, need := DesiredLockSlotForQuota(parts, "头部", quota, material); need {
					t.Fatal("cleanup must not add a third lock")
				}
				cleaned := effects
				for i, effect := range cleaned {
					if quota[effect] < 0 {
						cleaned[i] = "防御力增加"
					}
				}
				result, ok = r.decideQuota(arg, resultDecideParam{GlobalQuota: quota}, "头部", cleaned, [maxSlot]string{}, [maxSlot]string{})
				if !ok || result.Detail != resultDecisionDetail(ResultDecisionAccept) || !commitAcceptedResult(taskID) {
					t.Fatalf("cleaned candidate must be accepted: result=%+v ok=%v", result, ok)
				}
				parts, _ = GetEquipmentSlotScans(taskID)
				if !AllPartsSatisfiedQuota(parts, quota) || shouldContinueCurrentPartAfterAcceptWithMaterial("头部", cfg, parts, material) {
					t.Fatal("cleanup must finish after forbidden effect disappears")
				}
			})
		}
	}
}

func TestForbiddenCleanupSingleTargetAndSparseQuota(t *testing.T) {
	scan := partScanOf(TargetEffectAttackIncrease, "最大装弹数增加", "防御力增加")
	quota := map[string]int{TargetEffectAttackIncrease: 4, "最大装弹数增加": -1}
	for _, material := range []string{"订制模块", "自订密钥"} {
		if slot, cost := bestLockSlotAndCostForRequired(scan, quota, []string{TargetEffectAttackIncrease}, material); slot != 1 || cost <= 0 || cost >= costUnreachable {
			t.Fatalf("single target at slot 1 can be protected for cleanup: material=%s slot=%d cost=%.9f", material, slot, cost)
		}
	}
	// 其余三件已经提供全部配额时，只需清除本件禁项，不应为本件多余的目标付锁费。
	quota[TargetEffectAttackIncrease] = 1
	quota[TargetEffectElementalDamage] = 1
	parts := map[string]partScan{}
	for _, part := range equipmentParts {
		parts[part] = partScanOf(TargetEffectAttackIncrease, TargetEffectElementalDamage, "")
	}
	parts["头部"] = partScanOf(TargetEffectAttackIncrease, TargetEffectElementalDamage, "最大装弹数增加")
	for _, material := range []string{"订制模块", "自订密钥"} {
		if slot, need := DesiredLockSlotForQuota(parts, "头部", quota, material); need {
			t.Fatalf("surplus targets must not cause unnecessary cleanup locks: material=%s slot=%d", material, slot)
		}
		if AllPartsSatisfiedQuota(parts, quota) {
			t.Fatal("sparse quota still requires forbidden cleanup")
		}
	}
}

func TestForbiddenCleanupRejectsWorseAndPermanentlyForbidden(t *testing.T) {
	quota := map[string]int{TargetEffectAttackIncrease: 4, TargetEffectElementalDamage: 4, "最大装弹数增加": -1}
	current := partScanOf(TargetEffectAttackIncrease, TargetEffectElementalDamage, "")
	worse := partScanOf(TargetEffectAttackIncrease, TargetEffectElementalDamage, "最大装弹数增加")
	for _, material := range []string{"订制模块", "自订密钥"} {
		if got := decideResultPageQuotaWithMaterial(current.Effects(), worse.Effects(), current, quota, material); got != ResultDecisionKeep {
			t.Fatalf("already completed current state must reject a newly forbidden effect: %v", got)
		}
	}
	worse.Slots[2].Lock = LockPermanent
	if _, cost := bestLockSlotAndCostForRequired(worse, quota, positiveQuotaEffects(quota), "订制模块"); cost < costUnreachable {
		t.Fatalf("permanently locked forbidden effect must remain unreachable: %.9f", cost)
	}
	cfg := carrierConfig{Mode: rerollModeCharacter, Quota: quota}
	if result := validateCharacterPreexistingLocks(992202, "头部", worse, cfg); result.Passed {
		t.Fatal("locked forbidden effect must still fail local precheck")
	}
}

func TestForbiddenCleanupAddsFirstSlotLockAlongsideTemporaryLock(t *testing.T) {
	quota := map[string]int{TargetEffectAttackIncrease: 4, TargetEffectElementalDamage: 4, "最大装弹数增加": -1}
	required := positiveQuotaEffects(quota)
	scan := partScanOf(TargetEffectAttackIncrease, TargetEffectElementalDamage, "最大装弹数增加")
	scan.Slots[1].Lock = LockOneTime
	slot, cost := bestLockSlotAndCostForRequired(scan, quota, required, "订制模块")
	if slot != 1 {
		t.Fatalf("current one-time protection must allow a permanent first-slot cleanup lock: slot=%d cost=%.9f", slot, cost)
	}
	scan.Slots[0].Lock = LockPermanent
	_, continuation := bestLockSlotAndCostForRequired(scan, quota, required, "订制模块")
	if math.Abs(cost-3-continuation) > 1e-7 {
		t.Fatalf("second lock acquisition must be included: before=%.9f continuation=%.9f", cost, continuation)
	}
	_, expired := bestLockSlotAndCostForRequired(effectPostRollScan(scan), quota, required, "订制模块")
	if expired <= continuation {
		t.Fatalf("current temporary protection must expire after this round: current=%.9f expired=%.9f", continuation, expired)
	}
}
