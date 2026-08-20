package membership

import "time"

const multiplierScale = 1000

type quotaMultiplier struct {
	BasePermille  int64
	ExtraPermille int64
	Reason        string
}

// isHighConsumptionEntry 判断任务是否属于「高消耗」任务：非会员按 5 倍额度消耗，
// 配额路由与自动推图一致（专项额度优先，再走日常额度）。
func isHighConsumptionEntry(entry string) bool {
	switch entry {
	case "MapPushingFlow", "EquipmentRerollMain":
		return true
	default:
		return false
	}
}

func multiplierForEntry(entry string, isMember bool) quotaMultiplier {
	m := quotaMultiplier{
		BasePermille:  multiplierScale,
		ExtraPermille: multiplierScale,
		Reason:        "default",
	}

	if isHighConsumptionEntry(entry) && !isMember {
		m.BasePermille = 5 * multiplierScale
		m.Reason = "non_member_5x"
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
