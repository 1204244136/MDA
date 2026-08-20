package equipmentreroll

import (
	"testing"
)

func emptyEffects() [maxSlot]string {
	return [maxSlot]string{}
}

func TestPartHasEffect(t *testing.T) {
	effects := [maxSlot]string{"攻击力增加", "优越代码伤害增加", "蓄力速度增加"}
	if !PartHasEffect(effects, TargetEffectElementalDamage) {
		t.Fatal("expected part to have the four-elemental-damage target effect")
	}
	missing := [maxSlot]string{"攻击力增加", "蓄力速度增加", ""}
	if PartHasEffect(missing, TargetEffectElementalDamage) {
		t.Fatal("part without target effect should not match")
	}
	if PartHasEffect(emptyEffects(), TargetEffectElementalDamage) {
		t.Fatal("empty part should not match")
	}
	if PartHasEffect(effects, "") {
		t.Fatal("empty target should never match")
	}
}

func TestAllPartsSatisfied(t *testing.T) {
	parts := make(map[string][maxSlot]string, len(equipmentParts))
	for _, part := range equipmentParts {
		effects := emptyEffects()
		effects[0] = TargetEffectElementalDamage
		parts[part] = effects
	}
	if !AllPartsSatisfied(parts, TargetEffectElementalDamage) {
		t.Fatal("all parts with target effect should be satisfied")
	}

	// 去掉腿部目标后应判定为未满足。
	legs := parts["腿部"]
	legs[0] = "攻击力增加"
	parts["腿部"] = legs
	if AllPartsSatisfied(parts, TargetEffectElementalDamage) {
		t.Fatal("part missing target effect should not be satisfied")
	}

	// 缺一个部位（识别不完整）也视为未满足。
	delete(parts, "头部")
	if AllPartsSatisfied(parts, TargetEffectElementalDamage) {
		t.Fatal("missing part should not be satisfied")
	}
}

func TestChooseNextPart(t *testing.T) {
	parts := make(map[string][maxSlot]string, len(equipmentParts))
	for _, part := range equipmentParts {
		parts[part] = emptyEffects()
	}

	// 全部未达标：按顺序选第一件。
	got, ok := ChooseNextPart(parts, equipmentParts, TargetEffectElementalDamage)
	if !ok || got != "头部" {
		t.Fatalf("ChooseNextPart(all empty) = (%q, %v), want (头部, true)", got, ok)
	}

	// 头部达标后，应选中臂部。
	head := parts["头部"]
	head[0] = TargetEffectElementalDamage
	parts["头部"] = head
	got, ok = ChooseNextPart(parts, equipmentParts, TargetEffectElementalDamage)
	if !ok || got != "臂部" {
		t.Fatalf("ChooseNextPart(head done) = (%q, %v), want (臂部, true)", got, ok)
	}

	// 全部达标：返回 false。
	for _, part := range equipmentParts {
		effects := parts[part]
		effects[0] = TargetEffectElementalDamage
		parts[part] = effects
	}
	if got, ok := ChooseNextPart(parts, equipmentParts, TargetEffectElementalDamage); ok || got != "" {
		t.Fatalf("ChooseNextPart(all done) = (%q, %v), want (\"\", false)", got, ok)
	}
}

func TestDecideResultPageFourElementalDamage(t *testing.T) {
	current := [maxSlot]string{"攻击力增加", "蓄力速度增加", ""}
	changed := [maxSlot]string{"攻击力增加", "优越代码伤害增加", ""}

	if got := DecideResultPage(current, changed, TargetEffectElementalDamage); got != ResultDecisionAccept {
		t.Fatalf("changed with target should accept, got %v", got)
	}

	// 变更效果没有目标 → 维持。
	noTarget := [maxSlot]string{"攻击力增加", "命中率增加", ""}
	if got := DecideResultPage(current, noTarget, TargetEffectElementalDamage); got != ResultDecisionKeep {
		t.Fatalf("changed without target should keep, got %v", got)
	}

	// 变更效果在第三栏出现目标 → 接受。
	slot3 := [maxSlot]string{"攻击力增加", "蓄力速度增加", TargetEffectElementalDamage}
	if got := DecideResultPage(current, slot3, TargetEffectElementalDamage); got != ResultDecisionAccept {
		t.Fatalf("changed with target in slot 3 should accept, got %v", got)
	}

	// 防御分支：变更效果把当前已有目标洗掉了 → 维持。
	had := [maxSlot]string{TargetEffectElementalDamage, "攻击力增加", ""}
	lost := [maxSlot]string{"攻击力增加", "命中率增加", ""}
	if got := DecideResultPage(had, lost, TargetEffectElementalDamage); got != ResultDecisionKeep {
		t.Fatalf("changed losing existing target should keep, got %v", got)
	}

	// 双方都包含目标 → 接受。
	both := [maxSlot]string{TargetEffectElementalDamage, "蓄力速度增加", ""}
	if got := DecideResultPage(had, both, TargetEffectElementalDamage); got != ResultDecisionAccept {
		t.Fatalf("changed keeping target should accept, got %v", got)
	}

	// 双方都没有目标 → 维持。
	plain := [maxSlot]string{"攻击力增加", "暴击率增加", ""}
	if got := DecideResultPage(plain, noTarget, TargetEffectElementalDamage); got != ResultDecisionKeep {
		t.Fatalf("no target on either side should keep, got %v", got)
	}
}
