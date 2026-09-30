package membership

import (
	"fmt"
	"sync"

	"github.com/1204244136/MDA/agent/go-service/pkg/i18n"
	"github.com/1204244136/MDA/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type RuntimeQuotaCheckAction struct{}

var _ maa.CustomActionRunner = &RuntimeQuotaCheckAction{}

var notifyOnce sync.Once

func (a *RuntimeQuotaCheckAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	route := quotaRouteRegular
	if arg != nil {
		route = quotaRouteForEntry(arg.CurrentTaskName)
	}
	entry := ""
	if arg != nil {
		entry = arg.CurrentTaskName
	}
	return runRuntimeQuotaCheck(ctx, route, entry)
}

func runRuntimeQuotaCheck(ctx *maa.Context, route quotaRoute, entries ...string) bool {
	if isDebugEnvironment() {
		return true
	}

	status := GetMembershipStatus()
	if status.VerificationUnavailable {
		maafocus.Print(ctx, formatMembershipVerificationUnavailableMessage())
	}
	if status.UpdateRequired {
		if status.UpdateMessage != "" {
			maafocus.Print(ctx, status.UpdateMessage)
		} else {
			maafocus.Print(ctx, fmt.Sprintf(
				i18n.T("tasker.membership_check.update_required"),
				status.MinimumSupportedVersion,
			))
		}
		return false
	}

	maybePrintRenewalReminder(ctx, status)

	snapshot, ok, err := EnsureQuotaRouteAvailable(status, route, entries...)
	if err != nil {
		log.Warn().Err(err).Msg("RuntimeQuotaCheck: failed to read local quota state")
	}

	log.Info().
		Str("tier_code", snapshot.TierCode).
		Str("tier_name", snapshot.TierName).
		Str("quota_route", string(snapshot.Route)).
		Str("quota_pool", string(snapshot.Pool)).
		Int64("limit_seconds", snapshot.LimitSeconds).
		Int64("used_seconds", snapshot.UsedSeconds).
		Int64("remaining_seconds", snapshot.RemainingSeconds).
		Int64("special_remaining_seconds", snapshot.SpecialRemainingSeconds).
		Int64("regular_remaining_seconds", snapshot.RegularRemainingSeconds).
		Bool("unlimited_runtime", snapshot.UnlimitedRuntime).
		Str("period_key", snapshot.PeriodKey).
		Msg("RuntimeQuotaCheck: quota evaluated")

	if ok {
		if route == quotaRouteSpecialThenRegular && snapshot.SpecialRemainingSeconds <= 0 && snapshot.EventRemainingSeconds <= 0 {
			maafocus.Print(ctx, i18n.T("tasker.membership_check.no_special_quota_5x_multiplier"))
		}
		notifyOnce.Do(func() {
			maafocus.Print(ctx, formatQuotaStatusMessage(snapshot))
			if snapshot.EventRemainingSeconds > 0 {
				maafocus.Print(ctx, fmt.Sprintf(
					i18n.T("tasker.membership_check.verified_event"),
					FormatMinutes(snapshot.EventRemainingSeconds),
				))
			}
			if snapshot.UnlimitedRuntime {
				return
			}
			maafocus.Print(ctx, fmt.Sprintf(
				i18n.T("tasker.membership_check.sponsor"),
				snapshot.SponsorURL,
			))
		})
		return true
	}

	maafocus.Print(ctx, formatQuotaDeniedMessage(snapshot))
	return false
}

func formatMembershipVerificationUnavailableMessage() string {
	return i18n.T("tasker.membership_check.service_unavailable")
}

// formatQuotaStatusMessage 描述当前正在扣减的主额度池。
// 扣减顺序为“常规 → 专项 → 活动”，因此常规额度是默认主池，只有在常规额度已经用尽、
// 任务改用专项额度兜底时，才显示专项额度的进度。活动额度另由 runRuntimeQuotaCheck 单独提示。
func formatQuotaStatusMessage(snapshot QuotaSnapshot) string {
	if snapshot.UnlimitedRuntime {
		return i18n.T("tasker.membership_check.debug_unlimited")
	}
	if snapshot.Pool == quotaPoolSpecialPeriod {
		return fmt.Sprintf(
			i18n.T("tasker.membership_check.verified_special_after_regular"),
			snapshot.TierName,
			FormatMinutes(snapshot.RegularLimitSeconds),
			FormatMinutes(snapshot.SpecialUsedSeconds),
			FormatMinutes(snapshot.SpecialLimitSeconds),
		)
	}
	return fmt.Sprintf(
		i18n.T("tasker.membership_check.verified_regular"),
		snapshot.TierName,
		FormatMinutes(snapshot.UsedSeconds),
		FormatMinutes(snapshot.LimitSeconds),
		FormatMinutes(snapshot.RemainingSeconds),
	)
}

func formatQuotaDeniedMessage(snapshot QuotaSnapshot) string {
	if snapshot.Route == quotaRouteSpecialThenRegular {
		return fmt.Sprintf(
			i18n.T("tasker.membership_check.denied_special"),
			snapshot.TierName,
			FormatMinutes(snapshot.SpecialLimitSeconds),
			FormatMinutes(snapshot.RegularLimitSeconds),
			snapshot.SponsorURL,
		)
	}
	return fmt.Sprintf(
		i18n.T("tasker.membership_check.denied_regular"),
		snapshot.TierName,
		FormatMinutes(snapshot.LimitSeconds),
		snapshot.SponsorURL,
	)
}
