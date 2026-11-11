package collectible

import (
	"math"
	"math/rand"
	"testing"
)

// 仿真语义与 planner 的模型必须逐条对齐，否则这个测试就没有意义：
//   - 一批 k 次保养，中途命中非常成功就提前收工（跳到下一个 5 的倍数、经验清零）；
//   - 没命中就把 k 次做完，经验累加；累到阶段上限则进下一阶段并结转多余经验；
//   - 每次消耗 10 个工具箱，按真实档位权重（初级箱当量）计费。
type simStep struct {
	tier int
	k    int
}

// simulateJourney 从 (stage, exp) 跑到满级，返回该局的初级箱当量消耗。
// pick 返回当前状态下这一步要执行的动作；返回 tier<0 表示材料不足。
func simulateJourney(grade string, stage, exp int, held [3]int, data measured, rng *rand.Rand,
	pick func(stage, exp int, held [3]int) simStep) (float64, bool) {

	stock := held
	s, e := stage, exp
	cost := 0.0
	cap := stageCap(grade)
	for step := 0; step < 20000 && s < maxStage; step++ {
		act := pick(s, e, stock)
		if act.tier < 0 {
			return cost, false
		}
		k := act.k
		if max := stock[act.tier] / boxesPerMaintain; k > max {
			k = max
		}
		if k < 1 {
			return cost, false
		}
		ps := successProbWith(data, grade, s)
		for j := 0; j < k; j++ {
			stock[act.tier] -= boxesPerMaintain
			cost += float64(boxesPerMaintain) * tierWeight[act.tier]
			if rng.Float64() < ps[act.tier] {
				s, e = boundary(s), 0
				break
			}
			e += tierGain[act.tier]
			if e >= cap {
				s++
				e -= cap
			}
		}
	}
	return cost, true
}

// 动态规划只是「在真实流程上做期望」的另一种写法：
// 按 DP 自己给出的动作（档位 × 次数）逐步仿真，平均消耗必须收敛到 DP 报的期望成本。
// 这条断言守的是**仿真语义与 DP 模型一致**（跳阶、提前收工、阶段结转、每档 10 个箱子）；
// 它并不能区分「批量按期望次数计费」与「按 k 全额计费」——两种口径的最优值本来就相同，
// 那个口径由 charging_test.go 的 TestExpectedUses / TestSingleStepAchievesOptimum 钉住。
func TestPlannerMatchesSimulation(t *testing.T) {
	if testing.Short() {
		t.Skip("蒙特卡洛一致性测试，-short 下跳过")
	}
	cases := []struct {
		name  string
		grade string
		stage int
		exp   int
		held  [3]int
	}{
		{"R级0阶", "R", 0, 0, [3]int{100000, 100000, 100000}},
		{"SR级0阶", "SR", 0, 0, [3]int{100000, 100000, 100000}},
		{"SR级8阶", "SR", 8, 0, [3]int{100000, 100000, 100000}},
	}
	const trials = 1500
	for _, c := range cases {
		data := testMeasured
		want := decideWith(testMeasured, c.grade, c.stage, c.exp, c.held).Cost

		rng := rand.New(rand.NewSource(20261111))
		total := 0.0
		for i := 0; i < trials; i++ {
			cost, ok := simulateJourney(c.grade, c.stage, c.exp, c.held, data, rng,
				func(s, e int, held [3]int) simStep {
					p := newPlanner(c.grade, held, func(st int) [3]float64 {
						return successProbWith(data, c.grade, st)
					})
					tier, k, _ := p.action(s, e)
					return simStep{tier: tier, k: k}
				})
			if !ok {
				t.Fatalf("%s: 仿真中途材料不足", c.name)
			}
			total += cost
		}
		got := total / float64(trials)
		if math.Abs(got-want) > 0.05*want {
			t.Fatalf("%s: 仿真均值 %.1f 与 DP 期望 %.1f 相差超过 5%%", c.name, got, want)
		}
	}
}

// 实际流程用的是「把连续同档动作合并成一次批量」的次数，
// 合并后命中非常成功的概率与未命中时的总经验都不变，但按次停止让期望消耗只减不增，
// 所以真实流程不会比 DP 报的期望更贵。
func TestBatchFlowNotWorseThanModel(t *testing.T) {
	if testing.Short() {
		t.Skip("蒙特卡洛一致性测试，-short 下跳过")
	}
	const trials = 1500
	for _, grade := range []string{"R", "SR"} {
		held := [3]int{100000, 100000, 100000}
		data := testMeasured
		want := decideWith(testMeasured, grade, 0, 0, held).Cost

		rng := rand.New(rand.NewSource(1024))
		total := 0.0
		for i := 0; i < trials; i++ {
			cost, ok := simulateJourney(grade, 0, 0, held, data, rng,
				func(s, e int, stock [3]int) simStep {
					ch := decideWith(testMeasured, grade, s, e, stock)
					if ch.Maxed || ch.NoMaterial || ch.Tier < 0 {
						return simStep{tier: -1}
					}
					return simStep{tier: ch.Tier, k: ch.Count}
				})
			if !ok {
				t.Fatalf("%s: 仿真中途材料不足", grade)
			}
			total += cost
		}
		got := total / float64(trials)
		if got > want*1.05 {
			t.Fatalf("%s: 合并批量的实际期望 %.1f 比模型 %.1f 贵 5%% 以上", grade, got, want)
		}
	}
}
