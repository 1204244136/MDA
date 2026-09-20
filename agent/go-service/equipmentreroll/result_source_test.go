package equipmentreroll

import "testing"

func TestResultOCRSkipsOnlyCompleteLockedSlots(t *testing.T) {
	for _, tc := range []struct {
		name    string
		locks   [3]SlotLock
		invalid int
		calls   int
	}{
		{"unlocked", [3]SlotLock{}, -1, 3},
		{"one time", [3]SlotLock{LockNone, LockNone, LockOneTime}, -1, 2},
		{"mixed", [3]SlotLock{LockNone, LockPermanent, LockOneTime}, -1, 1},
		{"missing value", [3]SlotLock{LockNone, LockPermanent, LockOneTime}, 2, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var source partScan
			for i, lock := range tc.locks {
				source.Slots[i] = slotScanData{Effect: "攻击力增加", Value: "9.00%", Lock: lock}
			}
			if tc.invalid >= 0 {
				source.Slots[tc.invalid].Value = ""
			}
			calls := 0
			effects, values, raws, recognized := extractSlotOCRData(source, func(node string) (string, string, string, bool) {
				calls++
				for i, n := range resultChangedEffectSlotNodes {
					if n == node && tc.locks[i] != LockNone && i != tc.invalid {
						t.Errorf("OCR called for locked slot %d", i+1)
					}
				}
				return "蓄力伤害增加", "14.63%", "【蓄力伤害增加】14.63%", true
			})
			if calls != tc.calls {
				t.Fatalf("OCR calls=%d want %d", calls, tc.calls)
			}
			if !validateSlotRecognitionCompleteness(effects, values, raws, recognized) || hasTransientUnlockState(raws) {
				t.Fatal("complete merged result rejected")
			}
			for i, lock := range tc.locks {
				if lock != LockNone && i != tc.invalid && (effects[i] != "攻击力增加" || values[i] != "9.00%") {
					t.Fatalf("locked slot %d changed", i+1)
				}
			}
		})
	}
}

func TestUnlockedResultStillRejectsTransientOrMissingOCR(t *testing.T) {
	for _, raw := range []string{"已解除效果锁定。", ""} {
		effects, values, raws, recognized := extractSlotOCRData(partScan{}, func(string) (string, string, string, bool) { return "", "", raw, false })
		if !hasTransientUnlockState(raws) && validateSlotRecognitionCompleteness(effects, values, raws, recognized) {
			t.Fatalf("invalid OCR accepted: %q", raw)
		}
	}
}

func TestResultSourceSurvivesUnlockButNotAnotherRound(t *testing.T) {
	const id int64 = 9511
	clearMonitorState(id)
	t.Cleanup(func() { clearMonitorState(id) })
	scan := partScan{}
	scan.Slots[2] = slotScanData{Effect: "攻击力增加", Value: "9.00%", Lock: LockOneTime}
	stateMu.Lock()
	states[id] = monitorState{Part: "头部", Parts: map[string]partScan{"头部": scan}}
	stateMu.Unlock()
	if resultSourceForTask(id) != (partScan{}) {
		t.Fatal("uncommitted state reused")
	}
	cost := MaterialUsage{CustomModules: 2, RerollModules: 2, CustomLockKeys: 20}
	setPendingRerollCost(id, cost)
	if !commitPendingRerollCost(id) {
		t.Fatal("commit failed")
	}
	expireOneTimeLocks(id, "头部")
	if resultSourceForTask(id) != scan {
		t.Fatal("one-time unlock changed result source")
	}
	if commitPendingRerollCost(id) {
		t.Fatal("charged twice")
	}
	if resultSourceForTask(id) != scan {
		t.Fatal("repeat commit erased source")
	}
	setPendingRerollCost(id, cost)
	if resultSourceForTask(id) != (partScan{}) {
		t.Fatal("previous round source survived next preparation")
	}
	if !commitPendingRerollCost(id) {
		t.Fatal("next commit failed")
	}
	if resultSourceForTask(id).Slots[2].Lock != LockNone {
		t.Fatal("previous historical lock reused in unlocked round")
	}
	if err := setCurrentPart(id, "腿部"); err != nil {
		t.Fatal(err)
	}
	if err := setCurrentPart(id, "头部"); err != nil {
		t.Fatal(err)
	}
	if resultSourceForTask(id) != (partScan{}) {
		t.Fatal("source survived equipment switch")
	}
}
