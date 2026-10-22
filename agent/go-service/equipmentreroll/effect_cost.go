package equipmentreroll

import (
	"math"
	"math/bits"
	"sort"
	"sync"
)

// effectCostLocks 只保存会延续到下一轮的保护效果；未锁槽不参与成本表的键。
// 效果池不依赖未锁槽的旧效果或数值，因此同一锁态可在所有候选间复用。
type effectCostLocks struct {
	Effects [maxSlot]string
	Mask    uint8
}

type effectCostChoice struct {
	Slot int
	Cost float64
}

type effectCostModel struct {
	mu          sync.Mutex
	required    map[string]bool
	forbidden   map[string]bool
	allow       map[string]map[int]bool
	moduleLocks bool
	futureCosts map[effectCostLocks]float64
}

var dynamicEffectModels = struct {
	sync.Mutex
	models map[string]*effectCostModel
}{models: make(map[string]*effectCostModel)}

func resetDynamicEffectCostCache() {
	dynamicEffectModels.Lock()
	dynamicEffectModels.models = make(map[string]*effectCostModel)
	dynamicEffectModels.Unlock()
}

// dynamicEffectPlan 求解包含未来逐步加锁的单件效果成本，并返回当前最优的下一把锁。
// 三个槽位中属于 required 且位置正确的效果均可加锁，由续程成本决定是否值得，已有永久锁始终保留。
// 模块模式按首锁 2、次锁 3 计费；已有一次性锁只保护本轮，之后重新保护要重新付费。
// 密钥模式给出密钥可持续续用时的模块成本下界，密钥数量本身不折算为模块。
func dynamicEffectPlan(scan partScan, required, forbidden []string, slotAllow map[string]map[int]bool, material string) (slot int, cost float64) {
	key := dpCacheKeyCore(partScan{}, nil, required, forbidden, 0, slotAllow) + "#" + lockMaterialOrDefault(material)
	dynamicEffectModels.Lock()
	model := dynamicEffectModels.models[key]
	if model == nil {
		allow := make(map[string]map[int]bool, len(slotAllow))
		for effect, slots := range slotAllow {
			allow[effect] = make(map[int]bool, len(slots))
			for index, allowed := range slots {
				allow[effect][index] = allowed
			}
		}
		model = &effectCostModel{
			required:    requiredSet(required),
			forbidden:   forbiddenSet(forbidden),
			allow:       allow,
			moduleLocks: lockMaterialOrDefault(material) == "订制模块",
			futureCosts: make(map[effectCostLocks]float64),
		}
		dynamicEffectModels.models[key] = model
	}
	dynamicEffectModels.Unlock()
	model.mu.Lock()
	defer model.mu.Unlock()

	effects := scan.Effects()
	if model.satisfied(effects) {
		return 0, 0
	}
	if countLocks(scan) >= maxSlot || len(model.required) > maxSlot {
		return 0, costUnreachable
	}
	var locks effectCostLocks
	var temporary uint8
	for i, current := range scan.Slots {
		if current.Lock == LockNone {
			continue
		}
		if model.moduleLocks && current.Lock == LockOneTime {
			temporary |= 1 << i
			continue
		}
		locks.Mask |= 1 << i
		locks.Effects[i] = current.Effect
	}
	var choice effectCostChoice
	if temporary == 0 {
		choice = model.after(locks, effects)
	} else {
		choice = model.withTemporary(locks, temporary, effects)
	}
	return choice.Slot, choice.Cost
}

func (m *effectCostModel) satisfied(effects [maxSlot]string) bool {
	return partHasRequiredAndNoForbiddenSlot(effects, m.required, m.forbidden, m.allow)
}

func (m *effectCostModel) acquireCost(activeLocks int) float64 {
	if m.moduleLocks {
		return float64(lockAcquireCost(activeLocks))
	}
	return 0
}

func (m *effectCostModel) canLock(slot int, effect string, protected uint8) bool {
	return protected&(1<<slot) == 0 && effect != "" && m.required[effect] && !m.forbidden[effect] &&
		(len(m.allow[effect]) == 0 || m.allow[effect][slot])
}

// after 允许保留现有结果、立即达标结束，或继续增加一把有用的锁。
// future 只向锁数更多的状态递归，所以最多两层新增锁，不需要迭代整张马尔可夫表。
func (m *effectCostModel) after(locks effectCostLocks, effects [maxSlot]string) effectCostChoice {
	if m.satisfied(effects) {
		return effectCostChoice{}
	}
	best := effectCostChoice{Cost: m.future(locks)}
	count := bits.OnesCount8(locks.Mask)
	if count >= maxSlot-1 {
		return best
	}
	for _, slot := range []int{2, 1, 0} {
		if !m.canLock(slot, effects[slot], locks.Mask) {
			continue
		}
		next := locks
		next.Mask |= 1 << slot
		next.Effects[slot] = effects[slot]
		cost := m.acquireCost(count) + m.after(next, effects).Cost
		if cost < best.Cost-1e-9 {
			best = effectCostChoice{Slot: slot + 1, Cost: cost}
		}
	}
	return best
}

// future 求下一次重洗前的续程成本 F(L)。给定锁态 L，一次重洗结果 x 的成本是
// min(F(L), B(x))：没有值得保留的新结果时可维持旧效果，B 包含达标和后续新增锁。
// Bellman 方程 F=c+ΣP(x)min(F,B(x)) 按 B 排序后可直接解，无需把三条同时抽齐。
func (m *effectCostModel) future(locks effectCostLocks) float64 {
	if cached, ok := m.futureCosts[locks]; ok {
		return cached
	}
	unlocked, lockedEffects := effectCostUnlocked(locks)
	if len(unlocked) == 0 || !m.reachable(locks, len(unlocked)) {
		m.futureCosts[locks] = costUnreachable
		return costUnreachable
	}
	type threshold struct {
		cost, probability float64
	}
	var thresholds []threshold
	totalProbability := 0.0
	count := bits.OnesCount8(locks.Mask)
	for _, outcome := range enumerateSlotOutcomes(unlocked, lockedEffects) {
		if outcome.prob <= 0 {
			continue
		}
		totalProbability += outcome.prob
		effects := effectCostMerge(locks, outcome.effects)
		cost := costUnreachable
		if m.satisfied(effects) {
			cost = 0
		} else if count < maxSlot-1 {
			for _, slot := range []int{2, 1, 0} {
				if !m.canLock(slot, effects[slot], locks.Mask) {
					continue
				}
				next := locks
				next.Mask |= 1 << slot
				next.Effects[slot] = effects[slot]
				cost = min(cost, m.acquireCost(count)+m.after(next, effects).Cost)
			}
		}
		if cost < costUnreachable {
			thresholds = append(thresholds, threshold{cost, outcome.prob})
		}
	}
	sort.Slice(thresholds, func(i, j int) bool { return thresholds[i].cost < thresholds[j].cost })
	probability, weightedCost := 0.0, 0.0
	result := costUnreachable
	for i, candidate := range thresholds {
		probability += candidate.probability
		weightedCost += candidate.probability * candidate.cost
		// 概率总质量归一化也消除枚举求和的浮点误差。
		value := (float64(RerollModuleCost(count))*totalProbability + weightedCost) / probability
		if i == len(thresholds)-1 || value <= thresholds[i+1].cost {
			result = min(value, costUnreachable)
			break
		}
	}
	m.futureCosts[locks] = result
	return result
}

func (m *effectCostModel) reachable(locks effectCostLocks, unlocked int) bool {
	missing := len(m.required)
	for i, effect := range locks.Effects {
		if locks.Mask&(1<<i) == 0 {
			continue
		}
		if effect != "" && m.forbidden[effect] {
			return false
		}
		if m.required[effect] {
			if len(m.allow[effect]) > 0 && !m.allow[effect][i] {
				return false
			}
			missing--
		}
	}
	return missing <= unlocked
}

// withTemporary 只展开当前一次性锁保护的首轮。结果产生后，无论变更还是维持，
// 一次性锁都会失效；续程比较恢复为永久锁状态，不能把已消费的密钥当成免费永久锁。
func (m *effectCostModel) withTemporary(locks effectCostLocks, temporary uint8, effects [maxSlot]string) effectCostChoice {
	if m.satisfied(effects) {
		return effectCostChoice{}
	}
	protected := locks
	protected.Mask |= temporary
	for i := range effects {
		if temporary&(1<<i) != 0 {
			protected.Effects[i] = effects[i]
		}
	}
	count := bits.OnesCount8(protected.Mask)
	if count >= maxSlot {
		return effectCostChoice{Cost: costUnreachable}
	}
	unlocked, lockedEffects := effectCostUnlocked(protected)
	keep := m.after(locks, effects).Cost
	continuation, probability := 0.0, 0.0
	for _, outcome := range enumerateSlotOutcomes(unlocked, lockedEffects) {
		if outcome.prob <= 0 {
			continue
		}
		nextEffects := effectCostMerge(protected, outcome.effects)
		continuation += outcome.prob * min(keep, m.after(locks, nextEffects).Cost)
		probability += outcome.prob
	}
	best := effectCostChoice{Cost: costUnreachable}
	if probability > 0 {
		best.Cost = min(costUnreachable, float64(RerollModuleCost(count))+continuation/probability)
	}
	if count < maxSlot-1 {
		for _, slot := range []int{2, 1, 0} {
			if !m.canLock(slot, effects[slot], protected.Mask) {
				continue
			}
			next := locks
			next.Mask |= 1 << slot
			next.Effects[slot] = effects[slot]
			cost := m.acquireCost(count) + m.withTemporary(next, temporary, effects).Cost
			if cost < best.Cost-1e-9 {
				best = effectCostChoice{Slot: slot + 1, Cost: cost}
			}
		}
	}
	if math.IsNaN(best.Cost) || math.IsInf(best.Cost, 0) {
		best = effectCostChoice{Cost: costUnreachable}
	}
	return best
}

func effectCostUnlocked(locks effectCostLocks) (unlocked []int, lockedEffects []string) {
	for i, effect := range locks.Effects {
		if locks.Mask&(1<<i) == 0 {
			unlocked = append(unlocked, i)
		} else if effect != "" {
			lockedEffects = append(lockedEffects, effect)
		}
	}
	return unlocked, lockedEffects
}

func effectCostMerge(locks effectCostLocks, effects [maxSlot]string) [maxSlot]string {
	for i, effect := range locks.Effects {
		if locks.Mask&(1<<i) != 0 {
			effects[i] = effect
		}
	}
	return effects
}
