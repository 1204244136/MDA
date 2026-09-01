package membership

import (
	"sync"
	"time"

	"github.com/1204244136/MDA/agent/go-service/pkg/i18n"
	"github.com/1204244136/MDA/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type RuntimeTracker struct {
	mu         sync.Mutex
	active     bool
	taskID     uint64
	entry      string
	last       time.Time
	multiplier quotaMultiplier
	route      quotaRoute
	status     *MembershipStatus
	generation uint64
	realNs     int64
	stopCh     chan struct{}
	stopped    bool
	stopPosted bool
	lease      *runtimeTrackingLease
}

var _ maa.TaskerEventSink = &RuntimeTracker{}
var _ maa.ContextEventSink = &RuntimeTracker{}

const (
	quotaTickMinInterval = 5 * time.Second
	quotaTickMaxInterval = 60 * time.Second
)

func (t *RuntimeTracker) OnTaskerTask(tasker *maa.Tasker, event maa.EventStatus, detail maa.TaskerTaskDetail) {
	if detail.Entry == "MaaTaskerPostStop" {
		return
	}

	switch event {
	case maa.EventStatusStarting:
		t.start(tasker, detail)
	case maa.EventStatusSucceeded, maa.EventStatusFailed:
		t.finish()
	}
}

// takeRealSecondsLocked 从已累积的 realNs 中取出可计费的实际秒数。
// flush 为 true 时不足 1 秒也按 1 秒结算并清零；否则只取整秒，余数保留到下次。
func (t *RuntimeTracker) takeRealSecondsLocked(flush bool) int64 {
	if t.realNs <= 0 {
		return 0
	}
	if flush {
		seconds := (t.realNs + int64(time.Second) - 1) / int64(time.Second)
		t.realNs = 0
		return seconds
	}
	seconds := t.realNs / int64(time.Second)
	if seconds > 0 {
		t.realNs -= seconds * int64(time.Second)
	}
	return seconds
}

func (t *RuntimeTracker) OnNodePipelineNode(ctx *maa.Context, event maa.EventStatus, detail maa.NodePipelineNodeDetail) {
	t.postPendingStop(ctx)
}

func (t *RuntimeTracker) OnNodeRecognitionNode(ctx *maa.Context, event maa.EventStatus, detail maa.NodeRecognitionNodeDetail) {
	t.postPendingStop(ctx)
}

func (t *RuntimeTracker) OnNodeActionNode(ctx *maa.Context, event maa.EventStatus, detail maa.NodeActionNodeDetail) {
	t.postPendingStop(ctx)
}

func (t *RuntimeTracker) OnNodeNextList(ctx *maa.Context, event maa.EventStatus, detail maa.NodeNextListDetail) {
	t.postPendingStop(ctx)
}

func (t *RuntimeTracker) OnNodeRecognition(ctx *maa.Context, event maa.EventStatus, detail maa.NodeRecognitionDetail) {
	t.postPendingStop(ctx)
}

func (t *RuntimeTracker) OnNodeAction(ctx *maa.Context, event maa.EventStatus, detail maa.NodeActionDetail) {
	t.postPendingStop(ctx)
}

func (t *RuntimeTracker) postPendingStop(ctx *maa.Context) {
	// 保持 Agent 代理调用在 MaaFramework 的回调分发生命周期内，
	// 而不是在额度计时器 goroutine 中保留回调句柄。
	// 仅在成功标记停止已发布时才继续，确保 PostStop 只被调用一次且仅在回调线程内。
	if !t.takePendingStop() {
		return
	}

	tasker := ctx.GetTasker()
	if tasker == nil {
		log.Warn().Msg("RuntimeTracker: cannot post stop, tasker is nil")
		return
	}

	// 此调用发生在 MaaFramework 回调分发线程内，
	// 符合 Agent 代理 FFI 契约要求。
	tasker.PostStop()
}

func (t *RuntimeTracker) takePendingStop() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	// 确保仍处于活动状态，且停止已请求但尚未发布。
	// 防止在停止请求和此回调之间任务可能已完成或重启的竞争条件。
	if !t.active || !t.stopped || t.stopPosted {
		return false
	}
	t.stopPosted = true
	return true
}

func (t *RuntimeTracker) requestStop(generation uint64) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.active || t.generation != generation || t.stopped {
		return false
	}
	t.stopped = true
	return true
}

func (t *RuntimeTracker) start(tasker *maa.Tasker, detail maa.TaskerTaskDetail) {
	t.finish()
	if isQuotaExemptEntry(detail.Entry) {
		log.Info().
			Uint64("task_id", detail.TaskID).
			Str("entry", detail.Entry).
			Msg("RuntimeTracker: quota-exempt task skipped")
		return
	}

	lease, acquired, err := tryAcquireRuntimeTrackingLease(detail)
	if err != nil {
		log.Error().
			Err(err).
			Uint64("task_id", detail.TaskID).
			Str("entry", detail.Entry).
			Str("tasker_uuid", detail.UUID).
			Msg("RuntimeTracker: failed to acquire runtime tracking lease")
		tasker.PostStop()
		return
	}
	if !acquired {
		log.Warn().
			Uint64("task_id", detail.TaskID).
			Str("entry", detail.Entry).
			Str("tasker_uuid", detail.UUID).
			Msg("RuntimeTracker: duplicate quota tracker suppressed")
		return
	}
	keepLease := false
	defer func() {
		if keepLease {
			return
		}
		if err := lease.Release(); err != nil {
			log.Warn().
				Err(err).
				Uint64("task_id", detail.TaskID).
				Str("entry", detail.Entry).
				Msg("RuntimeTracker: failed to release unused runtime tracking lease")
		}
	}()

	status := GetMembershipStatus()
	if status.VerificationUnavailable {
		printMembershipVerificationUnavailable()
	}
	route := quotaRouteForEntry(detail.Entry)
	snapshot, ok, err := EnsureQuotaRouteAvailable(status, route)
	if err != nil {
		log.Warn().Err(err).Msg("RuntimeTracker: failed to check quota at task start")
	}
	if !ok {
		printQuotaExhausted(snapshot)
		tasker.PostStop()
		return
	}

	multiplier := multiplierForEntry(detail.Entry, snapshot.SpecialRemainingSeconds > 0)

	now := time.Now()

	t.mu.Lock()
	t.active = true
	t.taskID = detail.TaskID
	t.entry = detail.Entry
	t.last = now
	t.multiplier = multiplier
	t.route = route
	t.status = status
	t.generation++
	t.realNs = 0
	t.stopCh = make(chan struct{})
	t.stopped = false
	t.stopPosted = false
	t.lease = lease
	generation := t.generation
	stopCh := t.stopCh
	t.mu.Unlock()
	keepLease = true

	log.Info().
		Uint64("task_id", detail.TaskID).
		Str("entry", detail.Entry).
		Str("quota_route", string(route)).
		Str("quota_pool", string(snapshot.Pool)).
		Int64("remaining_seconds", snapshot.RemainingSeconds).
		Int64("base_multiplier_permille", multiplier.BasePermille).
		Int64("extra_multiplier_permille", multiplier.ExtraPermille).
		Int64("total_multiplier_permille", multiplier.totalPermille()).
		Str("multiplier_reason", multiplier.Reason).
		Bool("unlimited_runtime", snapshot.UnlimitedRuntime).
		Msg("RuntimeTracker: started quota tracking")
	if isHighConsumptionEntry(detail.Entry) && snapshot.SpecialRemainingSeconds <= 0 {
		log.Info().
			Uint64("task_id", detail.TaskID).
			Str("entry", detail.Entry).
			Int("quota_multiplier", 5).
			Msg("RuntimeTracker: high consumption task without special quota is 5x")
	}

	if snapshot.UnlimitedRuntime {
		return
	}

	go t.tick(status, route, generation, snapshot.RemainingSeconds, stopCh)
}

func (t *RuntimeTracker) finish() {
	t.mu.Lock()
	if !t.active {
		t.mu.Unlock()
		return
	}
	multiplier := t.multiplier
	route := t.route
	status := t.status
	entry := t.entry
	taskID := t.taskID
	stopCh := t.stopCh
	lease := t.lease
	realDelta := time.Since(t.last)
	t.realNs += realDelta.Nanoseconds()
	realSeconds := t.takeRealSecondsLocked(true)
	t.active = false
	t.status = nil
	t.stopCh = nil
	t.lease = nil
	t.generation++
	close(stopCh)
	t.mu.Unlock()

	if status == nil {
		status = GetMembershipStatus()
	}
	if realSeconds > 0 {
		_, currentMultiplier, _, err := addQuotaRouteUsageRealSeconds(status, entry, route, realSeconds, true)
		if err != nil {
			log.Warn().Err(err).Msg("RuntimeTracker: failed to flush final quota usage")
		} else {
			multiplier = currentMultiplier
		}
	}
	if err := lease.Release(); err != nil {
		log.Warn().
			Err(err).
			Uint64("task_id", taskID).
			Str("entry", entry).
			Msg("RuntimeTracker: failed to release runtime tracking lease")
	}
	log.Debug().
		Int64("real_seconds", int64(realDelta/time.Second)).
		Int64("billable_seconds", realSeconds).
		Str("quota_route", string(route)).
		Int64("base_multiplier_permille", multiplier.BasePermille).
		Int64("extra_multiplier_permille", multiplier.ExtraPermille).
		Int64("total_multiplier_permille", multiplier.totalPermille()).
		Str("multiplier_reason", multiplier.Reason).
		Msg("RuntimeTracker: final quota usage flushed")
}

func (t *RuntimeTracker) tick(status *MembershipStatus, route quotaRoute, generation uint64, remainingSeconds int64, stopCh <-chan struct{}) {
	for {
		timer := time.NewTimer(nextQuotaTickInterval(remainingSeconds))
		select {
		case <-timer.C:
			snapshot, done := t.consumeTick(status, route, generation)
			if done {
				return
			}
			remainingSeconds = snapshot.RemainingSeconds
		case <-stopCh:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return
		}
	}
}

func nextQuotaTickInterval(remainingSeconds int64) time.Duration {
	if remainingSeconds <= 0 {
		return quotaTickMinInterval
	}
	interval := time.Duration(remainingSeconds) * time.Second
	if interval < quotaTickMinInterval {
		return quotaTickMinInterval
	}
	if interval > quotaTickMaxInterval {
		return quotaTickMaxInterval
	}
	return interval
}

func (t *RuntimeTracker) consumeTick(status *MembershipStatus, route quotaRoute, generation uint64) (QuotaSnapshot, bool) {
	now := time.Now()

	t.mu.Lock()
	if !t.active || t.generation != generation {
		t.mu.Unlock()
		return QuotaSnapshot{}, true
	}
	taskID := t.taskID
	entry := t.entry
	delta := now.Sub(t.last)
	t.last = now
	t.realNs += delta.Nanoseconds()
	realSeconds := t.takeRealSecondsLocked(false)
	oldMultiplier := t.multiplier
	alreadyStopped := t.stopped
	t.mu.Unlock()

	snapshot, multiplier, exhausted, err := addQuotaRouteUsageRealSeconds(status, entry, route, realSeconds, false)
	if err != nil {
		log.Warn().Err(err).Msg("RuntimeTracker: failed to record quota usage")
		t.requestStop(generation)
		return QuotaSnapshot{}, true
	}

	if multiplier.totalPermille() > multiplierScale && oldMultiplier.totalPermille() <= multiplierScale {
		printNoSpecialQuota5x()
	}

	t.mu.Lock()
	t.multiplier = multiplier
	t.mu.Unlock()

	billableSeconds := multiplier.billableSecondsFromReal(realSeconds, false)
	log.Debug().
		Uint64("task_id", taskID).
		Str("entry", entry).
		Str("quota_route", string(route)).
		Str("quota_pool", string(snapshot.Pool)).
		Int64("real_seconds", int64(delta/time.Second)).
		Int64("billable_seconds", billableSeconds).
		Int64("base_multiplier_permille", multiplier.BasePermille).
		Int64("extra_multiplier_permille", multiplier.ExtraPermille).
		Int64("total_multiplier_permille", multiplier.totalPermille()).
		Str("multiplier_reason", multiplier.Reason).
		Int64("special_remaining_seconds", snapshot.SpecialRemainingSeconds).
		Int64("regular_remaining_seconds", snapshot.RegularRemainingSeconds).
		Int64("used_seconds", snapshot.UsedSeconds).
		Int64("remaining_seconds", snapshot.RemainingSeconds).
		Msg("RuntimeTracker: quota usage recorded")

	if exhausted {
		// Arm the pending stop instead of invoking PostStop directly from the
		// timer goroutine: Agent proxy calls must stay inside MaaFramework's
		// callback dispatch lifetime (see postPendingStop). The actual PostStop
		// is delivered by the next Node callback via takePendingStop.
		if t.requestStop(generation) {
			printQuotaExhausted(snapshot)
			log.Warn().
				Uint64("task_id", taskID).
				Str("entry", entry).
				Int64("daily_limit_seconds", snapshot.RegularLimitSeconds).
				Msg("RuntimeTracker: quota exhausted, terminating task")
		}
		return snapshot, true
	}

	if snapshot.RemainingSeconds > 0 || alreadyStopped {
		return snapshot, false
	}

	if t.requestStop(generation) {
		printQuotaExhausted(snapshot)
	}
	return snapshot, false
}

func printQuotaExhausted(snapshot QuotaSnapshot) {
	maafocus.PrintLargeContentTrimNewline(formatQuotaDeniedMessage(snapshot))
}

func printNoSpecialQuota5x() {
	maafocus.PrintLargeContentTrimNewline(i18n.T("tasker.membership_check.no_special_quota_5x_multiplier"))
}

func printMembershipVerificationUnavailable() {
	maafocus.PrintLargeContentTrimNewline(formatMembershipVerificationUnavailableMessage())
}
