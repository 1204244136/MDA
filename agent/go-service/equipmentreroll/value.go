package equipmentreroll

import "fmt"

// valueTarget 按效果指定范围内的最低总 T；0 在配置解析时已移除。
type valueTarget map[string]int

func (target valueTarget) validate(limit int) error {
	if len(target) == 0 {
		return fmt.Errorf("at least one value target is required")
	}
	for effect, tier := range target {
		if _, ok := effectTiers[effect]; !ok || tier < 1 || tier > limit {
			return fmt.Errorf("invalid value target %q: T%d (maximum %d)", effect, tier, limit)
		}
	}
	return nil
}

func (c carrierConfig) valueScope() []string {
	if c.isSingle() {
		return []string{c.Part}
	}
	return equipmentParts
}

type valueProgress struct {
	Totals  map[string]int
	Minimum map[string]int
	Maximum map[string]int
	Pending int
}

// valuePartTiers 只校验及解释快照；固定锁可解除，不能在这里认定低值锁不可达。
func valuePartTiers(scan partScan) ([maxSlot]int, error) {
	var tiers [maxSlot]int
	seen := map[string]bool{}
	for i, slot := range scan.Slots {
		if slot.Effect == "" {
			if slot.Value != "" || slot.Lock != LockNone {
				return tiers, fmt.Errorf("slot %d: inconsistent empty slot", i+1)
			}
			continue
		}
		if seen[slot.Effect] {
			return tiers, fmt.Errorf("slot %d: duplicate effect", i+1)
		}
		seen[slot.Effect] = true
		tier, _, ok := resolveEffectTier(slot.Effect, slot.Value)
		if !ok {
			return tiers, fmt.Errorf("slot %d: unrecognized value %q", i+1, slot.Value)
		}
		if slot.Lock < LockNone || slot.Lock > LockOneTime {
			return tiers, fmt.Errorf("slot %d: invalid lock", i+1)
		}
		tiers[i] = tier
	}
	return tiers, nil
}

func evaluateValueScope(parts map[string]partScan, cfg carrierConfig) (valueProgress, error) {
	p := valueProgress{Totals: map[string]int{}, Minimum: map[string]int{}, Maximum: map[string]int{}}
	if err := cfg.validateOperation(); err != nil {
		return p, err
	}
	if !cfg.isValue() {
		return p, fmt.Errorf("value planning requires value operation")
	}
	for _, part := range cfg.valueScope() {
		scan, ok := parts[part]
		if !ok {
			return p, fmt.Errorf("%s: missing snapshot", part)
		}
		tiers, err := valuePartTiers(scan)
		if err != nil {
			return p, fmt.Errorf("%s: %w", part, err)
		}
		for i, slot := range scan.Slots {
			if slot.Effect == "" {
				continue
			}
			p.Totals[slot.Effect] += tiers[i]
			p.Minimum[slot.Effect]++
			p.Maximum[slot.Effect] += len(tierProbabilities)
		}
	}
	for effect, target := range cfg.ValueTargets {
		if target > p.Maximum[effect] {
			return p, fmt.Errorf("%s: target T%d exceeds attainable total T%d; reset cannot create effects", effect, target, p.Maximum[effect])
		}
		if p.Totals[effect] < target {
			p.Pending++
		}
	}
	return p, nil
}

// localValueRequirements 将角色目标投影到当前装备，其他装备的贡献保持不变。
// 仅用于成本相同时比较截断总进度；不作为逐项禁止退步的约束。
func localValueRequirements(scan partScan, totals map[string]int, target valueTarget) [maxSlot]int {
	var required [maxSlot]int
	tiers, _ := valuePartTiers(scan)
	for i, slot := range scan.Slots {
		if wanted, ok := target[slot.Effect]; ok {
			required[i] = max(1, wanted-(totals[slot.Effect]-tiers[i]))
		}
	}
	return required
}

func decideValueResultForScope(parts map[string]partScan, part string, changed, values [maxSlot]string, cfg carrierConfig) (ResultDecision, error) {
	decision, _, _, err := evaluateValueResult(parts, part, changed, values, cfg)
	return decision, err
}

func evaluateValueResult(parts map[string]partScan, part string, changed, values [maxSlot]string, cfg carrierConfig) (ResultDecision, float64, float64, error) {
	progress, err := evaluateValueScope(parts, cfg)
	if err != nil {
		return ResultDecisionKeep, 0, 0, err
	}
	current, ok := parts[part]
	if !ok {
		return ResultDecisionKeep, 0, 0, fmt.Errorf("missing candidate part %s", part)
	}
	before, _ := valuePartTiers(current)
	candidate := current
	for i, slot := range current.Slots {
		if changed[i] != slot.Effect {
			return ResultDecisionKeep, 0, 0, fmt.Errorf("slot %d: effect changed during value reroll", i+1)
		}
		candidate.Slots[i].Value = values[i]
	}
	after, err := valuePartTiers(candidate)
	if err != nil {
		return ResultDecisionKeep, 0, 0, err
	}
	for i, slot := range current.Slots {
		if slot.Lock != LockNone && before[i] != after[i] {
			return ResultDecisionKeep, 0, 0, fmt.Errorf("slot %d: locked value changed", i+1)
		}
	}
	var locks [maxSlot]SlotLock
	for i, slot := range current.Slots {
		locks[i] = slot.Lock
	}
	table := newValueCostTable(parts, part, cfg, progress, locks)
	currentCost, candidateCost := table.cost(before), table.cost(after)
	if table.accepts(before, after) {
		return ResultDecisionAccept, currentCost, candidateCost, nil
	}
	return ResultDecisionKeep, currentCost, candidateCost, nil
}
