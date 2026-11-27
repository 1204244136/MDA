//go:build windows

package parentwatch

import (
	"errors"
	"fmt"

	"golang.org/x/sys/windows"
)

// windowsWatcher 在 Windows 下持有父进程的同步句柄。
// 启动时一次性 OpenProcess(SYNCHRONIZE)，循环里用 WaitForSingleObject(0) 探活，
// 避免每次轮询重新按 PID 查询时因 PID 复用造成误判。
type windowsWatcher struct {
	handle windows.Handle
}

func newWatcher(pid int) (watcher, error) {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
			return nil, fmt.Errorf("OpenProcess(%d): %w (%v)", pid, errParentExited, err)
		}
		return nil, fmt.Errorf("OpenProcess(%d): %w", pid, err)
	}
	return &windowsWatcher{handle: h}, nil
}

// IsAlive 通过 WaitForSingleObject(0) 探活：
// 句柄被信号化（WAIT_OBJECT_0）即表示进程已退出。
func (w *windowsWatcher) IsAlive() (bool, error) {
	if w.handle == 0 {
		return false, fmt.Errorf("parent handle is closed")
	}
	event, err := windows.WaitForSingleObject(w.handle, 0)
	switch event {
	case uint32(windows.WAIT_OBJECT_0):
		return false, nil
	case uint32(windows.WAIT_TIMEOUT):
		return true, nil
	default:
		if err != nil {
			return false, err
		}
		return false, fmt.Errorf("WaitForSingleObject returned unexpected status %d", event)
	}
}

func (w *windowsWatcher) Close() {
	if w.handle != 0 {
		_ = windows.CloseHandle(w.handle)
		w.handle = 0
	}
}
