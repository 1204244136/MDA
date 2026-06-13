package membership

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

var (
	appVersion string
	clientName string
)

// SetVersion sets the application version for debug-mode detection.
func SetVersion(v string) {
	appVersion = v
}

// SetClientName sets the PI client name for debug-mode detection.
func SetClientName(v string) {
	clientName = v
}

// isDebugVersion returns true when the version is below 1.0.0 (dev builds, pre-release).
func isDebugVersion() bool {
	if appVersion == "" || appVersion == "dev" {
		return true
	}
	v := strings.TrimPrefix(appVersion, "v")
	parts := strings.SplitN(v, ".", 3)
	if len(parts) == 0 {
		return true
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return true
	}
	return major < 1
}

func isVSCodeClient() bool {
	return strings.EqualFold(clientName, "VsCode")
}

func isDebugEnvironment() bool {
	return isDebugVersion() || isVSCodeClient()
}

type MemberStatusResponse struct {
	Matched                     bool   `json:"matched"`
	Score                       int    `json:"score"`
	IsMember                    bool   `json:"is_member"`
	UserID                      string `json:"user_id"`
	Tier                        string `json:"tier"`
	TierCode                    string `json:"tier_code"`
	TierName                    string `json:"tier_name"`
	PlanCode                    string `json:"plan_code"`
	PlanName                    string `json:"plan_name"`
	StartsOn                    string `json:"starts_on"`
	ExpiresOn                   string `json:"expires_on"`
	PaidThroughOn               string `json:"paid_through_on"`
	HasFutureRenewal            bool   `json:"has_future_renewal"`
	RemainingDays               int    `json:"remaining_days"`
	DailyRuntimeMinutes         int    `json:"daily_runtime_minutes"`
	RegularDailyRuntimeMinutes  int    `json:"regular_daily_runtime_minutes"`
	SpecialPeriodRuntimeMinutes int    `json:"special_period_runtime_minutes"`
	AllFeaturesUnlocked         bool   `json:"all_features_unlocked"`
}

type updateRequiredResponse struct {
	Error                   string `json:"error"`
	UpdateRequired          bool   `json:"update_required"`
	MinimumSupportedVersion string `json:"minimum_supported_version"`
}

type updateRequiredError struct {
	Message                 string
	MinimumSupportedVersion string
}

func (e *updateRequiredError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.MinimumSupportedVersion != "" {
		return "MDA version is no longer supported; update to " + e.MinimumSupportedVersion + " or later"
	}
	return "MDA version is no longer supported"
}

// MembershipStatus represents the current membership state.
type MembershipStatus struct {
	Tier                        string
	TierCode                    string
	TierName                    string
	PlanCode                    string
	PlanName                    string
	StartsOn                    string
	ExpiresOn                   string
	PaidThroughOn               string
	HasFutureRenewal            bool
	RemainingDays               int
	DailyRuntimeMinutes         int
	RegularDailyRuntimeMinutes  int
	SpecialPeriodRuntimeMinutes int
	AllFeaturesUnlocked         bool
	UnlimitedRuntime            bool
	IsMember                    bool
	UpdateRequired              bool
	UpdateMessage               string
	MinimumSupportedVersion     string
	UserID                      string
	DeviceCode                  DeviceCodeV7
}

var (
	cachedStatus     *MembershipStatus
	cachedStatusMu   sync.RWMutex
	cachedStatusTime time.Time
	cachedDeviceCode DeviceCodeV7
)

const (
	cacheExpiry      = 1 * time.Hour
	httpTimeout      = 15 * time.Second
	maxFetchAttempts = 3
)

// GetMembershipStatus returns the current membership status, using cache if available.
func GetMembershipStatus() *MembershipStatus {
	cachedStatusMu.RLock()
	if cachedStatus != nil && time.Since(cachedStatusTime) < cacheExpiry {
		status := cachedStatus
		cachedStatusMu.RUnlock()
		return status
	}
	cachedStatusMu.RUnlock()

	return checkMembership()
}

// checkMembership performs the full membership check flow.
func checkMembership() *MembershipStatus {
	if isDebugEnvironment() {
		log.Info().
			Str("version", appVersion).
			Str("client_name", clientName).
			Msg("Debug environment detected, bypassing membership verification")
		return &MembershipStatus{
			Tier:                "Debug",
			TierCode:            "debug",
			TierName:            "Debug",
			PlanCode:            "debug",
			PlanName:            "Debug",
			StartsOn:            "00000000",
			ExpiresOn:           "99991231",
			RemainingDays:       9999,
			AllFeaturesUnlocked: true,
			UnlimitedRuntime:    true,
			IsMember:            true,
		}
	}

	deviceCode := GenerateDeviceCodeV7()
	cachedDeviceCode = deviceCode

	defaultStatus := &MembershipStatus{
		Tier:                        "Orange Free",
		TierCode:                    "orange_free",
		TierName:                    "Orange Free",
		PlanName:                    "Orange Free",
		DailyRuntimeMinutes:         10,
		RegularDailyRuntimeMinutes:  10,
		SpecialPeriodRuntimeMinutes: 0,
		AllFeaturesUnlocked:         true,
		IsMember:                    false,
		DeviceCode:                  deviceCode,
	}

	log.Info().
		Str("cpu_hash", shortHash(deviceCode.CPUHash)).
		Str("uuid_hash", shortHash(deviceCode.UUIDHash)).
		Msg("Generated V7 device code")

	response, err := fetchMemberStatus(deviceCode)
	if err != nil {
		var updateErr *updateRequiredError
		if errors.As(err, &updateErr) {
			status := &MembershipStatus{
				Tier:                        defaultStatus.Tier,
				TierCode:                    defaultStatus.TierCode,
				TierName:                    defaultStatus.TierName,
				PlanName:                    defaultStatus.PlanName,
				DailyRuntimeMinutes:         defaultStatus.DailyRuntimeMinutes,
				RegularDailyRuntimeMinutes:  defaultStatus.RegularDailyRuntimeMinutes,
				SpecialPeriodRuntimeMinutes: defaultStatus.SpecialPeriodRuntimeMinutes,
				AllFeaturesUnlocked:         defaultStatus.AllFeaturesUnlocked,
				UpdateRequired:              true,
				UpdateMessage:               updateErr.Message,
				MinimumSupportedVersion:     updateErr.MinimumSupportedVersion,
				DeviceCode:                  deviceCode,
			}
			cacheStatus(status)
			return status
		}
		log.Warn().Err(err).Msg("Membership verification unavailable, treating as non-member for this check")
		return defaultStatus
	}

	if !response.Matched {
		log.Info().Int("score", response.Score).Msg("No matching member device found, using Orange Free quota")
		return defaultStatus
	}

	status := statusFromResponse(response, deviceCode)
	log.Info().
		Str("user_id", status.UserID).
		Int("score", response.Score).
		Str("tier", status.Tier).
		Str("plan_code", status.PlanCode).
		Str("plan_name", status.PlanName).
		Str("expiry", status.ExpiresOn).
		Int("remaining_days", status.RemainingDays).
		Msg("Matched active member subscription")

	cacheStatus(status)
	return status
}

func cacheStatus(status *MembershipStatus) {
	cachedStatusMu.Lock()
	cachedStatus = status
	cachedStatusTime = time.Now()
	cachedStatusMu.Unlock()
}

func statusFromResponse(response *MemberStatusResponse, deviceCode DeviceCodeV7) *MembershipStatus {
	tierCode := response.TierCode
	if tierCode == "" {
		tierCode = "orange_free"
	}
	tierName := response.TierName
	if tierName == "" {
		tierName = response.Tier
	}
	if tierName == "" {
		tierName = "Orange Free"
	}
	planName := response.PlanName
	if planName == "" {
		planName = tierName
	}
	regularDailyRuntimeMinutes := response.RegularDailyRuntimeMinutes
	if regularDailyRuntimeMinutes <= 0 {
		regularDailyRuntimeMinutes = response.DailyRuntimeMinutes
	}
	if regularDailyRuntimeMinutes <= 0 {
		regularDailyRuntimeMinutes = 10
	}
	specialPeriodRuntimeMinutes := response.SpecialPeriodRuntimeMinutes
	if specialPeriodRuntimeMinutes <= 0 {
		specialPeriodRuntimeMinutes = defaultSpecialPeriodRuntimeMinutes(tierCode)
	}

	return &MembershipStatus{
		Tier:                        tierName,
		TierCode:                    tierCode,
		TierName:                    tierName,
		PlanCode:                    response.PlanCode,
		PlanName:                    planName,
		StartsOn:                    response.StartsOn,
		ExpiresOn:                   response.ExpiresOn,
		PaidThroughOn:               response.PaidThroughOn,
		HasFutureRenewal:            response.HasFutureRenewal,
		RemainingDays:               response.RemainingDays,
		DailyRuntimeMinutes:         regularDailyRuntimeMinutes,
		RegularDailyRuntimeMinutes:  regularDailyRuntimeMinutes,
		SpecialPeriodRuntimeMinutes: specialPeriodRuntimeMinutes,
		AllFeaturesUnlocked:         response.AllFeaturesUnlocked,
		IsMember:                    response.IsMember,
		UserID:                      response.UserID,
		DeviceCode:                  deviceCode,
	}
}

func defaultSpecialPeriodRuntimeMinutes(tierCode string) int {
	switch tierCode {
	case "orange_plus":
		return 600
	case "orange_pro":
		return 1500
	default:
		return 0
	}
}

func fetchMemberStatus(deviceCode DeviceCodeV7) (*MemberStatusResponse, error) {
	client := &http.Client{Timeout: httpTimeout}
	payload, err := json.Marshal(deviceCode)
	if err != nil {
		return nil, err
	}

	var lastErr error
	for attempt := 1; attempt <= maxFetchAttempts; attempt++ {
		startedAt := time.Now()
		status, statusCode, err := fetchMemberStatusOnce(client, payload)
		duration := time.Since(startedAt)
		if err == nil {
			log.Info().
				Int("attempt", attempt).
				Int("status", statusCode).
				Bool("matched", status.Matched).
				Bool("is_member", status.IsMember).
				Dur("duration", duration).
				Msg("Fetched membership status")
			return status, nil
		}

		lastErr = err
		log.Warn().
			Int("attempt", attempt).
			Int("status", statusCode).
			Dur("duration", duration).
			Err(err).
			Msg("Failed to fetch membership status")

		if !shouldRetryFetch(statusCode, err) || attempt == maxFetchAttempts {
			break
		}
		time.Sleep(time.Duration(attempt*300) * time.Millisecond)
	}

	return nil, fmt.Errorf("failed to fetch membership status: %w", lastErr)
}

func fetchMemberStatusOnce(client *http.Client, payload []byte) (*MemberStatusResponse, int, error) {
	req, err := http.NewRequest("POST", MemberStatusURL, bytes.NewReader(payload))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", "MDA/"+appVersion)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		if resp.StatusCode == http.StatusUpgradeRequired {
			var updateResponse updateRequiredResponse
			if err := json.Unmarshal(body, &updateResponse); err == nil && updateResponse.UpdateRequired {
				return nil, resp.StatusCode, &updateRequiredError{
					Message:                 updateResponse.Error,
					MinimumSupportedVersion: updateResponse.MinimumSupportedVersion,
				}
			}
			return nil, resp.StatusCode, &updateRequiredError{}
		}
		return nil, resp.StatusCode, fmt.Errorf("HTTP %d from membership status source: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}

	var status MemberStatusResponse
	if err := json.Unmarshal(body, &status); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("failed to parse membership status JSON: %w", err)
	}

	return &status, resp.StatusCode, nil
}

func shouldRetryFetch(statusCode int, err error) bool {
	if statusCode >= http.StatusInternalServerError {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return statusCode == 0
}

func shortHash(s string) string {
	if len(s) > 8 {
		return s[:8] + "..."
	}
	if s == "" {
		return "(empty)"
	}
	return s
}
