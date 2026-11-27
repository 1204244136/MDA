// Package parentwatch 在后台监视父进程存活情况：父进程一旦退出即执行清理并结束当前进程。
//
// 用途：宿主进程崩溃或被强杀后，MaaAgentServer 会长期阻塞在 IPC 上而不会自动退出，
// 导致 go-service 残留为孤儿进程并继续占用日志文件锁与会员额度计时。
// 启动 watcher 后会周期性检查父进程：
//   - 父进程仍在  → 继续监视。
//   - 父进程已退出 → 执行收尾钩子（带超时兜底），随后 os.Exit(0) 退出。
//
// 平台差异：
//   - Windows：启动时 OpenProcess(SYNCHRONIZE) 持有句柄，循环中用 WaitForSingleObject(0)
//     探活，避免后续 PID 复用导致的误判；若初始化时父进程已不存在则返回 errParentExited。
//   - POSIX：syscall.Kill(pid, 0) 探活，ESRCH 视为已退出。
package parentwatch

import (
	"errors"
	"os"
	"time"

	"github.com/rs/zerolog/log"
)

// PollInterval 是父进程存活检测的默认轮询间隔。
const PollInterval = 1 * time.Second

const (
	logComponent    = "parent-watcher"
	exitHookTimeout = 3 * time.Second
)

var errParentExited = errors.New("parent process already exited")

type watcher interface {
	IsAlive() (bool, error)
	Close()
}

type config struct {
	getppid      func() int
	newWatcher   func(int) (watcher, error)
	pollInterval time.Duration
	hookTimeout  time.Duration
	exit         func(int)
}

func defaultConfig() config {
	return config{
		getppid:      os.Getppid,
		newWatcher:   newWatcher,
		pollInterval: PollInterval,
		hookTimeout:  exitHookTimeout,
		exit:         os.Exit,
	}
}

// Start 启动父进程监视器。
// 进程启动时绑定父进程句柄（Windows）或记录父进程 PID（POSIX），
// 在后台 goroutine 中周期性探活；若父进程退出则依次执行 onExit 清理钩子后退出。
func Start(onExit ...func()) {
	startWithConfig(defaultConfig(), onExit...)
}

func startWithConfig(cfg config, onExit ...func()) {
	parentPID := cfg.getppid()
	if parentPID <= 1 {
		log.Warn().
			Str("component", logComponent).
			Int("parent_pid", parentPID).
			Msg("invalid parent pid, watcher disabled")
		return
	}

	w, err := cfg.newWatcher(parentPID)
	if err != nil {
		if errors.Is(err, errParentExited) {
			log.Warn().
				Str("component", logComponent).
				Int("parent_pid", parentPID).
				Msg("parent process has exited before watcher start; shutting down")
			runExitHooks(onExit, cfg.hookTimeout)
			cfg.exit(0)
			return
		}
		log.Warn().
			Err(err).
			Str("component", logComponent).
			Int("parent_pid", parentPID).
			Msg("failed to initialize parent watcher")
		return
	}

	log.Info().
		Str("component", logComponent).
		Int("parent_pid", parentPID).
		Msg("parent process watcher started")

	go runLoop(w, parentPID, cfg.pollInterval, cfg.hookTimeout, cfg.exit, onExit)
}

func runLoop(
	w watcher,
	parentPID int,
	pollInterval time.Duration,
	hookTimeout time.Duration,
	exitFunc func(int),
	onExit []func(),
) {
	defer w.Close()

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for range ticker.C {
		alive, checkErr := w.IsAlive()
		if checkErr != nil {
			log.Warn().
				Err(checkErr).
				Str("component", logComponent).
				Int("parent_pid", parentPID).
				Msg("parent liveness check failed")
			continue
		}
		if !alive {
			log.Warn().
				Str("component", logComponent).
				Int("parent_pid", parentPID).
				Msg("parent process has exited; shutting down")
			w.Close()
			runExitHooks(onExit, hookTimeout)
			exitFunc(0)
			return
		}
	}
}

func runExitHooks(hooks []func(), timeout time.Duration) {
	active := make([]func(), 0, len(hooks))
	for _, hook := range hooks {
		if hook != nil {
			active = append(active, hook)
		}
	}
	if len(active) == 0 {
		return
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for _, hook := range active {
			runSafeHook(hook)
		}
	}()

	if timeout <= 0 {
		<-done
		return
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		log.Warn().
			Str("component", logComponent).
			Dur("timeout", timeout).
			Msg("parent watcher exit hooks timed out")
	}
}

func runSafeHook(hook func()) {
	defer func() {
		if r := recover(); r != nil {
			log.Error().
				Str("component", logComponent).
				Interface("panic", r).
				Msg("parent watcher exit hook panicked")
		}
	}()
	hook()
}
