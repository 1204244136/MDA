package membership

import (
	"fmt"
	"strings"
	"time"

	"github.com/1204244136/MDA/agent/go-service/pkg/i18n"
	"github.com/1204244136/MDA/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

const renewalReminderThresholdDays = 3

func maybePrintRenewalReminder(ctx *maa.Context, status *MembershipStatus) {
	if !shouldShowRenewalReminder(status) {
		return
	}
	maafocus.Print(ctx, formatRenewalReminderMessage(status))
}

func shouldShowRenewalReminder(status *MembershipStatus) bool {
	if status == nil || !status.IsMember || status.UpdateRequired || status.UnlimitedRuntime {
		return false
	}
	if status.ExpiresOn == "" || status.RemainingDays < 0 || status.RemainingDays > renewalReminderThresholdDays {
		return false
	}
	return !hasRenewedFuturePeriod(status)
}

func hasRenewedFuturePeriod(status *MembershipStatus) bool {
	if status == nil {
		return false
	}
	if status.HasFutureRenewal {
		return true
	}
	if strings.TrimSpace(status.PaidThroughOn) == "" {
		return false
	}
	expiresOn, ok := parseMembershipDate(status.ExpiresOn)
	if !ok {
		return false
	}
	paidThroughOn, ok := parseMembershipDate(status.PaidThroughOn)
	if !ok {
		return false
	}
	return paidThroughOn.After(expiresOn)
}

func parseMembershipDate(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{"2006-01-02", "20060102"} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func formatRenewalReminderMessage(status *MembershipStatus) string {
	return fmt.Sprintf(
		i18n.T("tasker.membership_check.renewal_reminder"),
		status.RemainingDays,
		status.ExpiresOn,
		SponsorURL(status),
	)
}
