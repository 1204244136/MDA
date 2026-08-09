package membership

import (
	"sync"
	"time"

	"github.com/1204244136/MDA/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type RuntimeTracker struct {
	mu             sync.Mutex
	active         bool
	taskID         uint64
	entry          string
	last           time.Time
	multiplier     quotaMultiplier
	route          quotaRoute
	status         *MembershipStatus
	generation     uint64
	realNs         int64
	chargedSeconds int64
	stopCh         chan struct{}
	stopped        bool
	stopPosted     bool
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

func (t *RuntimeTracker) consumeBillableSeconds(delta time.Duration, flush bool) int64 {
	if delta < 0 {
		delta = 0
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	return t.consumeBillableSecondsLocked(delta, flush)
}

func (t *RuntimeTracker) consumeBillableSecondsLocked(delta time.Duration, flush bool) int64 {
	if delta < 0 {
		delta = 0
	}

	t.realNs += delta.Nanoseconds()
	billableNs := t.multiplier.billableDuration(time.Duration(t.realNs)).Nanoseconds()
	seconds := billableNs / int64(time.Second)
	if flush && billableNs%int64(time.Second) > 0 {
		seconds++
	}
	if seconds <= t.chargedSeconds {
		return 0
	}
	deltaSeconds := seconds - t.chargedSeconds
	t.chargedSeconds = seconds
	return deltaSeconds
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
	if !t.takePendingStop() {
		return
	}

	// Keep Agent proxy calls inside MaaFramework's callback dispatch lifetime
	// instead of retaining a callback handle in the quota timer goroutine.
	ctx.GetTasker().PostStop()
}

func (t *RuntimeTracker) takePendingStop() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
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

	multiplier := multiplierForEntry(detail.Entry)

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
	t.chargedSeconds = 0
	t.stopCh = make(chan struct{})
	t.stopped = false
	t.stopPosted = false
	generation := t.generation
	stopCh := t.stopCh
	t.mu.Unlock()

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
	stopCh := t.stopCh
	realDelta := time.Since(t.last)
	billableSeconds := t.consumeBillableSecondsLocked(realDelta, true)
	t.active = false
	t.status = nil
	t.stopCh = nil
	t.generation++
	close(stopCh)
	t.mu.Unlock()

	if status == nil {
		status = GetMembershipStatus()
	}
	if _, err := AddQuotaRouteUsageSeconds(status, route, billableSeconds); err != nil {
		log.Warn().Err(err).Msg("RuntimeTracker: failed to flush final quota usage")
	}
	log.Debug().
		Int64("real_seconds", int64(realDelta/time.Second)).
		Int64("billable_seconds", billableSeconds).
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
	delta := now.Sub(t.last)
	t.last = now
	taskID := t.taskID
	entry := t.entry
	multiplier := t.multiplier
	alreadyStopped := t.stopped
	billableSeconds := t.consumeBillableSecondsLocked(delta, false)
	t.mu.Unlock()

	snapshot, exhausted, err := addQuotaRouteUsageSeconds(status, route, billableSeconds)
	if err != nil {
		log.Warn().Err(err).Msg("RuntimeTracker: failed to record quota usage")
		t.requestStop(generation)
		return QuotaSnapshot{}, true
	}

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

func printMembershipVerificationUnavailable() {
	maafocus.PrintLargeContentTrimNewline(formatMembershipVerificationUnavailableMessage())
}
