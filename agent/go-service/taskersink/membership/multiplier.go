package membership

import "time"

const multiplierScale = 1000

type quotaMultiplier struct {
	BasePermille  int64
	ExtraPermille int64
	Reason        string
}

func multiplierForEntry(entry string, isMember bool) quotaMultiplier {
	m := quotaMultiplier{
		BasePermille:  multiplierScale,
		ExtraPermille: multiplierScale,
		Reason:        "default",
	}

	if entry == "MapPushingFlow" && !isMember {
		m.BasePermille = 5 * multiplierScale
		m.Reason = "map_pushing_non_member"
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
