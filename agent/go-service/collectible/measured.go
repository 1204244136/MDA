// measured.go 是概率表的读取侧：运行期读采集结果，缺项回落到 decide.go 里的内置锚点。
//
// 采集（写侧：采样门禁、快照、写盘）已于 2026-11-11 收尾时移出仓库，
// 存档见 debug/character-prep/collectible/removed-probe/；这里只留决策需要的那部分。

package collectible

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/1204244136/MDA/agent/go-service/pkg/resource"
	"github.com/rs/zerolog/log"
)

// probDataPath 是采集结果的落盘位置（相对仓库根目录；仓库内写不进去时退回临时目录）。
const probDataPath = "debug/character-prep/collectible/prob-data.json"

// measured 是采集到的概率：measured[grade][stage][tier] = 非常成功几率（0~1）。
type measured map[string]map[int]map[int]float64

// probPaths 返回采集文件的候选路径：仓库内优先，不可写再用临时目录。
func probPaths() []string {
	out := []string{}
	if root := findRepoRoot(); root != "" {
		out = append(out, filepath.Join(root, filepath.FromSlash(probDataPath)))
	}
	return append(out, filepath.Join(os.TempDir(), "collectible-prob-data.json"))
}

// resolveProbPath 返回首选落盘路径，仅用于日志展示。
func resolveProbPath() string {
	return probPaths()[0]
}

// findRepoRoot 先按 MaaFramework 上报的资源路径反推仓库根，
// 失败再用 CWD / 可执行文件目录向上找（MSE 插件的 CWD 是插件目录，靠前者才准）。
func findRepoRoot() string {
	if base := resource.GetResourceBase(); base != "" {
		dir := filepath.Clean(base)
		for i := 0; i < 3; i++ {
			if _, err := os.Stat(filepath.Join(dir, "debug")); err == nil {
				return dir
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	starts := []string{}
	if wd, err := os.Getwd(); err == nil {
		starts = append(starts, wd)
	}
	if exe, err := os.Executable(); err == nil {
		starts = append(starts, filepath.Dir(exe))
	}
	for _, start := range starts {
		dir := start
		for i := 0; i < 8; i++ {
			if _, err := os.Stat(filepath.Join(dir, "assets", "resource")); err == nil {
				return dir
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	return ""
}

// loadMeasured 读取采集文件；不存在时返回空表。
func loadMeasured() measured {
	return loadMeasuredFrom(probPaths())
}

// loadMeasuredFrom 在候选路径里挑「存在且最新」的那个读：
// 文件不存在说明该路径没写过，跳过；存在多个时取修改时间最新的——
// 首选路径写不进去时样本会落到备用路径，只认首选路径会把新样本读丢。
// 文件存在但解析不了就直接用内置锚点，不退到备用路径（那可能是更旧的样本）。
func loadMeasuredFrom(paths []string) measured {
	best := ""
	var bestMod time.Time
	for _, file := range paths {
		info, err := os.Stat(file)
		if err != nil {
			continue
		}
		if best == "" || info.ModTime().After(bestMod) {
			best, bestMod = file, info.ModTime()
		}
	}
	if best == "" {
		return measured{}
	}
	raw, err := os.ReadFile(best)
	if err != nil {
		log.Warn().Err(err).Str("component", "Collectible").Str("file", best).Msg("prob data unreadable")
		return measured{}
	}
	data := measured{}
	if err := json.Unmarshal(raw, &data); err != nil {
		log.Error().Err(err).Str("component", "Collectible").Str("file", best).Msg("prob data corrupt, use builtin anchors")
		return measured{}
	}
	return data
}

// gradeRank 决定等级在文件里的先后（R 在 SR 前）。
func gradeRank(g string) int {
	switch strings.ToUpper(g) {
	case "R":
		return 0
	case "SR":
		return 1
	default:
		return 2
	}
}

// marshalMeasured 手写序列化，保证阶段按数值升序。
// encoding/json 对 map[int] 的键是按字符串排序的，会把 2 排到 14 后面，这里必须自己排。
func marshalMeasured(data measured) []byte {
	grades := make([]string, 0, len(data))
	for g := range data {
		grades = append(grades, g)
	}
	sort.Slice(grades, func(i, j int) bool {
		ri, rj := gradeRank(grades[i]), gradeRank(grades[j])
		if ri != rj {
			return ri < rj
		}
		return grades[i] < grades[j]
	})

	var b strings.Builder
	b.WriteString("{\n")
	for gi, g := range grades {
		stages := make([]int, 0, len(data[g]))
		for st := range data[g] {
			stages = append(stages, st)
		}
		sort.Ints(stages)

		b.WriteString("  ")
		b.WriteString(strconv.Quote(g))
		b.WriteString(": {\n")
		for si, st := range stages {
			tiers := make([]int, 0, len(data[g][st]))
			for t := range data[g][st] {
				tiers = append(tiers, t)
			}
			sort.Ints(tiers)

			b.WriteString("    ")
			b.WriteString(strconv.Quote(strconv.Itoa(st)))
			b.WriteString(": {\n")
			for ti, t := range tiers {
				b.WriteString("      ")
				b.WriteString(strconv.Quote(strconv.Itoa(t)))
				b.WriteString(": ")
				b.WriteString(strconv.FormatFloat(data[g][st][t], 'f', -1, 64))
				if ti < len(tiers)-1 {
					b.WriteString(",")
				}
				b.WriteString("\n")
			}
			b.WriteString("    }")
			if si < len(stages)-1 {
				b.WriteString(",")
			}
			b.WriteString("\n")
		}
		b.WriteString("  }")
		if gi < len(grades)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString("}\n")
	return []byte(b.String())
}

// lookupMeasured 查某个（等级, 阶段, 档位）的概率。
func lookupMeasured(data measured, grade string, stage, tier int) (float64, bool) {
	if byStage, ok := data[grade]; ok {
		if byTier, ok := byStage[stage]; ok {
			if p, ok := byTier[tier]; ok {
				return p, true
			}
		}
	}
	return 0, false
}

// decisionDump 是决策诊断快照，写出到采集目录方便定位“为什么判材料不足”。
type decisionDump struct {
	Grade      string  `json:"grade"`
	Stage      int     `json:"stage"`
	Exp        int     `json:"exp"`
	PreviewExp int     `json:"preview_exp"`
	Held       [3]int  `json:"held"`
	Tier       int     `json:"tier"`
	Count      int     `json:"count"`
	Cost       float64 `json:"expected_cost"`
	Maxed      bool    `json:"maxed"`
	NoMatl     bool    `json:"no_material"`
}

// dumpDecision 把最近一次决策的输入输出写到采集目录；采集目录不可写时再写临时目录，
// 并把实际落点打进日志，便于确认文件到底在哪。
func dumpDecision(d decisionDump) {
	if math.IsInf(d.Cost, 0) || math.IsNaN(d.Cost) {
		d.Cost = -1 // json 序列化不支持 Inf/NaN，用 -1 表示“没有可用档位”
	}
	raw, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		log.Warn().Err(err).Str("component", "Collectible").Msg("decision dump marshal failed")
		return
	}
	raw = append(raw, '\n')
	targets := []string{
		filepath.Join(filepath.Dir(resolveProbPath()), "last-decision.json"),
		filepath.Join(os.TempDir(), "last-decision.json"),
	}
	for _, file := range targets {
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			log.Warn().Err(err).Str("component", "Collectible").Str("file", file).Msg("decision dump mkdir failed")
			continue
		}
		if err := os.WriteFile(file, raw, 0o644); err != nil {
			log.Warn().Err(err).Str("component", "Collectible").Str("file", file).Msg("decision dump write failed")
			continue
		}
		log.Info().Str("component", "Collectible").Str("file", file).Msg("decision dump saved")
		return
	}
}

// parsePercentText 把 "3.6%" 解析成 0.036。
func parsePercentText(text string) (float64, bool) {
	t := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(text), "%"))
	v, err := strconv.ParseFloat(t, 64)
	if err != nil {
		return 0, false
	}
	// 游戏界面只显示到 0.1%，直接 v/100 会带出 0.14400000000000002 这种二进制尾数；
	// 统一四舍五入到 4 位小数，落盘和日志都干净。
	return math.Round(v/100*1e4) / 1e4, true
}
