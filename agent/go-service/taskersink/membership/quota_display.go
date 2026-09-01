package membership

import (
	"github.com/1204244136/MDA/agent/go-service/pkg/i18n"
	"github.com/1204244136/MDA/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
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
