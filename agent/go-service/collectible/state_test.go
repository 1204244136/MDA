package collectible

import "testing"

// 等级用经验条上限校正：等级是任务级记忆、可能记错，会让样本归到错的档，
// 上限 1000/3000 是当场可验的观测。
func TestGradeFromExpCap(t *testing.T) {
	cases := []struct {
		exp      string
		recorded string
		want     string
	}{
		{"0/1000", "R", "R"},
		{"500/3000", "SR", "SR"},
		{"200/3000", "R", "SR"},  // 记忆说 R，观测说 SR → 以观测为准
		{"900/1000", "SR", "R"},  // 反过来同样
		{"", "SR", "SR"},         // 读不到就保留记忆
		{"500/2000", "SR", "SR"}, // 未知上限不猜
	}
	for _, c := range cases {
		if got := gradeFromExpCap(c.exp, c.recorded); got != c.want {
			t.Errorf("gradeFromExpCap(%q, %q) = %q, want %q", c.exp, c.recorded, got, c.want)
		}
	}
}

// 面板状态解析：阶段与经验是决策的状态变量，持有数决定用哪一档，三者都不能用 0 兜底；
// 选中档读不到则退回「不预升级」的语义（面板没有选中档时本来就不预升级）。
func TestParsePanelState(t *testing.T) {
	ok := panelRead{
		stageText: "★★★11阶段",
		expText:   "1000/3000",
		heldTexts: [3]string{"持有数 23", "持有数：715", "持有数 585"},
		Selected:  tierSSR,
	}
	st, err := parsePanelState(ok)
	if err != nil {
		t.Fatalf("正常读数不应报错: %v", err)
	}
	if st.Stage != 11 || st.PreviewExp != 1000 || st.Exp != 0 {
		t.Fatalf("stage/preview/exp = %d/%d/%d, want 11/1000/0", st.Stage, st.PreviewExp, st.Exp)
	}
	if st.Held != [3]int{23, 715, 585} {
		t.Fatalf("held = %v, want [23 715 585]", st.Held)
	}

	// 选中初级箱：预升级只加了 200，真实经验要还原成 800
	st, err = parsePanelState(panelRead{
		stageText: "5阶段", expText: "1000/3000",
		heldTexts: [3]string{"23", "715", "585"}, Selected: tierR,
	})
	if err != nil || st.Exp != 800 {
		t.Fatalf("选中初级箱时 exp = %d (err=%v), want 800", st.Exp, err)
	}

	// 没认出选中档：按真实经验处理（面板不预升级），而不是报错
	st, err = parsePanelState(panelRead{
		stageText: "5阶段", expText: "1000/3000",
		heldTexts: [3]string{"23", "715", "585"}, Selected: -1,
	})
	if err != nil || st.Exp != 1000 {
		t.Fatalf("无选中档时 exp = %d (err=%v), want 1000", st.Exp, err)
	}

	// 满级：阶段之后就读不下去也不该报错，直接返回 Maxed 状态
	st, err = parsePanelState(panelRead{stageText: "15阶段", expText: "3000/3000", heldTexts: [3]string{"", "", ""}})
	if err != nil || st.Stage != maxStage {
		t.Fatalf("满级应直接返回, got stage=%d err=%v", st.Stage, err)
	}

	failures := []struct {
		name string
		in   panelRead
	}{
		{"阶段读不到", panelRead{stageText: "", expText: "1000/3000", heldTexts: [3]string{"1", "1", "1"}}},
		{"经验读不到", panelRead{stageText: "11阶段", expText: "", heldTexts: [3]string{"1", "1", "1"}}},
		{"初级箱持有数读不到", panelRead{stageText: "11阶段", expText: "1000/3000", heldTexts: [3]string{"持有", "1", "1"}}},
		{"高级箱持有数读不到", panelRead{stageText: "11阶段", expText: "1000/3000", heldTexts: [3]string{"1", "1", ""}}},
	}
	for _, c := range failures {
		if _, err := parsePanelState(c.in); err == nil {
			t.Fatalf("%s: 应当报错而不是拿 0 兜底", c.name)
		}
	}
}
