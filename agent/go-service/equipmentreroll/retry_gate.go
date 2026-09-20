package equipmentreroll

// 本文件实现自循环兜底节点的“重试闸门”。
//
// 背景：Pipeline 里常见 “点击后没进展就再点自己”（next 里含自身）的兜底写法。
// 这类节点只要识别持续成功（例如目标界面根本没打开、按钮点不动），就会无限循环点击。
//
// 为什么不用 max_hit：max_hit 是**任务级累计**命中次数，而洗词条会反复锁定 / 反复效果变更，
// 同一个节点在一局里被命中几十次属于正常，任何够用的上限都会误伤正常流程。
//
// 因此这里按“时间窗口内的连续命中次数”判定原地打转：同一闸门在 window_ms 内命中超过
// limit 次即认为没有进展，改路由到 give_up；否则路由回 retry 再试一次。窗口滑动天然自重置，
// 正常流程里两次命中通常隔着一次完整交互（> 数秒），不会被误判。

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// EquipmentRerollRetryGateAction 是自循环兜底节点的重试闸门：按时间窗口限制连续重试次数。
type EquipmentRerollRetryGateAction struct{}

var _ maa.CustomActionRunner = &EquipmentRerollRetryGateAction{}

type retryGateParam struct {
	// Key 是闸门标识；留空时用节点名。同一 key 共享计数窗口。
	Key string `json:"key"`
	// Retry 是未超限时的重试目标节点。
	Retry string `json:"retry"`
	// Next 保留成功状态、原按钮与等待闸门，避免旧按钮消失后丢失成功出口。
	Next []string `json:"next"`
	// GiveUp 是超限后的放弃目标节点；留空表示直接让当前流程失败。
	GiveUp string `json:"give_up"`
	// Limit 是窗口内允许的连续命中次数，默认 3。
	Limit int `json:"limit"`
	// WindowMs 是计数窗口毫秒数，默认 5000。
	WindowMs int `json:"window_ms"`
}

type retryGateState struct {
	count int
	last  time.Time
}

var (
	retryGateMu sync.Mutex
	retryGate   = make(map[string]retryGateState)
)

const (
	retryGateDefaultLimit    = 3
	retryGateDefaultWindowMs = 5000
)

func (a *EquipmentRerollRetryGateAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		return false
	}
	var params retryGateParam
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
		log.Error().Err(err).Str("component", "EquipmentReroll").Msg("failed to parse retry gate param")
		return false
	}
	key := params.Key
	if key == "" {
		key = arg.CurrentTaskName
	}
	limit := params.Limit
	if limit <= 0 {
		limit = retryGateDefaultLimit
	}
	windowMs := params.WindowMs
	if windowMs <= 0 {
		windowMs = retryGateDefaultWindowMs
	}
	window := time.Duration(windowMs) * time.Millisecond

	stateKey := fmt.Sprintf("%d|%s", arg.TaskID, key)
	now := time.Now()

	retryGateMu.Lock()
	state := retryGate[stateKey]
	if now.Sub(state.last) > window {
		state.count = 0
	}
	state.count++
	state.last = now
	count := state.count
	retryGate[stateKey] = state
	retryGateMu.Unlock()

	target := params.Retry
	if count > limit {
		target = params.GiveUp
		log.Warn().
			Str("component", "EquipmentReroll").
			Int64("task_id", arg.TaskID).
			Str("gate", key).
			Int("count", count).
			Int("limit", limit).
			Int("window_ms", windowMs).
			Str("give_up", params.GiveUp).
			Msg("retry gate tripped; stop looping")
	}
	if target == "" {
		// 没有放弃目标时让当前节点失败，交由 Maa 判定任务失败，避免继续原地打转。
		log.Error().
			Str("component", "EquipmentReroll").
			Int64("task_id", arg.TaskID).
			Str("gate", key).
			Int("count", count).
			Msg("retry gate tripped without give_up target; fail the task")
		return false
	}
	next := retryGateNext(params, arg.CurrentTaskName, count > limit)
	if err := ctx.OverrideNext(arg.CurrentTaskName, next); err != nil {
		log.Error().Err(err).Str("component", "EquipmentReroll").Str("gate", key).Msg("failed to route retry gate")
		return false
	}
	return true
}

// clearRetryGates 清空闸门状态，避免跨任务串味。
func clearRetryGates() {
	retryGateMu.Lock()
	retryGate = make(map[string]retryGateState)
	retryGateMu.Unlock()
}

// resetTaskRetryGates 在进入新一轮或完成锁定时清除上一阶段的重试计数。
func resetTaskRetryGates(taskID int64) {
	retryGateMu.Lock()
	defer retryGateMu.Unlock()
	prefix := fmt.Sprintf("%d|", taskID)
	for key := range retryGate {
		if strings.HasPrefix(key, prefix) {
			delete(retryGate, key)
		}
	}
}

// retryGateNext 保留等待期间的全部页面出口；超限只允许显式失败恢复。
func retryGateNext(params retryGateParam, gate string, exhausted bool) []maa.NextItem {
	if exhausted {
		return []maa.NextItem{{Name: params.GiveUp}}
	}
	names := params.Next
	if len(names) == 0 {
		names = []string{params.Retry, gate}
	}
	next := make([]maa.NextItem, 0, len(names))
	for _, name := range names {
		next = append(next, maa.NextItem{Name: name})
	}
	return next
}
