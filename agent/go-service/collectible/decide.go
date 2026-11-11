package collectible

import (
	"math"
)

// 工具箱档位：0=R 1=SR 2=SSR，与三档权重、经验增量的下标一致。
const (
	tierR = iota
	tierSR
	tierSSR
)

// 材料档位权重：由「随机宝箱」的概率情报折算。
// SSR 保养工具箱开出 高级/中级/初级 的概率是 10% / 20% / 70%，SR 箱是 20% / 80%，
// 即 1 个 SSR 箱 ≈ 7 个初级、1 个中级 ≈ 3.5 个初级（作者说的 6:2:1 就是这套比率的近似）。
var tierWeight = [3]float64{1, 3.5, 7}

// 每档每次保养提供的经验。
var tierGain = [3]int{200, 500, 1000}

// 每次保养消耗的工具箱数量。
const boxesPerMaintain = 10

// expUnit 是动态规划的经验量化单位。三档增量（200/500/1000）与每阶段上限
// （1000/3000）都是 100 的倍数，面板读数也只会是 100 的倍数，
// 所以按 100 归档不丢精度，状态数从 4.5 万降到 450。
const expUnit = 100

// stageCap 返回该等级收藏品每一阶段需要的经验。
func stageCap(grade string) int {
	if grade == "SR" {
		return 3000
	}
	return 1000
}

// pAnchor 是某个阶段的实测非常成功几率（R / SR / SSR 三档）。
type pAnchor struct {
	stage int
	p     [3]float64
}

// pTable 按等级存放实测锚点，未测到的阶段按相邻锚点线性插值。
// 数据来源：R 级 0 阶 17.6/55/100；SR 级 0 阶 3.6/11/25、10 阶 1.2/5.4/15、11 阶 2.2/9.9/27.5。
var pTable = map[string][]pAnchor{
	"R": {{0, [3]float64{0.176, 0.550, 1.000}}},
	"SR": {
		{0, [3]float64{0.036, 0.110, 0.250}},
		{10, [3]float64{0.012, 0.054, 0.150}},
		{11, [3]float64{0.022, 0.099, 0.275}},
	},
}

// successProbWith 用给定的采集表计算三档几率，避免重复读盘。
func successProbWith(data measured, grade string, stage int) [3]float64 {
	out := anchorProb(grade, stage)
	for t := 0; t < 3; t++ {
		if p, ok := lookupMeasured(data, grade, stage, t); ok {
			out[t] = p
		}
	}
	return out
}

// anchorProb 是内置锚点插值，采集文件里没有时使用。
func anchorProb(grade string, stage int) [3]float64 {
	anchors, ok := pTable[grade]
	if !ok || len(anchors) == 0 {
		return [3]float64{}
	}
	if stage <= anchors[0].stage {
		return anchors[0].p
	}
	for i := 1; i < len(anchors); i++ {
		if stage <= anchors[i].stage {
			lo, hi := anchors[i-1], anchors[i]
			span := float64(hi.stage - lo.stage)
			ratio := float64(stage-lo.stage) / span
			var out [3]float64
			for t := 0; t < 3; t++ {
				out[t] = lo.p[t] + (hi.p[t]-lo.p[t])*ratio
			}
			return out
		}
	}
	return anchors[len(anchors)-1].p
}

// expectedUses 返回一批 k 次保养实际会执行几次。
//
// 游戏在「次数耗尽或达到下一阶段时停止重复保养」，所以一旦命中非常成功就提前收工，
// 后面没执行的次数不消耗材料，一批 k 次的期望消耗是 (1-(1-p)^k)/p：27.5% 时 k=3 只有 2.25 次。
// 按 k 全额计费会高估大盘批量——不过实测这个口径不改变任何决策：
// k=1 时两者恒等，而最优解总能由单步达成（见 charging_test.go），
// R/SR 两级 600 个状态的 V 与选档逐位相同。这里保留期望口径是把账算对。
func expectedUses(k int, p float64) float64 {
	if k <= 0 {
		return 0
	}
	if p <= 0 {
		return float64(k)
	}
	if p >= 1 {
		return 1
	}
	return (1 - math.Pow(1-p, float64(k))) / p
}

// boundary 返回非常成功时跳到的阶段（下一个 5 的倍数）。
func boundary(stage int) int {
	b := (stage/5 + 1) * 5
	if b > maxStage {
		b = maxStage
	}
	return b
}

// planner 在「阶段 × 经验」上做动态规划，求到满级为止的最小期望成本。
//
// 关键点：非常成功不是「结束本阶段」，而是**直接跳到下一个 5 的倍数**
// （0–4 → 5、5–9 → 10、10–14 → 15），也就是说一次成功可能跳过中间好几阶。
// 所以高阶段起手用高成功率档位去「抽一次」往往比一路用便宜档更省，
// 这正是单阶段独立估算抓不到的部分。
//
// held 只用来判断某档还剩不剩一次保养，不参与递推——即规划时假设它「够用」。
// 这是刻意的取舍：实测（4000 局/场景的蒙特卡洛，见 decide_test.go 的
// TestPlannerMatchesSimulation 与文档）表明，把库存递归进状态或用影子价格配给，
// 相对这套 DP 的收益在 ±1.5% 以内且方向不稳定；而决定本身每批保养后都会重算，
// 某档真的用完后 usable 立刻把它排除，不会出现「规划里一直有、实际没有」的失控。
type planner struct {
	grade string
	held  [3]int
	prob  func(int) [3]float64
	memo  map[[2]int]float64
}

func newPlanner(grade string, held [3]int, prob func(int) [3]float64) *planner {
	return &planner{grade: grade, held: held, prob: prob, memo: map[[2]int]float64{}}
}

// usable 判断某档库存是否够一次保养。
func (p *planner) usable(t int) bool {
	return p.held[t] >= boxesPerMaintain
}

// planKey 把状态量化成 (阶段, 经验/100)，见 expUnit。
func planKey(stage, exp int) [2]int {
	if exp < 0 {
		exp = 0
	}
	return [2]int{stage, exp / expUnit}
}

// maxCount 返回当前阶段还要几次保养才够打满；批量次数就枚举到它。
func (p *planner) maxCount(exp, gain int) int {
	n := (stageCap(p.grade) - exp + gain - 1) / gain
	if n < 1 {
		n = 1
	}
	return n
}

// step 计算「在 (stage, exp) 用第 t 档连做 k 次」的期望成本。
func (p *planner) step(stage, exp, t, k int, prob float64) float64 {
	cap := stageCap(p.grade)
	gain := tierGain[t]
	uses := expectedUses(k, prob) // 提前收工的那几次不计费
	q := 1 - math.Pow(1-prob, float64(k))
	adv := exp + k*gain
	var after float64
	if adv >= cap {
		if stage+1 < maxStage {
			after = p.valueAt(stage+1, adv-cap)
		}
	} else {
		after = p.valueAt(stage, adv)
	}
	cost := uses * float64(boxesPerMaintain) * tierWeight[t]
	return cost + (1-q)*after + q*p.valueAt(boundary(stage), 0)
}

// valueAt 返回从 (stage, exp) 打到满级的最小期望成本。
func (p *planner) valueAt(stage, exp int) float64 {
	if stage >= maxStage {
		return 0
	}
	key := planKey(stage, exp)
	if v, ok := p.memo[key]; ok {
		return v
	}
	p.memo[key] = math.Inf(1) // 防御：异常输入时不至于无限递归
	best := math.Inf(1)
	ps := p.prob(stage)
	for t := 0; t < 3; t++ {
		if !p.usable(t) {
			continue
		}
		for k := 1; k <= p.maxCount(exp, tierGain[t]); k++ {
			if c := p.step(stage, exp, t, k, ps[t]); c < best {
				best = c
			}
		}
	}
	p.memo[key] = best
	return best
}

// action 返回 (stage, exp) 下的最优动作：档位、连续保养次数与该动作的期望成本。
func (p *planner) action(stage, exp int) (tier, count int, cost float64) {
	tier, count, cost = -1, 0, math.Inf(1)
	if stage >= maxStage {
		return
	}
	ps := p.prob(stage)
	for t := 0; t < 3; t++ {
		if !p.usable(t) {
			continue
		}
		for k := 1; k <= p.maxCount(exp, tierGain[t]); k++ {
			if c := p.step(stage, exp, t, k, ps[t]); c < cost {
				tier, count, cost = t, k, c
			}
		}
	}
	return
}

// batch 把「连续用同一档」的若干次动作合并成一次批量保养。
// 只按「全部失败」推演：真的触发非常成功时游戏会提前停止批量，回来后重新规划即可。
func (p *planner) batch(stage, exp int) (tier, count int) {
	tier, _, _ = p.action(stage, exp)
	if tier < 0 {
		return -1, 0
	}
	cap := stageCap(p.grade)
	stock := p.held[tier] / boxesPerMaintain
	st, e := stage, exp
	for st == stage && stock > 0 {
		t, k, _ := p.action(st, e)
		if t != tier || k < 1 {
			break
		}
		if k > stock {
			// 这一档只剩这么多次：把剩下的用完再去保养，别只喂一次就回来重来一遍
			count += stock
			stock = 0
			break
		}
		count += k
		stock -= k
		e += k * tierGain[t]
		if e >= cap {
			st++
			e -= cap
		}
	}
	if count < 1 {
		count = 1
	}
	return tier, count
}

// planChoice 是一次决策的结果。
type planChoice struct {
	Tier       int     // 0=R 1=SR 2=SSR
	Count      int     // 本次批量保养次数（对话框里的重复次数）
	Cost       float64 // 从当前状态打到满级的期望成本（R 箱当量）
	Maxed      bool    // 已满级
	NoMaterial bool    // 三档都不够
}

// decide 在当前状态下规划本阶段的第一步：
// 先用「阶段 × 经验」动态规划选档（非常成功会直接跳到下一个 5 的倍数），
// 再把该档的连续若干次合并成本次批量保养的重复次数。
//
// 概率表由调用方注入（decideWith），测试才能钉住固定数据：
// 采集文件在 gitignore 的 debug/ 下，CI 的干净检出里根本不存在。
func decide(grade string, stage, exp int, held [3]int) planChoice {
	return decideWith(loadMeasured(), grade, stage, exp, held)
}

// decideWith 用给定的采集表决策。
func decideWith(data measured, grade string, stage, exp int, held [3]int) planChoice {
	if stage >= maxStage {
		return planChoice{Maxed: true}
	}
	exp -= exp % expUnit // 面板经验必是 100 的倍数；向下取整保证 DP 状态可枚举
	cache := map[int][3]float64{}
	prob := func(s int) [3]float64 {
		if ps, ok := cache[s]; ok {
			return ps
		}
		ps := successProbWith(data, grade, s)
		cache[s] = ps
		return ps
	}

	p := newPlanner(grade, held, prob)
	tier, count := p.batch(stage, exp)
	if tier < 0 {
		return planChoice{Tier: -1, NoMaterial: true}
	}
	choice := planChoice{Tier: tier, Count: count, Cost: p.valueAt(stage, exp)}
	if choice.Count < 1 {
		choice.Count = 1
	}
	if max := held[choice.Tier] / boxesPerMaintain; choice.Count > max {
		choice.Count = max
	}
	return choice
}
