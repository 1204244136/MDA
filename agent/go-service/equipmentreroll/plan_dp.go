package equipmentreroll

import (
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/rs/zerolog/log"
)

// costUnreachable 是"目标不可达"的哨兵成本值。
//
// 期望成本模型里，任何无法通过效果变更达成的状态都返回这个值，例如：禁止词条被永久
// 锁死、需求词条被锁在了槽位限定不允许的槽、剩余空槽数少于缺口。它是哨兵而不是真实
// 成本——调用方拿到 >= costUnreachable 的结果必须当成"这个目标做不到"来处理并结束任务，
// 而不是当成"很贵但可以继续洗"，否则会把用户的材料全部消耗在不可能达成的目标上。
const costUnreachable = 1e9

// 全局 memo 缓存：同一 (装备快照, 配额, 全局计数, 锁定位) 的 DP 结果只算一次。
var (
	dpCacheMu sync.Mutex
	dpCache   = make(map[string]float64)
)

// lockStrategyCache 缓存“当前装备状态 → [最优锁定位, 期望模块消耗]”的策略表。
// 命中常用配额时直接查表；未命中时计算后也写入缓存。
var lockStrategyCache sync.Map // key: string -> [2]float64{slot, cost}

// choosePartCache 缓存全局 1 步前瞻的结果：四件快照 + 配额 → 最优部位。
var choosePartCache sync.Map // key: string -> string

// clearDPCaches 清空 DP / 策略 / 前瞻缓存，在任务开始和结束时调用，避免跨任务累积。
func clearDPCaches() {
	dpCacheMu.Lock()
	dpCache = make(map[string]float64)
	dpCacheMu.Unlock()
	lockStrategyCache = sync.Map{}
	choosePartCache = sync.Map{}
}

func mapToCacheKey(m map[string]int) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(strconv.Itoa(m[k]))
		b.WriteByte(';')
	}
	return b.String()
}

// strSliceKey 把字符串切片排序后拼成 key，用于 required/forbidden 集合的缓存键。
func strSliceKey(list []string) string {
	if len(list) == 0 {
		return ""
	}
	cp := make([]string, len(list))
	copy(cp, list)
	sort.Strings(cp)
	return strings.Join(cp, "\x01")
}

// dpCacheKeyCore 以“显式 required/forbidden 集合 + 槽位限定”为缓存键
// （DP 只依赖这些，不依赖 counts）。slotAllow 为空时表示不限定槽位。
func dpCacheKeyCore(scan partScan, quota map[string]int, required, forbidden []string, lockSlot int, slotAllow map[string]map[int]bool) string {
	var b strings.Builder
	for i := 0; i < maxSlot; i++ {
		b.WriteString(scan.Slots[i].Effect)
		b.WriteByte('|')
		b.WriteString(strconv.Itoa(int(scan.Slots[i].Lock)))
		b.WriteByte(';')
	}
	b.WriteString(mapToCacheKey(quota))
	b.WriteByte('#')
	b.WriteString(strSliceKey(required))
	b.WriteByte('#')
	b.WriteString(strSliceKey(forbidden))
	b.WriteByte('#')
	b.WriteString(strconv.Itoa(lockSlot))
	if len(slotAllow) > 0 {
		names := make([]string, 0, len(slotAllow))
		for e := range slotAllow {
			names = append(names, e)
		}
		sort.Strings(names)
		b.WriteByte('#')
		for _, e := range names {
			b.WriteString(e)
			b.WriteByte('=')
			slots := make([]int, 0, len(slotAllow[e]))
			for s := range slotAllow[e] {
				slots = append(slots, s)
			}
			sort.Ints(slots)
			for _, s := range slots {
				b.WriteString(strconv.Itoa(s))
				b.WriteByte(',')
			}
			b.WriteByte(';')
		}
	}
	return b.String()
}

func partsCacheKey(parts map[string]partScan, quota map[string]int) string {
	var b strings.Builder
	for _, part := range equipmentParts {
		scan, ok := parts[part]
		if !ok {
			continue
		}
		b.WriteString(part)
		b.WriteByte('=')
		for i := 0; i < maxSlot; i++ {
			b.WriteString(scan.Slots[i].Effect)
			b.WriteByte('|')
			b.WriteString(strconv.Itoa(int(scan.Slots[i].Lock)))
			b.WriteByte(';')
		}
	}
	b.WriteByte('#')
	b.WriteString(mapToCacheKey(quota))
	return b.String()
}

// 本文件实现“单件装备短视 DP”：给定当前装备、全局配额与剩余配额计数，
// 估算锁定某个槽位后完成该装备局部贡献的期望订制模块消耗。
//
// 文档索引：
//   - 策略/锁定/全局前瞻：docs/zh_cn/nikke/EquipmentReroll/洗词条策略与Agent逻辑.md
//   - 概率模型：docs/zh_cn/nikke/EquipmentReroll/洗词条概率与期望计算.md
//   - 游戏内原始数据：docs/zh_cn/nikke/EquipmentReroll/装备系统与洗词条研究.md
//
// 概率模型来自洗词条概率与期望计算.md：
//   - 1/2/3 号槽获得效果概率分别为 100% / 50% / 30%；
//   - 效果权重见 effectWeights；
//   - 同一结果内已抽到的效果会从后续抽取池中排除（未锁定旧效果不参与排除，可回落本格）；
//   - **已锁定的效果占用该装备名额，必须计入排除池**——同一件装备不可能出现两条相同效果，
//     锁定即占位，其余槽位不得再刷出同一种（见 enumerateSlotOutcomes 的 lockedEffects）。
//
// 为控制状态空间，DP 只区分：空槽、每个“仍需要的配额效果”、以及“其他效果”。
// 非配额/超配额效果统一合并为 other，因为它们不参与完成判定。

var effectWeights = map[string]float64{
	"优越代码伤害增加": 0.10,
	"命中率增加":    0.12,
	"最大装弹数增加":  0.12,
	"攻击力增加":    0.10,
	"蓄力伤害增加":   0.12,
	"蓄力速度增加":   0.12,
	"暴击率增加":    0.12,
	"暴击伤害增加":   0.10,
	"防御力增加":    0.10,
}

// sortedEffectNames 效果名的稳定排序，用于枚举/求和时避免 map 遍历顺序不定导致的浮点非确定。
var sortedEffectNames = sortedEffectKeys(effectWeights)

func sortedEffectKeys(m map[string]float64) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

var slotObtainProbs = [maxSlot]float64{1.0, 0.5, 0.3}

const otherEffectLabel = "\x00other"

// forbiddenLabelPrefix 压缩状态中禁止词条的标签前缀。
const forbiddenLabelPrefix = "\x00forbid:"

// slotOutcome 表示一次效果变更中未锁定槽位的完整结果。
type slotOutcome struct {
	effects [maxSlot]string
	prob    float64
}

// enumerateSlotOutcomes 枚举未锁定槽位的所有完整结果及其概率。
// 抽取顺序按 1→2→3，同一结果内已抽到的效果会从后续抽取池中排除。
// lockedEffects 是该件当前锁定（占用名额、不参与重抽）槽位所持效果，一并计入排除池：
// 同一件装备不可能出现两条相同效果，锁定的效果已在装备上，其余槽位不得再刷出同一种。
func enumerateSlotOutcomes(unlocked []int, lockedEffects []string) []slotOutcome {
	results := []slotOutcome{{effects: [maxSlot]string{}, prob: 1.0}}
	for _, slot := range unlocked {
		var next []slotOutcome
		for _, res := range results {
			empty := res
			empty.prob *= 1 - slotObtainProbs[slot]
			next = append(next, empty)

			drawn := make(map[string]bool, len(res.effects)+len(lockedEffects))
			for _, effect := range lockedEffects {
				drawn[effect] = true
			}
			for _, effect := range res.effects {
				if effect != "" {
					drawn[effect] = true
				}
			}
			totalWeight := 0.0
			for _, effect := range sortedEffectNames {
				if !drawn[effect] {
					totalWeight += effectWeights[effect]
				}
			}
			for _, effect := range sortedEffectNames {
				if drawn[effect] {
					continue
				}
				r := res
				r.effects[slot] = effect
				r.prob *= slotObtainProbs[slot] * (effectWeights[effect] / totalWeight)
				next = append(next, r)
			}
		}
		results = next
	}
	return results
}

// forbiddenEffects 返回被禁止（-1）的效果列表。
func forbiddenEffects(quota map[string]int) []string {
	var result []string
	for effect, count := range quota {
		if count == -1 {
			result = append(result, effect)
		}
	}
	return result
}

func requiredSet(required []string) map[string]bool {
	set := make(map[string]bool, len(required))
	for _, effect := range required {
		set[effect] = true
	}
	return set
}

func forbiddenSet(forbidden []string) map[string]bool {
	set := make(map[string]bool, len(forbidden))
	for _, effect := range forbidden {
		set[effect] = true
	}
	return set
}

func forbiddenLabel(effect string) string {
	return forbiddenLabelPrefix + effect
}

// compressEffectSlot 槽位感知的效果压缩：限定槽位（slotAllow）时，
// 需求效果出现在“不允许的槽位”会压缩为 other（不满足完成判定）。
func compressEffectSlot(effect string, slot int, required, forbidden map[string]bool, slotAllow map[string]map[int]bool) string {
	if effect == "" {
		return ""
	}
	if forbidden[effect] {
		return forbiddenLabel(effect)
	}
	if required[effect] {
		if allow := slotAllow[effect]; len(allow) == 0 || allow[slot] {
			return effect
		}
		return otherEffectLabel
	}
	return otherEffectLabel
}

// compressedOutcome 是完整结果在压缩状态空间下的投影。
type compressedOutcome struct {
	values []string // 与 unlocked 顺序一致
	prob   float64
}

// enumerateCompressedOutcomes 枚举未锁定槽位的压缩结果，并按压缩向量聚合概率。
// lockedEffects 为该件锁定槽位所持效果，一并计入排除池（见 enumerateSlotOutcomes）。
// slotAllow 非空时按槽位压缩（需求效果只允许出现在限定槽位，其余槽位按 other 处理）。
func enumerateCompressedOutcomes(unlocked []int, required, forbidden map[string]bool, lockedEffects []string, slotAllow map[string]map[int]bool) []compressedOutcome {
	raw := enumerateSlotOutcomes(unlocked, lockedEffects)
	aggregated := make(map[string]*compressedOutcome)
	for _, outcome := range raw {
		values := make([]string, len(unlocked))
		for i, slot := range unlocked {
			values[i] = compressEffectSlot(outcome.effects[slot], slot, required, forbidden, slotAllow)
		}
		key := strings.Join(values, "\x00")
		if existing, ok := aggregated[key]; ok {
			existing.prob += outcome.prob
		} else {
			aggregated[key] = &compressedOutcome{values: values, prob: outcome.prob}
		}
	}
	result := make([]compressedOutcome, 0, len(aggregated))
	for _, outcome := range aggregated {
		result = append(result, *outcome)
	}
	return result
}

func quotaStateKey(effects [maxSlot]string) string {
	return strings.Join(effects[:], "\x00")
}

// expectedModulesForPartAllocated 用“分配感知”的 required 集合计算单件期望成本（宏观分配口径）。
// required 应由 allocateQuotaRequired 给出，避免稀缺配额（配额<件数）被每件都要求。
func expectedModulesForPartAllocated(scan partScan, quota map[string]int, required []string, lockSlot int) float64 {
	quota = normalizeQuota(quota)
	forbidden := forbiddenEffects(quota)
	return expectedModulesForPartCore(scan, quota, required, forbidden, lockSlot, nil)
}

// expectedModulesForPartCore 单件在给定 required/forbidden、锁定位与槽位限定下的期望剩余模块成本。
// slotAllow：effect -> 允许出现的槽位集合（0 基下标）；为 nil 表示任意槽位（角色模式口径）。
// 返回 costUnreachable 表示当前目标不可达，调用方必须当成“做不到”处理而不是“很贵”。
func expectedModulesForPartCore(scan partScan, quota map[string]int, required, forbidden []string, lockSlot int, slotAllow map[string]map[int]bool) float64 {
	quota = normalizeQuota(quota)
	cacheKey := dpCacheKeyCore(scan, quota, required, forbidden, lockSlot, slotAllow)
	dpCacheMu.Lock()
	if cached, ok := dpCache[cacheKey]; ok {
		dpCacheMu.Unlock()
		return cached
	}
	dpCacheMu.Unlock()

	if len(required) == 0 && len(forbidden) == 0 {
		return 0
	}
	requiredSetMap := requiredSet(required)
	forbiddenSetMap := forbiddenSet(forbidden)

	locked := [maxSlot]bool{}
	for i, slot := range scan.Slots {
		if slot.Lock != LockNone {
			locked[i] = true
		}
	}
	if lockSlot != 0 && !locked[lockSlot-1] {
		locked[lockSlot-1] = true
	}
	activeLocks := countLocks(scan)
	if lockSlot != 0 && scan.Slots[lockSlot-1].Lock == LockNone {
		activeLocks++
	}

	unlocked := make([]int, 0, maxSlot)
	for i := 0; i < maxSlot; i++ {
		if !locked[i] {
			unlocked = append(unlocked, i)
		}
	}
	for i, slot := range scan.Slots {
		if locked[i] && slot.Effect != "" && forbiddenSetMap[slot.Effect] {
			// 禁止词条一旦锁定便无法通过重洗移除，当前目标不可达。
			return costUnreachable
		}
		// 槽位限定：需求效果被锁定在“不允许的槽位”时无法移动到限定槽，目标不可达。
		if locked[i] && slot.Effect != "" && requiredSetMap[slot.Effect] {
			if allow := slotAllow[slot.Effect]; len(allow) > 0 && !allow[i] {
				return costUnreachable
			}
		}
	}
	missingRequired := 0
	for effect := range requiredSetMap {
		if partHasEffect(scan, effect) {
			// 已有该效果，但需确认其落在允许槽位（锁定/未锁定均可判定，见下文完成判定）。
			ok := false
			for i, e := range scan.Slots {
				if e.Effect == effect && (len(slotAllow[effect]) == 0 || slotAllow[effect][i]) {
					ok = true
					break
				}
			}
			if ok {
				continue
			}
		}
		if effectWeights[effect] <= 0 {
			// 未知效果不可能由效果变更产生。
			return costUnreachable
		}
		missingRequired++
	}
	if missingRequired > len(unlocked) {
		// 每个槽位不能重复同一效果，剩余空槽不足以补齐缺口。
		return costUnreachable
	}
	if len(unlocked) == 0 {
		if partHasRequiredAndNoForbiddenSlot(scan.Effects(), requiredSetMap, forbiddenSetMap, slotAllow) {
			return 0
		}
		return costUnreachable
	}

	// 枚举压缩状态：空槽、每个必需效果、每个禁止效果、other。
	values := make([]string, 0, len(required)+len(forbidden)+2)
	values = append(values, "")
	values = append(values, required...)
	for _, effect := range forbidden {
		values = append(values, forbiddenLabel(effect))
	}
	values = append(values, otherEffectLabel)

	var states [][maxSlot]string
	var rec func(idx int, cur [maxSlot]string)
	rec = func(idx int, cur [maxSlot]string) {
		if idx == maxSlot {
			states = append(states, cur)
			return
		}
		if locked[idx] {
			cur[idx] = compressEffectSlot(scan.Slots[idx].Effect, idx, requiredSetMap, forbiddenSetMap, slotAllow)
			rec(idx+1, cur)
			return
		}
		for _, value := range values {
			cur[idx] = value
			rec(idx+1, cur)
		}
	}
	rec(0, [maxSlot]string{})

	partHasRequiredCompressed := func(state [maxSlot]string) bool {
		for _, effect := range required {
			found := false
			allow := slotAllow[effect]
			for i, value := range state {
				if value != effect {
					continue
				}
				if len(allow) == 0 || allow[i] {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
		for _, effect := range forbidden {
			label := forbiddenLabel(effect)
			for _, value := range state {
				if value == label {
					return false
				}
			}
		}
		return true
	}

	vals := make(map[string]float64, len(states))
	for _, state := range states {
		key := quotaStateKey(state)
		if partHasRequiredCompressed(state) {
			vals[key] = 0
		} else {
			vals[key] = costUnreachable
		}
	}

	// 锁定槽位所持效果计入排除池（同件不重复效果，锁定即占用名额）。
	lockedEffects := make([]string, 0, len(locked))
	for i := 0; i < maxSlot; i++ {
		if locked[i] && scan.Slots[i].Effect != "" {
			lockedEffects = append(lockedEffects, scan.Slots[i].Effect)
		}
	}
	outcomes := enumerateCompressedOutcomes(unlocked, requiredSetMap, forbiddenSetMap, lockedEffects, slotAllow)
	rerollCost := float64(RerollModuleCost(activeLocks))

	// 用线性方程组精确求解期望成本，替代价值迭代。
	incomplete := make([][maxSlot]string, 0)
	incompleteIndex := make(map[string]int)
	for _, state := range states {
		key := quotaStateKey(state)
		if vals[key] != 0 {
			incompleteIndex[key] = len(incomplete)
			incomplete = append(incomplete, state)
		}
	}
	n := len(incomplete)
	if n > 0 {
		a := make([][]float64, n)
		b := make([]float64, n)
		for i, state := range incomplete {
			a[i] = make([]float64, n)
			a[i][i] = 1
			b[i] = rerollCost
			for _, outcome := range outcomes {
				next := state
				for j, slot := range unlocked {
					next[slot] = outcome.values[j]
				}
				if idx, ok := incompleteIndex[quotaStateKey(next)]; ok {
					a[i][idx] -= outcome.prob
				}
				// 完成态 V=0，不贡献
			}
		}
		x, solvable := solveLinearSystem(a, b)
		if !solvable {
			// 不可达状态会产生奇异矩阵；不要把未求出的增广列值当成有效成本。
			return costUnreachable
		}
		for i, state := range incomplete {
			vals[quotaStateKey(state)] = x[i]
		}
	}

	// 用压缩后的当前状态查表：vals 的 key 是压缩状态（空/必需/禁止/other），
	// 若直接用原始词条名（如“命中率增加”压缩为 other）查询，key 不匹配会取到
	// map 零值 0，导致 DP 误判“当前装备已完成、期望成本为 0”，锁定决策失效。
	compressedCurrent := [maxSlot]string{}
	for i := 0; i < maxSlot; i++ {
		compressedCurrent[i] = compressEffectSlot(scan.Slots[i].Effect, i, requiredSetMap, forbiddenSetMap, slotAllow)
	}
	currentKey := quotaStateKey(compressedCurrent)
	result := vals[currentKey]

	// 验证映射正确性：如果结果为零但状态未完成，压缩键可能有误（不应该发生，但防止逻辑错误）。
	if result == 0 && !partHasRequiredCompressed(compressedCurrent) {
		log.Warn().
			Str("component", "EquipmentReroll").
			Strs("compressed_state", compressedCurrent[:]).
			Str("cache_key", currentKey).
			Msg("DP compressed state lookup returned zero for incomplete state; potential mapping error")
	}

	dpCacheMu.Lock()
	dpCache[cacheKey] = result
	dpCacheMu.Unlock()
	return result
}

// solveLinearSystem 用高斯消元（列主元）求解 Ax=b。
// 返回解向量和求解是否成功的标志。
func solveLinearSystem(a [][]float64, b []float64) ([]float64, bool) {
	n := len(b)
	if n == 0 {
		return nil, true
	}
	// 保存原始系数矩阵副本：消元会原地把 a 改为单位阵，残差验证需要原始 A。
	orig := make([][]float64, n)
	for i := 0; i < n; i++ {
		orig[i] = make([]float64, n)
		copy(orig[i], a[i])
	}
	// 增广矩阵
	for i := 0; i < n; i++ {
		a[i] = append(a[i], b[i])
	}

	const epsilon = 1e-12

	// 高斯消元（列主元）
	for col := 0; col < n; col++ {
		// 选择列主元
		pivot := col
		for r := col + 1; r < n; r++ {
			if absFloat(a[r][col]) > absFloat(a[pivot][col]) {
				pivot = r
			}
		}
		if pivot != col {
			a[col], a[pivot] = a[pivot], a[col]
		}

		div := a[col][col]
		if absFloat(div) < epsilon {
			// 主元接近零，矩阵奇异或数值不稳定
			log.Warn().
				Str("component", "EquipmentReroll").
				Int("pivot_col", col).
				Float64("pivot_value", div).
				Msg("DP linear system: pivot too small, matrix may be singular")
			return nil, false
		}

		// 归一化主元行
		for j := col; j <= n; j++ {
			a[col][j] /= div
		}

		// 消元
		for r := 0; r < n; r++ {
			if r == col {
				continue
			}
			factor := a[r][col]
			if absFloat(factor) < epsilon {
				continue
			}
			for j := col; j <= n; j++ {
				a[r][j] -= factor * a[col][j]
			}
		}
	}

	// 提取解
	x := make([]float64, n)
	for i := 0; i < n; i++ {
		x[i] = a[i][n]
	}

	// 验证解的合理性（残差检查，使用消元前的原始系数矩阵）
	if !verifySolutionResidual(orig, b, x, n) {
		log.Warn().
			Str("component", "EquipmentReroll").
			Msg("DP linear system: solution residual too large, numerical instability detected")
		return nil, false
	}

	return x, true
}

// verifySolutionResidual 计算最大残差 max|Ax - b| 验证解是否满足原方程，
// 同时检查解向量的合理性（期望成本不应为负或超过 costUnreachable）。
func verifySolutionResidual(a [][]float64, b, x []float64, n int) bool {
	const maxResidualNorm = 1e-6
	maxResidual := 0.0

	for i := 0; i < n; i++ {
		// 用原始系数矩阵重算 Ax，与常数项 b 对比得残差
		sum := 0.0
		for j := 0; j < n; j++ {
			sum += a[i][j] * x[j]
		}
		residual := absFloat(sum - b[i])
		if residual > maxResidual {
			maxResidual = residual
		}
		// 解的合理性检查
		if x[i] < -1e-6 || x[i] > costUnreachable {
			log.Warn().
				Str("component", "EquipmentReroll").
				Int("row", i).
				Float64("x_value", x[i]).
				Msg("DP solution contains unreasonable value")
			return false
		}
	}

	return maxResidual < maxResidualNorm
}

func absFloat(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// lockAcquireCost 返回新增一把"永久蓝锁(订制模块)"的获取成本：0→1=2、1→2=3 模块。
// 自订密钥是一次性橙锁，只消耗密钥、不占订制模块，因此折算的模块获取成本为 0。
func lockAcquireCost(activeLocks int) int {
	if activeLocks <= 0 {
		return 2
	}
	return 3
}

// partHasEffect 判断单件是否已持有某效果（任一槽位）。
func partHasEffect(scan partScan, effect string) bool {
	for _, slot := range scan.Slots {
		if slot.Effect == effect {
			return true
		}
	}
	return false
}

// slotIsPositiveQuota 判断某效果是否为全局正数配额效果。
func slotIsPositiveQuota(effect string, quota map[string]int) bool {
	return effect != "" && quota[effect] > 0
}

// pieceCarrierRole 判断某件在当前状态下的“角色”（对任意正数配额都成立）：
//   - “carrier”（承载者）：slot3 命中正数配额效果（该件把难出的 3 号槽填了配额，是承载者候选）；
//   - “demoted”（降格者）：slot3 未命中 且 slot2 持有**满配额效果**（quota[e] >= 件数，
//     如 优/攻——该件把“公共配额”放在可锁的 2 号槽，是好的 优/攻 双词条件，作为承载者候选被搁置）；
//   - “undecided”（未定）：其余（既不是承载者也不是降格者，还可“边刷边找”成为承载者）。
func pieceCarrierRole(scan partScan, quota map[string]int) string {
	s3 := scan.Slots[2].Effect
	s2 := scan.Slots[1].Effect
	if slotIsPositiveQuota(s3, quota) {
		return "carrier"
	}
	if quota[s2] >= len(equipmentParts) {
		return "demoted"
	}
	return "undecided"
}

// hasNaturalCarrier 判断当前是否已存在“slot3 命中正数配额”的天然承载者。
func hasNaturalCarrier(parts map[string]partScan, quota map[string]int) bool {
	for _, p := range equipmentParts {
		if scan, ok := parts[p]; ok && pieceCarrierRole(scan, quota) == "carrier" {
			return true
		}
	}
	return false
}

// partHasAllAssigned 判断某件是否已持有其 required（assigned）集合中的全部效果。
// 已完整的件不应再被洗（洗了只会破坏已有成果），调度时应跳过。
func partHasAllAssigned(scan partScan, assigned []string) bool {
	for _, e := range assigned {
		if !partHasEffect(scan, e) {
			return false
		}
	}
	return true
}

// partCompleteForQuota 判断某件是否已经完成其分配目标且不含禁止词条。
// 禁止词条必须继续触发重洗，即使该件已经持有全部 assigned 效果。
func partCompleteForQuota(scan partScan, assigned []string, quota map[string]int) bool {
	return partHasAllAssigned(scan, assigned) && !partHasForbiddenEffect(scan, quota)
}

// effectWeight 返回效果权重（效果度缺失/异常时按 0.10 兜底）。
func effectWeight(e string) float64 {
	w := effectWeights[e]
	if w <= 0 {
		w = 0.10
	}
	return w
}

// washPriority 估算“该件需要被洗的迫切程度”（越高越优先洗）。
// 由三部分组成（位置 + 数量 + 概率权重补正）：
//  1. 缺口分：本件负责（assigned）但尚未持有的效果，按 1/权重 加权（权重越低越难出 → 越迫切）；
//  2. 1号槽风险分：已持有但落在 1 号槽（策略上不锁、易被洗掉）的本件负责效果 +0.5；
//  3. 废槽分：占槽但非本件 assigned 的效果（非配额 / 未负责配额 / 其它）+0.3。
//
// 物理有效词条越多、位置越安全 → 分越低 → 越不该洗。
func washPriority(scan partScan, quota map[string]int, assigned []string) float64 {
	reqSet := requiredSet(assigned)
	score := 0.0
	// 缺口：缺失的 required 效果，按难度（1/权重）加权。
	for _, e := range assigned {
		if !partHasEffect(scan, e) {
			score += 1.0 / effectWeight(e)
		}
	}
	// 1号槽风险：已持有但落在 1 号槽（不可锁保护）的本件负责效果。
	if slotIsPositiveQuota(scan.Slots[0].Effect, quota) && reqSet[scan.Slots[0].Effect] {
		score += 0.5
	}
	// 废槽分：占槽但非本件 required 的效果。
	for _, slot := range scan.Slots {
		if slot.Effect == "" {
			continue
		}
		if !reqSet[slot.Effect] {
			score += 0.3
		}
	}
	return score
}

// pickCheapestCarriersForEffect 对稀缺效果 e，按“补齐 required（assigned + e）期望成本最低”
// 选出 need 个承载者。这是**显式的交换/冒泡判定**：每步比较各件作为承载者的期望成本，
// 成本更低者晋升为承载者、原承载者退行。角色（slot3 命中与否）只是观察——通常 slot3 命中者
// 成本更低，但若某件更接近完整（如已有 优+攻 只差装弹），其承载成本可能更低，就会与之交换。
// used 记录每件已承载稀缺数；used[p] >= capacity 的件不再承载（超出 3 槽）。
func pickCheapestCarriersForEffect(parts map[string]partScan, quota map[string]int, assigned map[string][]string, e string, need int, used map[string]int, capacity int) []string {
	type cand struct {
		part string
		cost float64
	}
	var cs []cand
	for _, p := range equipmentParts {
		if used[p] >= capacity {
			continue
		}
		scan, ok := parts[p]
		if !ok {
			continue
		}
		required := append(append([]string{}, assigned[p]...), e)
		cs = append(cs, cand{p, partExpectedCostForRequired(scan, quota, required, "")})
	}
	// 主排序：补齐 required 的期望成本（越低越接近承载者）。
	// 次排序：washPriority 越小 = 物理上更接近承载者（缺口少、位置安全、权重好出），
	// 避免在 DP 成本并列时把“已完成 优/攻 双词条件”的件随机晋升为稀缺承载者。
	sort.Slice(cs, func(i, j int) bool {
		pi, ok := parts[cs[i].part]
		if !ok {
			return false
		}
		pj, ok := parts[cs[j].part]
		if !ok {
			return false
		}
		ri := append(append([]string{}, assigned[cs[i].part]...), e)
		rj := append(append([]string{}, assigned[cs[j].part]...), e)
		if cs[i].cost != cs[j].cost {
			return cs[i].cost < cs[j].cost
		}
		wi := washPriority(pi, quota, ri)
		wj := washPriority(pj, quota, rj)
		if wi != wj {
			return wi < wj // 更接近承载者(更小)优先
		}
		return cs[i].part < cs[j].part
	})
	var carriers []string
	for i := 0; i < need && i < len(cs); i++ {
		carriers = append(carriers, cs[i].part)
	}
	return carriers
}

// allocateQuotaRequired 把全局正数配额“持有名额”分配到各件，返回每件的 required 配额效果集合。
//
// 分配口径为“先按 12 词条、slot3 命中者晋升为承载者、其余退行”，并泛化到任意配额：
//   - 满配额效果（quota[e] >= 件数，如 优4/攻4）分给全部件，占用该件槽位；
//   - **每件承载稀缺效果的容量 = maxSlot − 满配额数**（满配额占槽，剩余槽可承载多个稀缺）；
//   - 稀缺配额效果（quota[e] < 件数）按优先级选承载者：
//     1. 优先选 **slot3 已命中 优/攻/装弹** 的件（继承了“slot3先中谁谁先当”的涌现规则）；
//     2. 多件命中时，按“补齐该件 required 的期望成本最低”选择（槽位获得概率/效果权重）；
//     3. 无命中时，在“未定”件（slot3 未命中且 slot2 非 攻/优）中选成本最低者；
//     4. 再无未定件（全部是降格者：slot2=攻/优 且 slot3 未命中）时，被迫在降格者中强选成本最低者；
//     5. 每件累计承载稀缺数不得超过容量（否则会超出 3 槽）。
//   - 承载者之外的件退行为 {优,攻}（仅满配额效果，不额外承担稀缺效果）。
//
// 关键：稀缺配额只让需要的 quota[e] 件承担；承载者判定依赖 slot2/slot3 当前状态，随每步重算而“流转”。
func allocateQuotaRequired(parts map[string]partScan, quota map[string]int) map[string][]string {
	quota = normalizeQuota(quota)
	assigned := make(map[string][]string, len(equipmentParts))
	for _, p := range equipmentParts {
		assigned[p] = []string{}
	}

	var posEffects []string
	for e, c := range quota {
		if c > 0 {
			posEffects = append(posEffects, e)
		}
	}
	sort.Strings(posEffects)

	// 先分配“满配额”效果（所需数量 >= 件数 → 全部件承担），并统计满配额占用的槽数。
	fullCount := 0
	for _, e := range posEffects {
		if quota[e] >= len(equipmentParts) {
			for _, p := range equipmentParts {
				assigned[p] = append(assigned[p], e)
			}
			fullCount++
		}
	}
	// 每件承载稀缺效果的容量 = maxSlot - 满配额数（满配额占槽，剩余槽留给稀缺）。
	capacity := maxSlot - fullCount
	used := make(map[string]int, len(equipmentParts))

	// 稀缺配额（quota[e] < 件数）：用“显式交换判定” pickCheapestCarriersForEffect 按期望成本
	// 选择承载者（成本更低者晋升、原承载者退行/交换）。每件承载稀缺数 ≤ capacity。
	for _, e := range posEffects {
		need := quota[e]
		if need >= len(equipmentParts) {
			continue
		}
		carriers := pickCheapestCarriersForEffect(parts, quota, assigned, e, need, used, capacity)
		for _, c := range carriers {
			assigned[c] = append(assigned[c], e)
			used[c]++
		}
	}
	return assigned
}

// bestLockSlotAndCostForRequired 用“显式 required 集合”计算最优锁定位与期望成本（角色模式，不限槽位）。
// 只有在“该件负责某配额效果”且该槽持有它时才考虑锁定；材料获取成本按材质计入。
// material 为 “自订密钥”（获取成本 0）或 “订制模块”（计入获取成本）。
func bestLockSlotAndCostForRequired(scan partScan, quota map[string]int, required []string, material string) (int, float64) {
	return bestLockSlotAndCostCore(scan, quota, required, forbiddenEffects(quota), material, nil)
}

// bestLockSlotAndCostForTarget 槽位感知版本：给定 required/forbidden 与 slotAllow，计算最优锁定位与期望成本。
func bestLockSlotAndCostForTarget(scan partScan, quota map[string]int, required, forbidden []string, material string, slotAllow map[string]map[int]bool) (int, float64) {
	return bestLockSlotAndCostCore(scan, quota, required, forbidden, material, slotAllow)
}

func bestLockSlotAndCostCore(scan partScan, quota map[string]int, required, forbidden []string, material string, slotAllow map[string]map[int]bool) (int, float64) {
	quota = normalizeQuota(quota)
	lockMat := lockMaterialOrDefault(material)
	cacheKey := "lock2:" + dpCacheKeyCore(scan, quota, required, forbidden, 0, slotAllow) + "#" + lockMat
	if v, ok := lockStrategyCache.Load(cacheKey); ok {
		pair := v.([2]float64)
		return int(pair[0]), pair[1]
	}
	reqSet := requiredSet(required)
	bestSlot := 0
	bestCost := expectedModulesForPartCore(scan, quota, required, forbidden, 0, slotAllow)
	for _, slot := range []int{3, 2} {
		if scan.Slots[slot-1].Lock != LockNone {
			continue
		}
		effect := scan.Slots[slot-1].Effect
		if effect == "" || !reqSet[effect] {
			continue
		}
		if allow := slotAllow[effect]; len(allow) > 0 && !allow[slot-1] {
			continue
		}
		cost := expectedModulesForPartCore(scan, quota, required, forbidden, slot, slotAllow)
		if lockMat == "订制模块" {
			cost += float64(lockAcquireCost(countLocks(scan)))
		}
		if cost < bestCost {
			bestCost = cost
			bestSlot = slot
		}
	}
	lockStrategyCache.Store(cacheKey, [2]float64{float64(bestSlot), bestCost})
	return bestSlot, bestCost
}

// slotHoldsRequired 判断某可锁槽（2/3）是否已持有本件负责的配额效果。
func slotHoldsRequired(scan partScan, slot int, reqSet map[string]bool) bool {
	effect := scan.Slots[slot-1].Effect
	return effect != "" && reqSet[effect]
}

// slotHoldsRequiredTarget 槽位感知版本：槽位必须同时是需求效果允许出现的槽位。
func slotHoldsRequiredTarget(scan partScan, slot int, reqSet map[string]bool, slotAllow map[string]map[int]bool) bool {
	effect := scan.Slots[slot-1].Effect
	if effect == "" || !reqSet[effect] {
		return false
	}
	if allow := slotAllow[effect]; len(allow) > 0 && !allow[slot-1] {
		return false
	}
	return true
}

// lockStillWorthIt 校验“锁定某槽”在含获取成本（若用订制模组）下仍严格低于不锁（角色模式，不限槽位）。
// 自订密钥获取成本为 0，仅比较效果变更期望成本。
func lockStillWorthIt(scan partScan, quota map[string]int, required []string, slot int, material string) bool {
	return lockStillWorthItCore(scan, quota, required, forbiddenEffects(quota), slot, material, nil)
}

// lockStillWorthItCore 槽位感知版本。
func lockStillWorthItCore(scan partScan, quota map[string]int, required, forbidden []string, slot int, material string, slotAllow map[string]map[int]bool) bool {
	lockCost := expectedModulesForPartCore(scan, quota, required, forbidden, slot, slotAllow)
	noLockCost := expectedModulesForPartCore(scan, quota, required, forbidden, 0, slotAllow)
	if lockMaterialOrDefault(material) == "订制模块" {
		lockCost += float64(lockAcquireCost(countLocks(scan)))
	}
	return lockCost < noLockCost-1e-6
}

// desiredLockSlotBitterSweet 先苦后甜：饱和度配额（本件负责 >=3 种，每槽都需填配额，
// 如 优4/攻4/装弹4 → 每件 优1/攻1/装弹1）下，先刷最难得出的 3 号（当前未持本件负责效果
// 的最高难度槽），拿到才锁；不先锁已有效的低优先槽（角色模式，不限槽位）。
// 难度序 3→2（获得概率 30%/50%，越高越难得，故“先苦后甜”先做 3 号）。
// 返回 0 表示当前应便宜刷硬目标（不锁）；非 0 为建议锁定的槽位。
func desiredLockSlotBitterSweet(scan partScan, quota map[string]int, required []string, material string) int {
	return desiredLockSlotBitterSweetCore(scan, quota, required, forbiddenEffects(quota), material, nil)
}

// desiredLockSlotBitterSweetCore 槽位感知版本：需求效果仅在其允许槽位（slotAllow）算作持有。
func desiredLockSlotBitterSweetCore(scan partScan, quota map[string]int, required, forbidden []string, material string, slotAllow map[string]map[int]bool) int {
	reqSet := requiredSet(required)
	hardOrder := [2]int{3, 2}
	// 先苦：难度序中第一个“尚未在允许槽位持有本件负责效果”的槽 = 当前要刷的硬目标。
	targetIdx := -1
	for i, slot := range hardOrder {
		if !slotHoldsRequiredTarget(scan, slot, reqSet, slotAllow) {
			targetIdx = i
			break
		}
	}
	// 拿到就锁：先保护“优先于硬目标、已在允许槽位持有本件负责效果、且未锁定”的更高难度槽。
	if targetIdx >= 0 {
		for j := 0; j < targetIdx; j++ {
			slot := hardOrder[j]
			if scan.Slots[slot-1].Lock == LockNone && slotHoldsRequiredTarget(scan, slot, reqSet, slotAllow) {
				if lockStillWorthItCore(scan, quota, required, forbidden, slot, material, slotAllow) {
					return slot
				}
			}
		}
		return 0 // 无更高难度已持有槽可保 → 便宜刷硬目标
	}
	// 全部可锁槽都已在允许槽位持有本件负责效果 → 顺序锁最高难度未锁槽（拿到就锁）。
	for _, slot := range hardOrder {
		if scan.Slots[slot-1].Lock == LockNone && slotHoldsRequiredTarget(scan, slot, reqSet, slotAllow) {
			if lockStillWorthItCore(scan, quota, required, forbidden, slot, material, slotAllow) {
				return slot
			}
		}
	}
	return 0
}

// lockMaterialOrDefault 归一化锁定材料：空串视为“自订密钥”。
func lockMaterialOrDefault(material string) string {
	if strings.TrimSpace(material) == "" {
		return "自订密钥"
	}
	return strings.TrimSpace(material)
}

// positiveQuotaEffects 返回全局正数配额效果列表（排序，供降级/单件上下文用）。
func positiveQuotaEffects(quota map[string]int) []string {
	quota = normalizeQuota(quota)
	var pos []string
	for e, c := range quota {
		if c > 0 {
			pos = append(pos, e)
		}
	}
	sort.Strings(pos)
	return pos
}

// partExpectedCostForRequired 单件在给定 required 集合（含最优单锁）下的期望剩余模块成本。
func partExpectedCostForRequired(scan partScan, quota map[string]int, required []string, material string) float64 {
	_, cost := bestLockSlotAndCostForRequired(scan, quota, required, material)
	return cost
}

// expectedModulesForQuota 对全局状态计算"达到配额目标"的期望剩余订制模块数（方向 A 的核心量）。
// 采用“分配感知”口径：把全局正数配额名额分配到各件后，逐件计算其补齐“该件负责的效果”
// 的期望成本。相比旧口径（每件独立补齐其余件尚未满足的效果），该口径不会在稀缺配额
// （配额<件数）下让每件都去抢同一种效果，因此对 4/4/1、4/4/1/1 等不均匀配额更准确。
// 全局配额已满足时返回 0。
func expectedModulesForQuota(parts map[string]partScan, quota map[string]int) float64 {
	quota = normalizeQuota(quota)
	if AllPartsSatisfiedQuota(parts, quota) {
		return 0
	}
	assigned := allocateQuotaRequired(parts, quota)
	total := 0.0
	for _, part := range equipmentParts {
		scan, ok := parts[part]
		if !ok {
			continue
		}
		total += partExpectedCostForRequired(scan, quota, assigned[part], "")
	}
	return total
}

// chooseBestPartForQuota 使用 1 步全局前瞻选择"期望剩余成本降幅最大"的装备。
// 对每件装备枚举一次效果变更的所有压缩结果，用 expectedModulesForQuota 计算变更后的
// 全局期望成本，再按（当前期望成本 − 变更后期望成本）/ 本次效果变更模块成本 排序。
// 其余三件在结果间只随"本件新词条带来的配额计数变化"改变，required 集合不变时复用其基础成本，
// 避免每个结果都重算四件 DP。
//
// 注意：全局配额未满足时，即使单步期望增益为负也必须继续洗（策略文档 §4.1：
// “只要还有尚未满足的正数配额，就继续洗”）。因此这里只用增益排序选择“最优/最不差”
// 的部位，绝不因单步增益为负而返回“无部位”；仅当没有可动作部位（全部槽位锁定、
// 成本非法或快照缺失）时才返回空。
func chooseBestPartForQuota(parts map[string]partScan, quota map[string]int, order []string) (string, bool) {
	quota = normalizeQuota(quota)
	if AllPartsSatisfiedQuota(parts, quota) {
		return "", false
	}

	cacheKey := "choose:" + strings.Join(order, ",") + "#" + partsCacheKey(parts, quota)
	if v, ok := choosePartCache.Load(cacheKey); ok {
		part := v.(string)
		return part, part != ""
	}

	currentCost := expectedModulesForQuota(parts, quota)
	assigned := allocateQuotaRequired(parts, quota)

	// 调度优先级（先找承载者 → 洗未完整非承载者探索交换 → 补完整承载者 → 兜底未完整件）：
	//   - 无天然承载者（无 slot3 命中）→ 只洗“未完整非承载者”（降格者 + 未定者）；
	//     因为角色是阶梯（未定→降格→承载），降格者也通过洗 slot3 翻盘成承载者，所以找承载者阶段
	//     不应排除降格者——只要“未完整非承载者”都参与竞争承载者；
	//   - 已有天然承载者 → 不再强制“先补完整承载者”，而**先洗“未完整非承载者”**：
	//     洗它们有概率翻盘成更优承载者（期望收益超出当前承载者时与它“交换/冒泡”），或直接补好 优/攻；
	//   - 无未完整非承载者 → 再补完整承载者（其 1、2 槽还缺 优/装弹）；
	//   - 仍无 → 落到任意未完整件（兜底）。
	// 已完整（持有全部 assigned）的件不洗，避免破坏已有成果。
	priority := map[string]bool{}
	if !hasNaturalCarrier(parts, quota) {
		for _, p := range equipmentParts {
			if scan, ok := parts[p]; ok && pieceCarrierRole(scan, quota) != "carrier" && !partCompleteForQuota(scan, assigned[p], quota) {
				priority[p] = true
			}
		}
	} else {
		for _, p := range equipmentParts {
			if scan, ok := parts[p]; ok && pieceCarrierRole(scan, quota) != "carrier" && !partCompleteForQuota(scan, assigned[p], quota) {
				priority[p] = true
			}
		}
		if len(priority) == 0 {
			for _, p := range equipmentParts {
				if scan, ok := parts[p]; ok && pieceCarrierRole(scan, quota) == "carrier" && !partCompleteForQuota(scan, assigned[p], quota) {
					priority[p] = true
				}
			}
		}
	}
	if len(priority) == 0 {
		// 兜底：任意未完整件（避免无动作；已完整件仍不洗）。
		for _, p := range equipmentParts {
			if scan, ok := parts[p]; ok && !partCompleteForQuota(scan, assigned[p], quota) {
				priority[p] = true
			}
		}
	}

	bestPart := ""
	bestWp := math.Inf(-1)
	bestGainPerCost := math.Inf(-1)
	bestGain := math.Inf(-1)
	for _, part := range order {
		scan, ok := parts[part]
		if !ok {
			continue
		}
		if len(priority) > 0 && !priority[part] {
			continue
		}
		// 材料视为无限，不再按库存过滤可洗部位（用户材料不足不会执行任务）。
		// 顺位主排序：washPriority 越高越该洗（物理有效词条越少/位置越险/缺口越难 → 越迫切）。
		wp := washPriority(scan, quota, assigned[part])
		unlocked := make([]int, 0, maxSlot)
		lockedEffects := make([]string, 0, maxSlot)
		for i, slot := range scan.Slots {
			if slot.Lock == LockNone {
				unlocked = append(unlocked, i)
			} else if slot.Effect != "" {
				lockedEffects = append(lockedEffects, slot.Effect)
			}
		}
		if len(unlocked) == 0 {
			continue
		}

		// 本件负责的配额效果（分配感知）：outcomes 按压缩状态枚举（空/必需/禁止/other）。
		required := assigned[part]
		forbidden := forbiddenEffects(quota)
		outcomes := enumerateCompressedOutcomes(unlocked, requiredSet(required), forbiddenSet(forbidden), lockedEffects, nil)

		// 其余三件在当前分配下的基础成本与 required（结果间复用）。
		type qBase struct {
			required []string
			cost     float64
		}
		bases := make(map[string]qBase, len(equipmentParts))
		for _, q := range equipmentParts {
			if q == part {
				continue
			}
			qScan, okq := parts[q]
			if !okq {
				continue
			}
			_, c := bestLockSlotAndCostForRequired(qScan, quota, assigned[q], "")
			bases[q] = qBase{required: assigned[q], cost: c}
		}

		expectedCost := 0.0
		for _, out := range outcomes {
			newScan := scan
			for i, slot := range unlocked {
				// 压缩值还原为真实效果名：禁止词条取回原名，其余（必需/other/空）按原样写入。
				v := out.values[i]
				if strings.HasPrefix(v, forbiddenLabelPrefix) {
					v = strings.TrimPrefix(v, forbiddenLabelPrefix)
				}
				// 未获得效果时槽位应变为空，而不是保留旧效果
				newScan.Slots[slot].Effect = v
			}
			// 本件自身成本（分配感知）：required 集合不随本件结果变化（近似）。
			_, costP := bestLockSlotAndCostForRequired(newScan, quota, assigned[part], "")

			// 其余件成本：近似用当前分配的基础成本（不重算候选引起的分配变化，属可接受近似）。
			others := 0.0
			for _, q := range equipmentParts {
				if q == part {
					continue
				}
				others += bases[q].cost
			}

			expectedCost += out.prob * (costP + others)
		}

		cost := float64(RerollModuleCost(countLocks(scan)))
		if cost <= 0 {
			continue
		}
		gain := currentCost - expectedCost
		gainPerCost := gain / cost
		// 主排序：gainPerCost = 期望降本 / 每次洗的模块数（性价比，已把“洗一次所需模块数”计入）。
		// 次排序：gain（降本更大优先）；再按 washPriority（位置+数量+权重）兜底。
		if gainPerCost > bestGainPerCost ||
			(gainPerCost == bestGainPerCost && (gain > bestGain || (gain == bestGain && wp > bestWp))) {
			bestGainPerCost = gainPerCost
			bestGain = gain
			bestWp = wp
			bestPart = part
		}
	}
	choosePartCache.Store(cacheKey, bestPart)
	return bestPart, bestPart != ""
}

// partHasRequiredAndNoForbidden 判断当前三槽是否已包含所有需要保留/补齐的配额效果，
// 且不包含任何被禁止的效果。
func partHasRequiredAndNoForbidden(effects [maxSlot]string, required, forbidden map[string]bool) bool {
	return partHasRequiredAndNoForbiddenSlot(effects, required, forbidden, nil)
}

// partHasRequiredAndNoForbiddenSlot 槽位感知版本：需求效果必须落在其允许槽位（slotAllow）。
func partHasRequiredAndNoForbiddenSlot(effects [maxSlot]string, required, forbidden map[string]bool, slotAllow map[string]map[int]bool) bool {
	for effect := range required {
		if !effectInAllowedSlots(effects, effect, slotAllow[effect]) {
			return false
		}
	}
	for effect := range forbidden {
		if PartHasEffect(effects, effect) {
			return false
		}
	}
	return true
}

// effectInAllowedSlots 判断 effects 中 effect 是否出现在允许槽位（allow 为空表示任意槽位）。
func effectInAllowedSlots(effects [maxSlot]string, effect string, allow map[int]bool) bool {
	for i, e := range effects {
		if e == effect && (len(allow) == 0 || allow[i]) {
			return true
		}
	}
	return false
}
