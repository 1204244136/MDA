package equipmentreroll

import (
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type taskLifecycle struct{}

var _ maa.TaskerEventSink = &taskLifecycle{}

// logMaterialConsumption 输出本次任务累计消耗的材料（用户可见）。
// 仅在任务**成功**结束（EventStatusSucceeded）时调用；失败/手动停止不打印。
func logMaterialConsumption(taskID int64, entry string) {
	if usage, ok := GetMaterialUsage(taskID); ok {
		log.Info().
			Str("component", "EquipmentReroll").
			Int64("task_id", taskID).
			Str("entry", entry).
			Int("custom_modules", usage.CustomModules).
			Int("custom_lock_keys", usage.CustomLockKeys).
			Int("reroll_modules", usage.RerollModules).
			Int("lock_modules", usage.LockModules).
			Msg("equipment reroll material consumption")
	}
}

// OnTaskerTask keeps the equipment snapshot scoped to one Maa task.
func (s *taskLifecycle) OnTaskerTask(_ *maa.Tasker, event maa.EventStatus, detail maa.TaskerTaskDetail) {
	if detail.Entry == "MaaTaskerPostStop" {
		return
	}
	switch event {
	case maa.EventStatusStarting:
		clearMonitorState(int64(detail.TaskID))
		clearPendingLock(int64(detail.TaskID))
		clearDPCaches()
	case maa.EventStatusSucceeded:
		// 完整洗词条任务的最终详情由 EquipmentRerollFinalSummaryAction 通过 focus 输出；
		// 独立装备检测已在各部位扫描完成时实时输出，这里只记录材料消耗。
		flushPendingRerollCost(int64(detail.TaskID))
		logMaterialConsumption(int64(detail.TaskID), detail.Entry)
		clearMonitorState(int64(detail.TaskID))
		clearPendingLock(int64(detail.TaskID))
		clearDPCaches()
	case maa.EventStatusFailed:
		// 失败可能发生在确认按钮尚未点击时，pending 成本不能计入使用量。
		clearPendingRerollCost(int64(detail.TaskID))
		clearMonitorState(int64(detail.TaskID))
		clearPendingLock(int64(detail.TaskID))
		clearDPCaches()
	}
}
