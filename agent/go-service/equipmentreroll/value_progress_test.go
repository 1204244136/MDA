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
	for _, want := range []string{
		"【数值目标】",
		"优越代码伤害增加：4 条",
		"当前 44 档（94.2%）",
		"目标 34 档（≈80.2%）",
		"已达标",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("进度摘要缺少 %q：\n%s", want, msg)
		}
	}

	// 目标高于当前：给出还差多少档。
	cfg.ValueTargets["优越代码伤害增加"] = 60
	if msg := buildValueProgressMessage(parts, cfg); !strings.Contains(msg, "还差 16 档") {
		t.Fatalf("未达标时应给出差距：\n%s", msg)
	}

	// 目标超过可达上限（条目数不足）时不输出进度，交由错误路径提示。
	parts["腿部"] = partScan{}
	cfg.ValueTargets["优越代码伤害增加"] = 50
	if msg := buildValueProgressMessage(parts, cfg); msg != "" {
		t.Fatalf("不可达目标不应输出进度：\n%s", msg)
	}
}
