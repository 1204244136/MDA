package equipmentreroll

import "testing"

func TestValueReloadLocksBothModes(t *testing.T) {
	for _, mode := range []rerollMode{rerollModeSingle, rerollModeCharacter} {
		t.Run(string(mode), func(t *testing.T) {
			cfg := valueTestConfig()
			cfg.Mode = mode
			cfg.ValueTargets = valueTarget{"攻击力增加": 11, "优越代码伤害增加": 11}
			parts := map[string]partScan{}
			for _, part := range equipmentParts {
				parts[part] = partScan{Slots: [maxSlot]slotScanData{valueTestSlot("暴击率增加", 1, LockNone)}}
			}
			parts["头部"] = partScan{Slots: [maxSlot]slotScanData{
				valueTestSlot("攻击力增加", 11, LockNone),
				valueTestSlot("优越代码伤害增加", 1, LockNone),
			}}
			inv := Inventory{CustomModules: 100, CustomLockKeys: 1000}
			plan, err := planValueRerollWithInventory(parts, cfg, inv, "头部")
			if err != nil || plan.Locks[0] != LockOneTime {
				t.Fatalf("expected reusable first-slot lock: %+v %v", plan, err)
			}
			history := previousLockSettings{Part: "头部", Locks: plan.Locks}
			if _, ok := reusableLockPlan(cfg, parts, "头部", inv, history); !ok {
				t.Fatal("matching value plan must reuse historical locks")
			}
			history.Locks[2] = LockOneTime
			if _, ok := reusableLockPlan(cfg, parts, "头部", inv, history); ok {
				t.Fatal("extra historical lock must not be restored")
			}
		})
	}
}

func reloadFixture() (carrierConfig, map[string]partScan, previousLockSettings) {
	cfg := carrierConfig{Mode: rerollModeSingle, Part: "头部", Target: singleTarget{Want: map[string]int{"攻击力增加": 0, "优越代码伤害增加": 0}}}
	scan := partScan{}
	scan.Slots[0].Effect = "蓄力伤害增加"
	scan.Slots[1].Effect = "暴击率增加"
	scan.Slots[2].Effect = "攻击力增加"
	return cfg, map[string]partScan{"头部": scan}, previousLockSettings{Part: "头部", Locks: [maxSlot]SlotLock{LockNone, LockNone, LockOneTime}}
}

func TestReusableLockPlan(t *testing.T) {
	for _, name := range []string{
		"same", "other part", "different slot", "extra historical slot", "no history",
		"insufficient keys", "insufficient modules", "goal changed", "already locked",
		"permanent lock same", "permanent lock insufficient keys", "permanent lock insufficient modules",
	} {
		t.Run(name, func(t *testing.T) {
			cfg, parts, history := reloadFixture()
			inv := Inventory{CustomModules: 100, CustomLockKeys: 100}
			switch name {
			case "other part":
				history.Part = "腿部"
			case "different slot":
				history.Locks = [maxSlot]SlotLock{LockNone, LockOneTime, LockNone}
			case "extra historical slot":
				history.Locks[1] = LockOneTime
			case "no history":
				history = previousLockSettings{}
			case "insufficient keys":
				inv.CustomLockKeys = 0
			case "insufficient modules":
				inv.CustomModules = 1
			case "goal changed":
				cfg.Target.Want = map[string]int{"优越代码伤害增加": 0}
			case "already locked":
				s := parts["头部"]
				s.Slots[2].Lock = LockOneTime
				parts["头部"] = s
			case "permanent lock same":
				// 槽位3为常驻固定锁，历史为[0, LockOneTime, LockPermanent]，计划恢复槽位2为LockOneTime（第2把锁，需30密钥、3模组）
				cfg.Target.Want = map[string]int{"暴击率增加": 0, "攻击力增加": 0, "优越代码伤害增加": 0}
				s := parts["头部"]
				s.Slots[2].Lock = LockPermanent
				parts["头部"] = s
				history.Locks = [maxSlot]SlotLock{LockNone, LockOneTime, LockPermanent}
			case "permanent lock insufficient keys":
				cfg.Target.Want = map[string]int{"暴击率增加": 0, "攻击力增加": 0, "优越代码伤害增加": 0}
				s := parts["头部"]
				s.Slots[2].Lock = LockPermanent
				parts["头部"] = s
				history.Locks = [maxSlot]SlotLock{LockNone, LockOneTime, LockPermanent}
				inv.CustomLockKeys = 25 // 槽位2作为第2把锁需要30密钥
			case "permanent lock insufficient modules":
				cfg.Target.Want = map[string]int{"暴击率增加": 0, "攻击力增加": 0, "优越代码伤害增加": 0}
				s := parts["头部"]
				s.Slots[2].Lock = LockPermanent
				parts["头部"] = s
				history.Locks = [maxSlot]SlotLock{LockNone, LockOneTime, LockPermanent}
				inv.CustomModules = 2 // 2锁状态下基础洗练需要3模组
			}
			planned, ok := reusableLockPlan(cfg, parts, "头部", inv, history)
			wantOK := name == "same" || name == "permanent lock same"
			if ok != wantOK {
				t.Fatalf("reuse=%v want=%v planned=%v", ok, wantOK, planned)
			}
			if name == "permanent lock same" && planned != ([maxSlot]SlotLock{LockNone, LockOneTime, LockPermanent}) {
				t.Fatalf("planned=%v want [0, 2, 1]", planned)
			}
		})
	}
}

func TestLockHistoryRecordedAtActualConsumption(t *testing.T) {
	const id int64 = 9301
	clearMonitorState(id)
	t.Cleanup(func() { clearMonitorState(id) })
	cfg, parts, _ := reloadFixture()
	stateMu.Lock()
	states[id] = monitorState{Part: "头部", Parts: parts, Inventory: Inventory{CustomModules: 100, CustomLockKeys: 100}, InventoryInitialized: true}
	stateMu.Unlock()
	applyLockToSnapshot(id, "头部", 3, "自订密钥")
	setPendingRerollCost(id, MaterialUsage{CustomModules: 2, CustomLockKeys: 20, RerollModules: 2})
	if !commitPendingRerollCost(id) {
		t.Fatal("missing charge")
	}
	expireOneTimeLocks(id, "头部")
	if _, ok := reusableLocksForTask(id, cfg); !ok {
		t.Fatal("history lost when one-time locks expired")
	}
	clearMonitorState(id)
	if _, ok := reusableLocksForTask(id, cfg); ok {
		t.Fatal("history survived task cleanup")
	}
}

// 回归现场：仅同步模组不能允许点击历史加载；读全库存后才开放。
func TestReloadRequiresCompleteInventoryBeforeClick(t *testing.T) {
	const id int64 = 9302
	clearMonitorState(id)
	t.Cleanup(func() { clearMonitorState(id) })
	cfg, parts, history := reloadFixture()
	stateMu.Lock()
	states[id] = monitorState{Part: "头部", Parts: parts, PreviousLocks: history}
	stateMu.Unlock()
	setInventoryModules(id, 1357)
	if _, ok := reusableLocksForTask(id, cfg); ok {
		t.Fatal("unknown keys must not allow clicking reload")
	}
	setInventory(id, Inventory{CustomModules: 1357, CustomLockKeys: 5867})
	if _, ok := reusableLocksForTask(id, cfg); !ok {
		t.Fatal("matching plan with known sufficient inventory should allow reload")
	}
	setInventory(id, Inventory{CustomModules: 1357, CustomLockKeys: 0})
	if _, ok := reusableLocksForTask(id, cfg); ok {
		t.Fatal("known insufficient keys must not allow reload")
	}
}
