package membership

import "time"

const multiplierScale = 1000

// 内部任务分级表：高级/低级信息不对外暴露，仅在运行时参与计费与配额路由。
type taskTier int

const (
	taskTierLow taskTier = iota
	taskTierHigh
)

const (
	entryMapPushingFlow      = "MapPushingFlow"
	entryEquipmentRerollMain = "EquipmentRerollMain"
	entryCustomBurstMain     = "CustomBurstMain"
)

var taskTierByEntry = map[string]taskTier{
	entryMapPushingFlow:      taskTierHigh,
	entryEquipmentRerollMain: taskTierHigh,
	entryCustomBurstMain:     taskTierHigh,
}

func taskTierForEntry(entry string) taskTier {
	if tier, ok := taskTierByEntry[entry]; ok {
		return tier
	}
	return taskTierLow
}

type quotaMultiplier struct {
	BasePermille  int64
	ExtraPermille int64
	Reason        string
}

// isHighConsumptionEntry 判断任务是否属于「高消耗」任务：非会员按 5 倍额度消耗，
// 配额路由与自动推图一致（专项额度优先，再走日常额度）。
func isHighConsumptionEntry(entry string) bool {
	return taskTierForEntry(entry) == taskTierHigh
}

func multiplierForEntry(entry string, hasSpecialQuota bool) quotaMultiplier {
	m := quotaMultiplier{
		BasePermille:  multiplierScale,
		ExtraPermille: multiplierScale,
		Reason:        "default",
	}

	if isHighConsumptionEntry(entry) && !hasSpecialQuota {
		m.BasePermille = 5 * multiplierScale
		m.Reason = "no_special_quota_5x"
	}

	return m
}

func (m quotaMultiplier) totalPermille() int64 {
	base := m.BasePermille
	if base <= 0 {
		base = multiplierScale
	}
	extra := m.ExtraPermille
	if extra <= 0 {
		extra = multiplierScale
	}
	return base * extra / multiplierScale
}

func (m quotaMultiplier) billableDuration(delta time.Duration) time.Duration {
	if delta <= 0 {
		return 0
	}
	total := m.totalPermille()
	if total <= 0 {
		total = multiplierScale
	}
	return time.Duration((delta.Nanoseconds() * total) / multiplierScale)
}

func (m quotaMultiplier) billableSecondsFromReal(realSeconds int64, flush bool) int64 {
	if realSeconds <= 0 {
		return 0
	}
	billable := m.billableDuration(time.Duration(realSeconds) * time.Second)
	seconds := int64(billable / time.Second)
	if flush && billable%time.Second > 0 {
		seconds++
	}
	return seconds
}
