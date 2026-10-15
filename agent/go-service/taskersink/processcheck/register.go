package processcheck

import (
	"runtime"

	"github.com/MaaXYZ/maa-framework-go/v4"
)

var (
	_ maa.TaskerEventSink = &ProcessChecker{}
)

// Register registers the process checker as a tasker sink
func Register() {
	// 此检查针对 Windows 注入进程；Android/Linux 不支持枚举这些进程。
	if runtime.GOOS != "windows" {
		return
	}
	maa.AgentServerAddTaskerSink(&ProcessChecker{})
}
