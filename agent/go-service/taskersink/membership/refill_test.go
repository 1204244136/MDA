package membership

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

const testCouponID = "00112233445566778899aabbccddeeff"

func refillTestDevice() DeviceCodeV7 {
	return DeviceCodeV7{
		CPUHash:   "cpu",
		UUIDHash:  "uuid",
		BIOSHash:  "bios",
		BoardHash: "board",
		DiskHash:  "disk",
		GUIDHash:  "guid",
	}
}

func sponsorURLForDevice(device DeviceCodeV7) string {
	status := &MembershipStatus{DeviceCode: device}
	return SponsorURL(status)
}

func testRefillCoupon(refillType QuotaRefillType, sponsorURL string) QuotaRefillCoupon {
	return QuotaRefillCoupon{
		ID:         testCouponID,
		IssuedOn:   "2026-06-03",
		ValidDays:  7,
		RefillType: refillType,
		SponsorURL: sponsorURL,
	}
}

func TestRedeemQuotaRefillCouponDailyOnlyClearsDailyPool(t *testing.T) {
	path := isolateQuotaState(t)
	device := refillTestDevice()
	specialBefore := quotaPoolState{
		PeriodKey:    "2026-06-01..2026-07-01",
		LimitSeconds: 3600,
		UsedSeconds:  500,
		UpdatedAt:    "2026-06-02T12:00:00+08:00",
	}
	mustSaveQuotaState(t, path, quotaState{
		Version:      2,
		DeviceHash:   deviceHash(device),
		TierCode:     "orange_plus",
		BusinessDate: "2026-06-02",
		LimitSeconds: 600,
		UsedSeconds:  725,
		UpdatedAt:    "2026-06-02T12:00:00+08:00",
		Pools: map[string]quotaPoolState{
			string(quotaPoolRegularDaily): {
				PeriodKey:    "2026-06-02",
				LimitSeconds: 600,
				UsedSeconds:  725,
			},
			string(quotaPoolSpecialPeriod): specialBefore,
		},
	})

	result, err := redeemQuotaRefillCouponAt(
		testRefillCoupon(QuotaRefillTypeDaily, ""),
		time.Date(2026, 6, 3, 12, 0, 0, 0, beijingLocation),
		func() DeviceCodeV7 { return device },
	)
	if err != nil {
		t.Fatalf("redeemQuotaRefillCouponAt() failed: %v", err)
	}
	if result.ValidThrough != "2026-06-09" {
		t.Fatalf("ValidThrough = %s, want 2026-06-09", result.ValidThrough)
	}

	state := mustLoadQuotaState(t, path)
	regular := state.Pools[string(quotaPoolRegularDaily)]
	if regular.UsedSeconds != 0 || regular.PeriodKey != "2026-06-03" {
		t.Fatalf("daily pool was not reset correctly: %+v", regular)
	}
	if got := state.Pools[string(quotaPoolSpecialPeriod)]; !reflect.DeepEqual(got, specialBefore) {
		t.Fatalf("monthly pool changed: got %+v, want %+v", got, specialBefore)
	}
	if state.UsedSeconds != 0 || state.BusinessDate != "2026-06-03" {
		t.Fatalf("legacy daily mirror was not reset: %+v", state)
	}
	if state.Version != quotaStateVersion {
		t.Fatalf("Version = %d, want %d", state.Version, quotaStateVersion)
	}
	if _, ok := state.RedeemedCoupons[testCouponID]; !ok {
		t.Fatal("coupon redemption was not recorded")
	}
}

func TestRedeemQuotaRefillCouponMonthlyOnlyClearsMonthlyPool(t *testing.T) {
	path := isolateQuotaState(t)
	device := refillTestDevice()
	regularBefore := quotaPoolState{
		PeriodKey:    "2026-06-02",
		LimitSeconds: 600,
		UsedSeconds:  725,
		UpdatedAt:    "2026-06-02T12:00:00+08:00",
	}
	mustSaveQuotaState(t, path, quotaState{
		Version:      2,
		DeviceHash:   deviceHash(device),
		TierCode:     "orange_plus",
		BusinessDate: "2026-06-02",
		LimitSeconds: 600,
		UsedSeconds:  725,
		UpdatedAt:    "2026-06-02T12:00:00+08:00",
		Pools: map[string]quotaPoolState{
			string(quotaPoolRegularDaily): regularBefore,
			string(quotaPoolSpecialPeriod): {
				PeriodKey:    "2026-06-01..2026-07-01",
				LimitSeconds: 3600,
				UsedSeconds:  500,
			},
		},
	})

	_, err := redeemQuotaRefillCouponAt(
		testRefillCoupon(QuotaRefillTypeMonthly, ""),
		time.Date(2026, 6, 3, 12, 0, 0, 0, beijingLocation),
		func() DeviceCodeV7 { return device },
	)
	if err != nil {
		t.Fatalf("redeemQuotaRefillCouponAt() failed: %v", err)
	}

	state := mustLoadQuotaState(t, path)
	if got := state.Pools[string(quotaPoolRegularDaily)]; !reflect.DeepEqual(got, regularBefore) {
		t.Fatalf("daily pool changed: got %+v, want %+v", got, regularBefore)
	}
	monthly := state.Pools[string(quotaPoolSpecialPeriod)]
	if monthly.UsedSeconds != 0 {
		t.Fatalf("monthly pool was not reset: %+v", monthly)
	}
	if state.BusinessDate != "2026-06-02" || state.UsedSeconds != 725 {
		t.Fatalf("legacy daily mirror changed: %+v", state)
	}
}

func TestUniversalCouponKeepsOtherPoolWhenDeviceLookupFails(t *testing.T) {
	path := isolateQuotaState(t)
	device := refillTestDevice()
	specialBefore := quotaPoolState{
		PeriodKey:    "2026-06-01..2026-07-01",
		LimitSeconds: 3600,
		UsedSeconds:  500,
	}
	mustSaveQuotaState(t, path, quotaState{
		Version:    2,
		DeviceHash: deviceHash(device),
		Pools: map[string]quotaPoolState{
			string(quotaPoolRegularDaily): {
				PeriodKey:    "2026-06-03",
				LimitSeconds: 600,
				UsedSeconds:  500,
			},
			string(quotaPoolSpecialPeriod): specialBefore,
		},
	})

	_, err := redeemQuotaRefillCouponAt(
		testRefillCoupon(QuotaRefillTypeDaily, ""),
		time.Date(2026, 6, 3, 12, 0, 0, 0, beijingLocation),
		func() DeviceCodeV7 { return DeviceCodeV7{} },
	)
	if err != nil {
		t.Fatalf("redeemQuotaRefillCouponAt() failed: %v", err)
	}
	state := mustLoadQuotaState(t, path)
	if got := state.Pools[string(quotaPoolSpecialPeriod)]; !reflect.DeepEqual(got, specialBefore) {
		t.Fatalf("monthly pool changed after device lookup failure: got %+v, want %+v", got, specialBefore)
	}
}

func TestCouponRejectsQuotaStateFromDifferentDevice(t *testing.T) {
	path := isolateQuotaState(t)
	oldDevice := refillTestDevice()
	currentDevice := oldDevice
	currentDevice.UUIDHash = "new-uuid"
	mustSaveQuotaState(t, path, quotaState{
		Version:    quotaStateVersion,
		DeviceHash: deviceHash(oldDevice),
		Pools: map[string]quotaPoolState{
			string(quotaPoolRegularDaily):  {UsedSeconds: 500},
			string(quotaPoolSpecialPeriod): {UsedSeconds: 700},
		},
	})
	before := mustLoadQuotaState(t, path)

	_, err := redeemQuotaRefillCouponAt(
		testRefillCoupon(QuotaRefillTypeDaily, ""),
		time.Date(2026, 6, 3, 12, 0, 0, 0, beijingLocation),
		func() DeviceCodeV7 { return currentDevice },
	)
	if !errors.Is(err, ErrRefillStateMismatch) {
		t.Fatalf("err = %v, want ErrRefillStateMismatch", err)
	}
	after := mustLoadQuotaState(t, path)
	if !reflect.DeepEqual(after, before) {
		t.Fatal("state mismatch redemption changed quota state")
	}
}

func TestCouponRechecksValidityAfterAcquiringLock(t *testing.T) {
	path := isolateQuotaState(t)
	times := []time.Time{
		time.Date(2026, 6, 9, 23, 59, 59, 0, beijingLocation),
		time.Date(2026, 6, 10, 0, 0, 0, 0, beijingLocation),
	}
	clockCalls := 0
	clock := func() time.Time {
		index := clockCalls
		if index >= len(times) {
			index = len(times) - 1
		}
		clockCalls++
		return times[index]
	}

	_, err := redeemQuotaRefillCoupon(testRefillCoupon(QuotaRefillTypeDaily, ""), clock, refillTestDevice)
	if !errors.Is(err, ErrRefillExpired) {
		t.Fatalf("err = %v, want ErrRefillExpired", err)
	}
	if clockCalls < 2 {
		t.Fatalf("clock called %d times, want at least 2", clockCalls)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("quota state changed after expiration at commit: %v", err)
	}
}

func TestRedeemQuotaRefillCouponValidityBoundaries(t *testing.T) {
	tests := []struct {
		name    string
		now     time.Time
		wantErr error
	}{
		{name: "before issue date", now: time.Date(2026, 6, 2, 23, 59, 0, 0, beijingLocation), wantErr: ErrRefillNotYetValid},
		{name: "first day", now: time.Date(2026, 6, 3, 0, 0, 0, 0, beijingLocation)},
		{name: "last day", now: time.Date(2026, 6, 9, 23, 59, 59, 0, beijingLocation)},
		{name: "after validity", now: time.Date(2026, 6, 10, 0, 0, 0, 0, beijingLocation), wantErr: ErrRefillExpired},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateQuotaState(t)
			_, err := redeemQuotaRefillCouponAt(
				testRefillCoupon(QuotaRefillTypeDaily, ""),
				tt.now,
				refillTestDevice,
			)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestRedeemQuotaRefillCouponRejectsWrongDevice(t *testing.T) {
	isolateQuotaState(t)
	target := refillTestDevice()
	current := target
	current.UUIDHash = "other-uuid"
	current.CPUHash = "other-cpu"

	_, err := redeemQuotaRefillCouponAt(
		testRefillCoupon(QuotaRefillTypeDaily, sponsorURLForDevice(target)),
		time.Date(2026, 6, 3, 12, 0, 0, 0, beijingLocation),
		func() DeviceCodeV7 { return current },
	)
	if !errors.Is(err, ErrRefillDeviceMismatch) {
		t.Fatalf("err = %v, want ErrRefillDeviceMismatch", err)
	}
}

func TestRedeemQuotaRefillCouponAcceptsMatchingSponsorURL(t *testing.T) {
	isolateQuotaState(t)
	device := refillTestDevice()

	_, err := redeemQuotaRefillCouponAt(
		testRefillCoupon(QuotaRefillTypeDaily, sponsorURLForDevice(device)),
		time.Date(2026, 6, 3, 12, 0, 0, 0, beijingLocation),
		func() DeviceCodeV7 { return device },
	)
	if err != nil {
		t.Fatalf("redeemQuotaRefillCouponAt() failed: %v", err)
	}
}

func TestRedeemQuotaRefillCouponOnlyOnce(t *testing.T) {
	path := isolateQuotaState(t)
	coupon := testRefillCoupon(QuotaRefillTypeDaily, "")
	now := time.Date(2026, 6, 3, 12, 0, 0, 0, beijingLocation)

	if _, err := redeemQuotaRefillCouponAt(coupon, now, refillTestDevice); err != nil {
		t.Fatalf("first redemption failed: %v", err)
	}
	before := mustLoadQuotaState(t, path)
	if _, err := redeemQuotaRefillCouponAt(coupon, now.Add(time.Hour), refillTestDevice); !errors.Is(err, ErrRefillAlreadyRedeemed) {
		t.Fatalf("second redemption err = %v, want ErrRefillAlreadyRedeemed", err)
	}
	after := mustLoadQuotaState(t, path)
	if !reflect.DeepEqual(after, before) {
		t.Fatal("duplicate redemption changed quota state")
	}
}

func TestRedeemQuotaRefillCouponConcurrentOnlyOneSucceeds(t *testing.T) {
	isolateQuotaState(t)
	coupon := testRefillCoupon(QuotaRefillTypeDaily, "")
	now := time.Date(2026, 6, 3, 12, 0, 0, 0, beijingLocation)
	const callers = 8
	start := make(chan struct{})
	results := make(chan error, callers)
	for range callers {
		go func() {
			<-start
			_, err := redeemQuotaRefillCouponAt(coupon, now, refillTestDevice)
			results <- err
		}()
	}
	close(start)

	succeeded := 0
	alreadyRedeemed := 0
	for range callers {
		err := <-results
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrRefillAlreadyRedeemed):
			alreadyRedeemed++
		default:
			t.Fatalf("unexpected redemption error: %v", err)
		}
	}
	if succeeded != 1 || alreadyRedeemed != callers-1 {
		t.Fatalf("success = %d, already redeemed = %d", succeeded, alreadyRedeemed)
	}
}

func TestRefillCouponProcessHelper(t *testing.T) {
	if os.Getenv("MDA_REFILL_PROCESS_HELPER") != "1" {
		return
	}
	syncDir := os.Getenv("MDA_REFILL_SYNC_DIR")
	worker := os.Getenv("MDA_REFILL_WORKER")
	if err := os.WriteFile(filepath.Join(syncDir, "ready-"+worker), []byte("ready"), 0644); err != nil {
		t.Fatalf("write ready marker: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(syncDir, "release")); err == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	_, err := redeemQuotaRefillCouponAt(
		testRefillCoupon(QuotaRefillTypeDaily, ""),
		time.Date(2026, 6, 3, 12, 0, 0, 0, beijingLocation),
		refillTestDevice,
	)
	switch {
	case err == nil:
		fmt.Println("REFILL_RESULT=success")
	case errors.Is(err, ErrRefillAlreadyRedeemed):
		fmt.Println("REFILL_RESULT=already_redeemed")
	default:
		t.Fatalf("redeemQuotaRefillCouponAt() failed: %v", err)
	}
}

func TestRedeemQuotaRefillCouponAcrossProcessesOnlyOneSucceeds(t *testing.T) {
	path := isolateQuotaState(t)
	syncDir := t.TempDir()
	const processCount = 4
	commands := make([]*exec.Cmd, 0, processCount)
	outputs := make([]bytes.Buffer, processCount)
	for i := range processCount {
		cmd := exec.Command(os.Args[0], "-test.run=^TestRefillCouponProcessHelper$")
		cmd.Env = append(os.Environ(),
			"MDA_REFILL_PROCESS_HELPER=1",
			"MDA_REFILL_SYNC_DIR="+syncDir,
			"MDA_REFILL_WORKER="+strconv.Itoa(i),
		)
		cmd.Stdout = &outputs[i]
		cmd.Stderr = &outputs[i]
		if err := cmd.Start(); err != nil {
			t.Fatalf("start helper %d: %v", i, err)
		}
		commands = append(commands, cmd)
	}

	deadline := time.Now().Add(20 * time.Second)
	for {
		ready := 0
		for i := range processCount {
			if _, err := os.Stat(filepath.Join(syncDir, "ready-"+strconv.Itoa(i))); err == nil {
				ready++
			}
		}
		if ready == processCount {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for refill helper processes")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := os.WriteFile(filepath.Join(syncDir, "release"), []byte("go"), 0644); err != nil {
		t.Fatalf("write release marker: %v", err)
	}

	succeeded := 0
	alreadyRedeemed := 0
	for i, cmd := range commands {
		if err := cmd.Wait(); err != nil {
			t.Fatalf("helper %d failed: %v\n%s", i, err, outputs[i].String())
		}
		output := outputs[i].String()
		switch {
		case strings.Contains(output, "REFILL_RESULT=success"):
			succeeded++
		case strings.Contains(output, "REFILL_RESULT=already_redeemed"):
			alreadyRedeemed++
		default:
			t.Fatalf("helper %d returned no result:\n%s", i, output)
		}
	}
	if succeeded != 1 || alreadyRedeemed != processCount-1 {
		t.Fatalf("success = %d, already redeemed = %d", succeeded, alreadyRedeemed)
	}
	state := mustLoadQuotaState(t, path)
	if len(state.RedeemedCoupons) != 1 {
		t.Fatalf("redeemed coupon count = %d, want 1", len(state.RedeemedCoupons))
	}
}

func TestRedeemQuotaRefillCouponRejectsMalformedState(t *testing.T) {
	path := isolateQuotaState(t)
	malformed := []byte(`{"version":3,"redeemed_coupons":`)
	if err := os.WriteFile(path, malformed, 0644); err != nil {
		t.Fatalf("WriteFile() failed: %v", err)
	}

	_, err := redeemQuotaRefillCouponAt(
		testRefillCoupon(QuotaRefillTypeDaily, ""),
		time.Date(2026, 6, 3, 12, 0, 0, 0, beijingLocation),
		refillTestDevice,
	)
	if err == nil {
		t.Fatal("redemption accepted malformed quota state")
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("ReadFile() failed: %v", readErr)
	}
	if string(got) != string(malformed) {
		t.Fatal("malformed quota state was overwritten")
	}
}

func TestRefillQuotaForSponsorURLClearsAllPoolsForLegacyCallers(t *testing.T) {
	path := isolateQuotaState(t)
	device := refillTestDevice()
	mustSaveQuotaState(t, path, quotaState{
		Version:    2,
		DeviceHash: deviceHash(device),
		TierCode:   "orange_plus",
		Pools: map[string]quotaPoolState{
			string(quotaPoolRegularDaily): {
				PeriodKey:    "2026-06-03",
				LimitSeconds: 600,
				UsedSeconds:  725,
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

	state := mustLoadQuotaState(t, path)
	regular := state.Pools[string(quotaPoolRegularDaily)]
	if regular.UsedSeconds != 0 {
		t.Fatalf("regular pool was not cleared: %+v", regular)
	}
	special := state.Pools[string(quotaPoolSpecialPeriod)]
	if special.UsedSeconds != 0 {
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
	target := refillTestDevice()
	current := target
	current.CPUHash = "cpu-b"
	current.UUIDHash = "uuid-b"

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
