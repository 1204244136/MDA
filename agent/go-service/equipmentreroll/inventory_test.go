package equipmentreroll

import (
	"testing"
)

func TestRerollModuleCost(t *testing.T) {
	cases := []struct {
		name        string
		activeLocks int
		want        int
	}{
		{name: "no locks", activeLocks: 0, want: 1},
		{name: "one lock", activeLocks: 1, want: 2},
		{name: "two locks", activeLocks: 2, want: 3},
		{name: "negative clamps to zero", activeLocks: -3, want: 1},
		{name: "more than two clamps", activeLocks: 5, want: 3},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := RerollModuleCost(tt.activeLocks); got != tt.want {
				t.Fatalf("RerollModuleCost(%d) = %d, want %d", tt.activeLocks, got, tt.want)
			}
		})
	}
}

func TestCanAffordReroll(t *testing.T) {
	inv := Inventory{CustomModules: 1, CustomLockKeys: 99}
	if !inv.CanAffordReroll(0) {
		t.Fatal("one module should afford a zero-lock reroll")
	}
	if inv.CanAffordReroll(1) {
		t.Fatal("one module should not afford a one-lock reroll")
	}
	if inv.CanAffordReroll(2) {
		t.Fatal("one module should not afford a two-lock reroll")
	}

	empty := Inventory{}
	if empty.CanAffordReroll(0) {
		t.Fatal("empty inventory cannot afford any reroll")
	}
}

func TestEstimateSupportedEffectChanges(t *testing.T) {
	if got := (Inventory{CustomModules: 0}).EstimateSupportedEffectChanges(); got != 0 {
		t.Fatalf("zero modules estimate = %d, want 0", got)
	}
	if got := (Inventory{CustomModules: 5}).EstimateSupportedEffectChanges(); got != 5 {
		t.Fatalf("five modules estimate = %d, want 5", got)
	}
}

func TestCanAffordLock(t *testing.T) {
	inv := Inventory{CustomModules: 2, CustomLockKeys: 1}

	if !inv.CanAffordLock("订制模块", 0) {
		t.Fatal("two modules should afford the first module lock")
	}
	if !inv.CanAffordLock("订制模块", 1) {
		t.Fatal("two modules should afford the second module lock")
	}
	if inv.CanAffordLock("订制模块", 2) {
		t.Fatal("two modules should not afford the third module lock")
	}
	if !inv.CanAffordLock("自订密钥", 0) {
		t.Fatal("one key should afford the first key lock")
	}
	if inv.CanAffordLock("自订密钥", 1) {
		t.Fatal("one key should not afford the second key lock")
	}
	if inv.CanAffordLock("未知材料", 0) {
		t.Fatal("unknown lock material should never be affordable")
	}
}
