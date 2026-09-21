package membership

import (
	"fmt"
	"github.com/1204244136/MDA/agent/go-service/pkg/i18n"
	"github.com/1204244136/MDA/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
	"time"
)

// QuotaDisplayAction displays today's regular runtime quota without consuming it.
type QuotaDisplayAction struct{}

var _ maa.CustomActionRunner = &QuotaDisplayAction{}

func (a *QuotaDisplayAction) Run(ctx *maa.Context, _ *maa.CustomActionArg) bool {
	status := GetMembershipStatus()
	if status.VerificationUnavailable {
		maafocus.Print(ctx, formatMembershipVerificationUnavailableMessage())
	}

	snapshot, err := GetQuotaSnapshot(status, quotaPoolRegularDaily)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", "QuotaDisplayAction").
			Msg("failed to read local quota state")
		maafocus.Print(ctx, i18n.T("tasker.quota_display.failed"))
		return false
	}

	maafocus.Print(ctx, formatQuotaStatusMessage(snapshot))
	grants, err := eventQuotaGrants(status)
	if err != nil {
		log.Error().Err(err).Msg("failed to read event quota")
		maafocus.Print(ctx, i18n.T("tasker.quota_display.failed"))
		return false
	}
	for _, grant := range grants {
		remaining := grant.LimitSeconds - grant.UsedSeconds
		if remaining <= 0 {
			continue
		}
		task := grant.TaskEntry
		if task == "" {
			task = i18n.T("tasker.quota_display.all_tasks")
		}
		maafocus.Print(ctx, fmt.Sprintf(i18n.T("tasker.quota_display.event"), task, FormatMinutes(remaining)))
	}
	log.Info().
		Str("component", "QuotaDisplayAction").
		Str("tier_code", snapshot.TierCode).
		Int64("used_seconds", snapshot.UsedSeconds).
		Int64("remaining_seconds", snapshot.RemainingSeconds).
		Bool("unlimited_runtime", snapshot.UnlimitedRuntime).
		Str("business_date", snapshot.BusinessDate).
		Msg("quota displayed")
	return true
}

// eventQuotaGrants 与计时器、兑换器使用同一把文件锁，避免覆盖并发兑换结果。
func eventQuotaGrants(status *MembershipStatus) ([]eventQuotaGrant, error) {
	quotaMu.Lock()
	defer quotaMu.Unlock()
	unlock, err := lockQuotaStateFile()
	if err != nil {
		return nil, err
	}
	defer unlock()
	_, state, err := normalizeQuotaState(status, quotaPoolRegularDaily, time.Now())
	if err != nil {
		return nil, err
	}
	return state.EventGrants, nil
}
