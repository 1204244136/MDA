package collectible

import (
	"math"
	"testing"
)

// 一批 k 次的期望执行次数：命中非常成功游戏就提前收工，后面没执行的次数不消耗材料。
func TestExpectedUses(t *testing.T) {
	cases := []struct {
		k    int
		p    float64
		want float64
	}{
		{0, 0.275, 0},
		{1, 0.275, 1},        // 一批一次：无论成功率都只执行一次
		{3, 0.275, 2.250625}, // (1-0.725³)/0.275
		{2, 0.5, 1.5},
		{5, 0, 5}, // 不可能成功 → 每次都执行
		{3, 1, 1}, // 必定成功 → 只执行一次
	}
	for _, c := range cases {
		if got := expectedUses(c.k, c.p); math.Abs(got-c.want) > 1e-6 {
			t.Errorf("expectedUses(%d, %v) = %v, want %v", c.k, c.p, got, c.want)
		}
	}

	// 合并成一批比拆成两批更省（同一批只会更早被非常成功打断）：
	// E(a) + E(b) − E(a+b) = (1−(1−p)^a)(1−(1−p)^b)/p ≥ 0，两个口径的差异就出在这里。
	for _, p := range []float64{0.036, 0.275, 0.7} {
		if merged, split := expectedUses(5, p), expectedUses(2, p)+expectedUses(3, p); merged > split+1e-12 {
			t.Errorf("p=%v 合并 %v 不应比拆分 %v 更贵", p, merged, split)
		}
	}
}

// 最优解总能由「单步」达成：k=1 时 expectedUses(1,p) 恒等于 1，
// 也就是两种计费口径（按期望次数 / 按 k 全额）在最优解上完全一致。
//
// 这解释了为什么把批量成本改成按期望次数计费**不会改变任何决策**——
// 用对照实现（把 expectedUses(k,p) 换成 k）跑遍 R/SR 两级共 600 个状态，
// V 与选的档位、次数逐位相同。这条测试把这个结构性质钉住：
// 哪天 k=1 不再最优，就说明模型真的变了，必须重新审视计费口径。
func TestSingleStepAchievesOptimum(t *testing.T) {
	for _, grade := range []string{"R", "SR"} {
		cap := stageCap(grade)
		held := [3]int{100000, 100000, 100000}
		prob := func(s int) [3]float64 { return successProbWith(testMeasured, grade, s) }
		for stage := 0; stage < maxStage; stage++ {
			for exp := 0; exp < cap; exp += expUnit {
				p := newPlanner(grade, held, prob)
				want := p.valueAt(stage, exp)
				ps := prob(stage)
				found := false
				for tier := 0; tier < 3; tier++ {
					if !p.usable(tier) {
						continue
					}
					if math.Abs(p.step(stage, exp, tier, 1, ps[tier])-want) < 1e-9 {
						found = true
					}
				}
				if !found {
					t.Fatalf("%s stage=%d exp=%d：单步拿不到最优值 %.3f", grade, stage, exp, want)
				}
			}
		}
	}
}
