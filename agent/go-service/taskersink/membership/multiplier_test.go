package membership

import (
	"testing"
	"time"
)

func TestMultiplierForEntry(t *testing.T) {
	cases := []struct {
		entry           string
		hasSpecialQuota bool
		want            int64
	}{
		{entry: "SmallEventMain", hasSpecialQuota: false, want: 1000},
		{entry: "SmallEventMain", hasSpecialQuota: true, want: 1000},
		{entry: "LargeEventMain", hasSpecialQuota: false, want: 1000},
		{entry: "MapPushingFlow", hasSpecialQuota: true, want: 1000},
		{entry: "MapPushingFlow", hasSpecialQuota: false, want: 5000},
		{entry: "EquipmentRerollMain", hasSpecialQuota: true, want: 1000},
		{entry: "EquipmentRerollMain", hasSpecialQuota: false, want: 5000},
		{entry: "CustomBurstMain", hasSpecialQuota: true, want: 1000},
		{entry: "CustomBurstMain", hasSpecialQuota: false, want: 5000},
		{entry: "DailyRewardsMain", hasSpecialQuota: false, want: 1000},
	}
	for _, test := range cases {
		if got := multiplierForEntry(test.entry, test.hasSpecialQuota).BasePermille; got != test.want {
			t.Fatalf("multiplierForEntry(%s, %t).BasePermille = %d, want %d", test.entry, test.hasSpecialQuota, got, test.want)
		}
	}
}

func TestQuotaDisplayEntryIsQuotaExempt(t *testing.T) {
	if !isQuotaExemptEntry(entryQuotaDisplayMain) {
		t.Fatal("QuotaDisplayMain should be quota exempt")
	}
	if isQuotaExemptEntry("DailyRewardsMain") {
		t.Fatal("DailyRewardsMain should not be quota exempt")
	}
}

func TestBillableDuration(t *testing.T) {
	multiplier := quotaMultiplier{BasePermille: 3000, ExtraPermille: 1500}
	if got := multiplier.billableDuration(time.Minute); got != 270*time.Second {
		t.Fatalf("billableDuration() = %s, want 4m30s", got)
	}
}

func TestTakeRealSecondsLockedKeepsFractionUntilFlush(t *testing.T) {
	tracker := &RuntimeTracker{}

	tracker.realNs = 500 * int64(time.Millisecond)
	if got := tracker.takeRealSecondsLocked(false); got != 0 {
		t.Fatalf("first takeRealSecondsLocked() = %d, want 0", got)
	}
	tracker.realNs += 500 * int64(time.Millisecond)
	if got := tracker.takeRealSecondsLocked(false); got != 1 {
		t.Fatalf("second takeRealSecondsLocked() = %d, want 1", got)
	}
	if got := tracker.takeRealSecondsLocked(true); got != 0 {
		t.Fatalf("flush with no remainder = %d, want 0", got)
	}
}

func TestTakeRealSecondsLockedCeilsOnFlush(t *testing.T) {
	tracker := &RuntimeTracker{realNs: 500 * int64(time.Millisecond)}
	if got := tracker.takeRealSecondsLocked(true); got != 1 {
		t.Fatalf("flush takeRealSecondsLocked() = %d, want 1", got)
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

func TestConsumeTickSwitchesTo5xWhenSpecialQuotaExhausted(t *testing.T) {
	isolateQuotaState(t)
	status := &MembershipStatus{
		TierCode:                    "orange_plus",
		TierName:                    "Orange Plus",
		RegularDailyRuntimeMinutes:  100,
		SpecialPeriodRuntimeMinutes: 1,
		StartsOn:                    "2026-05-01",
		ExpiresOn:                   "2026-06-01",
		IsMember:                    true,
		DeviceCode: DeviceCodeV7{
			CPUHash: "device-a",
		},
	}
	// 先把专项额度全部耗尽，使高级任务进入“无可用专项额度”的 5 倍状态。
	if _, _, err := addQuotaRouteUsageSeconds(status, quotaRouteSpecialThenRegular, 60); err != nil {
		t.Fatalf("failed to exhaust special quota: %v", err)
	}

	tracker := &RuntimeTracker{
		active:     true,
		generation: 1,
		entry:      "MapPushingFlow",
		route:      quotaRouteSpecialThenRegular,
		status:     status,
		last:       time.Now().Add(-2 * time.Second),
		multiplier: quotaMultiplier{BasePermille: multiplierScale, ExtraPermille: multiplierScale},
		realNs:     0,
		stopCh:     make(chan struct{}),
	}

	snapshot, done := tracker.consumeTick(status, quotaRouteSpecialThenRegular, 1)
	if done {
		t.Fatal("consumeTick() should not stop when regular quota remains")
	}
	if tracker.multiplier.BasePermille != 5*multiplierScale {
		t.Fatalf("tracker.multiplier.BasePermille = %d, want %d", tracker.multiplier.BasePermille, 5*multiplierScale)
	}
	if snapshot.RegularUsedSeconds != 10 {
		t.Fatalf("RegularUsedSeconds = %d, want 10 (2 real seconds at 5x)", snapshot.RegularUsedSeconds)
	}
}

func TestFinishFlushesFractionTail(t *testing.T) {
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
		realNs: 500 * int64(time.Millisecond),
		stopCh: make(chan struct{}),
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
