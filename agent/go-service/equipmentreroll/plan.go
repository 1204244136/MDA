package equipmentreroll

// 本文件实现"洗词条"的纯决策逻辑（与界面无关、可单元测试）。
// 目前实现：
//   - 四优模板：四件装备分别至少出现 1 条"优越代码伤害增加"，任意栏位出现即接受，无锁定。
//   - 四攻四优模板：四件装备分别同时拥有 1 条"优越代码伤害增加"和 1 条"攻击力增加"，需 3→2→1 先苦后甜锁定。

// TargetEffectElementalDamage 是四优/四攻四优模板的目标效果之一（官方名称）。
const TargetEffectElementalDamage = "优越代码伤害增加"

// TargetEffectAttackIncrease 是四攻四优模板的第二目标效果。
const TargetEffectAttackIncrease = "攻击力增加"

// ResultDecision 表示效果变更结果页的接受方向。
type ResultDecision int

const (
	// ResultDecisionKeep 表示点击"效果维持"：放弃变更效果，保留当前效果。
	ResultDecisionKeep ResultDecision = iota
	// ResultDecisionAccept 表示点击"效果变更"：接受变更效果，写入装备。
	ResultDecisionAccept
)

func (d ResultDecision) String() string {
	switch d {
	case ResultDecisionAccept:
		return "效果变更"
	default:
		return "效果维持"
	}
}

// PartHasEffect 判断单件装备的三个栏位中是否存在目标效果。
// 未获得效果的栏位（空字符串）永远不会匹配。
func PartHasEffect(effects [maxSlot]string, target string) bool {
	if target == "" {
		return false
	}
	for _, effect := range effects {
		if effect == target {
			return true
		}
	}
	return false
}

// AllPartsSatisfied 判断四件装备是否都已有目标效果。
func AllPartsSatisfied(parts map[string][maxSlot]string, target string) bool {
	for _, part := range equipmentParts {
		effects, ok := parts[part]
		if !ok || !PartHasEffect(effects, target) {
			return false
		}
	}
	return true
}

// ChooseNextPart 按给定顺序选出第一件尚未达标的装备。
// 返回 (部位, true)；若全部达标或没有任何部位，返回 ("", false)。
// 四优模板每轮只挑一件装备执行一次效果变更，完成后回到整体重新调度。
func ChooseNextPart(parts map[string][maxSlot]string, order []string, target string) (string, bool) {
	for _, part := range order {
		effects, ok := parts[part]
		if !ok {
			continue
		}
		if !PartHasEffect(effects, target) {
			return part, true
		}
	}
	return "", false
}

// DecideResultPage 决策结果页点击"效果变更"还是"效果维持"。
//
// 四优模板的规则是"任意栏位出现该效果即接受结果"：
//   - 变更效果包含目标且当前效果不包含 → 接受（效果变更）；
//   - 变更效果丢失了当前已有的目标 → 防御性保留（效果维持）；
//   - 双方都包含目标（理论上不会发生，因为达标装备不会被洗）→ 接受；
//   - 其余情况（变更效果没有目标）→ 维持（效果维持）。
func DecideResultPage(current, changed [maxSlot]string, target string) ResultDecision {
	changedHas := PartHasEffect(changed, target)
	currentHas := PartHasEffect(current, target)

	switch {
	case changedHas && !currentHas:
		return ResultDecisionAccept
	case currentHas && !changedHas:
		return ResultDecisionKeep
	case changedHas && currentHas:
		return ResultDecisionAccept
	default:
		return ResultDecisionKeep
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 四攻四优（8有效）专用逻辑
// 每件装备需同时拥有 优越代码伤害增加 + 攻击力增加，各至少1条，栏位不限。
// 策略：两条有效按 3→2→1 先苦后甜；2/3号先出现任一目标即锁定，再追另一条；不先锁1号。
// ─────────────────────────────────────────────────────────────────────────────

// PartHasBothEffects 判断单件是否同时拥有攻与优。
func PartHasBothEffects(effects [maxSlot]string) bool {
	return PartHasEffect(effects, TargetEffectElementalDamage) && PartHasEffect(effects, TargetEffectAttackIncrease)
}

// PartTargetCount 返回该件中攻/优的去重计数（0/1/2）。
// 同一效果在多槽重复出现只计1，需与 AllPartsSatisfied 的语义一致。
func PartTargetCount(effects [maxSlot]string) int {
	n := 0
	if PartHasEffect(effects, TargetEffectElementalDamage) {
		n++
	}
	if PartHasEffect(effects, TargetEffectAttackIncrease) {
		n++
	}
	return n
}

// AllPartsSatisfiedFourAtkFourElem 判断四件是否都满足四攻四优。
func AllPartsSatisfiedFourAtkFourElem(parts map[string][maxSlot]string) bool {
	for _, part := range equipmentParts {
		effects, ok := parts[part]
		if !ok || !PartHasBothEffects(effects) {
			return false
		}
	}
	return true
}

// ChooseNextPartFourAtkFourElem 按顺序选第一件未达标（未同时拥有攻优）的装备。
func ChooseNextPartFourAtkFourElem(parts map[string][maxSlot]string, order []string) (string, bool) {
	for _, part := range order {
		effects, ok := parts[part]
		if !ok {
			continue
		}
		if !PartHasBothEffects(effects) {
			return part, true
		}
	}
	return "", false
}

// FindTargetSlot 返回第一个匹配 target 的槽位下标（0-based），未找到返回 -1。
func FindTargetSlot(effects [maxSlot]string, target string) int {
	for i, e := range effects {
		if e == target {
			return i
		}
	}
	return -1
}

// DesiredLockSlotForFourAtkFourElem 根据当前快照决定应锁的槽位。
// 只锁定"2/3号中已出现且未锁定的单一目标"，符合先苦后甜；1号命中不锁。
// 返回 (slotIndex 1-based, true) 或 (0,false) 表示暂不锁定。
func DesiredLockSlotForFourAtkFourElem(scan partScan) (int, bool) {
	// 已满足双目标：无需再锁定
	if PartHasBothEffects(scan.Effects()) {
		return 0, false
	}
	// 已有锁：不再新增（四攻四优最多1锁，避免成本激增）
	for _, s := range scan.Slots {
		if s.Lock != LockNone {
			return 0, false
		}
	}
	count := PartTargetCount(scan.Effects())
	if count != 1 {
		return 0, false
	}
	// 单目标时，检查2/3号是否有该目标，有则锁定对应槽
	for _, slot := range []int{3, 2} { // 优先3号，其次2号（3比2更难）
		idx := slot - 1
		eff := scan.Slots[idx].Effect
		if eff == TargetEffectElementalDamage || eff == TargetEffectAttackIncrease {
			return slot, true
		}
	}
	// 仅1号命中 → 不锁，继续重洗2/3
	return 0, false
}

// DecideResultPageFourAtkFourElem 四攻四优结果页决策。
// 规则：
//   - 若 current 已满足双目标（防御分支）：changed 丢失任一目标 → 维持
//   - changed 达成双目标且 current 未达成 → 接受
//   - 双方均未达成双目标：比较达成目标数（0/1/2），多者胜；数相等时优先 2/3号命中（锁定价值高）
//   - 若 current 有1目标在1号，changed 有1目标在2/3号 → 接受（为后续锁定创造条件）
func DecideResultPageFourAtkFourElem(current, changed [maxSlot]string, currentScan partScan) ResultDecision {
	currentCount := PartTargetCount(current)
	changedCount := PartTargetCount(changed)
	currentBoth := currentCount == 2
	changedBoth := changedCount == 2

	// 防御：已达标的被洗掉任何一个 → 维持
	if currentBoth && !changedBoth {
		return ResultDecisionKeep
	}
	if !currentBoth && changedBoth {
		return ResultDecisionAccept
	}
	if currentBoth && changedBoth {
		return ResultDecisionAccept
	}
	// 均未达标，按数量比较；0→1 时需检查槽位价值，1号单优不接受（重洗以搏2/3）
	if changedCount > currentCount {
		if currentCount == 0 && changedCount == 1 {
			changedSlot := FindTargetSlot(changed, TargetEffectElementalDamage)
			if changedSlot == -1 {
				changedSlot = FindTargetSlot(changed, TargetEffectAttackIncrease)
			}
			if changedSlot == 0 {
				// 1号单优价值低，放弃以保留重洗机会
				return ResultDecisionKeep
			}
		}
		return ResultDecisionAccept
	}
	if changedCount < currentCount {
		return ResultDecisionKeep
	}
	// 数量相等且均为1时，比较命中槽位优先级：3/2号优于1号
	if currentCount == 1 && changedCount == 1 {
		currentSlot := FindTargetSlot(current, TargetEffectElementalDamage)
		if currentSlot == -1 {
			currentSlot = FindTargetSlot(current, TargetEffectAttackIncrease)
		}
		changedSlot := FindTargetSlot(changed, TargetEffectElementalDamage)
		if changedSlot == -1 {
			changedSlot = FindTargetSlot(changed, TargetEffectAttackIncrease)
		}
		// slot index越小表示1号，越大表示3号；优先锁定价值高的3/2
		// 简单判定：若 changed 在2/3而 current 在1 → 接受
		isCurrentSlot23 := currentSlot == 1 || currentSlot == 2
		isChangedSlot23 := changedSlot == 1 || changedSlot == 2
		if isChangedSlot23 && !isCurrentSlot23 {
			return ResultDecisionAccept
		}
		if !isChangedSlot23 && isCurrentSlot23 {
			// 现有更优，不接受把2/3洗回1号
			return ResultDecisionKeep
		}
	}
	// 其余平局（如均空）维持
	if changedCount == currentCount {
		// 若本次能为后续锁定创造 2/3 条件，而当前在1号，已在上面处理
		return ResultDecisionKeep
	}
	return ResultDecisionKeep
}
