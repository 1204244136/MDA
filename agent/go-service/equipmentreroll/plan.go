package equipmentreroll

// 本文件实现"洗词条"的纯决策逻辑（与界面无关、可单元测试）。
// 目前只实现"四优"模板：四件装备分别至少出现 1 条"优越代码伤害增加"，
// 任意栏位出现该效果即接受结果，该装备完成，不需要锁定。

// TargetEffectElementalDamage 是四优模板的目标效果（官方效果名称）。
const TargetEffectElementalDamage = "优越代码伤害增加"

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
