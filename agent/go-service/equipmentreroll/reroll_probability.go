package equipmentreroll

// tierProbabilities 是两类重洗共用的基础档位池；新规则只排除整组三槽完全重复。
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

func sameEffectOutcome(out slotOutcome, scan partScan, unlocked []int) bool {
	for _, i := range unlocked {
		if out.effects[i] != scan.Slots[i].Effect {
			return false
		}
	}
	return true
}

func exactEffectRepeatProbability(scan partScan, unlocked []int, outcomes []slotOutcome) float64 {
	for _, out := range outcomes {
		if !sameEffectOutcome(out, scan, unlocked) {
			continue
		}
		p := out.prob
		for _, i := range unlocked {
			if scan.Slots[i].Effect != "" {
				p *= sameValueProbability(scan.Slots[i])
			}
		}
		return p
	}
	return 0
}

// conditionedEffectOutcomes 在压缩之前只扣除整组重复的概率质量。
func conditionedEffectOutcomes(scan partScan, unlocked []int, lockedEffects []string, required, forbidden map[string]bool, allow map[string]map[int]bool) []compressedOutcome {
	raw := enumerateSlotOutcomes(unlocked, lockedEffects)
	repeat := exactEffectRepeatProbability(scan, unlocked, raw)
	var result []compressedOutcome
	for _, out := range raw {
		p := out.prob
		if sameEffectOutcome(out, scan, unlocked) {
			p -= repeat
		}
		if p <= 0 || repeat >= 1 {
			continue
		}
		values := make([]string, len(unlocked))
		for i, slot := range unlocked {
			values[i] = compressEffectSlot(out.effects[slot], slot, required, forbidden, allow)
		}
		result = append(result, compressedOutcome{values: values, prob: p / (1 - repeat)})
	}
	return result
}

// effectFirstPassageCost 精确解固定锁、每次接受非终态的首次到达期望。
// Q(y|x)=P(y)/(1-P(x)), y!=x；V(x)=c*((1-Σ失败态P(y)^2)/P(成功)-P(x))。
// 效果结果与档位独立，平方概率可按档位碰撞率消元，不用枚举 15^3 数值。
func effectFirstPassageCost(scan partScan, unlocked []int, outcomes []slotOutcome, success func([maxSlot]string) bool, cost float64) float64 {
	if success(scan.Effects()) {
		return 0
	}
	win, collision := 0.0, 0.0
	for _, out := range outcomes {
		effects := scan.Effects()
		p2 := out.prob * out.prob
		for _, i := range unlocked {
			effects[i] = out.effects[i]
			if effects[i] != "" {
				p2 *= tierCollisionProbability()
			}
		}
		if success(effects) {
			win += out.prob
		} else {
			collision += p2
		}
	}
	if win <= 0 {
		return costUnreachable
	}
	return cost * ((1-collision)/win - exactEffectRepeatProbability(scan, unlocked, outcomes))
}
