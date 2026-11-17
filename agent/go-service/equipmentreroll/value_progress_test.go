package equipmentreroll

import (
	"math"
	"strings"
	"testing"
)

func TestValueEffectPercent(t *testing.T) {
	// 优越代码 4 条、合计 44 档 = 四件各第 11 档 ≈ 94.22%（精确值 4×23.56 = 94.24%）。
	if pct, ok := valueEffectPercent("优越代码伤害增加", 4, 44); !ok || math.Abs(pct-94.22) > 0.05 {
		t.Fatalf("优越 4 条 44 档 = %.2f, want ≈94.22", pct)
	}
	// 暴击率 2 条、合计 22 档 = 两条各第 11 档 = 11.40%（插值 11.41%，误差来自档位表取整）。
	if pct, ok := valueEffectPercent("暴击率增加", 2, 22); !ok || math.Abs(pct-11.40) > 0.02 {
		t.Fatalf("暴击率 2 条 22 档 = %.2f, want ≈11.40", pct)
	}
	if _, ok := valueEffectPercent("攻击力增加", 0, 0); ok {
		t.Fatal("条目数为 0 时不应换算")
	}
	if _, ok := valueEffectPercent("未知效果", 1, 1); ok {
		t.Fatal("未知效果不应换算")
	}
}

func TestBuildValueProgressMessage(t *testing.T) {
	cfg := carrierConfig{
		Operation:    rerollOperationValue,
		Mode:         rerollModeCharacter,
		ValueTargets: valueTarget{"优越代码伤害增加": 34},
	}
	parts := map[string]partScan{}
	for _, part := range equipmentParts {
		parts[part] = partScan{Slots: [maxSlot]slotScanData{valueTestSlot("优越代码伤害增加", 11, LockNone)}}
	}
	msg := buildValueProgressMessage(parts, cfg)
	wantSatisfied := strings.Join([]string{
		"【数值目标】",
		"优越代码伤害增加（4条）已达标",
		"  44/34档（94.2% / ≈80.2%）",
	}, "\n")
	if msg != wantSatisfied {
		t.Fatalf("进度摘要不匹配：\ngot:\n%s\nwant:\n%s", msg, wantSatisfied)
	}

	// 目标高于当前：给出还差多少档，且单行宽度保持紧凑避免窄日志栏中途折行。
	cfg.ValueTargets["优越代码伤害增加"] = 60
	wantPending := strings.Join([]string{
		"【数值目标】",
		"优越代码伤害增加（4条）差16档",
		"  44/60档（94.2% / ≈116.6%）",
	}, "\n")
	if msg := buildValueProgressMessage(parts, cfg); msg != wantPending {
		t.Fatalf("未达标时进度摘要不匹配：\ngot:\n%s\nwant:\n%s", msg, wantPending)
	}

	// 多项目标时按官方顺序分行展示，每行不超过窄日志栏安全宽度（30 等宽列）。
	for i, part := range equipmentParts {
		p := parts[part]
		if i == 0 {
			p.Slots[1] = valueTestSlot("最大装弹数增加", 3, LockNone)
		}
		p.Slots[2] = valueTestSlot("攻击力增加", 6, LockNone) // 4 条合计 24 档
		parts[part] = p
	}
	leg := parts["腿部"]
	leg.Slots[2] = valueTestSlot("攻击力增加", 7, LockNone) // 6+6+6+7 = 25 档（33.9%）
	parts["腿部"] = leg
	cfg.ValueTargets = valueTarget{
		"优越代码伤害增加": 36,
		"最大装弹数增加":  3,
		"攻击力增加":    36,
	}
	multiMsg := buildValueProgressMessage(parts, cfg)
	for _, want := range []string{
		"优越代码伤害增加（4条）已达标",
		"  44/36档（94.2% / ≈83.0%）",
		"最大装弹数增加（1条）已达标",
		"  3/3档（36.1% / ≈36.1%）",
		"攻击力增加（4条）差11档",
		"  25/36档（33.9% / ≈41.6%）",
	} {
		if !strings.Contains(multiMsg, want) {
			t.Fatalf("多项目标缺少 %q：\n%s", want, multiMsg)
		}
	}
	for _, line := range strings.Split(multiMsg, "\n") {
		if cols := displayWidth(line); cols > 30 {
			t.Fatalf("日志行过长（%d 列 > 30）：%q", cols, line)
		}
	}

	// 单件模式同样按两行输出且宽度安全。
	singleCfg := carrierConfig{
		Operation:    rerollOperationValue,
		Mode:         rerollModeSingle,
		Part:         "头部",
		ValueTargets: valueTarget{"优越代码伤害增加": 13, "攻击力增加": 6},
	}
	singleMsg := buildValueProgressMessage(parts, singleCfg)
	wantSingle := strings.Join([]string{
		"【数值目标】",
		"优越代码伤害增加（1条）差2档",
		"  11/13档（23.6% / ≈26.4%）",
		"攻击力增加（1条）已达标",
		"  6/6档（8.3% / ≈8.3%）",
	}, "\n")
	if singleMsg != wantSingle {
		t.Fatalf("单件模式进度摘要不匹配：\ngot:\n%s\nwant:\n%s", singleMsg, wantSingle)
	}

	// 目标超过可达上限（条目数不足）时不输出进度，交由错误路径提示。
	parts["腿部"] = partScan{}
	cfg.ValueTargets = valueTarget{"优越代码伤害增加": 50}
	if msg := buildValueProgressMessage(parts, cfg); msg != "" {
		t.Fatalf("不可达目标不应输出进度：\n%s", msg)
	}
}

func displayWidth(s string) int {
	cols := 0
	for _, r := range s {
		if r <= 0x7f {
			cols++
		} else {
			cols += 2
		}
	}
	return cols
}
