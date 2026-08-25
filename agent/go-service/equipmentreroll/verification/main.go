// 说明：这是 EquipmentReroll 洗词条“宏观分配/禁止词条/验收口径”的独立蒙特卡洛验证程序。
// 它不参与运行时（独立 main 包），仅用于开发期验证与文档数据核对。
// 运行方式（在 agent/go-service 下）：
//
//	go run ./equipmentreroll/verification
package main

import (
	"fmt"
	"math/rand"
	"runtime"
	"sort"
	"sync"
	"time"
)

// =============================================================================
// 全局联合分配蒙特卡洛模拟：
//   4 件装备 × 3 槽，全局配额 Q（各效果要凑够多少条，-1 表示禁止词条）。
//   目标：全局各正数配额 count >= Q，且任何件都不得出现禁止词条。
//   策略“感知全局剩余需求”：稀缺配额（配额<件数）只由需要的件承担，其余件不追；
//   正数但超额的效果不激进清洗（避免洗到低于配额再反噬）；禁止词条优先洗且绝不锁。
//   锁定：永久蓝锁（订制模块），渐进式、类型互斥、先难后易；获取成本 0→1=2、1→2=3。
// 概率模型见 docs/zh_cn/nikke/EquipmentReroll/洗词条概率与期望计算.md；
// 同件不重复 = 已锁效果参与排除池。
// =============================================================================

const (
	idxYou    = 0
	idxHit    = 1
	idxAmmo   = 2
	idxAtk    = 3
	idxChgDmg = 4
	idxChgSpd = 5
	idxCrit   = 6
	idxCritDm = 7
	idxDef    = 8
	nEffects  = 9
	nPieces   = 4
)

var weights = [nEffects]float64{0.10, 0.12, 0.12, 0.10, 0.12, 0.12, 0.12, 0.10, 0.10}
var slotObtain = [3]float64{1.0, 0.5, 0.3}

// 可参与配额的四种效果（扩展测试用）
var quotaTypes = []int{idxYou, idxAtk, idxAmmo, idxChgSpd}

func isQuota(i int) bool { return i == idxYou || i == idxAtk || i == idxAmmo || i == idxChgSpd }

type piece struct {
	slots [3]int
	locks [3]bool
}

func newPiece(slots [3]int) piece {
	return piece{slots: slots, locks: [3]bool{}}
}

func globalCount(pieces [nPieces]piece, eff int) int {
	c := 0
	for _, p := range pieces {
		for _, s := range p.slots {
			if s == eff {
				c++
				break
			}
		}
	}
	return c
}

func need(Q map[int]int, pieces [nPieces]piece, eff int) int {
	d := Q[eff] - globalCount(pieces, eff)
	if d < 0 {
		return 0
	}
	return d
}

func forbiddenEffects(K map[int]int) map[int]bool {
	f := map[int]bool{}
	for eff, c := range K {
		if c == -1 {
			f[eff] = true
		}
	}
	return f
}

func pieceHasForbidden(p piece, forbidden map[int]bool) bool {
	for _, s := range p.slots {
		if s >= 0 && forbidden[s] {
			return true
		}
	}
	return false
}

func satisfied(K map[int]int, pieces [nPieces]piece) bool {
	for _, eff := range quotaTypes {
		if need(K, pieces, eff) > 0 {
			return false
		}
	}
	for _, p := range pieces {
		if pieceHasForbidden(p, forbiddenEffects(K)) {
			return false
		}
	}
	return true
}

func pieceHas(p piece, eff int) bool {
	for _, s := range p.slots {
		if s == eff {
			return true
		}
	}
	return false
}

func drawEffect(drawn *[nEffects]bool, rng *rand.Rand) int {
	total := 0.0
	for i := 0; i < nEffects; i++ {
		if !drawn[i] {
			total += weights[i]
		}
	}
	if total <= 0 {
		return -1
	}
	r := rng.Float64() * total
	for i := 0; i < nEffects; i++ {
		if !drawn[i] {
			r -= weights[i]
			if r <= 0 {
				return i
			}
		}
	}
	for i := 0; i < nEffects; i++ {
		if !drawn[i] {
			return i
		}
	}
	return -1
}

func rerollPiece(p piece, rng *rand.Rand) piece {
	var drawn [nEffects]bool
	for i := 0; i < 3; i++ {
		if p.locks[i] && p.slots[i] >= 0 {
			drawn[p.slots[i]] = true
		}
	}
	for i := 0; i < 3; i++ {
		if p.locks[i] {
			continue
		}
		if rng.Float64() <= slotObtain[i] {
			e := drawEffect(&drawn, rng)
			p.slots[i] = e
			if e >= 0 {
				drawn[e] = true
			}
		} else {
			p.slots[i] = -1
		}
	}
	return p
}

func countLocks(p piece) int {
	n := 0
	for _, l := range p.locks {
		if l {
			n++
		}
	}
	return n
}

func lockAcquireCost(activeLocks int) int {
	if activeLocks <= 0 {
		return 2
	}
	return 3
}

// carrierCost 估算“让该件补齐 required 效果集合”的近似期望成本（越低越好），用于成本交换选承载者。
// 与运行时 partExpectedCostForRequired 的简化差异：只按“缺失效果 × 1/权重难度” + “已在1号槽(不可锁)的风险成本”估算，
// 用来做承载者的成本排序（更接近完整/权重更高效果 → 成本更低 → 优先当承载者，体现“成本更低者晋升、原承载者退行”）。
func carrierCost(p piece, required []int) float64 {
	cost := 0.0
	for _, e := range required {
		if pieceHas(p, e) {
			// 已持有：若在1号槽(策略上不锁、易被洗掉)记小风险成本；2/3号槽可锁记0。
			if p.slots[0] == e && !p.locks[0] {
				cost += 0.5
			}
			continue
		}
		w := weights[e]
		if w <= 0 {
			w = 0.1
		}
		cost += 1.0 / w // 权重越低越难出 → 成本越高
	}
	return cost
}

// allocateAssigned：把全局正数配额“持有名额”分配到各件（每件至多 1 条，同件不重复），采用
// “角色阶梯 + 成本交换”口径（与运行时 allocateQuotaRequired / pickCheapestCarriersForEffect 一致）：
//   - 满配额效果（K[e] >= 件数）：分给全部件；
//   - 稀缺效果（K[e] < 件数）：按“补齐该件 required（assigned + e）的期望成本最低”选 K[e] 个承载者
//     （成本更低者晋升、原承载者退行），并遵守每件容量 = maxSlot − 满配额数。
//
// 说明：这是独立模拟器的成本交换分配（成本为 carrierCost 近似），运行时 DP 口径见 plan_dp.go。
func allocateAssigned(pieces [nPieces]piece, K map[int]int) [nPieces][]int {
	var assigned [nPieces][]int
	var posEffects []int
	for eff, c := range K {
		if c > 0 {
			posEffects = append(posEffects, eff)
		}
	}
	sort.Ints(posEffects)
	// 满配额效果 → 全部分配；统计满配额占槽数。
	fullCount := 0
	for _, eff := range posEffects {
		if K[eff] >= nPieces {
			for i := 0; i < nPieces; i++ {
				assigned[i] = append(assigned[i], eff)
			}
			fullCount++
		}
	}
	capacity := 3 - fullCount
	used := make([]int, nPieces)

	// 稀缺效果：成本交换选承载者。
	for _, eff := range posEffects {
		need := K[eff]
		if need >= nPieces {
			continue
		}
		type cand struct {
			idx  int
			cost float64
		}
		var cs []cand
		for i := 0; i < nPieces; i++ {
			if used[i] >= capacity {
				continue
			}
			required := append(append([]int{}, assigned[i]...), eff)
			cs = append(cs, cand{i, carrierCost(pieces[i], required)})
		}
		sort.Slice(cs, func(a, b int) bool {
			if cs[a].cost != cs[b].cost {
				return cs[a].cost < cs[b].cost
			}
			return cs[a].idx < cs[b].idx
		})
		for k := 0; k < need && k < len(cs); k++ {
			assigned[cs[k].idx] = append(assigned[cs[k].idx], eff)
			used[cs[k].idx]++
		}
	}
	return assigned
}

func sliceIntContains(list []int, v int) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// decideLocks：加锁“本件负责（assigned[idx]）且已持有、全局恰好达标（<=Q）”的配额效果（先难后易、类型互斥）。
// 本件未负责的效果不锁（可被洗掉）；禁止词条不锁；超额（globalCount>Q）不锁。
func decideLocks(idx int, p piece, K map[int]int, pieces [nPieces]piece, assigned [nPieces][]int) piece {
	seen := map[int]bool{}
	forbidden := forbiddenEffects(K)
	mine := assigned[idx]
	for _, si := range []int{2, 1} { // slot3, slot2（先难后易）
		eff := p.slots[si]
		if eff < 0 || !isQuota(eff) || p.locks[si] || seen[eff] {
			continue
		}
		if forbidden[eff] || !sliceIntContains(mine, eff) {
			continue
		}
		if globalCount(pieces, eff) > K[eff] {
			continue
		}
		p.locks[si] = true
		seen[eff] = true
	}
	return p
}

// pickPiece：选“最需改进”的件——缺的“本件负责”效果越多、越废（禁用/未负责却占用/非配额槽位）→ 越优先修。
// 分配感知：只追本件负责的效果，避免“每件都去抢稀缺配额”导致破坏优/攻、振荡不收敛。
func pickPiece(pieces [nPieces]piece, K map[int]int, assigned [nPieces][]int) int {
	best := -1
	bestScore := -1e9
	forbidden := forbiddenEffects(K)
	for i := 0; i < nPieces; i++ {
		p := pieces[i]
		miss := 0
		waste := 0
		if pieceHasForbidden(p, forbidden) {
			waste += 10
		}
		// miss：本件负责但尚未持有的效果（且全局仍需）。
		for _, eff := range assigned[i] {
			if !pieceHas(p, eff) && need(K, pieces, eff) > 0 {
				miss++
			}
		}
		// waste：携带了“本件不负责”的配额效果（占用名额、挡了本件负责的配额）→ 可洗掉。
		for _, eff := range assignedAllQuotaTypes(K) {
			if pieceHas(p, eff) && !sliceIntContains(assigned[i], eff) {
				waste++
			}
		}
		// 非配额槽位占用。
		for _, s := range p.slots {
			if s >= 0 && !isQuota(s) && !forbidden[s] {
				waste++
			}
		}
		score := float64(miss)*10 + float64(waste)*6
		if score > bestScore {
			bestScore = score
			best = i
		}
	}
	return best
}

// assignedAllQuotaTypes 返回 K 中正数配额的效应索引列表（供 waste 判定“本件是否负责”）。
func assignedAllQuotaTypes(K map[int]int) []int {
	var out []int
	for eff, c := range K {
		if c > 0 {
			out = append(out, eff)
		}
	}
	return out
}

func runJoint(init [nPieces]piece, K map[int]int, rng *rand.Rand, maxCycles int) (modules, cycles int, capHit bool, finalAmmo int, finalForbidden bool) {
	pieces := init
	// 固定分配：按初始状态算“每件负责的配额效果集合”，采用**成本交换口径**（见 allocateAssigned，
	// carrierCost 近似）。稀缺配额只分给需要数量件，且按“补齐成本最低”选承载者（一致于运行时
	// pickCheapestCarriersForEffect）。运行时还会每步重算（动态流转），模拟器此处固定一次。
	assigned := allocateAssigned(init, K)
	for c := 0; c < maxCycles; c++ {
		if satisfied(K, pieces) {
			return modules, c, false, globalCount(pieces, idxAmmo), false
		}
		idx := pickPiece(pieces, K, assigned)
		before := countLocks(pieces[idx])
		pieces[idx] = decideLocks(idx, pieces[idx], K, pieces, assigned)
		after := countLocks(pieces[idx])
		for k := before; k < after; k++ {
			modules += lockAcquireCost(k)
		}
		modules += 1 + after // 效果变更费 0锁1/1锁2/2锁3
		pieces[idx] = rerollPiece(pieces[idx], rng)
	}
	forbid := false
	for _, p := range pieces {
		if pieceHasForbidden(p, forbiddenEffects(K)) {
			forbid = true
		}
	}
	return modules, maxCycles, true, globalCount(pieces, idxAmmo), forbid
}

type quotaCase struct {
	label string
	Q     map[int]int
	init  [nPieces]piece
}

func q4(u, a, am int) map[int]int { return map[int]int{idxYou: u, idxAtk: a, idxAmmo: am} }

func main() {
	startedAt := time.Now()
	// startDefense：洗词条统一起点——每件装备只有 1 号槽且为防御词条（[防御,空,空]），其余槽未获得。
	startDefense := func() [nPieces]piece {
		p := [nPieces]piece{}
		for i := 0; i < nPieces; i++ {
			p[i] = newPiece([3]int{idxDef, -1, -1})
		}
		return p
	}

	cases := []quotaCase{
		{"4 = 优4 (四优)", q4(4, 0, 0), startDefense()},
		{"4/1 = 优4 + 攻1", q4(4, 1, 0), startDefense()},
		{"4/2/1 = 优4 + 攻2 + 装弹1", q4(4, 2, 1), startDefense()},
		{"4/4 = 优4 + 攻4 (攻4优4)", q4(4, 4, 0), startDefense()},
		{"4/4/1 = 优4 + 攻4 + 装弹1", q4(4, 4, 1), startDefense()},
		{"4/4/1/1 = 优4+攻4+装弹1+蓄力速度1", map[int]int{idxYou: 4, idxAtk: 4, idxAmmo: 1, idxChgSpd: 1}, startDefense()},
		{"4/4/4 = 优4 + 攻4 + 装弹4", q4(4, 4, 4), startDefense()},
		{"4/4/1 + 命中率-1(禁)", map[int]int{idxYou: 4, idxAtk: 4, idxAmmo: 1, idxHit: -1}, startDefense()},

		// ---- 泛化配额（新增）：验证“任意配额 + 容量 + 涌现承载者 + 调度优先级”框架在不同结构下的期望成本 ----
		{"4/4/2 = 优4+攻4+装弹2", q4(4, 4, 2), startDefense()},
		{"仅稀缺 = 装弹1+蓄力速度1", map[int]int{idxAmmo: 1, idxChgSpd: 1}, startDefense()},
		{"混合多稀缺 = 优4+装弹1+蓄力速度1", map[int]int{idxYou: 4, idxAmmo: 1, idxChgSpd: 1}, startDefense()},
		{"混合多稀缺 = 优4+攻2+装弹2+蓄力速度1", map[int]int{idxYou: 4, idxAtk: 2, idxAmmo: 2, idxChgSpd: 1}, startDefense()},
	}

	ncpu := runtime.GOMAXPROCS(0)
	per := 20000
	maxCycles := 30000
	fmt.Printf("全局联合分配模拟 —— 4件×3槽; 模块锁定(永久); 效果变更 0锁1/1锁2/2锁3; 并行=%d per=%d maxCycles=%d\n\n", ncpu, per, maxCycles)

	for _, c := range cases {
		fmt.Printf("== %s ==\n", c.label)
		var totM, totC, caps, forbidLeft int
		ammoHist := map[int]int{}
		var wg sync.WaitGroup
		var mu sync.Mutex
		for w := 0; w < ncpu; w++ {
			wg.Add(1)
			go func(w int) {
				defer wg.Done()
				rng := rand.New(rand.NewSource(int64(3000 + w)))
				var m, cc, cp, fb int
				for i := 0; i < per; i++ {
					mm, cx, hit, ammo, forbid := runJoint(c.init, c.Q, rng, maxCycles)
					m += mm
					cc += cx
					if hit {
						cp++
					}
					if forbid {
						fb++
					}
					mu.Lock()
					ammoHist[ammo]++
					mu.Unlock()
				}
				mu.Lock()
				totM += m
				totC += cc
				caps += cp
				forbidLeft += fb
				mu.Unlock()
			}(w)
		}
		wg.Wait()
		done := ncpu * per
		fmt.Printf("  期望模块=%.1f  期望刷新=%.1f  触顶=%.2f%%  残禁=%.2f%%\n",
			float64(totM)/float64(done), float64(totC)/float64(done), float64(caps)/float64(done)*100, float64(forbidLeft)/float64(done)*100)
		ammo := c.Q[idxAmmo]
		if ammo <= 0 {
			ammo = 0
		}
		fmt.Printf("  最终装弹条数: ")
		for k := 0; k <= 4; k++ {
			if ammoHist[k] > 0 {
				fmt.Printf("%d条=%.1f%% ", k, float64(ammoHist[k])/float64(done)*100)
			}
		}
		fmt.Printf("(目标装弹=%d)\n", ammo)
	}
	fmt.Printf("\n总耗时: %v\n", time.Since(startedAt))
}
