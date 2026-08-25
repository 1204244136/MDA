package equipmentreroll

import (
	"encoding/json"
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
//   - mode：           "character"（洗角色词条，默认）/ "single"（洗单件词条）；
//   - part：           单件模式要洗的部位（"头部"/"臂部"/"身躯"/"腿部"）；
//   - want1/2/3：      单件模式的需求词条效果名（空串表示该行不需求）；
//   - slot1/2/3：      单件模式各需求词条的槽位限定（0=任意，1-3=限定第 N 号槽）；
//   - quota_<效果名>： 角色模式每个效果的配额（-1 禁止 / 0 不要求 / 1-4 需求数）。
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
}

// isSingle 判断是否为单件模式。
func (c carrierConfig) isSingle() bool { return c.Mode == rerollModeSingle }

// singleTargetOK 判断单件目标是否可直接用于决策。
func (c carrierConfig) singleTargetOK() bool { return c.TargetProblem == "" }

// resolveQuota 返回本次生效的角色配额：承载点优先，承载点没有正数配额时
// 回落到调用节点自带的 global_quota 默认值。
func (c carrierConfig) resolveQuota(nodeDefault map[string]int) map[string]int {
	if quotaTotal(c.Quota) > 0 {
		return c.Quota
	}
	return normalizeQuota(nodeDefault)
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
		return cfg
	}
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
		return cfg
	}

	if mode := strings.TrimSpace(attachString(data.Attach, "mode")); mode != "" {
		cfg.Mode = rerollMode(mode)
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

	// 角色配额：quota_<效果名> 动态顶层键，每个效果一个 select 写一个键。
	quota := make(map[string]int, len(data.Attach))
	for key, rawValue := range data.Attach {
		if !strings.HasPrefix(key, quotaAttachPrefix) {
			continue
		}
		var count int
		if err := json.Unmarshal(rawValue, &count); err != nil {
			continue
		}
		quota[strings.TrimPrefix(key, quotaAttachPrefix)] = count
	}
	cfg.Quota = normalizeQuota(quota)
	// 兜底：attach 未携带任何配额键时（旧客户端未应用选项），回落承载点自带的默认配额。
	if len(cfg.Quota) == 0 {
		cfg.Quota = normalizeQuota(data.Recognition.Param.CustomRecognitionParam.GlobalQuota)
	}
	return cfg
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
