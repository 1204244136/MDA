package equipmentreroll

import "testing"

func TestAcceptedResultCommitsOnlyAfterConfirmation(t *testing.T) {
	const id int64 = 9201
	t.Cleanup(func() { clearMonitorState(id) })
	_ = setCurrentPart(id, "臂部")
	before := [maxSlot]string{"蓄力伤害增加", "暴击伤害增加", ""}
	updatePartEffects(id, "臂部", before, [maxSlot]string{"11.81%", "16.44%", ""})
	applyLockToSnapshot(id, "臂部", 1, "订制模块")
	changed := [maxSlot]string{"蓄力伤害增加", "最大装弹数增加", ""}
	values := [maxSlot]string{"7.59%", "31.95%", ""}
	stageAcceptedResult(id, "臂部", changed, values)
	if scan, _ := GetPartScan(id, "臂部"); scan.Effects() != before {
		t.Fatal("decision changed snapshot before click/warning completion")
	}
	if !commitAcceptedResult(id) {
		t.Fatal("confirmed result did not commit")
	}
	if scan, _ := GetPartScan(id, "臂部"); scan.Effects() != changed || scan.Slots[1].Value != values[1] || scan.Slots[0].Lock != LockPermanent {
		t.Fatalf("incorrect committed snapshot: %+v", scan)
	}
	if commitAcceptedResult(id) {
		t.Fatal("result committed twice")
	}
}

func TestPendingResultCannotLeakAcrossPartsOrRounds(t *testing.T) {
	const id int64 = 9202
	t.Cleanup(func() { clearMonitorState(id) })
	_ = setCurrentPart(id, "头部")
	updatePartEffects(id, "头部", [maxSlot]string{"防御力增加"}, [maxSlot]string{})
	stageAcceptedResult(id, "头部", [maxSlot]string{"攻击力增加"}, [maxSlot]string{})
	_ = setCurrentPart(id, "腿部")
	if commitAcceptedResult(id) {
		t.Fatal("committed candidate for a different part")
	}
	_ = setCurrentPart(id, "头部")
	setPendingRerollCost(id, MaterialUsage{CustomModules: 1})
	if commitAcceptedResult(id) {
		t.Fatal("old result leaked into next round")
	}
	stageAcceptedResult(id, "头部", [maxSlot]string{"攻击力增加"}, [maxSlot]string{})
	clearMonitorState(id)
	if commitAcceptedResult(id) {
		t.Fatal("stopped task retained pending snapshot")
	}
}
