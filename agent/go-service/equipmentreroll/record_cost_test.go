package equipmentreroll

import "testing"

func TestDeferredMaterialCost(t *testing.T) {
	const id int64 = 9101
	clearMonitorState(id)
	t.Cleanup(func() { clearMonitorState(id) })
	initial := Inventory{CustomModules: 1393, CustomLockKeys: 5907}
	setInventory(id, initial)
	applyLockToSnapshot(id, "头部", 3, "自订密钥")
	if inv, _ := getInventory(id); inv != initial {
		t.Fatalf("selecting lock charged inventory: %+v", inv)
	}
	cost := MaterialUsage{CustomModules: 2, CustomLockKeys: 20, RerollModules: 2}
	setPendingRerollCost(id, cost)
	if inv, _ := getInventory(id); inv != initial {
		t.Fatal("preparing cost charged inventory")
	}
	clearPendingRerollCost(id)
	if commitPendingRerollCost(id) {
		t.Fatal("cancelled pending cost committed")
	}
	setPendingRerollCost(id, cost)
	if !commitPendingRerollCost(id) {
		t.Fatal("cost not committed")
	}
	if commitPendingRerollCost(id) {
		t.Fatal("cost committed twice")
	}
	if inv, _ := getInventory(id); inv != (Inventory{CustomModules: 1391, CustomLockKeys: 5887}) {
		t.Fatalf("wrong remaining inventory: %+v", inv)
	}
	if usage, _ := GetMaterialUsage(id); usage != cost {
		t.Fatalf("wrong usage: %+v", usage)
	}
}

func TestDeferredModuleLockCost(t *testing.T) {
	const id int64 = 9102
	clearMonitorState(id)
	t.Cleanup(func() { clearMonitorState(id) })
	setInventory(id, Inventory{CustomModules: 10})
	// 总额来自页面；此处验证记账模型不会把锁费用额外重复加一次。
	cost := MaterialUsage{CustomModules: 4, RerollModules: 2, LockModules: 2}
	setPendingRerollCost(id, cost)
	if !commitPendingRerollCost(id) {
		t.Fatal("missing commit")
	}
	if inv, _ := getInventory(id); inv.CustomModules != 6 {
		t.Fatalf("wrong modules: %+v", inv)
	}
	if usage, _ := GetMaterialUsage(id); usage != cost {
		t.Fatalf("wrong cost split: %+v", usage)
	}
}

// TestCostGuardProceedsWhenInventoryUnknown 回归 2026-09-20 实机空转：
// 本轮无需锁定 → 流程不经过效果锁定页 → 库存从未初始化。
// 此时费用校验必须放行（由游戏侧兜底），而不是结束任务。
func TestCostGuardProceedsWhenInventoryUnknown(t *testing.T) {
	const id int64 = 9103
	clearMonitorState(id)
	t.Cleanup(func() { clearMonitorState(id) })
	cost := MaterialUsage{CustomModules: 1, RerollModules: 1}
	if outcome := guardRerollCost(id, cost); outcome != costGuardProceed {
		t.Fatalf("unknown inventory must not end the task, got outcome=%v", outcome)
	}
	if _, known := getModulesHeld(id); known {
		t.Fatal("modules should stay unknown so the operator can see the cost was not verified")
	}
}

// TestCostGuardUsesConfirmPageModules 确认页只同步到订制模块时也应生效：
// 模块足够 → 放行；模块不足 → 结束。密钥未知不影响判定。
func TestCostGuardUsesConfirmPageModules(t *testing.T) {
	const id int64 = 9104
	clearMonitorState(id)
	t.Cleanup(func() { clearMonitorState(id) })
	setInventoryModules(id, 1386)
	if modules, ok := getModulesHeld(id); !ok || modules != 1386 {
		t.Fatalf("confirm page modules not recorded: %d ok=%v", modules, ok)
	}
	if outcome := guardRerollCost(id, MaterialUsage{CustomModules: 1, RerollModules: 1}); outcome != costGuardProceed {
		t.Fatalf("sufficient modules should proceed, got %v", outcome)
	}
	if outcome := guardRerollCost(id, MaterialUsage{CustomModules: 2000, RerollModules: 1}); outcome != costGuardInsufficient {
		t.Fatalf("modules below cost should end the task, got %v", outcome)
	}
}

// TestCostGuardKeysOnlyWhenKnown 密钥未知时不猜测；读到不足才结束。
func TestCostGuardKeysOnlyWhenKnown(t *testing.T) {
	const id int64 = 9105
	clearMonitorState(id)
	t.Cleanup(func() { clearMonitorState(id) })
	setInventory(id, Inventory{CustomModules: 100, CustomLockKeys: 5907})
	if outcome := guardRerollCost(id, MaterialUsage{CustomModules: 2, CustomLockKeys: 20, RerollModules: 2}); outcome != costGuardProceed {
		t.Fatalf("affordable key lock should proceed, got %v", outcome)
	}
	if outcome := guardRerollCost(id, MaterialUsage{CustomModules: 2, CustomLockKeys: 6000, RerollModules: 2}); outcome != costGuardInsufficient {
		t.Fatalf("keys below cost should end the task, got %v", outcome)
	}
}

// TestCommitDoesNotGuessUnknownInventory 未读到的材料余额不能被扣成负数，但消耗仍要记账。
func TestCommitDoesNotGuessUnknownInventory(t *testing.T) {
	const id int64 = 9106
	clearMonitorState(id)
	t.Cleanup(func() { clearMonitorState(id) })
	setPendingRerollCost(id, MaterialUsage{CustomModules: 1, RerollModules: 1})
	if !commitPendingRerollCost(id) {
		t.Fatal("pending cost should commit")
	}
	if _, ok := getModulesHeld(id); ok {
		t.Fatal("modules must stay unknown when never read")
	}
	if usage, _ := GetMaterialUsage(id); usage.CustomModules != 1 {
		t.Fatalf("usage should still be recorded: %+v", usage)
	}
}
