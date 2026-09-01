package membership

import (
	"strings"
	"testing"

	"github.com/1204244136/MDA/agent/go-service/pkg/i18n"
)

func initTestI18n() {
	i18n.Init()
}

func TestFormatQuotaStatusMessageUsesRegularQuotaSummary(t *testing.T) {
	initTestI18n()
	message := formatQuotaStatusMessage(QuotaSnapshot{
		Route:            quotaRouteRegular,
		TierName:         "Orange Plus",
		LimitSeconds:     3600,
		UsedSeconds:      600,
		RemainingSeconds: 3000,
	})

	if !strings.Contains(message, "10/60") {
		t.Fatalf("message does not show used/total runtime: %s", message)
	}
	if !strings.Contains(message, "剩余约 50") && !strings.Contains(message, "about 50 minute(s) remaining") {
		t.Fatalf("message does not show remaining runtime: %s", message)
	}
}

func TestFormatQuotaStatusMessageUsesExistingUnlimitedText(t *testing.T) {
	initTestI18n()
	message := formatQuotaStatusMessage(QuotaSnapshot{UnlimitedRuntime: true})

	if !strings.Contains(message, "无限运行") && !strings.Contains(message, "unlimited runtime") {
		t.Fatalf("message does not show unlimited runtime: %s", message)
	}
}

func TestFormatQuotaDeniedMessageUsesNormalText(t *testing.T) {
	initTestI18n()
	message := formatQuotaDeniedMessage(QuotaSnapshot{
		Route:        quotaRouteRegular,
		TierName:     "Orange Free",
		LimitSeconds: 600,
		SponsorURL:   "https://example.test",
	})

	if strings.Contains(message, "此前超额运行") || strings.Contains(message, "previous over-quota runtime") {
		t.Fatalf("message unexpectedly mentions carried debt: %s", message)
	}
}

func TestFormatQuotaStatusMessageUsesSpecialRoute(t *testing.T) {
	initTestI18n()
	message := formatQuotaStatusMessage(QuotaSnapshot{
		Route:               quotaRouteSpecialThenRegular,
		TierName:            "Orange Plus",
		SpecialLimitSeconds: 36000,
		SpecialUsedSeconds:  600,
	})

	if !strings.Contains(message, "专项额度") && !strings.Contains(message, "special quota") {
		t.Fatalf("message does not mention special quota: %s", message)
	}
}

func TestQuotaRouteForRuntimeQuotaCheckEntry(t *testing.T) {
	if got := quotaRouteForEntry("MapPushingFlow"); got != quotaRouteSpecialThenRegular {
		t.Fatalf("quotaRouteForEntry(MapPushingFlow) = %s, want %s", got, quotaRouteSpecialThenRegular)
	}
	if got := quotaRouteForEntry("EquipmentRerollMain"); got != quotaRouteSpecialThenRegular {
		t.Fatalf("quotaRouteForEntry(EquipmentRerollMain) = %s, want %s", got, quotaRouteSpecialThenRegular)
	}
}

func TestFormatMembershipVerificationUnavailableMessage(t *testing.T) {
	initTestI18n()
	message := formatMembershipVerificationUnavailableMessage()

	if !strings.Contains(message, "会员校验服务暂不可用") && !strings.Contains(message, "temporarily unavailable") {
		t.Fatalf("message does not mention membership verification service unavailable: %s", message)
	}
	if strings.Contains(message, "任务已停止") || strings.Contains(message, "Task stopped") {
		t.Fatalf("message should not say task stopped: %s", message)
	}
}

func TestNoSpecialQuota5XMultiplierMessage(t *testing.T) {
	initTestI18n()
	message := i18n.T("tasker.membership_check.no_special_quota_5x_multiplier")

	if !strings.Contains(message, "5 倍") && !strings.Contains(message, "5x") {
		t.Fatalf("message does not mention 5x quota consumption: %s", message)
	}
}
