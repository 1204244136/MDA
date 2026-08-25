package equipmentreroll

import "testing"

func partScanOf(s1, s2, s3 string) partScan {
	return partScan{Slots: [maxSlot]slotScanData{
		{Effect: s1, Lock: LockNone},
		{Effect: s2, Lock: LockNone},
		{Effect: s3, Lock: LockNone},
	}}
}

func TestSingleTargetFromRows(t *testing.T) {
	// 三行"需求词条 + 槽位"：优@3、攻任意、装弹@2。
	target, ok := singleTargetFromRows("优越代码伤害增加", "攻击力增加", "最大装弹数增加", 3, 0, 2)
	if !ok {
		t.Fatal("distinct rows should be accepted")
	}
	if !singleTargetValid(target) {
		t.Fatal("valid target should pass validation")
	}
	if target.Want["优越代码伤害增加"] != 3 {
		t.Fatalf("优 slot = %d, want 3", target.Want["优越代码伤害增加"])
	}
	if target.Want["攻击力增加"] != 0 {
		t.Fatalf("攻 slot = %d, want 0(any)", target.Want["攻击力增加"])
	}
	if target.Want["最大装弹数增加"] != 2 {
		t.Fatalf("装弹 slot = %d, want 2", target.Want["最大装弹数增加"])
	}
}

func TestSingleTargetFromRowsRejectsDuplicatedAffix(t *testing.T) {
	// 同一效果出现在两行 → 组装失败（单件同效果不重复）。
	if _, ok := singleTargetFromRows("优越代码伤害增加", "优越代码伤害增加", "", 3, 0, 0); ok {
		t.Fatal("duplicated affix rows should be rejected")
	}
}

func TestSingleTargetFromRowsAllowsEmptyRows(t *testing.T) {
	// 只填一行，其余为"不需求"。
	target, ok := singleTargetFromRows("优越代码伤害增加", "", "", 0, 0, 0)
	if !ok {
		t.Fatal("rows with empty wants should be accepted")
	}
	if len(target.Want) != 1 {
		t.Fatalf("want count = %d, want 1", len(target.Want))
	}
}

func TestSingleTargetValidRejectsCountOutOfRange(t *testing.T) {
	// 空需求 → 非法
	if singleTargetValid(parseSingleTarget(map[string]int{})) {
		t.Fatal("empty target should be invalid")
	}
	// 4 条需求（超过单件 3 槽）→ 非法
	t0 := parseSingleTarget(map[string]int{
		"优越代码伤害增加": 0, "攻击力增加": 0, "暴击率增加": 0, "命中率增加": 0,
	})
	if singleTargetValid(t0) {
		t.Fatal("4 affix target should be invalid for single equipment")
	}
	// 同一槽位限定了两个词条 → 非法
	t1 := parseSingleTarget(map[string]int{
		"优越代码伤害增加": 2, "攻击力增加": 2,
	})
	if singleTargetValid(t1) {
		t.Fatal("two affixes pinned to same slot should be invalid")
	}
}

func TestSinglePartSatisfiedWithSlotRestriction(t *testing.T) {
	target := parseSingleTarget(map[string]int{
		"优越代码伤害增加": 3, // 必须落在 3 号槽
		"攻击力增加":    0, // 任意槽位
	})
	// 达标：优@3 + 攻@1
	if !singlePartSatisfied(partScanOf("攻击力增加", "", "优越代码伤害增加"), target) {
		t.Fatal("优@3 + 攻@1 should satisfy target")
	}
	// 优 落在 1 号槽（未限定到 3）→ 不达标
	if singlePartSatisfied(partScanOf("优越代码伤害增加", "", "攻击力增加"), target) {
		t.Fatal("优@1 should NOT satisfy a slot-3-restricted requirement")
	}
}

func TestSingleEffectiveAffixCount(t *testing.T) {
	target := parseSingleTarget(map[string]int{
		"优越代码伤害增加": 3, // 3 号槽
	})
	// 优@3 → 1 条有效；优@2 → 0 条有效（限定 3 号槽）
	if got := singleEffectiveAffixCount(partScanOf("", "优越代码伤害增加", ""), target); got != 0 {
		t.Fatalf("effective count of 优@2 under slot-3 restriction = %d, want 0", got)
	}
	if got := singleEffectiveAffixCount(partScanOf("", "", "优越代码伤害增加"), target); got != 1 {
		t.Fatalf("effective count of 优@3 = %d, want 1", got)
	}
}

func TestSingleDesiredLockSlotNotNeededForSingleAffix(t *testing.T) {
	// 只需求一条词条 → 不锁（找到即达标）。
	target := parseSingleTarget(map[string]int{"优越代码伤害增加": 0})
	scan := partScanOf("优越代码伤害增加", "", "")
	slot, need := singleDesiredLockSlot(scan, target, "自订密钥")
	if need || slot != 0 {
		t.Fatalf("single-affix target should not lock, got slot=%d need=%v", slot, need)
	}
}

func TestSingleDesiredLockSlotLocksHelpedSlotForTwoAffixes(t *testing.T) {
	// 两条需求：优 已落在 3 号槽（30% 难出），攻 尚未持有。
	// 应锁定已落地的优@3 保护它，再追攻（拿到就锁 / 期望收益差距）。
	target := parseSingleTarget(map[string]int{
		"优越代码伤害增加": 3, // 已落在 3 号槽
		"攻击力增加":    2, // 需落在 2 号槽（尚未持有）
	})
	scan := partScanOf("", "", "优越代码伤害增加")
	slot, need := singleDesiredLockSlot(scan, target, "自订密钥")
	if !need {
		t.Fatal("two-affix target with a held hard slot should lock")
	}
	if slot != 3 {
		t.Fatalf("expected to lock slot 3 to protect 优越代码, got slot %d", slot)
	}
}

func TestSingleExpectedCostSlotRestrictionLowers(t *testing.T) {
	// 优 限定 3 号槽：已落在 3 号槽的状态应比落在 1 号槽更接近达标（期望成本更低）。
	target := parseSingleTarget(map[string]int{"优越代码伤害增加": 3})
	ok := partScanOf("", "", "优越代码伤害增加")  // 达标
	off := partScanOf("优越代码伤害增加", "", "") // 优 在 1 号槽，未达标
	okCost := singleExpectedCost(ok, target)
	offCost := singleExpectedCost(off, target)
	if okCost != 0 {
		t.Fatalf("satisfied state cost = %f, want 0", okCost)
	}
	if offCost <= okCost {
		t.Fatalf("off-slot state cost = %f should be strictly greater than satisfied cost %f", offCost, okCost)
	}
	if offCost >= costUnreachable {
		t.Fatalf("off-slot state cost = %f should be reachable (slot 1 is unlocked and can be rerolled)", offCost)
	}
}

func TestSingleTargetUnreachableWhenRequiredAffixLockedOffSlot(t *testing.T) {
	// 3 号槽被永久锁住“攻击力增加”，而用户把“攻击力增加”限定到 2 号槽：
	// 锁定无法解除、词条也不跨槽移动 → 目标永远达不成，必须被判定为不可达。
	target := parseSingleTarget(map[string]int{"攻击力增加": 2})
	scan := partScan{Slots: [maxSlot]slotScanData{
		{Effect: "", Lock: LockNone},
		{Effect: "", Lock: LockNone},
		{Effect: "攻击力增加", Lock: LockPermanent},
	}}
	if !singleTargetUnreachable(scan, target) {
		t.Fatalf("required affix locked in a disallowed slot should be unreachable, cost = %f", singleExpectedCost(scan, target))
	}
	// 同样的锁定，但槽位限定改为“任意槽位”时目标当即达标，不应误判为不可达。
	anySlot := parseSingleTarget(map[string]int{"攻击力增加": 0})
	if singleTargetUnreachable(scan, anySlot) {
		t.Fatal("already-satisfied any-slot target must not be reported unreachable")
	}
}

func TestSingleTargetProblemCodes(t *testing.T) {
	tests := []struct {
		name   string
		target singleTarget
		want   string
	}{
		{name: "valid", target: parseSingleTarget(map[string]int{"攻击力增加": 2}), want: ""},
		{name: "empty", target: parseSingleTarget(map[string]int{}), want: singleTargetProblemCountOutOfRange},
		{
			name:   "too many",
			target: parseSingleTarget(map[string]int{"优越代码伤害增加": 0, "攻击力增加": 0, "暴击率增加": 0, "命中率增加": 0}),
			want:   singleTargetProblemCountOutOfRange,
		},
		{name: "unknown affix", target: parseSingleTarget(map[string]int{"不存在的词条": 0}), want: singleTargetProblemUnknownAffix},
		{name: "slot out of range", target: parseSingleTarget(map[string]int{"攻击力增加": 9}), want: singleTargetProblemSlotOutOfRange},
		{
			name:   "slot conflict",
			target: parseSingleTarget(map[string]int{"优越代码伤害增加": 2, "攻击力增加": 2}),
			want:   singleTargetProblemSlotConflict,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := singleTargetProblem(tt.target); got != tt.want {
				t.Fatalf("singleTargetProblem() = %q, want %q", got, tt.want)
			}
			if valid := singleTargetValid(tt.target); valid != (tt.want == "") {
				t.Fatalf("singleTargetValid() = %v, want %v", valid, tt.want == "")
			}
		})
	}
}

func TestDecideResultPageSingleAcceptsWhenProgress(t *testing.T) {
	target := parseSingleTarget(map[string]int{"优越代码伤害增加": 0})
	// 当前全空，变更后拿到优 → 接受。
	empty := partScanOf("", "", "")
	changed := [maxSlot]string{"优越代码伤害增加", "", ""}
	if got := DecideResultPageSingle(changed, empty, target); got != ResultDecisionAccept {
		t.Fatalf("changed-to-satisfied should accept, got %v", got)
	}
	// 当前已达标，变更后丢失优 → 维持。
	satisfied := partScanOf("优越代码伤害增加", "", "")
	changed2 := [maxSlot]string{"防御力增加", "", ""}
	if got := DecideResultPageSingle(changed2, satisfied, target); got != ResultDecisionKeep {
		t.Fatalf("breaking a satisfied state should keep, got %v", got)
	}
}
