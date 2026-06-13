package membership

import (
	"strings"
	"testing"
)

func testRenewalStatus(remainingDays int) *MembershipStatus {
	return &MembershipStatus{
		IsMember:      true,
		RemainingDays: remainingDays,
		ExpiresOn:     "2026-06-11",
		DeviceCode: DeviceCodeV7{
			CPUHash: "cpu",
		},
	}
}

func TestFormatRenewalReminderMessage(t *testing.T) {
	initTestI18n()
	status := testRenewalStatus(3)

	message := formatRenewalReminderMessage(status)

	for _, want := range []string{"3", "2026-06-11", "doropay.top"} {
		if !strings.Contains(message, want) {
			t.Fatalf("message %q does not contain %q", message, want)
		}
	}
}

func TestShouldShowRenewalReminder(t *testing.T) {
	cases := []struct {
		name   string
		status *MembershipStatus
		want   bool
	}{
		{
			name:   "at threshold",
			status: testRenewalStatus(3),
			want:   true,
		},
		{
			name:   "below threshold",
			status: testRenewalStatus(2),
			want:   true,
		},
		{
			name:   "zero days remaining",
			status: testRenewalStatus(0),
			want:   true,
		},
		{
			name:   "above threshold",
			status: testRenewalStatus(4),
			want:   false,
		},
		{
			name:   "negative days remaining",
			status: testRenewalStatus(-1),
			want:   false,
		},
		{
			name: "non member",
			status: func() *MembershipStatus {
				status := testRenewalStatus(1)
				status.IsMember = false
				return status
			}(),
			want: false,
		},
		{
			name: "update required",
			status: func() *MembershipStatus {
				status := testRenewalStatus(1)
				status.UpdateRequired = true
				return status
			}(),
			want: false,
		},
		{
			name: "unlimited runtime",
			status: func() *MembershipStatus {
				status := testRenewalStatus(1)
				status.UnlimitedRuntime = true
				return status
			}(),
			want: false,
		},
		{
			name: "missing expiry",
			status: func() *MembershipStatus {
				status := testRenewalStatus(1)
				status.ExpiresOn = ""
				return status
			}(),
			want: false,
		},
		{
			name: "has future renewal flag",
			status: func() *MembershipStatus {
				status := testRenewalStatus(1)
				status.HasFutureRenewal = true
				return status
			}(),
			want: false,
		},
		{
			name: "paid through future period",
			status: func() *MembershipStatus {
				status := testRenewalStatus(1)
				status.PaidThroughOn = "2026-07-11"
				return status
			}(),
			want: false,
		},
		{
			name: "paid through current expiry",
			status: func() *MembershipStatus {
				status := testRenewalStatus(1)
				status.PaidThroughOn = "2026-06-11"
				return status
			}(),
			want: true,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldShowRenewalReminder(tt.status); got != tt.want {
				t.Fatalf("shouldShowRenewalReminder() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHasRenewedFuturePeriodSupportsDateFormats(t *testing.T) {
	cases := []struct {
		name          string
		expiresOn     string
		paidThroughOn string
		want          bool
	}{
		{
			name:          "hyphenated dates",
			expiresOn:     "2026-06-11",
			paidThroughOn: "2026-07-11",
			want:          true,
		},
		{
			name:          "compact dates",
			expiresOn:     "20260611",
			paidThroughOn: "20260711",
			want:          true,
		},
		{
			name:          "same date is not future period",
			expiresOn:     "20260611",
			paidThroughOn: "20260611",
			want:          false,
		},
		{
			name:          "invalid paid-through date is not future period",
			expiresOn:     "2026-06-11",
			paidThroughOn: "2026/07/11",
			want:          false,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			status := testRenewalStatus(1)
			status.ExpiresOn = tt.expiresOn
			status.PaidThroughOn = tt.paidThroughOn
			if got := hasRenewedFuturePeriod(status); got != tt.want {
				t.Fatalf("hasRenewedFuturePeriod() = %v, want %v", got, tt.want)
			}
		})
	}
}
