package membership

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func resetMembershipTestGlobals(t *testing.T) {
	t.Helper()

	oldAppVersion := appVersion
	oldClientName := clientName
	oldGenerateDeviceCodeV7 := generateDeviceCodeV7
	oldFetchMemberStatusFn := fetchMemberStatusFn

	t.Cleanup(func() {
		appVersion = oldAppVersion
		clientName = oldClientName
		generateDeviceCodeV7 = oldGenerateDeviceCodeV7
		fetchMemberStatusFn = oldFetchMemberStatusFn

		cachedStatusMu.Lock()
		cachedStatus = nil
		cachedStatusTime = time.Time{}
		cachedStatusMu.Unlock()

		deviceCodeMu.Lock()
		cachedDeviceCode = DeviceCodeV7{}
		deviceCodeCached = false
		deviceCodeMu.Unlock()
	})

	cachedStatusMu.Lock()
	cachedStatus = nil
	cachedStatusTime = time.Time{}
	cachedStatusMu.Unlock()

	deviceCodeMu.Lock()
	cachedDeviceCode = DeviceCodeV7{}
	deviceCodeCached = false
	deviceCodeMu.Unlock()
}

func TestStatusFromResponseUsesNewQuotaFields(t *testing.T) {
	status := statusFromResponse(&MemberStatusResponse{
		TierCode:                    "orange_pro",
		TierName:                    "Orange Pro",
		DailyRuntimeMinutes:         180,
		RegularDailyRuntimeMinutes:  60,
		SpecialPeriodRuntimeMinutes: 300,
		PaidThroughOn:               "20260701",
		HasFutureRenewal:            true,
	}, DeviceCodeV7{})

	if status.PaidThroughOn != "20260701" {
		t.Fatalf("PaidThroughOn = %q, want 20260701", status.PaidThroughOn)
	}
	if !status.HasFutureRenewal {
		t.Fatalf("HasFutureRenewal = false, want true")
	}
	if status.RegularDailyRuntimeMinutes != 60 {
		t.Fatalf("RegularDailyRuntimeMinutes = %d, want 60", status.RegularDailyRuntimeMinutes)
	}
	if status.DailyRuntimeMinutes != 60 {
		t.Fatalf("DailyRuntimeMinutes = %d, want compatibility alias 60", status.DailyRuntimeMinutes)
	}
	if status.SpecialPeriodRuntimeMinutes != 300 {
		t.Fatalf("SpecialPeriodRuntimeMinutes = %d, want 300", status.SpecialPeriodRuntimeMinutes)
	}
}

func TestStatusFromResponseFallsBackToTierSpecialQuota(t *testing.T) {
	status := statusFromResponse(&MemberStatusResponse{
		TierCode:            "orange_plus",
		TierName:            "Orange Plus",
		DailyRuntimeMinutes: 30,
	}, DeviceCodeV7{})

	if status.RegularDailyRuntimeMinutes != 30 {
		t.Fatalf("RegularDailyRuntimeMinutes = %d, want 30", status.RegularDailyRuntimeMinutes)
	}
	if status.SpecialPeriodRuntimeMinutes != 120 {
		t.Fatalf("SpecialPeriodRuntimeMinutes = %d, want fallback 120", status.SpecialPeriodRuntimeMinutes)
	}
}

func TestCheckMembershipUnavailableFallsBackToFreeStatus(t *testing.T) {
	resetMembershipTestGlobals(t)
	appVersion = "1.0.0"
	clientName = "MFAWPF"
	generateDeviceCodeV7 = func() DeviceCodeV7 {
		return DeviceCodeV7{CPUHash: "cpu-hash"}
	}
	fetchMemberStatusFn = func(DeviceCodeV7) (*MemberStatusResponse, error) {
		return nil, errors.New("temporary service failure")
	}

	status := checkMembership()

	if !status.VerificationUnavailable {
		t.Fatalf("VerificationUnavailable = false, want true")
	}
	if status.UpdateRequired {
		t.Fatalf("UpdateRequired = true, want false")
	}
	if status.IsMember {
		t.Fatalf("IsMember = true, want false")
	}
	if status.TierCode != "orange_free" {
		t.Fatalf("TierCode = %q, want orange_free", status.TierCode)
	}
	if status.RegularDailyRuntimeMinutes != 10 {
		t.Fatalf("RegularDailyRuntimeMinutes = %d, want 10", status.RegularDailyRuntimeMinutes)
	}
	if status.DeviceCode.CPUHash != "cpu-hash" {
		t.Fatalf("DeviceCode.CPUHash = %q, want cpu-hash", status.DeviceCode.CPUHash)
	}
}

func TestGetMembershipStatusCachesUnmatchedFreeStatus(t *testing.T) {
	resetMembershipTestGlobals(t)
	appVersion = "1.0.0"
	clientName = "MFAWPF"

	var deviceCodeCalls int
	var fetchCalls int
	generateDeviceCodeV7 = func() DeviceCodeV7 {
		deviceCodeCalls++
		return DeviceCodeV7{CPUHash: "cpu-hash"}
	}
	fetchMemberStatusFn = func(DeviceCodeV7) (*MemberStatusResponse, error) {
		fetchCalls++
		return &MemberStatusResponse{Matched: false, Score: 75}, nil
	}

	first := GetMembershipStatus()
	second := GetMembershipStatus()

	if first != second {
		t.Fatal("GetMembershipStatus() did not return the cached free status")
	}
	if deviceCodeCalls != 1 {
		t.Fatalf("device code generated %d times, want 1", deviceCodeCalls)
	}
	if fetchCalls != 1 {
		t.Fatalf("membership status fetched %d times, want 1", fetchCalls)
	}
}

func TestGetMembershipStatusReusesDeviceCodeAfterStatusExpiry(t *testing.T) {
	resetMembershipTestGlobals(t)
	appVersion = "1.0.0"
	clientName = "MFAWPF"

	var deviceCodeCalls int
	var fetchCalls int
	generateDeviceCodeV7 = func() DeviceCodeV7 {
		deviceCodeCalls++
		return DeviceCodeV7{CPUHash: "cpu-hash"}
	}
	fetchMemberStatusFn = func(DeviceCodeV7) (*MemberStatusResponse, error) {
		fetchCalls++
		return &MemberStatusResponse{Matched: false, Score: 75}, nil
	}

	GetMembershipStatus()
	cachedStatusMu.Lock()
	cachedStatusTime = time.Now().Add(-cacheExpiry)
	cachedStatusMu.Unlock()
	GetMembershipStatus()

	if deviceCodeCalls != 1 {
		t.Fatalf("device code generated %d times, want 1", deviceCodeCalls)
	}
	if fetchCalls != 2 {
		t.Fatalf("membership status fetched %d times, want 2", fetchCalls)
	}
}

func TestGetMembershipStatusCoalescesConcurrentChecks(t *testing.T) {
	resetMembershipTestGlobals(t)
	appVersion = "1.0.0"
	clientName = "MFAWPF"

	var fetchCalls atomic.Int32
	generateDeviceCodeV7 = func() DeviceCodeV7 {
		return DeviceCodeV7{CPUHash: "cpu-hash"}
	}
	started := make(chan struct{})
	release := make(chan struct{})
	fetchMemberStatusFn = func(DeviceCodeV7) (*MemberStatusResponse, error) {
		if fetchCalls.Add(1) == 1 {
			close(started)
		}
		<-release
		return &MemberStatusResponse{Matched: false, Score: 75}, nil
	}

	const callers = 8
	var wg sync.WaitGroup
	wg.Add(callers)
	for range callers {
		go func() {
			defer wg.Done()
			GetMembershipStatus()
		}()
	}
	<-started
	close(release)
	wg.Wait()

	if got := fetchCalls.Load(); got != 1 {
		t.Fatalf("membership status fetched %d times, want 1", got)
	}
}
