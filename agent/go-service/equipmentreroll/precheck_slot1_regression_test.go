package equipmentreroll

import "testing"

func slot1PrecheckQuota() map[string]int {
	return map[string]int{TargetEffectElementalDamage: 4, TargetEffectAttackIncrease: 4, "最大装弹数增加": -1}
}

func slot1PrecheckScreenshotBody() partScan {
	return partScanFromArrays(
		[maxSlot]string{TargetEffectAttackIncrease, TargetEffectElementalDamage, ""},
		[maxSlot]string{"11.81%", "23.56%", ""},
		[maxSlot]SlotLock{LockPermanent, LockPermanent, LockNone},
	)
}

func slot1PrecheckParts(body partScan) map[string]partScan {
	return map[string]partScan{
		"头部": partScanOf("防御力增加", TargetEffectAttackIncrease, ""),
		"臂部": partScanOf(TargetEffectAttackIncrease, "", ""),
		"身躯": body,
		"腿部": partScanOf("命中率增加", TargetEffectAttackIncrease, ""),
	}
}

func TestSlot1PrecheckScreenshotCompletedPartPasses(t *testing.T) {
	quota := slot1PrecheckQuota()
	body := slot1PrecheckScreenshotBody()
	cfg := carrierConfig{Mode: rerollModeCharacter, Quota: quota}
	if result := validateCharacterPreexistingLocks(991001, "身躯", body, cfg); !result.Passed {
		t.Errorf("completed attack/elemental body with empty slot 3 must pass local scan: %s", result.Message)
	}
	parts := slot1PrecheckParts(body)
	if AllPartsSatisfiedQuota(parts, quota) {
		t.Fatal("fixture must leave other equipment incomplete")
	}
	for _, material := range []string{"订制模块", "自订密钥"} {
		if result := validateCharacterGlobalPreexistingLocks(parts, quota, material); !result.Passed {
			t.Errorf("completed body must pass while other pieces need reroll (%s): %s", material, result.Message)
		}
		part, ok := chooseBestPartForQuotaWithMaterial(parts, quota, equipmentParts, material)
		if !ok || part == "身躯" {
			t.Errorf("scheduler must choose an incomplete piece, got %q/%v (%s)", part, ok, material)
		}
	}
}

func TestSlot1PrecheckIncompleteTripleTargetStillFails(t *testing.T) {
	quota := slot1PrecheckQuota()
	quota["最大装弹数增加"] = 4
	parts := slot1PrecheckParts(slot1PrecheckScreenshotBody())
	if result := validateCharacterGlobalPreexistingLocks(parts, quota, "订制模块"); result.Passed {
		t.Fatal("slot 1 lock must still fail when this body must acquire a third effect")
	}
}

func TestSlot1PrecheckCompletedPartMayRetainAnUnrequiredThirdEffect(t *testing.T) {
	body := slot1PrecheckScreenshotBody()
	body.Slots[2].Effect = "防御力增加"
	if result := validateCharacterGlobalPreexistingLocks(slot1PrecheckParts(body), slot1PrecheckQuota(), "订制模块"); !result.Passed {
		t.Fatalf("unlocked and non-forbidden defense does not make the attack/elemental target incomplete: %s", result.Message)
	}
}

func TestSlot1PrecheckSparseQuotaUsesAssignedResponsibility(t *testing.T) {
	quota := map[string]int{TargetEffectElementalDamage: 2, TargetEffectAttackIncrease: 3, "最大装弹数增加": 1}
	body := slot1PrecheckScreenshotBody()
	parts := slot1PrecheckParts(body)
	assigned := allocateQuotaRequiredWithMaterial(parts, quota, "订制模块")
	if !partCompleteForQuota(body, assigned["身躯"], quota) {
		t.Fatalf("fixture should assign the missing ammo to a different piece: %v", assigned)
	}
	if result := validateCharacterGlobalPreexistingLocks(parts, quota, "订制模块"); !result.Passed {
		t.Fatalf("body need not supply every positive effect when another piece can carry scarce ammo: %s", result.Message)
	}
}

func TestSlot1PrecheckUnlockedForbiddenThirdSlotCanBeCleaned(t *testing.T) {
	body := slot1PrecheckScreenshotBody()
	body.Slots[2].Effect = "最大装弹数增加"
	parts := slot1PrecheckParts(body)
	if result := validateCharacterGlobalPreexistingLocks(parts, slot1PrecheckQuota(), "订制模块"); !result.Passed {
		t.Fatalf("locked attack/elemental must be retained while reroll removes unlocked forbidden ammo: %s", result.Message)
	}
	if !PartNeedsRerollQuota(parts, "身躯", slot1PrecheckQuota()) {
		t.Fatal("forbidden ammo must still trigger cleanup")
	}
}

func TestSlot1PrecheckInvalidLockedEffectsStillFail(t *testing.T) {
	cfg := carrierConfig{Mode: rerollModeCharacter, Quota: slot1PrecheckQuota()}
	for _, effect := range []string{"", "最大装弹数增加", "防御力增加"} {
		t.Run(effect, func(t *testing.T) {
			scan := slot1PrecheckScreenshotBody()
			scan.Slots[0].Effect = effect
			if result := validateCharacterPreexistingLocks(991002, "身躯", scan, cfg); result.Passed {
				t.Fatalf("invalid locked effect %q must fail local validation", effect)
			}
		})
	}
}

func TestSlot1PrecheckSingleCompletedAndConflictingTargets(t *testing.T) {
	body := slot1PrecheckScreenshotBody()
	target := parseSingleTarget(map[string]int{TargetEffectAttackIncrease: 0, TargetEffectElementalDamage: 0})
	if result := validateSinglePreexistingLocks("身躯", body, target, "订制模块"); !result.Passed {
		t.Fatalf("single mode already satisfied by slot 1/2 must pass: %s", result.Message)
	}
	target.Want[TargetEffectAttackIncrease] = 3
	if result := validateSinglePreexistingLocks("身躯", body, target, "订制模块"); result.Passed {
		t.Fatal("attack locked in slot 1 cannot satisfy a target restricted to slot 3")
	}
}

func TestSlot1PrecheckUnlockedLastPieceChecksEarlierLocks(t *testing.T) {
	const taskID = 991003
	body := slot1PrecheckScreenshotBody()
	body.Slots[1] = slotScanData{Effect: "防御力增加"}
	parts := slot1PrecheckParts(body)
	stateMu.Lock()
	states[taskID] = monitorState{Part: "腿部", Parts: parts}
	stateMu.Unlock()
	t.Cleanup(func() { clearMonitorState(taskID) })
	cfg := carrierConfig{Mode: rerollModeCharacter, Quota: slot1PrecheckQuota()}
	if countLocks(parts["腿部"]) != 0 {
		t.Fatal("fixture must have no lock on the final piece")
	}
	if result := validatePreexistingLocksForConfig(taskID, "腿部", parts["腿部"], cfg); result.Passed {
		t.Fatal("an unlocked final piece must still trigger global validation and reject the incomplete locked body")
	}
}

func TestSlot1PrecheckUnlockedLastPieceAllowsEarlierCompletedBody(t *testing.T) {
	const taskID = 991004
	parts := slot1PrecheckParts(slot1PrecheckScreenshotBody())
	stateMu.Lock()
	states[taskID] = monitorState{Part: "腿部", Parts: parts}
	stateMu.Unlock()
	t.Cleanup(func() { clearMonitorState(taskID) })
	cfg := carrierConfig{Mode: rerollModeCharacter, Quota: slot1PrecheckQuota()}
	if countLocks(parts["腿部"]) != 0 {
		t.Fatal("fixture must have no lock on the final piece")
	}
	if result := validatePreexistingLocksForConfig(taskID, "腿部", parts["腿部"], cfg); !result.Passed {
		t.Fatalf("final scan must preserve a completed earlier body while other pieces need work: %s", result.Message)
	}
}

func TestSlot1PrecheckConfigRoutesPreserveSingleAndValueBehavior(t *testing.T) {
	body := slot1PrecheckScreenshotBody()
	target := parseSingleTarget(map[string]int{TargetEffectAttackIncrease: 0, TargetEffectElementalDamage: 0})
	single := carrierConfig{Mode: rerollModeSingle, Target: target}
	if result := validatePreexistingLocksForConfig(991005, "身躯", body, single); !result.Passed {
		t.Fatalf("single routing must retain its completed-target exception: %s", result.Message)
	}
	single.Target = parseSingleTarget(map[string]int{TargetEffectAttackIncrease: 3, TargetEffectElementalDamage: 0})
	if result := validatePreexistingLocksForConfig(991005, "身躯", body, single); result.Passed {
		t.Fatal("single routing must still reject a locked effect in a conflicting slot")
	}
	value := carrierConfig{Operation: rerollOperationValue, Mode: rerollModeCharacter}
	if result := validatePreexistingLocksForConfig(991005, "腿部", body, value); !result.Passed {
		t.Fatalf("value routing must defer lock validation to its own full-scope stage: %s", result.Message)
	}
}
