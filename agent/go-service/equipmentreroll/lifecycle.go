package equipmentreroll

import maa "github.com/MaaXYZ/maa-framework-go/v4"

type taskLifecycle struct{}

var _ maa.TaskerEventSink = &taskLifecycle{}

// OnTaskerTask keeps the equipment snapshot scoped to one Maa task.
func (s *taskLifecycle) OnTaskerTask(_ *maa.Tasker, event maa.EventStatus, detail maa.TaskerTaskDetail) {
	if detail.Entry == "MaaTaskerPostStop" {
		return
	}
	switch event {
	case maa.EventStatusStarting, maa.EventStatusSucceeded, maa.EventStatusFailed:
		clearMonitorState(int64(detail.TaskID))
		clearPendingLock(int64(detail.TaskID))
	}
}
