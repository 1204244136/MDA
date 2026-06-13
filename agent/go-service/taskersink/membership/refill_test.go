package membership

import (
	"errors"
	"testing"
	"time"
)

func sponsorURLForDevice(device DeviceCodeV7) string {
	status := &MembershipStatus{DeviceCode: device}
	return SponsorURL(status)
}

func TestRefillQuotaForSponsorURLClearsUsageForMatchingDeviceAndDate(t *testing.T) {
	path := isolateQuotaState(t)
	device := DeviceCodeV7{
		CPUHash:   "cpu",
		UUIDHash:  "uuid",
		BIOSHash:  "bios",
		BoardHash: "board",
		DiskHash:  "disk",
		GUIDHash:  "guid",
	}
	mustSaveQuotaState(t, path, quotaState{
		Version:    2,
		DeviceHash: deviceHash(device),
		TierCode:   "orange_plus",
		Pools: map[string]quotaPoolState{
			string(quotaPoolRegularDaily): {
				PeriodKey:          "2026-06-03",
				LimitSeconds:       600,
				UsedSeconds:        725,
				CarriedDebtSeconds: 125,
			},
			string(quotaPoolSpecialPeriod): {
				PeriodKey:    "2026-06-01..2026-07-01",
				LimitSeconds: 3600,
				UsedSeconds:  500,
			},
		},
	})

	result, err := refillQuotaForSponsorURLAt(
		"2026-06-03",
		sponsorURLForDevice(device),
		time.Date(2026, 6, 3, 12, 0, 0, 0, time.Local),
		func() DeviceCodeV7 { return device },
	)
	if err != nil {
		t.Fatalf("refillQuotaForSponsorURLAt() failed: %v", err)
	}
	if result.BusinessDate != "2026-06-03" {
		t.Fatalf("BusinessDate = %s, want 2026-06-03", result.BusinessDate)
	}

	state := loadQuotaState(path)
	regular := state.Pools[string(quotaPoolRegularDaily)]
	if regular.UsedSeconds != 0 || regular.CarriedDebtSeconds != 0 {
		t.Fatalf("regular pool was not cleared: %+v", regular)
	}
	special := state.Pools[string(quotaPoolSpecialPeriod)]
	if special.UsedSeconds != 0 || special.CarriedDebtSeconds != 0 {
		t.Fatalf("special pool was not cleared: %+v", special)
	}
}

func TestRefillQuotaForSponsorURLRejectsWrongDate(t *testing.T) {
	isolateQuotaState(t)
	device := DeviceCodeV7{CPUHash: "cpu", UUIDHash: "uuid"}

	_, err := refillQuotaForSponsorURLAt(
		"2026-06-03",
		sponsorURLForDevice(device),
		time.Date(2026, 6, 4, 12, 0, 0, 0, time.Local),
		func() DeviceCodeV7 { return device },
	)
	if !errors.Is(err, ErrRefillDateMismatch) {
		t.Fatalf("err = %v, want ErrRefillDateMismatch", err)
	}
}

func TestRefillQuotaForSponsorURLRejectsWrongDevice(t *testing.T) {
	isolateQuotaState(t)
	target := DeviceCodeV7{
		CPUHash:   "cpu-a",
		UUIDHash:  "uuid-a",
		BIOSHash:  "bios-a",
		BoardHash: "board-a",
		DiskHash:  "disk-a",
		GUIDHash:  "guid-a",
	}
	current := DeviceCodeV7{
		CPUHash:   "cpu-b",
		UUIDHash:  "uuid-b",
		BIOSHash:  "bios-b",
		BoardHash: "board-b",
		DiskHash:  "disk-b",
		GUIDHash:  "guid-b",
	}

	_, err := refillQuotaForSponsorURLAt(
		"2026-06-03",
		sponsorURLForDevice(target),
		time.Date(2026, 6, 3, 12, 0, 0, 0, time.Local),
		func() DeviceCodeV7 { return current },
	)
	if !errors.Is(err, ErrRefillDeviceMismatch) {
		t.Fatalf("err = %v, want ErrRefillDeviceMismatch", err)
	}
}
