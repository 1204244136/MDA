package equipmentreroll

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

// 本文件实现洗词条任务"配置承载点"的读取。
//
// 两种模式（洗角色词条 / 洗单件词条）的全部用户选项都写在 EquipmentRerollLockNeed
// 节点的 attach 顶层键上：MaaFramework 对 attach 按 key 浅合并，多个 option 写不同
// 顶层键互不覆盖；而 custom_recognition_param 是整体替换，多个 option 写同一节点会
// 互相覆盖（参见 MaaFramework source/MaaFramework/Resource/PipelineParser.cpp）。
//
// attach 键约定：
//   - operation：      "effect"（默认）/ "value"，与 mode 正交；
//   - value_tier_<效果名>：数值目标最低 T 档（0 忽略 / 1~15 门槛）；
//   - mode：           "character"（洗角色词条，默认）/ "single"（洗单件词条）；
//   - part：           单件模式要洗的部位（"头部"/"臂部"/"身躯"/"腿部"）；
//   - want1/2/3：      单件模式的需求词条效果名（空串表示该行不需求）；
//   - slot1/2/3：      单件模式各需求词条的槽位限定（0=任意，1-3=限定第 N 号槽）；
//   - quota_<效果名>： 角色模式每个效果的配额（-1 禁止 / 0 不要求 / 1-4 需求数）。
//   - enabled2/3：     是否同时接受额外方案，缺失时关闭；仅洗角色效果时生效。
//   - quota2/3_<效果名>：额外方案各自的配额，不能合并为同一个嵌套对象。
//
// 模式判定只认 attach.mode 这一个来源。不要再用"单件目标是否为空"反推模式：
// 一个非法的单件配置（例如同一效果被重复选择）会让目标解析为空，从而静默退化成
// 角色模式配额去洗，用户完全无从察觉。
//
// 文档索引：docs/zh_cn/nikke/EquipmentReroll/洗词条策略与Agent逻辑.md（§9.2 配置承载点）

const (
	// carrierNode 是承载全部任务选项的节点名。
	carrierNode = "EquipmentRerollLockNeed"
	// quotaAttachPrefix 是角色模式配额 attach 键的前缀。
	quotaAttachPrefix = "quota_"
)

// rerollMode 洗词条模式。
type rerollMode string

const (
	// rerollModeCharacter 洗角色词条：四件装备共享一份全局配额。
	rerollModeCharacter rerollMode = "character"
	// rerollModeSingle 洗单件词条：只洗用户选定的一件，词条可限定落槽。
	rerollModeSingle rerollMode = "single"
)

// carrierConfig 是从承载点解析出的任务配置。
type carrierConfig struct {
	// Operation 与 Mode 正交；空值兼容已有洗效果配置。
	Operation rerollOperation
	// ValueTargets 是效果名 -> 最低数值档位；不复用效果配额和单件槽位目标。
	ValueTargets  valueTarget
	ConfigProblem string
	// Mode 洗词条模式；attach.mode 缺失时按 rerollModeCharacter 处理（兼容旧配置）。
	Mode rerollMode
	// Part 单件模式要洗的部位；角色模式为空串。
	Part string
	// Target 单件模式的词条目标（effect -> 槽位限定）。
	Target singleTarget
	// TargetProblem 非空表示单件需求词条行非法，内容为可记录的英文原因码。
	TargetProblem string
	// Quota 角色模式的全局配额。
	Quota map[string]int
	// AlternativeQuotas 与 AlternativeEnabled 分别保存方案 2、3；关闭时残留字段不参与解析或判定。
	AlternativeQuotas  [2]map[string]int
	AlternativeEnabled [2]bool
	// GoalProblem 保留完整原因供日志使用，方案编号用于本地化用户提示。
	GoalProblem      string
	GoalProblemIndex int
	quotaExplicit    bool
}

// isSingle 判断是否为单件模式。
func (c carrierConfig) isSingle() bool { return c.Mode == rerollModeSingle }

// singleTargetOK 判断单件目标是否可直接用于决策。
func (c carrierConfig) singleTargetOK() bool { return c.TargetProblem == "" }

// resolveQuota 返回方案 1；只有完全缺少配置时才使用调用节点的默认值。
// 显式选择全为“不要求”必须保留为空并报错，不能静默变成默认方案。
func (c carrierConfig) resolveQuota(nodeDefault map[string]int) map[string]int {
	if c.quotaExplicit || len(c.Quota) > 0 {
		return normalizeQuota(c.Quota)
	}
	return normalizeQuota(nodeDefault)
}

// resolveGoalQuotas 按方案编号返回已启用的完整目标，方案之间是 OR 而非逐效果合并。
func (c carrierConfig) resolveGoalQuotas(nodeDefault map[string]int) []map[string]int {
	goals := []map[string]int{c.resolveQuota(nodeDefault)}
	if c.isValue() || c.isSingle() {
		return goals
	}
	for i, enabled := range c.AlternativeEnabled {
		if enabled {
			goals = append(goals, normalizeQuota(c.AlternativeQuotas[i]))
		}
	}
	return goals
}

// validateGoalQuotas 校验每个已启用方案；不能让空方案把 OR 目标变成无条件成功。
func (c carrierConfig) validateGoalQuotas(nodeDefault map[string]int) error {
	if err := c.validateOperation(); err != nil {
		return err
	}
	if c.isValue() || c.isSingle() {
		return nil
	}
	if problem := goalQuotaProblem(c.resolveQuota(nodeDefault)); problem != "" {
		return &goalQuotaError{Number: 1, Problem: problem}
	}
	for i, enabled := range c.AlternativeEnabled {
		if !enabled {
			continue
		}
		if problem := goalQuotaProblem(c.AlternativeQuotas[i]); problem != "" {
			return &goalQuotaError{Number: i + 2, Problem: problem}
		}
	}
	return nil
}

func goalQuotaProblem(quota map[string]int) string {
	keys := make([]string, 0, len(quota))
	for effect := range quota {
		keys = append(keys, effect)
	}
	sort.Strings(keys)
	for _, effect := range keys {
		if !isOfficialEffect(strings.TrimSpace(effect)) {
			return fmt.Sprintf("unknown effect %q", effect)
		}
		if quota[effect] < -1 || quota[effect] > len(equipmentParts) {
			return fmt.Sprintf("invalid count for %q: %d", effect, quota[effect])
		}
	}
	if quotaTotal(quota) == 0 {
		return "empty quota"
	}
	if !quotaIsValid(quota) {
		return "total quota exceeds 12 affixes"
	}
	return ""
}

func (c *carrierConfig) setGoalProblem(number int, problem string) {
	if c.GoalProblem == "" && problem != "" {
		c.GoalProblemIndex, c.GoalProblem = number, problem
	}
}

// loadCarrierConfig 读取一次承载点节点 JSON 并解析出完整配置。
//
// 识别器/动作在一次 Run 内应只调用一次并把结果往下传：每次调用都要走一次
// GetNodeJSON（FFI）加一次全量 json 解析，而识别器是按帧调用的。
func loadCarrierConfig(ctx *maa.Context) carrierConfig {
	cfg := carrierConfig{Mode: rerollModeCharacter}
	if ctx == nil {
		return cfg
	}
	raw, err := ctx.GetNodeJSON(carrierNode)
	if err != nil {
		cfg.ConfigProblem = err.Error()
		return cfg
	}
	return parseCarrierConfig(raw)
}

func parseCarrierConfig(raw string) carrierConfig {
	cfg := carrierConfig{Mode: rerollModeCharacter}
	// attach 用 RawMessage 承接：quota_<效果名> 是动态键，无法用固定字段表达。
	var data struct {
		Attach      map[string]json.RawMessage `json:"attach"`
		Recognition struct {
			Param struct {
				CustomRecognitionParam struct {
					GlobalQuota map[string]int `json:"global_quota"`
				} `json:"custom_recognition_param"`
			} `json:"param"`
		} `json:"recognition"`
	}
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		cfg.ConfigProblem = err.Error()
		return cfg
	}
	if operation, exists := data.Attach["operation"]; exists {
		if err := json.Unmarshal(operation, &cfg.Operation); err != nil {
			cfg.ConfigProblem = "invalid operation type"
		}
	}
	cfg.ValueTargets = make(valueTarget)
	for key, value := range data.Attach {
		if !strings.HasPrefix(key, "value_tier_") {
			continue
		}
		var tier int
		if err := json.Unmarshal(value, &tier); err != nil {
			cfg.ConfigProblem = fmt.Sprintf("invalid tier: %s", key)
			continue
		}
		if tier != 0 {
			cfg.ValueTargets[strings.TrimPrefix(key, "value_tier_")] = tier
		}
	}

	if rawMode, exists := data.Attach["mode"]; exists {
		var mode string
		if err := json.Unmarshal(rawMode, &mode); err != nil {
			cfg.ConfigProblem = "invalid mode type"
		} else if mode = strings.TrimSpace(mode); mode != "" {
			cfg.Mode = rerollMode(mode)
		}
	}
	cfg.Part = strings.TrimSpace(attachString(data.Attach, "part"))

	// 单件目标：want/slot 三行组装。组装失败（重复效果）与内容非法都记进 TargetProblem，
	// 由调用方按模式决定是报错结束还是忽略。
	target, ok := singleTargetFromRows(
		attachString(data.Attach, "want1"),
		attachString(data.Attach, "want2"),
		attachString(data.Attach, "want3"),
		attachInt(data.Attach, "slot1"),
		attachInt(data.Attach, "slot2"),
		attachInt(data.Attach, "slot3"),
	)
	cfg.Target = target
	if ok {
		cfg.TargetProblem = singleTargetProblem(target)
	} else {
		cfg.TargetProblem = singleTargetProblemDuplicatedAffix
	}

	// 角色配额保持顶层逐键承载；额外组只有开关启用时才读取其字段。
	quota, present, problem := parseQuotaAttach(data.Attach, quotaAttachPrefix)
	cfg.Quota, cfg.quotaExplicit = normalizeQuota(quota), present
	// 兜底：attach 未携带任何配额键时（旧客户端未应用选项），回落承载点自带的默认配额。
	if !present {
		cfg.Quota = normalizeQuota(data.Recognition.Param.CustomRecognitionParam.GlobalQuota)
	}
	if !cfg.isValue() && !cfg.isSingle() {
		cfg.setGoalProblem(1, problem)
		if present {
			cfg.setGoalProblem(1, goalQuotaProblem(quota))
		}
		for i := range cfg.AlternativeEnabled {
			number := i + 2
			rawEnabled, exists := data.Attach[fmt.Sprintf("enabled%d", number)]
			if !exists {
				continue
			}
			if err := json.Unmarshal(rawEnabled, &cfg.AlternativeEnabled[i]); err != nil || strings.TrimSpace(string(rawEnabled)) == "null" {
				cfg.setGoalProblem(number, "invalid enabled flag")
				continue
			}
			if !cfg.AlternativeEnabled[i] {
				continue
			}
			quota, _, problem := parseQuotaAttach(data.Attach, fmt.Sprintf("quota%d_", number))
			cfg.AlternativeQuotas[i] = normalizeQuota(quota)
			cfg.setGoalProblem(number, problem)
			cfg.setGoalProblem(number, goalQuotaProblem(quota))
		}
	}
	return cfg
}

func parseQuotaAttach(attach map[string]json.RawMessage, prefix string) (map[string]int, bool, string) {
	quota := make(map[string]int)
	present, problem := false, ""
	keys := make([]string, 0, len(attach))
	for key := range attach {
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		present = true
		var count int
		raw := attach[key]
		if err := json.Unmarshal(raw, &count); err != nil || strings.TrimSpace(string(raw)) == "null" {
			if problem == "" {
				problem = fmt.Sprintf("invalid quota type: %s", key)
			}
			continue
		}
		quota[strings.TrimSpace(strings.TrimPrefix(key, prefix))] = count
	}
	return quota, present, problem
}

// attachString 取 attach 顶层键的字符串值；缺失或类型不符时返回空串。
func attachString(attach map[string]json.RawMessage, key string) string {
	rawValue, ok := attach[key]
	if !ok {
		return ""
	}
	var value string
	if err := json.Unmarshal(rawValue, &value); err != nil {
		return ""
	}
	return value
}

// attachInt 取 attach 顶层键的整数值；缺失或类型不符时返回 0。
func attachInt(attach map[string]json.RawMessage, key string) int {
	rawValue, ok := attach[key]
	if !ok {
		return 0
	}
	var value int
	if err := json.Unmarshal(rawValue, &value); err != nil {
		return 0
	}
	return value
}
