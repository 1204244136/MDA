package equipmentreroll

import "strings"

// 本文件实现"洗词条"的纯决策逻辑（与界面无关、可单元测试）。
// 文档索引：docs/zh_cn/nikke/EquipmentReroll/洗词条策略与Agent逻辑.md
//
// 目前只支持自定义词条配额：
//   - 每种效果 -1（禁止）/ 0（不要求）/ 1~4（需求）；
//   - 例如「四优」就是 {优越代码伤害增加: 4} 的配额；
//   - 禁止词条出现时视为未完成，需要洗掉。

// TargetEffectElementalDamage 是常用目标效果（官方名称）。
const TargetEffectElementalDamage = "优越代码伤害增加"

// TargetEffectAttackIncrease 是常用目标效果（官方名称）。
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

// normalizeQuota 清理全局配额：去空格；保留 -1（禁止）和正数（需求），丢弃 0（不要求）。
func normalizeQuota(quota map[string]int) map[string]int {
	if len(quota) == 0 {
		return quota
	}
	normalized := make(map[string]int, len(quota))
	for effect, count := range quota {
		effect = strings.TrimSpace(effect)
		if count != 0 {
			normalized[effect] = count
		}
	}
	return normalized
}

// quotaTotal 返回全局正数配额总数（-1 禁止项不计入需求数量）。
func quotaTotal(quota map[string]int) int {
	total := 0
	for _, count := range quota {
		if count > 0 {
			total += count
		}
	}
	return total
}

func quotaIsValid(quota map[string]int) bool {
	normalized := normalizeQuota(quota)
	for _, count := range normalized {
		// 每种效果一件装备最多出现一次，因此全局需求不能超过四件；-1 是唯一允许的负值。
		if count < -1 || count > len(equipmentParts) {
			return false
		}
	}
	total := quotaTotal(normalized)
	return total >= 1 && total <= maxSlot*len(equipmentParts)
}

// quotaSatisfied 判断当前各正数配额效果占用数是否达到全局配额。
// -1 禁止项由 hasForbiddenEffect 单独判断，这里直接跳过。
func quotaSatisfied(counts, quota map[string]int) bool {
	for effect, need := range quota {
		if need < 0 {
			continue
		}
		if counts[effect] < need {
			return false
		}
	}
	return true
}

// countQuotaEffectsExcluding 统计装备中命中配额的词条数量（任意槽位都算，可排除指定部位）。
// exclude 为空串时统计全部四件。
func countQuotaEffectsExcluding(parts map[string]partScan, exclude string, quota map[string]int) map[string]int {
	counts := make(map[string]int, len(quota))
	for _, part := range equipmentParts {
		if part == exclude {
			continue
		}
		scan, ok := parts[part]
		if !ok {
			continue
		}
		for _, slot := range scan.Slots {
			if slot.Effect != "" && quota[slot.Effect] > 0 {
				counts[slot.Effect]++
			}
		}
	}
	return counts
}

// countQuotaEffects 统计四件装备中所有命中配额的词条数量（任意槽位都算）。
func countQuotaEffects(parts map[string]partScan, quota map[string]int) map[string]int {
	return countQuotaEffectsExcluding(parts, "", quota)
}

// hasForbiddenEffect 判断四件装备中是否存在被禁止（-1）的词条。
func hasForbiddenEffect(parts map[string]partScan, quota map[string]int) bool {
	for _, part := range equipmentParts {
		scan, ok := parts[part]
		if !ok {
			continue
		}
		if partHasForbiddenEffect(scan, quota) {
			return true
		}
	}
	return false
}

// partHasForbiddenEffect 判断单件装备是否含有被禁止（-1）的词条。
func partHasForbiddenEffect(scan partScan, quota map[string]int) bool {
	for _, slot := range scan.Slots {
		if slot.Effect != "" && quota[slot.Effect] == -1 {
			return true
		}
	}
	return false
}

// AllPartsSatisfiedQuota 判断四件装备是否满足自定义配额全局目标。
// 不要求每件都带攻/优，也不要求三槽全满；只要全局配额效果达到用户设定的数量即可。
func AllPartsSatisfiedQuota(parts map[string]partScan, quota map[string]int) bool {
	quota = normalizeQuota(quota)
	if !quotaIsValid(quota) {
		return false
	}
	counts := countQuotaEffects(parts, quota)
	if hasForbiddenEffect(parts, quota) {
		return false
	}
	return quotaSatisfied(counts, quota)
}

// PartNeedsRerollQuota 判断单件装备是否仍需为自定义配额目标贡献。
// 全局配额已满足且无禁止词条时不需要；否则优先选择有空槽、含非配额词条、含禁止词条或含超配额词条的装备。
func PartNeedsRerollQuota(parts map[string]partScan, part string, quota map[string]int) bool {
	quota = normalizeQuota(quota)
	counts := countQuotaEffects(parts, quota)

	scan, ok := parts[part]
	if !ok {
		return false
	}
	if partHasForbiddenEffect(scan, quota) {
		return true
	}
	if quotaSatisfied(counts, quota) {
		return false
	}
	effects := scan.Effects()
	nonEmpty := 0
	for _, effect := range effects {
		if effect != "" {
			nonEmpty++
		}
	}
	if nonEmpty < maxSlot {
		return true
	}
	for _, effect := range effects {
		if effect != "" && quota[effect] <= 0 {
			return true
		}
	}
	for _, effect := range effects {
		if effect != "" && counts[effect] > quota[effect] {
			return true
		}
	}
	return false
}

// effectiveAffixCount 统计一件装备上“正数配额效果”的数量（物理有效词条数）。
// 超额/临时的配额效果也计入（短期最优会用“有效词条数上升”作为接受理由）。
func effectiveAffixCount(scan partScan, quota map[string]int) int {
	n := 0
	for _, e := range scan.Effects() {
		if slotIsPositiveQuota(e, quota) {
			n++
		}
	}
	return n
}

// globalEffectiveAffixCount 统计全局（四件装备）的物理有效词条总数。
func globalEffectiveAffixCount(parts map[string]partScan, quota map[string]int) int {
	total := 0
	for _, p := range equipmentParts {
		if scan, ok := parts[p]; ok {
			total += effectiveAffixCount(scan, quota)
		}
	}
	return total
}

// decideQuotaByExpectedCost 按"期望剩余订制模块成本"决定结果页是否接受变更。
// 候选状态的全局期望成本严格低于当前状态才接受（方向 A：决策改为期望成本）。
// 期望成本由 expectedModulesForQuota 计算（基于槽位获得概率 / 效果权重 / 同结果排除 / 锁定与重洗费用）。
func decideQuotaByExpectedCost(current, candidate map[string]partScan, quota map[string]int) ResultDecision {
	curCost := expectedModulesForQuota(current, quota)
	candCost := expectedModulesForQuota(candidate, quota)
	// 宏观口径：期望成本严格更低才接受。
	if candCost < curCost-1e-6 {
		return ResultDecisionAccept
	}
	// 短期最优：宏观成本**未变差**（持平/相邻）时，若物理有效词条数上升也接受（即使下次洗会被洗掉）。
	// 这是“多拿多算”加成，不覆盖“宏观更差”的状态（不违反宏观最优）。
	if candCost <= curCost+1e-6 && globalEffectiveAffixCount(candidate, quota) > globalEffectiveAffixCount(current, quota) {
		return ResultDecisionAccept
	}
	return ResultDecisionKeep
}

// DesiredLockSlotForQuota 决定是否锁定某件装备的 2/3 号配额效果（分配感知 + 先苦后甜）。
// 用 allocateQuotaRequired 得到“本件负责的配额效果集合”，再：
//   - 饱和配额（本件负责 >=3 种，即每槽都需填配额，如 优4/攻4/装弹4 → 每件 优1/攻1/装弹1）：
//     按“先苦后甜”先刷最难的 3 号（当前未持本件负责效果的最高难度槽），拿到才锁；
//     不先锁已有效的低优先槽（避免把“赌 3 号一次成型”抬高成本）。
//   - 非饱和（本件负责 1~2 种）：维持“拿到就锁”，用短视 DP 锁已落地的有效可锁槽。
//
// 每件最多 2 锁；策略上只支持 2/3 号槽（1 号不锁——1 号 100% 易得、锁 1 追 23 代价高）。
// material 指定锁定材料（“自订密钥”/“订制模块”，空串默认密钥）：用订制模组锁定时计入获取成本，决策更保守。
func DesiredLockSlotForQuota(parts map[string]partScan, part string, quota map[string]int, material string) (int, bool) {
	quota = normalizeQuota(quota)
	scan, ok := parts[part]
	if !ok {
		return 0, false
	}
	if countLocks(scan) >= 2 {
		// 每件最多 2 锁。
		return 0, false
	}
	assigned := allocateQuotaRequired(parts, quota)
	if len(assigned[part]) == 0 {
		// 本件无需再提供任何配额效果 → 不锁。
		return 0, false
	}
	if len(assigned[part]) >= 3 {
		// 饱和配额：先苦后甜。
		slot := desiredLockSlotBitterSweet(scan, quota, assigned[part], material)
		return slot, slot != 0
	}
	// 非饱和：锁已落地的有效可锁槽（拿到就锁）。
	slot, _ := bestLockSlotAndCostForRequired(scan, quota, assigned[part], material)
	return slot, slot != 0
}

// DecideResultPageQuota 自定义配额结果页决策（全局快照缺失时的降级路径）。
// 按单件期望成本比较：候选（变更后）的期望剩余订制模块消耗严格更低才接受。
// 退化场景下其余三件贡献未知，按“本件需提供全部正数配额效果”（positiveQuotaEffects）保守计算。
func DecideResultPageQuota(current, changed [maxSlot]string, currentScan partScan, quota map[string]int) ResultDecision {
	quota = normalizeQuota(quota)
	required := positiveQuotaEffects(quota)
	currentScanE := currentScan
	for i := range currentScanE.Slots {
		currentScanE.Slots[i].Effect = current[i]
	}
	changedScan := currentScanE
	for i := range changedScan.Slots {
		changedScan.Slots[i].Effect = changed[i]
	}
	currentCost := partExpectedCostForRequired(currentScanE, quota, required, "")
	changedCost := partExpectedCostForRequired(changedScan, quota, required, "")
	if changedCost < currentCost-1e-6 {
		return ResultDecisionAccept
	}
	// 短期最优：宏观成本未变差（持平/相邻）时，本件有效词条数上升也接受（即使下次会被洗掉）。
	if changedCost <= currentCost+1e-6 && effectiveAffixCount(changedScan, quota) > effectiveAffixCount(currentScanE, quota) {
		return ResultDecisionAccept
	}
	return ResultDecisionKeep
}
