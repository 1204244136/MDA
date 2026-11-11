package collectible

import (
	"encoding/json"
	"math"
	"regexp"
	"strings"
	"testing"
)

func near(a, b float64) bool { return math.Abs(a-b) < 0.6 }

// 实测锚点下各阶段的选档结果（模型含「非常成功跳阶」，见 decide.go 的 planner）。
func TestDecideStageChoices(t *testing.T) {
	cases := []struct {
		name  string
		grade string
		stage int
		exp   int
		held  [3]int
		tier  int
		count int
	}{
		{"R级0阶", "R", 0, 0, [3]int{1000, 100, 100}, tierR, 5},
		{"SR级0阶", "SR", 0, 0, [3]int{1000, 100, 100}, tierR, 15},
		{"SR级5阶", "SR", 5, 0, [3]int{1000, 100, 100}, tierSSR, 1},
		{"SR级8阶", "SR", 8, 0, [3]int{1000, 100, 100}, tierSSR, 2},
		{"SR级10阶", "SR", 10, 0, [3]int{1000, 100, 100}, tierSSR, 2},
		{"SR级11阶", "SR", 11, 0, [3]int{1000, 100, 100}, tierSSR, 3},
		{"SR级14阶", "SR", 14, 0, [3]int{1000, 100, 100}, tierSSR, 1},
		{"SR级0阶末段", "SR", 0, 2800, [3]int{1000, 100, 100}, tierR, 1},
	}
	for _, c := range cases {
		got := decideWith(testMeasured, c.grade, c.stage, c.exp, c.held)
		if got.Maxed || got.NoMaterial {
			t.Fatalf("%s: unexpected flags %+v", c.name, got)
		}
		if got.Tier != c.tier || got.Count != c.count {
			t.Errorf("%s: got %s x%d, want %s x%d",
				c.name, tierName(got.Tier), got.Count, tierName(c.tier), c.count)
		}
	}
}

// 全程期望成本：含跳阶的模型比「每阶段独立、永远用最便宜档」低一半以上。
func TestPlannerCostBeatsPerStageGreedy(t *testing.T) {
	got := decideWith(testMeasured, "SR", 0, 0, [3]int{1000, 100, 100})
	if got.Cost > 900 {
		t.Fatalf("planner cost %.1f, want < 900", got.Cost)
	}
	greedy := 0.0
	for st := 0; st < maxStage; st++ {
		uses := expectedUses((stageCap("SR")+tierGain[tierR]-1)/tierGain[tierR], anchorProb("SR", st)[tierR])
		greedy += uses * float64(boxesPerMaintain) * tierWeight[tierR]
	}
	if got.Cost >= greedy {
		t.Fatalf("planner cost %.1f should beat per-stage greedy %.1f", got.Cost, greedy)
	}
}

// 库存不足时自动降级：R 为 0 时 11 阶改走 SSR。
func TestDecideFallsBackWhenNoR(t *testing.T) {
	got := decideWith(testMeasured, "SR", 11, 0, [3]int{0, 100, 100})
	if got.Tier != tierSSR {
		t.Fatalf("want SSR, got %s", tierName(got.Tier))
	}
}

// 三档都不足一次保养 → 材料不足。
func TestDecideNoMaterial(t *testing.T) {
	got := decideWith(testMeasured, "SR", 3, 0, [3]int{9, 0, 0})
	if !got.NoMaterial {
		t.Fatalf("want NoMaterial, got %+v", got)
	}
}

// 满级直接返回 Maxed。
func TestDecideMaxed(t *testing.T) {
	got := decideWith(testMeasured, "R", 15, 0, [3]int{1000, 100, 100})
	if !got.Maxed {
		t.Fatalf("want Maxed, got %+v", got)
	}
}

// 批量次数不能超过库存能支撑的次数。
func TestDecideCountClampedByStock(t *testing.T) {
	got := decideWith(testMeasured, "SR", 0, 0, [3]int{30, 100, 100})
	if got.Count > 3 {
		t.Fatalf("count %d exceeds stock 30 boxes (3 maintenances)", got.Count)
	}
}

// 计划次数大于库存时，把这一档剩下的次数用完，而不是只喂一次就回去重来。
func TestBatchUsesPartialStock(t *testing.T) {
	// SR 11 阶起手计划高级箱 ×3，但只剩 2 次的高级箱
	got := decideWith(testMeasured, "SR", 11, 0, [3]int{0, 0, 20})
	if got.Tier != tierSSR || got.Count != 2 {
		t.Fatalf("want SSR x2, got %s x%d", tierName(got.Tier), got.Count)
	}
}

// 非常成功跳到下一个 5 的倍数。
func TestBoundary(t *testing.T) {
	cases := map[int]int{0: 5, 4: 5, 5: 10, 9: 10, 10: 15, 14: 15}
	for in, want := range cases {
		if got := boundary(in); got != want {
			t.Fatalf("boundary(%d) = %d, want %d", in, got, want)
		}
	}
}

// 阶段不在锚点上时按相邻锚点线性插值。
func TestSuccessProbInterpolates(t *testing.T) {
	p := anchorProb("SR", 5)
	wantR := (0.036 + 0.012) / 2
	if math.Abs(p[tierR]-wantR) > 1e-6 {
		t.Fatalf("SR stage5 pR = %.4f, want %.4f", p[tierR], wantR)
	}
}

// parsePercentText 必须消掉二进制尾数：14.4% 不能变成 0.14400000000000002。
func TestParsePercentText(t *testing.T) {
	cases := map[string]float64{
		"3.1%":  0.031,
		"14.4%": 0.144,
		"27.5%": 0.275,
		"7%":    0.07,
		"40%":   0.4,
	}
	for text, want := range cases {
		got, ok := parsePercentText(text)
		if !ok || got != want {
			t.Fatalf("%q -> %v %v, want %v", text, got, ok, want)
		}
	}
}

// parseNumberText 必须兼容“没有捕获组”的正则。
func TestParseNumberText(t *testing.T) {
	cases := []struct {
		text string
		re   string
		want int
	}{
		{"719", `(\d+)`, 719},
		{"350", `(\d+)`, 350},
		{"★★·11阶段", `(\d+)\s*阶段`, 11},
		{"200/3000", `(\d+)\s*/\s*(\d+)`, 200},
	}
	for _, c := range cases {
		got, ok := parseNumberText(c.text, regexp.MustCompile(c.re))
		if !ok || got != c.want {
			t.Fatalf("%q -> %d %v, want %d", c.text, got, ok, c.want)
		}
	}
	if _, ok := parseNumberText("持有", regexp.MustCompile(`(\d+)`)); ok {
		t.Fatal("「持有」不应解析出数字")
	}
}

// 采集表必须按等级 → 阶段数值升序 → 档位排序。
func TestMarshalMeasuredSorted(t *testing.T) {
	data := measured{
		"SR": {
			14: {0: 0.1, 1: 0.45, 2: 1},
			2:  {0: 0.01, 1: 0.02, 2: 0.03},
			11: {0: 0.022, 1: 0.099, 2: 0.275},
		},
		"R": {
			5: {0: 0.176, 1: 0.55, 2: 1},
		},
	}
	out := string(marshalMeasured(data))
	if err := json.Unmarshal([]byte(out), &measured{}); err != nil {
		t.Fatalf("输出不是合法 JSON: %v\n%s", err, out)
	}
	order := []string{"\"R\": {", "\"SR\": {", "\n    \"2\": {", "\n    \"11\": {", "\n    \"14\": {"}
	last := -1
	for _, token := range order {
		idx := strings.Index(out, token)
		if idx < 0 {
			t.Fatalf("missing %s in output:\n%s", token, out)
		}
		if idx < last {
			t.Fatalf("token %s out of order:\n%s", token, out)
		}
		last = idx
	}
}

// 重复次数的点击配额：只能点「目标 − 当前」次，绝不能点超过上限
// （R 级上限只有 5，到顶后「+」变灰，继续点会卡死）。
func TestRepeatClickBudget(t *testing.T) {
	taskID := int64(1)
	resetRepeat(taskID)
	if !repeatNeedsInit(taskID) {
		t.Fatal("resetRepeat 后应需要初始化")
	}
	initRepeatClicks(taskID, 5, 1) // 目标 5、当前 1 → 点 4 次
	for i := 0; i < 4; i++ {
		if !takeRepeatClick(taskID) {
			t.Fatalf("第 %d 次应还需要点击", i+1)
		}
	}
	if takeRepeatClick(taskID) {
		t.Fatal("点满 4 次后不应再点击")
	}
	if repeatNeedsInit(taskID) {
		t.Fatal("初始化后不应再需要初始化")
	}
}

// 目标小于当前时不应产生负数点击。
func TestRepeatClickBudgetAlreadyReached(t *testing.T) {
	taskID := int64(2)
	resetRepeat(taskID)
	initRepeatClicks(taskID, 3, 5)
	if takeRepeatClick(taskID) {
		t.Fatal("已经超过目标时不应再点击")
	}
}

// 档位核对重试同样有上限：连续两次对不上就接受现状，避免来回开关对话框。
func TestTierRetryGuard(t *testing.T) {
	taskID := int64(4)
	resetRepeat(taskID)
	if got := bumpTierRetry(taskID); got != 1 {
		t.Fatalf("第一次应为 1, got %d", got)
	}
	if got := bumpTierRetry(taskID); got != 2 {
		t.Fatalf("第二次应为 2, got %d", got)
	}
	resetRepeat(taskID)
	if got := bumpTierRetry(taskID); got != 1 {
		t.Fatalf("reset 后应重新从 1 开始, got %d", got)
	}
}

// 读不到次数时保持对话框默认（1 次），既不会点 MAX 超额消耗，也不会卡住。
func TestRepeatUnreadableKeepsDefault(t *testing.T) {
	taskID := int64(5)
	resetRepeat(taskID)
	initRepeatClicks(taskID, 1, 1)
	if takeRepeatClick(taskID) {
		t.Fatal("读不到次数时不应再点「+」")
	}
	if repeatNeedsInit(taskID) {
		t.Fatal("放行后不应重新进入初始化")
	}
}

// 选中态判定：三张卡里最多只有一张命中，取第一个即可。
func TestFirstHitTier(t *testing.T) {
	cases := []struct {
		hits [3]bool
		want int
		ok   bool
	}{
		{[3]bool{true, false, false}, tierR, true},
		{[3]bool{false, true, false}, tierSR, true},
		{[3]bool{false, false, true}, tierSSR, true},
		{[3]bool{false, false, false}, -1, false},
	}
	for _, c := range cases {
		got, ok := firstHitTier(c.hits)
		if ok != c.ok || got != c.want {
			t.Fatalf("%v -> %d %v, want %d %v", c.hits, got, ok, c.want, c.ok)
		}
	}
}

// 档位 → 节点名映射：缺失时报错并返回 false，避免静默路由到空节点。
func TestTierNode(t *testing.T) {
	m := map[string]string{"R": "NodeR", "SSR": "NodeSSR"}
	if got, ok := tierNode(m, tierR); !ok || got != "NodeR" {
		t.Fatalf("R -> %q %v", got, ok)
	}
	if got, ok := tierNode(m, tierSSR); !ok || got != "NodeSSR" {
		t.Fatalf("SSR -> %q %v", got, ok)
	}
	if _, ok := tierNode(m, tierSR); ok {
		t.Fatal("缺少 SR 时应返回 false")
	}
	if _, ok := tierNode(map[string]string{"R": "  "}, tierR); ok {
		t.Fatal("空白节点名应返回 false")
	}
}
