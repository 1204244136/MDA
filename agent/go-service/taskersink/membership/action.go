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
	return runRuntimeQuotaCheck(ctx, route)
}

func runRuntimeQuotaCheck(ctx *maa.Context, route quotaRoute) bool {
	if isDebugEnvironment() {
		return true
	}

	status := GetMembershipStatus()
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

	snapshot, ok, err := EnsureQuotaRouteAvailable(status, route)
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
		Int64("carried_debt_seconds", snapshot.CarriedDebtSeconds).
		Bool("unlimited_runtime", snapshot.UnlimitedRuntime).
		Str("period_key", snapshot.PeriodKey).
		Msg("RuntimeQuotaCheck: quota evaluated")

	if ok {
		notifyOnce.Do(func() {
			if snapshot.UnlimitedRuntime {
				maafocus.Print(ctx, i18n.T("tasker.membership_check.debug_unlimited"))
				return
			}
			maafocus.Print(ctx, formatQuotaVerifiedMessage(snapshot))
			if snapshot.CarriedDebtSeconds > 0 {
				maafocus.Print(ctx, fmt.Sprintf(
					i18n.T("tasker.membership_check.debt"),
					FormatMinutes(snapshot.CarriedDebtSeconds),
				))
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

func formatQuotaVerifiedMessage(snapshot QuotaSnapshot) string {
	if snapshot.Route == quotaRouteSpecialThenRegular {
		if snapshot.FallbackToRegular {
			return fmt.Sprintf(
				i18n.T("tasker.membership_check.verified_special_fallback_regular"),
				snapshot.TierName,
				FormatMinutes(snapshot.SpecialLimitSeconds),
				FormatMinutes(snapshot.RegularUsedSeconds),
				FormatMinutes(snapshot.RegularLimitSeconds),
			)
		}
		return fmt.Sprintf(
			i18n.T("tasker.membership_check.verified_special"),
			snapshot.TierName,
			FormatMinutes(snapshot.SpecialUsedSeconds),
			FormatMinutes(snapshot.SpecialLimitSeconds),
		)
	}
	return fmt.Sprintf(
		i18n.T("tasker.membership_check.verified_regular"),
		snapshot.TierName,
		FormatMinutes(snapshot.UsedSeconds),
		FormatMinutes(snapshot.LimitSeconds),
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
	messageKey := "tasker.membership_check.denied_regular"
	if snapshot.CarriedDebtSeconds > 0 {
		messageKey = "tasker.membership_check.denied_debt"
	}
	return fmt.Sprintf(
		i18n.T(messageKey),
		snapshot.TierName,
		FormatMinutes(snapshot.LimitSeconds),
		snapshot.SponsorURL,
	)
}
