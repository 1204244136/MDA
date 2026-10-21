package equipmentreroll

// tierProbabilities 是数值档位的基础抽样池；效果种类概率不受数值判重影响。
var tierProbabilities = [15]float64{.12, .12, .12, .12, .12, .07, .07, .07, .07, .07, .01, .01, .01, .01, .01}

func tierCollisionProbability() float64 {
	p := 0.0
	for _, v := range tierProbabilities {
		p += v * v
	}
	return p
}

// sameValueProbability 真实档位使用准确概率，合成状态用独立抽样的碰撞率。
// 合成状态必须清空数值，不能借用另一效果的旧数值来校正。
func sameValueProbability(slot slotScanData) float64 {
	if tier, _, ok := resolveEffectTier(slot.Effect, slot.Value); ok {
		return tierProbabilities[tier-1]
	}
	return tierCollisionProbability()
}

// conditionedEffectOutcomes 将同件不重复的效果抽样投影到目标状态空间。
// 官方 2026-09-17 公告规定同效果、同数值时只重抽数值，因此不排除旧效果组合。
// 保留 scan 参数与函数名供已有效果规划调用；数值模式使用自己的候选分布。
func conditionedEffectOutcomes(_ partScan, unlocked []int, lockedEffects []string, required, forbidden map[string]bool, allow map[string]map[int]bool) []compressedOutcome {
	raw := enumerateSlotOutcomes(unlocked, lockedEffects)
	var result []compressedOutcome
	for _, out := range raw {
		if out.prob <= 0 {
			continue
		}
		values := make([]string, len(unlocked))
		for i, slot := range unlocked {
			values[i] = compressEffectSlot(out.effects[slot], slot, required, forbidden, allow)
		}
		result = append(result, compressedOutcome{values: values, prob: out.prob})
	}
	return result
}

// effectFirstPassageCost 计算固定锁集合下首次达标的几何分布期望 c/P(成功)。
// 相同效果组合仍能出现；数值重抽不会改变效果种类的成功概率。
// 该辅助函数不负责规划未来新增锁，动态锁策略由效果成本规划层计算。
func effectFirstPassageCost(scan partScan, unlocked []int, outcomes []slotOutcome, success func([maxSlot]string) bool, cost float64) float64 {
	if success(scan.Effects()) {
		return 0
	}
	win := 0.0
	for _, out := range outcomes {
		effects := scan.Effects()
		for _, i := range unlocked {
			effects[i] = out.effects[i]
		}
		if success(effects) {
			win += out.prob
		}
	}
	if win <= 0 {
		return costUnreachable
	}
	return cost / win
}
