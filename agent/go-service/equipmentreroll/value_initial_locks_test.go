package equipmentreroll

import (
	"testing"
	"time"
)

func TestValueInitialLocksRejectFlashAndSingleFrame(t *testing.T) {
	const id int64 = 92200
	t.Cleanup(func() { clearMonitorState(id) })
	_ = setCurrentPart(id, "身躯")
	updatePartEffects(id, "身躯", [maxSlot]string{"攻击力增加", "最大装弹数增加", "优越代码伤害增加"}, [maxSlot]string{"10.40%", "36.06%", "24.96%"})
	setInventory(id, Inventory{1353, 200})
	locks := [maxSlot]SlotLock{LockNone, LockOneTime, LockPermanent}
	verified := previousLockSettings{Part: "身躯", Locks: locks}
	now := time.Now()
	if observeInitialValueLocks(id, "身躯", locks, true, now) || syncValueInitialLocks(id, verified) {
		t.Fatal("single frame accepted")
	}
	if observeInitialValueLocks(id, "身躯", locks, true, now.Add(100*time.Millisecond)) {
		t.Fatal("frames too close")
	}
	if observeInitialValueLocks(id, "身躯", [maxSlot]SlotLock{}, false, now.Add(250*time.Millisecond)) {
		t.Fatal("dark phase accepted")
	}
	if observeInitialValueLocks(id, "身躯", locks, true, now.Add(500*time.Millisecond)) {
		t.Fatal("unknown frame failed to reset evidence")
	}
	if !observeInitialValueLocks(id, "身躯", locks, true, now.Add(750*time.Millisecond)) || !syncValueInitialLocks(id, verified) {
		t.Fatal("stable colored locks rejected")
	}
	scan, _ := GetPartScan(id, "身躯")
	for i, slot := range scan.Slots {
		if slot.Lock != locks[i] {
			t.Fatal("wrong lock", scan)
		}
	}
	inv, _ := getInventory(id)
	if inv != (Inventory{1353, 200}) {
		t.Fatal("observation changed inventory")
	}
	storeValuePlan(id, valuePlan{Part: "身躯", Locks: locks})
	if observeInitialValueLocks(id, "身躯", [maxSlot]SlotLock{}, true, now.Add(time.Second)) || syncValueInitialLocks(id, previousLockSettings{Part: "身躯"}) {
		t.Fatal("frozen lock plan resynchronized")
	}
	clearValuePlan(id)
	if observeInitialValueLocks(id, "身躯", [maxSlot]SlotLock{}, true, now.Add(2*time.Second)) {
		t.Fatal("round evidence leaked")
	}
	if !observeInitialValueLocks(id, "身躯", [maxSlot]SlotLock{}, true, now.Add(2300*time.Millisecond)) || !syncValueInitialLocks(id, previousLockSettings{Part: "身躯"}) {
		t.Fatal("stable genuine unlocked state rejected")
	}
}

func BenchmarkValueCharacterPlan(b *testing.B) {
	parts, cfg := valueLogTestParts()
	scan := parts["腿部"]
	for i, tier := range [maxSlot]int{5, 10, 8} {
		scan.Slots[i] = valueTestSlot(scan.Slots[i].Effect, tier, LockNone)
	}
	parts["腿部"] = scan
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := planValueRerollWithInventory(parts, cfg, Inventory{1353, 999}, ""); err != nil {
			b.Fatal(err)
		}
	}
}
