package equipmentreroll

import (
	"testing"
)

func TestIsLocksSubsequenceOrEqual(t *testing.T) {
	cases := []struct {
		name        string
		actualLocks []int
		simLocks    []int
		want        bool
	}{
		{
			name:        "empty actual locks",
			actualLocks: []int{},
			simLocks:    []int{3, 2},
			want:        true,
		},
		{
			name:        "single lock matches first sim lock",
			actualLocks: []int{3},
			simLocks:    []int{3, 2},
			want:        true,
		},
		{
			name:        "single lock does not match first sim lock",
			actualLocks: []int{2},
			simLocks:    []int{3, 2},
			want:        false,
		},
		{
			name:        "single lock with empty sim locks",
			actualLocks: []int{3},
			simLocks:    []int{},
			want:        false,
		},
		{
			name:        "two locks match exactly",
			actualLocks: []int{2, 3},
			simLocks:    []int{3, 2},
			want:        true,
		},
		{
			name:        "two locks match reversed",
			actualLocks: []int{3, 2},
			simLocks:    []int{2, 3},
			want:        true,
		},
		{
			name:        "two locks mismatch sim locks",
			actualLocks: []int{1, 2},
			simLocks:    []int{3, 2},
			want:        false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isLocksSubsequenceOrEqual(tc.actualLocks, tc.simLocks)
			if got != tc.want {
				t.Fatalf("isLocksSubsequenceOrEqual(%v, %v) = %v, want %v", tc.actualLocks, tc.simLocks, got, tc.want)
			}
		})
	}
}

func TestValidateSinglePreexistingLocks(t *testing.T) {
	target := singleTarget{
		Want: map[string]int{
			"攻击力增加":    0,
			"优越代码伤害增加": 0,
		},
	}

	t.Run("unlocked equipment passes", func(t *testing.T) {
		scan := partScan{
			Slots: [maxSlot]slotScanData{
				{Effect: "防御力增加", Lock: LockNone},
				{Effect: "生命力增加", Lock: LockNone},
				{Effect: "暴击率增加", Lock: LockNone},
			},
		}
		res := validateSinglePreexistingLocks("头部", scan, target, "")
		if !res.Passed {
			t.Fatalf("expected pass for unlocked equipment, got failure: %s", res.Message)
		}
	})

	t.Run("all slots locked fails", func(t *testing.T) {
		scan := partScan{
			Slots: [maxSlot]slotScanData{
				{Effect: "攻击力增加", Lock: LockPermanent},
				{Effect: "优越代码伤害增加", Lock: LockPermanent},
				{Effect: "暴击率增加", Lock: LockPermanent},
			},
		}
		res := validateSinglePreexistingLocks("头部", scan, target, "")
		if res.Passed {
			t.Fatal("expected failure for 3 locked slots")
		}
	})

	t.Run("already satisfied with locks passes", func(t *testing.T) {
		scan := partScan{
			Slots: [maxSlot]slotScanData{
				{Effect: "攻击力增加", Lock: LockNone},
				{Effect: "优越代码伤害增加", Lock: LockPermanent},
				{Effect: "暴击率增加", Lock: LockNone},
			},
		}
		res := validateSinglePreexistingLocks("头部", scan, target, "")
		if !res.Passed {
			t.Fatalf("expected pass for satisfied equipment, got failure: %s", res.Message)
		}
		if !res.IsInfo {
			t.Fatal("expected IsInfo true for satisfied equipment with lock")
		}
	})

	t.Run("slot 1 locked on unsatisfied equipment fails", func(t *testing.T) {
		scan := partScan{
			Slots: [maxSlot]slotScanData{
				{Effect: "攻击力增加", Lock: LockPermanent},
				{Effect: "防御力增加", Lock: LockNone},
				{Effect: "生命力增加", Lock: LockNone},
			},
		}
		res := validateSinglePreexistingLocks("头部", scan, target, "")
		if res.Passed {
			t.Fatal("expected failure for slot 1 lock on unsatisfied equipment")
		}
	})

	t.Run("locked non-target affix fails", func(t *testing.T) {
		scan := partScan{
			Slots: [maxSlot]slotScanData{
				{Effect: "生命力增加", Lock: LockNone},
				{Effect: "防御力增加", Lock: LockNone},
				{Effect: "暴击伤害增加", Lock: LockPermanent}, // 暴击伤害增加不在 target 中
			},
		}
		res := validateSinglePreexistingLocks("头部", scan, target, "")
		if res.Passed {
			t.Fatal("expected failure for non-target locked affix")
		}
	})

	t.Run("locked slot conflicts with slot restriction fails", func(t *testing.T) {
		restrictedTarget := singleTarget{
			Want: map[string]int{
				"攻击力增加":    2, // 限定在第 2 槽
				"优越代码伤害增加": 0,
			},
		}
		scan := partScan{
			Slots: [maxSlot]slotScanData{
				{Effect: "生命力增加", Lock: LockNone},
				{Effect: "防御力增加", Lock: LockNone},
				{Effect: "攻击力增加", Lock: LockPermanent}, // 锁在第 3 槽，冲突
			},
		}
		res := validateSinglePreexistingLocks("头部", scan, restrictedTarget, "")
		if res.Passed {
			t.Fatal("expected failure for slot restriction conflict")
		}
	})

	t.Run("pre-existing lock matches first reroll step passes", func(t *testing.T) {
		// 需求：攻击力增加 + 优越代码伤害增加
		// 装备当前：1槽防御力，2槽生命力，3槽优越代码（已锁）
		// 若无锁，要洗该装备第 1 件事就是锁定 3 槽优越代码！已有锁定应当通过！
		scan := partScan{
			Slots: [maxSlot]slotScanData{
				{Effect: "防御力增加", Lock: LockNone},
				{Effect: "生命力增加", Lock: LockNone},
				{Effect: "优越代码伤害增加", Lock: LockPermanent},
			},
		}
		res := validateSinglePreexistingLocks("头部", scan, target, "")
		if !res.Passed {
			t.Fatalf("expected pass for valid matching lock, got failure: %s", res.Message)
		}
		if !res.IsInfo {
			t.Fatal("expected IsInfo true for valid retained lock")
		}
	})

	t.Run("strategy mismatch on bittersweet order fails", func(t *testing.T) {
		// 饱和目标：3条全要（攻击力、优越代码、最大装弹数）
		// 装备当前：1槽防御力，2槽攻击力（已锁），3槽生命力（未出需求）
		// 先苦后甜策略：3槽未出时，应在无锁状态便宜刷 3 槽，不应提前锁 2 槽！
		bittersweetTarget := singleTarget{
			Want: map[string]int{
				"攻击力增加":    0,
				"优越代码伤害增加": 0,
				"最大装弹数增加":  0,
			},
		}
		scan := partScan{
			Slots: [maxSlot]slotScanData{
				{Effect: "防御力增加", Lock: LockNone},
				{Effect: "攻击力增加", Lock: LockPermanent},
				{Effect: "生命力增加", Lock: LockNone},
			},
		}
		res := validateSinglePreexistingLocks("头部", scan, bittersweetTarget, "")
		if res.Passed {
			t.Fatal("expected failure for premature lock violating bittersweet strategy")
		}
	})

	t.Run("single affix target should not lock slot fails", func(t *testing.T) {
		// 目标只有 1 条：攻击力增加
		// 策略对单条目标从不建议加锁（直接刷即可）
		// 若用户在 2 槽锁了其他或攻击力但未达标，不建议加锁
		singleTargetOnly := singleTarget{
			Want: map[string]int{
				"攻击力增加": 0,
			},
		}
		// 1槽防御力，2槽攻击力（如果2槽是攻击力，则已达标；此处设2槽锁了防御力则命中not target，
		// 若2槽攻击力则singlePartSatisfied达标。这里测若2槽是攻击力限定在1槽）
		restrictedSingle := singleTarget{
			Want: map[string]int{
				"攻击力增加": 1, // 限定1槽
			},
		}
		scan := partScan{
			Slots: [maxSlot]slotScanData{
				{Effect: "防御力增加", Lock: LockNone},
				{Effect: "攻击力增加", Lock: LockPermanent}, // 锁在2槽且限定在1槽
				{Effect: "生命力增加", Lock: LockNone},
			},
		}
		res := validateSinglePreexistingLocks("头部", scan, restrictedSingle, "")
		if res.Passed {
			t.Fatal("expected failure for single target conflict/mismatch")
		}
		_ = singleTargetOnly
	})
}

func TestValidateCharacterPreexistingLocks(t *testing.T) {
	quota := map[string]int{
		"优越代码伤害增加": 4,
		"攻击力增加":    4,
		"命中率增加":    -1, // 禁止
		"防御力增加":    0,  // 不要求
	}
	cfg := carrierConfig{
		Mode:  rerollModeCharacter,
		Quota: quota,
	}

	t.Run("part scan 3 slots locked fails", func(t *testing.T) {
		scan := partScan{
			Slots: [maxSlot]slotScanData{
				{Effect: "优越代码伤害增加", Lock: LockPermanent},
				{Effect: "攻击力增加", Lock: LockPermanent},
				{Effect: "暴击率增加", Lock: LockPermanent},
			},
		}
		res := validateCharacterPreexistingLocks(4001, "头部", scan, cfg)
		if res.Passed {
			t.Fatal("expected failure for 3 locked slots in character mode")
		}
	})

	t.Run("part scan locked forbidden affix fails", func(t *testing.T) {
		scan := partScan{
			Slots: [maxSlot]slotScanData{
				{Effect: "防御力增加", Lock: LockNone},
				{Effect: "生命力增加", Lock: LockNone},
				{Effect: "命中率增加", Lock: LockPermanent}, // 命中率是禁止词条
			},
		}
		res := validateCharacterPreexistingLocks(4002, "头部", scan, cfg)
		if res.Passed {
			t.Fatal("expected failure for locked forbidden affix")
		}
	})

	t.Run("part scan locked unwanted affix fails", func(t *testing.T) {
		scan := partScan{
			Slots: [maxSlot]slotScanData{
				{Effect: "生命力增加", Lock: LockNone},
				{Effect: "防御力增加", Lock: LockPermanent}, // 防御力是未要求词条
				{Effect: "攻击力增加", Lock: LockNone},
			},
		}
		res := validateCharacterPreexistingLocks(4003, "头部", scan, cfg)
		if res.Passed {
			t.Fatal("expected failure for locked unwanted affix")
		}
	})

	t.Run("part scan slot 1 locked fails if unsatisfied", func(t *testing.T) {
		scan := partScan{
			Slots: [maxSlot]slotScanData{
				{Effect: "攻击力增加", Lock: LockPermanent}, // 1槽有锁，但2/3槽是杂词条
				{Effect: "生命力增加", Lock: LockNone},
				{Effect: "防御力增加", Lock: LockNone},
			},
		}
		res := validateCharacterPreexistingLocks(4004, "头部", scan, cfg)
		if res.Passed {
			t.Fatal("expected failure for slot 1 lock on unsatisfied part")
		}
	})

	t.Run("part scan valid candidate lock passes non-legs", func(t *testing.T) {
		scan := partScan{
			Slots: [maxSlot]slotScanData{
				{Effect: "生命力增加", Lock: LockNone},
				{Effect: "防御力增加", Lock: LockNone},
				{Effect: "攻击力增加", Lock: LockPermanent}, // 3槽锁了需求词条
			},
		}
		res := validateCharacterPreexistingLocks(4005, "头部", scan, cfg)
		if !res.Passed {
			t.Fatalf("expected pass for non-legs candidate lock, got failure: %s", res.Message)
		}
	})
}

func TestValidateCharacterGlobalPreexistingLocks(t *testing.T) {
	quota := map[string]int{
		"优越代码伤害增加": 4,
		"攻击力增加":    4,
	}

	t.Run("global matching lock on piece needing reroll passes", func(t *testing.T) {
		// 头部 3 号槽锁了优越代码伤害增加，且为需求词条
		// 其余槽位为未获得效果
		parts := map[string]partScan{
			"头部": {
				Slots: [maxSlot]slotScanData{
					{Effect: "防御力增加", Lock: LockNone},
					{Effect: "生命力增加", Lock: LockNone},
					{Effect: "优越代码伤害增加", Lock: LockPermanent},
				},
			},
			"臂部": {
				Slots: [maxSlot]slotScanData{
					{Effect: "防御力增加", Lock: LockNone},
					{Effect: "生命力增加", Lock: LockNone},
					{Effect: "暴击率增加", Lock: LockNone},
				},
			},
			"身躯": {
				Slots: [maxSlot]slotScanData{
					{Effect: "防御力增加", Lock: LockNone},
					{Effect: "生命力增加", Lock: LockNone},
					{Effect: "暴击率增加", Lock: LockNone},
				},
			},
			"腿部": {
				Slots: [maxSlot]slotScanData{
					{Effect: "防御力增加", Lock: LockNone},
					{Effect: "生命力增加", Lock: LockNone},
					{Effect: "暴击率增加", Lock: LockNone},
				},
			},
		}

		res := validateCharacterGlobalPreexistingLocks(parts, quota, "")
		if !res.Passed {
			t.Fatalf("expected pass for globally valid lock, got failure: %s", res.Message)
		}
	})

	t.Run("already satisfied piece with locks passes", func(t *testing.T) {
		// 头部已经拥有两条有效配额词条（攻击+优越）
		// 需求配额只有 优1 攻1
		smallQuota := map[string]int{
			"优越代码伤害增加": 1,
			"攻击力增加":    1,
		}
		parts := map[string]partScan{
			"头部": {
				Slots: [maxSlot]slotScanData{
					{Effect: "攻击力增加", Lock: LockPermanent},
					{Effect: "优越代码伤害增加", Lock: LockPermanent},
					{Effect: "防御力增加", Lock: LockNone},
				},
			},
			"臂部": {Slots: [maxSlot]slotScanData{}},
			"身躯": {Slots: [maxSlot]slotScanData{}},
			"腿部": {Slots: [maxSlot]slotScanData{}},
		}

		res := validateCharacterGlobalPreexistingLocks(parts, smallQuota, "")
		if !res.Passed {
			t.Fatalf("expected pass for satisfied piece with locks, got failure: %s", res.Message)
		}
	})

	t.Run("global bittersweet premature lock on slot 2 fails", func(t *testing.T) {
		// 饱和配额：优4 攻4 装弹4（每件负责3种）
		// 头部：3槽未出，2槽锁了攻击力。先苦后甜策略下，不应提前锁2槽
		satQuota := map[string]int{
			"优越代码伤害增加": 4,
			"攻击力增加":    4,
			"最大装弹数增加":  4,
		}
		parts := map[string]partScan{
			"头部": {
				Slots: [maxSlot]slotScanData{
					{Effect: "防御力增加", Lock: LockNone},
					{Effect: "攻击力增加", Lock: LockPermanent},
					{Effect: "生命力增加", Lock: LockNone},
				},
			},
			"臂部": {
				Slots: [maxSlot]slotScanData{
					{Effect: "防御力增加", Lock: LockNone},
					{Effect: "生命力增加", Lock: LockNone},
					{Effect: "暴击率增加", Lock: LockNone},
				},
			},
			"身躯": {
				Slots: [maxSlot]slotScanData{
					{Effect: "防御力增加", Lock: LockNone},
					{Effect: "生命力增加", Lock: LockNone},
					{Effect: "暴击率增加", Lock: LockNone},
				},
			},
			"腿部": {
				Slots: [maxSlot]slotScanData{
					{Effect: "防御力增加", Lock: LockNone},
					{Effect: "生命力增加", Lock: LockNone},
					{Effect: "暴击率增加", Lock: LockNone},
				},
			},
		}

		res := validateCharacterGlobalPreexistingLocks(parts, satQuota, "")
		if res.Passed {
			t.Fatal("expected failure for premature slot 2 lock violating bittersweet strategy")
		}
	})
}
