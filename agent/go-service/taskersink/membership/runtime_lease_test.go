package membership

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func TestRuntimeTrackingLeaseSuppressesDuplicateAgent(t *testing.T) {
	isolateQuotaState(t)
	detail := maa.TaskerTaskDetail{
		TaskID: 42,
		Entry:  "CashShopMain",
		UUID:   "shared-tasker",
	}

	first, acquired, err := tryAcquireRuntimeTrackingLease(detail)
	if err != nil {
		t.Fatalf("first tryAcquireRuntimeTrackingLease() failed: %v", err)
	}
	if !acquired {
		t.Fatal("first lease should be acquired")
	}

	duplicate, acquired, err := tryAcquireRuntimeTrackingLease(detail)
	if err != nil {
		t.Fatalf("duplicate tryAcquireRuntimeTrackingLease() failed: %v", err)
	}
	if acquired || duplicate != nil {
		t.Fatal("duplicate agent acquired the same runtime tracking lease")
	}

	if err := first.Release(); err != nil {
		t.Fatalf("first lease release failed: %v", err)
	}
	replacement, acquired, err := tryAcquireRuntimeTrackingLease(detail)
	if err != nil {
		t.Fatalf("replacement tryAcquireRuntimeTrackingLease() failed: %v", err)
	}
	if !acquired {
		t.Fatal("lease should be acquirable after the owner releases it")
	}
	if err := replacement.Release(); err != nil {
		t.Fatalf("replacement lease release failed: %v", err)
	}
}

func TestRuntimeTrackerStartSuppressesDuplicateAgent(t *testing.T) {
	isolateQuotaState(t)
	detail := maa.TaskerTaskDetail{
		TaskID: 99,
		Entry:  "ArenaMain",
		UUID:   "shared-tasker-start",
	}
	owner, acquired, err := tryAcquireRuntimeTrackingLease(detail)
	if err != nil || !acquired {
		t.Fatalf("owner lease: acquired=%t, err=%v", acquired, err)
	}
	defer owner.Release()

	tracker := &RuntimeTracker{}
	tracker.start(nil, detail)
	if tracker.active {
		t.Fatal("duplicate tracker became active")
	}
	if tracker.lease != nil {
		t.Fatal("duplicate tracker retained a runtime tracking lease")
	}
}

func TestRuntimeTrackingLeaseIsScopedPerTasker(t *testing.T) {
	isolateQuotaState(t)
	firstDetail := maa.TaskerTaskDetail{TaskID: 42, Entry: "CashShopMain", UUID: "tasker-a"}
	secondDetail := maa.TaskerTaskDetail{TaskID: 42, Entry: "CashShopMain", UUID: "tasker-b"}

	first, acquired, err := tryAcquireRuntimeTrackingLease(firstDetail)
	if err != nil || !acquired {
		t.Fatalf("first tasker lease: acquired=%t, err=%v", acquired, err)
	}
	defer first.Release()

	second, acquired, err := tryAcquireRuntimeTrackingLease(secondDetail)
	if err != nil || !acquired {
		t.Fatalf("second tasker lease: acquired=%t, err=%v", acquired, err)
	}
	defer second.Release()
}

func TestRuntimeTrackingLeaseIsScopedPerHostProcess(t *testing.T) {
	isolateQuotaState(t)
	originalParentPID := runtimeLeaseParentPID
	t.Cleanup(func() { runtimeLeaseParentPID = originalParentPID })
	detail := maa.TaskerTaskDetail{TaskID: 42, Entry: "CashShopMain", UUID: "same-tasker-id"}

	runtimeLeaseParentPID = func() int { return 1001 }
	first, acquired, err := tryAcquireRuntimeTrackingLease(detail)
	if err != nil || !acquired {
		t.Fatalf("first host lease: acquired=%t, err=%v", acquired, err)
	}
	defer first.Release()

	runtimeLeaseParentPID = func() int { return 1002 }
	second, acquired, err := tryAcquireRuntimeTrackingLease(detail)
	if err != nil || !acquired {
		t.Fatalf("second host lease: acquired=%t, err=%v", acquired, err)
	}
	defer second.Release()
}

func TestRuntimeTrackingLeaseReleaseIsIdempotent(t *testing.T) {
	isolateQuotaState(t)
	detail := maa.TaskerTaskDetail{TaskID: 7, Entry: "ShopMain", UUID: "tasker-release"}
	lease, acquired, err := tryAcquireRuntimeTrackingLease(detail)
	if err != nil || !acquired {
		t.Fatalf("tryAcquireRuntimeTrackingLease(): acquired=%t, err=%v", acquired, err)
	}
	if err := lease.Release(); err != nil {
		t.Fatalf("first Release() failed: %v", err)
	}
	if err := lease.Release(); err != nil {
		t.Fatalf("second Release() failed: %v", err)
	}
}

func TestRuntimeTrackingLeaseBlocksAnotherProcess(t *testing.T) {
	isolateQuotaState(t)
	path := filepath.Join(t.TempDir(), "runtime-cross-process.lock")
	cmd := exec.Command(os.Args[0], "-test.run=^TestRuntimeTrackingLeaseHelperProcess$")
	cmd.Env = append(os.Environ(),
		"MDA_RUNTIME_LEASE_HELPER=1",
		"MDA_RUNTIME_LEASE_PATH="+path,
	)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("StdinPipe() failed: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe() failed: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("helper process start failed: %v", err)
	}
	helperRunning := true
	t.Cleanup(func() {
		stdin.Close()
		if helperRunning && cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})

	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatalf("failed to read helper readiness: %v", err)
	}
	if line != "locked\n" {
		t.Fatalf("helper readiness = %q, want %q", line, "locked\\n")
	}

	release, acquired, err := tryAcquireQuotaFileLock(path)
	if err != nil {
		t.Fatalf("tryAcquireQuotaFileLock() failed: %v", err)
	}
	if acquired {
		release()
		t.Fatal("parent acquired a lease already held by another process")
	}

	if err := stdin.Close(); err != nil {
		t.Fatalf("failed to stop helper process: %v", err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("helper process failed: %v", err)
	}
	helperRunning = false
}

func TestRuntimeTrackingLeaseHelperProcess(t *testing.T) {
	if os.Getenv("MDA_RUNTIME_LEASE_HELPER") != "1" {
		return
	}
	path := os.Getenv("MDA_RUNTIME_LEASE_PATH")
	release, acquired, err := tryAcquireQuotaFileLock(path)
	if err != nil || !acquired {
		fmt.Fprintf(os.Stderr, "helper acquire: acquired=%t, err=%v\n", acquired, err)
		os.Exit(2)
	}
	fmt.Println("locked")
	_, _ = os.Stdin.Read(make([]byte, 1))
	if err := release(); err != nil {
		fmt.Fprintf(os.Stderr, "helper release: %v\n", err)
		os.Exit(3)
	}
	os.Exit(0)
}
