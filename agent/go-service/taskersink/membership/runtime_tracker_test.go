package membership

import (
	"testing"
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func resetGlobalRuntimeTrackerForTest(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		runtimeTracker.finish()
		runtimeTracker.mu.Lock()
		runtimeTracker.shuttingDown = false
		runtimeTracker.mu.Unlock()
	})

	runtimeTracker.finish()
	runtimeTracker.mu.Lock()
	runtimeTracker.shuttingDown = false
	runtimeTracker.mu.Unlock()
}

func TestRuntimeTrackerShutdownStopsActiveTrackingAndReleasesLease(t *testing.T) {
	quotaPath := isolateQuotaState(t)
	resetMembershipTestGlobals(t)
	resetGlobalRuntimeTrackerForTest(t)

	SetVersion("2.5.0")
	cacheStatus(testStatus(10, "tracker-shutdown-device"))

	detail := maa.TaskerTaskDetail{
		TaskID: 101,
		Entry:  "CashShopMain",
		UUID:   "shutdown-tasker",
	}

	runtimeTracker.start(nil, detail)

	runtimeTracker.mu.Lock()
	active := runtimeTracker.active
	if active {
		runtimeTracker.last = time.Now().Add(-2 * time.Second)
	}
	runtimeTracker.mu.Unlock()
	if !active {
		t.Fatal("runtimeTracker did not become active on start")
	}

	if dup, acquired, err := tryAcquireRuntimeTrackingLease(detail); err != nil || acquired {
		if dup != nil {
			_ = dup.Release()
		}
		t.Fatalf("expected active lease to block duplicate acquisition (acquired=%t, err=%v)", acquired, err)
	}

	Shutdown()

	runtimeTracker.mu.Lock()
	stillActive := runtimeTracker.active
	shuttingDown := runtimeTracker.shuttingDown
	leaseCleared := runtimeTracker.lease == nil
	runtimeTracker.mu.Unlock()
	if stillActive {
		t.Fatal("runtimeTracker stayed active after Shutdown()")
	}
	if !shuttingDown {
		t.Fatal("runtimeTracker.shuttingDown = false, want true")
	}
	if !leaseCleared {
		t.Fatal("runtimeTracker retained lease reference after Shutdown()")
	}

	nextLease, acquired, err := tryAcquireRuntimeTrackingLease(detail)
	if err != nil || !acquired {
		t.Fatalf("expected lease to be released after Shutdown() (acquired=%t, err=%v)", acquired, err)
	}
	_ = nextLease.Release()

	state := mustLoadQuotaState(t, quotaPath)
	if state.Pools[string(quotaPoolRegularDaily)].UsedSeconds <= 0 {
		t.Fatalf("expected Shutdown() to flush elapsed quota usage, got %+v", state.Pools)
	}

	runtimeTracker.start(nil, maa.TaskerTaskDetail{
		TaskID: 102,
		Entry:  "ArenaMain",
		UUID:   "post-shutdown-tasker",
	})
	runtimeTracker.mu.Lock()
	restarted := runtimeTracker.active
	runtimeTracker.mu.Unlock()
	if restarted {
		t.Fatal("runtimeTracker started a new task after Shutdown()")
	}
}
