package parentwatch

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"testing"
	"time"
)

type fakeStep struct {
	alive bool
	err   error
}

type fakeWatcher struct {
	mu     sync.Mutex
	steps  []fakeStep
	closed bool
}

func (f *fakeWatcher) IsAlive() (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.steps) == 0 {
		return false, nil
	}
	step := f.steps[0]
	f.steps = f.steps[1:]
	return step.alive, step.err
}

func (f *fakeWatcher) Close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
}

func (f *fakeWatcher) isClosed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}

func TestStartIgnoresInvalidParentPID(t *testing.T) {
	called := false
	startWithConfig(config{
		getppid: func() int { return 1 },
		newWatcher: func(int) (watcher, error) {
			called = true
			return nil, errors.New("should not be called")
		},
		pollInterval: 10 * time.Millisecond,
		hookTimeout:  100 * time.Millisecond,
		exit: func(int) {
			t.Fatal("exit should not be called for invalid parent pid")
		},
	})
	if called {
		t.Fatal("newWatcher should not be called when parent pid <= 1")
	}
}

func TestStartHandlesParentAlreadyExited(t *testing.T) {
	var events []string
	var exitCode = -1
	startWithConfig(config{
		getppid: func() int { return 43210 },
		newWatcher: func(int) (watcher, error) {
			return nil, errParentExited
		},
		pollInterval: 10 * time.Millisecond,
		hookTimeout:  200 * time.Millisecond,
		exit: func(code int) {
			events = append(events, "exit")
			exitCode = code
		},
	}, func() {
		events = append(events, "hook1")
	}, func() {
		events = append(events, "hook2")
	})

	if exitCode != 0 {
		t.Fatalf("exitCode = %d, want 0", exitCode)
	}
	want := []string{"hook1", "hook2", "exit"}
	if len(events) != len(want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
	for i := range want {
		if events[i] != want[i] {
			t.Fatalf("events[%d] = %q, want %q", i, events[i], want[i])
		}
	}
}

func TestRunLoopRunsExitHooksBeforeExit(t *testing.T) {
	fw := &fakeWatcher{
		steps: []fakeStep{
			{alive: true},
			{err: errors.New("transient error")},
			{alive: false},
		},
	}

	var events []string
	var mu sync.Mutex
	record := func(item string) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, item)
	}

	done := make(chan int, 1)
	go runLoop(fw, 12345, 5*time.Millisecond, 200*time.Millisecond, func(code int) {
		record("exit")
		done <- code
	}, []func(){
		func() { record("shutdown") },
		func() {
			panic("boom")
		},
		func() { record("closeLog") },
	})

	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit code = %d, want 0", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for runLoop exit")
	}

	if !fw.isClosed() {
		t.Fatal("watcher was not closed before exit")
	}
	want := []string{"shutdown", "closeLog", "exit"}
	mu.Lock()
	defer mu.Unlock()
	if len(events) != len(want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
	for i := range want {
		if events[i] != want[i] {
			t.Fatalf("events[%d] = %q, want %q", i, events[i], want[i])
		}
	}
}

func TestRunExitHooksHonorsTimeout(t *testing.T) {
	release := make(chan struct{})
	defer close(release)

	start := time.Now()
	runExitHooks([]func(){
		func() { <-release },
	}, 40*time.Millisecond)

	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("runExitHooks took %v, expected prompt timeout", elapsed)
	}
}

func TestWatcherDetectsRealSubprocessExit(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestParentWatchHelperProcess$")
	cmd.Env = append(os.Environ(), "MDA_PARENTWATCH_HELPER=1")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("StdinPipe() failed: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe() failed: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("helper Start() failed: %v", err)
	}
	helperRunning := true
	t.Cleanup(func() {
		_ = stdin.Close()
		if helperRunning && cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})

	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatalf("failed to read helper ready line: %v", err)
	}
	if line != "ready\n" {
		t.Fatalf("helper ready line = %q, want ready\\n", line)
	}

	w, err := newWatcher(cmd.Process.Pid)
	if err != nil {
		t.Fatalf("newWatcher(alive) failed: %v", err)
	}
	wClosed := false
	defer func() {
		if !wClosed {
			w.Close()
		}
	}()

	alive, err := w.IsAlive()
	if err != nil || !alive {
		t.Fatalf("IsAlive() while child running = (%t, %v), want (true, nil)", alive, err)
	}

	deadPID := cmd.Process.Pid
	_ = stdin.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatalf("helper Wait() failed: %v", err)
	}
	helperRunning = false

	alive, err = w.IsAlive()
	if err != nil || alive {
		t.Fatalf("IsAlive() after child exited = (%t, %v), want (false, nil)", alive, err)
	}
	// 必须关闭本测试自身持有的进程句柄，Windows 才会释放该进程对象并回收 PID，随后再次 OpenProcess 才能触发 ERROR_INVALID_PARAMETER。
	w.Close()
	wClosed = true

	if runtime.GOOS == "windows" {
		deadWatcher, err := newWatcher(deadPID)
		if !errors.Is(err, errParentExited) {
			if deadWatcher != nil {
				deadWatcher.Close()
			}
			t.Fatalf("newWatcher(deadPID) err = %v, want errParentExited", err)
		}
	}
}

func TestParentWatchHelperProcess(t *testing.T) {
	if os.Getenv("MDA_PARENTWATCH_HELPER") != "1" {
		return
	}
	fmt.Println("ready")
	_, _ = os.Stdin.Read(make([]byte, 1))
	os.Exit(0)
}
