//go:build !windows

package parentwatch

import (
	"errors"
	"syscall"
)

// posixWatcher 在 POSIX 下基于 kill(pid, 0) 探活：
// 进程不存在时 errno 返回 ESRCH；启动时记录的 pid 不受后续 reparent 影响。
type posixWatcher struct {
	pid int
}

func newWatcher(pid int) (watcher, error) {
	return &posixWatcher{pid: pid}, nil
}

func (w *posixWatcher) IsAlive() (bool, error) {
	err := syscall.Kill(w.pid, 0)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, syscall.ESRCH) {
		return false, nil
	}
	// EPERM 表示目标进程存在但无权发送信号，依然视为存活。
	if errors.Is(err, syscall.EPERM) {
		return true, nil
	}
	return false, err
}

func (w *posixWatcher) Close() {}
