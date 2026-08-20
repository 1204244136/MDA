package membership

import (
	"testing"
	"time"
)

func TestMultiplierForEntry(t *testing.T) {
	cases := []struct {
		entry    string
		isMember bool
		want     int64
	}{
		{entry: "SmallEventMain", isMember: false, want: 1000},
		{entry: "LargeEventMain", isMember: false, want: 1000},
		{entry: "MapPushingFlow", isMember: true, want: 1000},
		{entry: "MapPushingFlow", isMember: false, want: 5000},
		{entry: "EquipmentRerollMain", isMember: true, want: 1000},
		{entry: "EquipmentRerollMain", isMember: false, want: 5000},
		{entry: "DailyRewardsMain", isMember: false, want: 1000},
	}
	for _, test := range cases {
		if got := multiplierForEntry(test.entry, test.isMember).BasePermille; got != test.want {
			t.Fatalf("multiplierForEntry(%s, %t).BasePermille = %d, want %d", test.entry, test.isMember, got, test.want)
		}
	}
}

func TestBillableDuration(t *testing.T) {
	multiplier := quotaMultiplier{BasePermille: 3000, ExtraPermille: 1500}
	if got := multiplier.billableDuration(time.Minute); got != 270*time.Second {
		t.Fatalf("billableDuration() = %s, want 4m30s", got)
	}
}

func TestConsumeBillableSecondsKeepsFractionUntilFlush(t *testing.T) {
	tracker := &RuntimeTracker{
		multiplier: quotaMultiplier{
			BasePermille:  multiplierScale,
			ExtraPermille: 1500,
		},
	}

	if got := tracker.consumeBillableSeconds(500*time.Millisecond, false); got != 0 {
		t.Fatalf("first consumeBillableSeconds() = %d, want 0", got)
	}
	if got := tracker.consumeBillableSeconds(500*time.Millisecond, false); got != 1 {
		t.Fatalf("second consumeBillableSeconds() = %d, want 1", got)
	}
	if got := tracker.consumeBillableSeconds(0, true); got != 1 {
		t.Fatalf("flush consumeBillableSeconds() = %d, want 1", got)
	}
}

func TestConsumeBillableSecondsCeilsOnFlush(t *testing.T) {
	tracker := &RuntimeTracker{
		multiplier: quotaMultiplier{
			BasePermille:  multiplierScale,
			ExtraPermille: 1500,
		},
	}

	if got := tracker.consumeBillableSeconds(500*time.Millisecond, true); got != 1 {
		t.Fatalf("flush consumeBillableSeconds() = %d, want 1", got)
	}
}

func TestConsumeTickIgnoresStaleGeneration(t *testing.T) {
	isolateQuotaState(t)
	status := testStatus(10, "device-a")
	tracker := &RuntimeTracker{
		active:     true,
		generation: 2,
		last:       time.Now().Add(-time.Minute),
		multiplier: quotaMultiplier{
			BasePermille:  multiplierScale,
			ExtraPermille: multiplierScale,
		},
	}

	if _, done := tracker.consumeTick(status, quotaRouteRegular, 1); !done {
		t.Fatalf("consumeTick() with stale generation should stop")
	}

	snapshot, err := GetQuotaSnapshot(status, quotaPoolRegularDaily)
	if err != nil {
		t.Fatalf("GetQuotaSnapshot() failed: %v", err)
	}
	if snapshot.UsedSeconds != 0 {
		t.Fatalf("UsedSeconds = %d, want 0", snapshot.UsedSeconds)
	}
}

func TestConsumeTickTerminatesWhenQuotaExhausted(t *testing.T) {
	isolateQuotaState(t)
	status := testStatus(10, "device-a")
	if _, exceeded, err := addQuotaRouteUsageSeconds(status, quotaRouteRegular, 599); err != nil {
		t.Fatalf("addQuotaRouteUsageSeconds() failed: %v", err)
	} else if exceeded {
		t.Fatal("consuming part of the quota should not report it exhausted")
	}

	tracker := &RuntimeTracker{
		active:     true,
		generation: 3,
		last:       time.Now().Add(-2 * time.Second),
		multiplier: quotaMultiplier{
			BasePermille:  multiplierScale,
			ExtraPermille: multiplierScale,
		},
	}

	snapshot, done := tracker.consumeTick(status, quotaRouteRegular, 3)
	if !done {
		t.Fatal("consumeTick() should stop tracking after the quota is exhausted")
	}
	if snapshot.RegularUsedSeconds != 600 {
		t.Fatalf("RegularUsedSeconds = %d, want 600", snapshot.RegularUsedSeconds)
	}
	if !tracker.stopped {
		t.Fatal("stopped = false, want true (pending stop should be armed)")
	}
	// PostStop must be deferred to MaaFramework's callback dispatch lifetime,
	// not invoked from the quota timer goroutine.
	if tracker.stopPosted {
		t.Fatal("stopPosted = true, want false (PostStop must not be posted from the timer goroutine)")
	}
	if !tracker.takePendingStop() {
		t.Fatal("takePendingStop() = false, want true (stop should fire on next callback)")
	}
}

func TestPendingStopIsTakenOnce(t *testing.T) {
	tracker := &RuntimeTracker{
		active:     true,
		generation: 3,
	}

	if !tracker.requestStop(3) {
		t.Fatal("requestStop() = false, want true")
	}
	if !tracker.takePendingStop() {
		t.Fatal("first takePendingStop() = false, want true")
	}
	if tracker.takePendingStop() {
		t.Fatal("second takePendingStop() = true, want false")
	}
}

func TestRequestStopIgnoresStaleGeneration(t *testing.T) {
	tracker := &RuntimeTracker{
		active:     true,
		generation: 4,
	}

	if tracker.requestStop(3) {
		t.Fatal("requestStop() with stale generation = true, want false")
	}
	if tracker.takePendingStop() {
		t.Fatal("takePendingStop() = true without a valid request")
	}
}

func TestFinishFlushesOnlyUnchargedTail(t *testing.T) {
	isolateQuotaState(t)
	status := testStatus(10, "device-a")
	tracker := &RuntimeTracker{
		active: true,
		route:  quotaRouteRegular,
		status: status,
		last:   time.Now().Add(-500 * time.Millisecond),
		multiplier: quotaMultiplier{
			BasePermille:  multiplierScale,
			ExtraPermille: multiplierScale,
		},
		realNs:         int64(time.Minute),
		chargedSeconds: 60,
		stopCh:         make(chan struct{}),
	}

	tracker.finish()

	snapshot, err := GetQuotaSnapshot(status, quotaPoolRegularDaily)
	if err != nil {
		t.Fatalf("GetQuotaSnapshot() failed: %v", err)
	}
	if snapshot.UsedSeconds != 1 {
		t.Fatalf("UsedSeconds = %d, want 1", snapshot.UsedSeconds)
	}
}
