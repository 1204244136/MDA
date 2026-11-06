package membership

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestFetchMemberStatusCancelsSlowResponse(t *testing.T) {
	for _, sendHeaders := range []bool{false, true} {
		t.Run(fmt.Sprintf("headers_%t", sendHeaders), func(t *testing.T) {
			var calls atomic.Int32
			release := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				if sendHeaders {
					w.WriteHeader(http.StatusOK)
					w.(http.Flusher).Flush()
				}
				<-release
			}))
			defer func() {
				close(release)
				server.Close()
			}()
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()

			started := time.Now()
			_, err := fetchMemberStatusFrom(ctx, DeviceCodeV7{}, server.URL, &http.Transport{})
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("error = %v, want context deadline exceeded", err)
			}
			if elapsed := time.Since(started); elapsed > time.Second {
				t.Fatalf("fetch took %v after a 100ms deadline", elapsed)
			}
			if got := calls.Load(); got != 1 {
				t.Fatalf("membership status fetched %d times, want 1", got)
			}
		})
	}
}

func TestFetchMemberStatusCancelsRetryWait(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		http.Error(w, "temporary failure", http.StatusBadGateway)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	started := time.Now()
	_, err := fetchMemberStatusFrom(ctx, DeviceCodeV7{}, server.URL, &http.Transport{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context deadline exceeded", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("retry wait took %v after a 100ms deadline", elapsed)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("membership status fetched %d times after deadline, want 1", got)
	}
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
	if status.UsingPersistentCache {
		t.Fatalf("UsingPersistentCache = true, want false for Orange Free fallback")
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

func TestCheckMembershipUnavailableUsesCachedMemberNotice(t *testing.T) {
	resetMembershipTestGlobals(t)
	isolateQuotaState(t)
	initTestI18n()
	appVersion = "1.0.0"
	clientName = "MXU"
	device := DeviceCodeV7{
		CPUHash:   "cached-cpu",
		UUIDHash:  "cached-uuid",
		BIOSHash:  "cached-bios",
		BoardHash: "cached-board",
		DiskHash:  "cached-disk",
		GUIDHash:  "cached-guid",
	}
	generateDeviceCodeV7 = func() DeviceCodeV7 { return device }
	fetchMemberStatusFn = func(DeviceCodeV7) (*MemberStatusResponse, error) {
		return nil, errors.New("temporary service failure")
	}
	savePersistentMemberStatus(device, &MemberStatusResponse{
		Matched:                     true,
		IsMember:                    true,
		TierCode:                    "orange_plus",
		TierName:                    "Orange Plus",
		RegularDailyRuntimeMinutes:  30,
		SpecialPeriodRuntimeMinutes: 120,
		ExpiresOn:                   time.Now().In(beijingLocation).AddDate(0, 1, 0).Format("20060102"),
	})

	status := GetMembershipStatus()
	if !status.VerificationUnavailable || !status.UsingPersistentCache || !status.IsMember {
		t.Fatalf("expected cached membership after verification failure, got %+v", status)
	}
	if status.RegularDailyRuntimeMinutes != 30 {
		t.Fatalf("cached regular quota = %d minutes, want 30", status.RegularDailyRuntimeMinutes)
	}
	message := formatMembershipVerificationUnavailableMessage(status)
	if !strings.Contains(message, "Orange Plus") || strings.Contains(message, "Orange Free") {
		t.Fatalf("cached membership notice does not match the actual quota tier: %s", message)
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
