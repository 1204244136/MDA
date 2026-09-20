package equipmentreroll

// rerollResultSource 仅在本轮结果页确认出现、实际记账时保存。
// 与长期保留的 PreviousLocks 分开，避免拿上一轮历史锁位跳过本轮 OCR。
type rerollResultSource struct {
	Part string
	Scan partScan
}

func resultSourceForTask(taskID int64) partScan {
	stateMu.Lock()
	defer stateMu.Unlock()
	state := states[taskID]
	if state.ResultSource == nil || state.ResultSource.Part != state.Part {
		return partScan{}
	}
	return state.ResultSource.Scan
}

// pendingResult 保存候选词条；点击及警告确认尚未完成时不能影响任务快照。
type pendingResult struct {
	Part    string
	Effects [maxSlot]string
	Values  [maxSlot]string
}

func stageAcceptedResult(taskID int64, part string, effects, values [maxSlot]string) {
	stateMu.Lock()
	defer stateMu.Unlock()
	state := states[taskID]
	state.PendingResult = &pendingResult{Part: part, Effects: effects, Values: values}
	states[taskID] = state
}

// commitAcceptedResult 只由接受完成哨兵调用，原子提交并消费候选快照。
func commitAcceptedResult(taskID int64) bool {
	stateMu.Lock()
	defer stateMu.Unlock()
	state := states[taskID]
	pending := state.PendingResult
	if pending == nil || pending.Part != state.Part {
		return false
	}
	scan, ok := state.Parts[pending.Part]
	if !ok {
		return false
	}
	for i := range scan.Slots {
		scan.Slots[i].Effect = pending.Effects[i]
		scan.Slots[i].Value = pending.Values[i]
	}
	state.Parts[pending.Part] = scan
	state.PendingResult = nil
	state.ResultSource = nil
	states[taskID] = state
	return true
}
