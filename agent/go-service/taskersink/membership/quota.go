package membership

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type quotaPool string

type quotaRoute string

const (
	quotaPoolRegularDaily  quotaPool = "regular_daily"
	quotaPoolSpecialPeriod quotaPool = "special_period"

	quotaRouteRegular            quotaRoute = "regular"
	quotaRouteSpecialThenRegular quotaRoute = "special_then_regular"
)

type quotaPoolState struct {
	PeriodKey          string `json:"period_key"`
	LimitSeconds       int64  `json:"limit_seconds"`
	UsedSeconds        int64  `json:"used_seconds"`
	CarriedDebtSeconds int64  `json:"carried_debt_seconds,omitempty"`
	UpdatedAt          string `json:"updated_at"`
}

type quotaState struct {
	Version    int                       `json:"version,omitempty"`
	DeviceHash string                    `json:"device_hash"`
	TierCode   string                    `json:"tier_code"`
	Pools      map[string]quotaPoolState `json:"pools,omitempty"`

	BusinessDate       string `json:"business_date,omitempty"`
	LimitSeconds       int64  `json:"limit_seconds,omitempty"`
	UsedSeconds        int64  `json:"used_seconds,omitempty"`
	CarriedDebtSeconds int64  `json:"carried_debt_seconds,omitempty"`
	UpdatedAt          string `json:"updated_at,omitempty"`
}

type QuotaSnapshot struct {
	Pool                    quotaPool
	Route                   quotaRoute
	PeriodKey               string
	PeriodLabel             string
	FallbackToRegular       bool
	TierName                string
	TierCode                string
	LimitSeconds            int64
	UsedSeconds             int64
	RemainingSeconds        int64
	CarriedDebtSeconds      int64
	BusinessDate            string
	SponsorURL              string
	UnlimitedRuntime        bool
	SpecialLimitSeconds     int64
	SpecialUsedSeconds      int64
	SpecialRemainingSeconds int64
	RegularLimitSeconds     int64
	RegularUsedSeconds      int64
	RegularRemainingSeconds int64
}

var quotaMu sync.Mutex

func quotaBusinessDate(now time.Time) string {
	return now.Add(-4 * time.Hour).Format("2006-01-02")
}

func quotaSpecialPeriodKey(status *MembershipStatus) string {
	if status == nil || status.StartsOn == "" || status.ExpiresOn == "" {
		return "no_subscription"
	}
	return status.StartsOn + ".." + status.ExpiresOn
}

func quotaStatePath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		dir = "."
	}
	path := filepath.Join(dir, "MDA", "go-service")
	if err := os.MkdirAll(path, 0755); err != nil {
		return "", err
	}
	return filepath.Join(path, "membership-quota.json"), nil
}

func deviceHash(device DeviceCodeV7) string {
	sum := sha256.Sum256([]byte(device.CPUHash + device.UUIDHash + device.BIOSHash + device.BoardHash + device.DiskHash + device.GUIDHash))
	return hex.EncodeToString(sum[:])
}

func loadQuotaState(path string) quotaState {
	data, err := os.ReadFile(path)
	if err != nil {
		return quotaState{}
	}
	var state quotaState
	if err := json.Unmarshal(data, &state); err != nil {
		return quotaState{}
	}
	return state
}

func saveQuotaState(path string, state quotaState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func quotaLimitSeconds(status *MembershipStatus, pool quotaPool) int64 {
	switch pool {
	case quotaPoolSpecialPeriod:
		if status.SpecialPeriodRuntimeMinutes <= 0 {
			return 0
		}
		return int64(status.SpecialPeriodRuntimeMinutes) * 60
	default:
		minutes := status.RegularDailyRuntimeMinutes
		if minutes <= 0 {
			minutes = status.DailyRuntimeMinutes
		}
		if minutes <= 0 {
			minutes = 10
		}
		return int64(minutes) * 60
	}
}

func isRuntimeQuotaSubject(status *MembershipStatus) bool {
	return !status.UnlimitedRuntime
}

func normalizeTierCode(status *MembershipStatus) string {
	if status.TierCode != "" {
		return status.TierCode
	}
	return "orange_free"
}

func quotaRouteForEntry(entry string) quotaRoute {
	if entry == "MapPushingFlow" {
		return quotaRouteSpecialThenRegular
	}
	return quotaRouteRegular
}

func parseBusinessDate(date string) (time.Time, bool) {
	parsed, err := time.Parse("2006-01-02", date)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

func carriedQuotaDebt(state quotaState, businessDate string, fallbackLimit int64) int64 {
	return carriedDailyQuotaDebt(state.BusinessDate, state.UsedSeconds, state.LimitSeconds, businessDate, fallbackLimit)
}

func carriedDailyQuotaDebt(previousPeriod string, usedSeconds int64, limitSeconds int64, businessDate string, fallbackLimit int64) int64 {
	if previousPeriod == "" || previousPeriod == businessDate {
		return usedSeconds
	}

	previousDate, ok := parseBusinessDate(previousPeriod)
	if !ok {
		return 0
	}
	currentDate, ok := parseBusinessDate(businessDate)
	if !ok {
		return 0
	}
	days := int64(currentDate.Sub(previousDate).Hours() / 24)
	if days <= 0 {
		return usedSeconds
	}

	limit := limitSeconds
	if limit <= 0 {
		limit = fallbackLimit
	}
	debt := usedSeconds - limit*days
	if debt < 0 {
		return 0
	}
	return debt
}

func quotaPeriodKey(status *MembershipStatus, pool quotaPool, now time.Time) string {
	if pool == quotaPoolSpecialPeriod {
		return quotaSpecialPeriodKey(status)
	}
	return quotaBusinessDate(now)
}

func quotaPeriodLabel(pool quotaPool) string {
	if pool == quotaPoolSpecialPeriod {
		return "membership_period"
	}
	return "business_day"
}

func migrateLegacyQuotaState(state *quotaState) {
	if len(state.Pools) > 0 {
		return
	}
	state.Pools = map[string]quotaPoolState{}
	if state.BusinessDate == "" && state.LimitSeconds == 0 && state.UsedSeconds == 0 && state.CarriedDebtSeconds == 0 {
		return
	}
	state.Pools[string(quotaPoolRegularDaily)] = quotaPoolState{
		PeriodKey:          state.BusinessDate,
		LimitSeconds:       state.LimitSeconds,
		UsedSeconds:        state.UsedSeconds,
		CarriedDebtSeconds: state.CarriedDebtSeconds,
		UpdatedAt:          state.UpdatedAt,
	}
	state.BusinessDate = ""
	state.LimitSeconds = 0
	state.UsedSeconds = 0
	state.CarriedDebtSeconds = 0
	state.UpdatedAt = ""
}

func normalizeQuotaState(status *MembershipStatus, args ...any) (string, quotaState, error) {
	pool := quotaPoolRegularDaily
	now := time.Now()
	if len(args) == 1 {
		if parsedNow, ok := args[0].(time.Time); ok {
			now = parsedNow
		}
	} else if len(args) >= 2 {
		if parsedPool, ok := args[0].(quotaPool); ok {
			pool = parsedPool
		}
		if parsedNow, ok := args[1].(time.Time); ok {
			now = parsedNow
		}
	}
	path, err := quotaStatePath()
	if err != nil {
		return "", quotaState{}, err
	}
	state := loadQuotaState(path)
	state = normalizeQuotaStateInMemory(state, status, pool, now)
	return path, state, nil
}

func normalizeQuotaStateInMemory(state quotaState, status *MembershipStatus, pool quotaPool, now time.Time) quotaState {
	device := deviceHash(status.DeviceCode)
	tierCode := normalizeTierCode(status)
	updatedAt := now.Format(time.RFC3339)

	if state.DeviceHash != device || !isRuntimeQuotaSubject(status) {
		state = quotaState{
			Version:    2,
			DeviceHash: device,
			TierCode:   tierCode,
			Pools:      map[string]quotaPoolState{},
		}
	} else {
		migrateLegacyQuotaState(&state)
		if state.Pools == nil {
			state.Pools = map[string]quotaPoolState{}
		}
		state.Version = 2
		state.DeviceHash = device
		state.TierCode = tierCode
	}

	normalizeQuotaPool(status, &state, pool, now, updatedAt)
	return state
}

func normalizeQuotaPool(status *MembershipStatus, state *quotaState, pool quotaPool, now time.Time, updatedAt string) {
	periodKey := quotaPeriodKey(status, pool, now)
	limit := quotaLimitSeconds(status, pool)
	poolKey := string(pool)
	poolState := state.Pools[poolKey]

	if poolState.PeriodKey != periodKey {
		if pool == quotaPoolRegularDaily {
			poolState.UsedSeconds = carriedDailyQuotaDebt(poolState.PeriodKey, poolState.UsedSeconds, poolState.LimitSeconds, periodKey, limit)
			poolState.CarriedDebtSeconds = poolState.UsedSeconds
		} else {
			poolState.UsedSeconds = 0
			poolState.CarriedDebtSeconds = 0
		}
		poolState.PeriodKey = periodKey
	}

	poolState.LimitSeconds = limit
	poolState.UpdatedAt = updatedAt
	if pool == quotaPoolSpecialPeriod {
		poolState.CarriedDebtSeconds = 0
		if poolState.UsedSeconds > limit {
			poolState.UsedSeconds = limit
		}
	}
	if poolState.UsedSeconds < 0 {
		poolState.UsedSeconds = 0
	}
	state.Pools[poolKey] = poolState
	if pool == quotaPoolRegularDaily {
		state.BusinessDate = poolState.PeriodKey
		state.LimitSeconds = poolState.LimitSeconds
		state.UsedSeconds = poolState.UsedSeconds
		state.CarriedDebtSeconds = poolState.CarriedDebtSeconds
		state.UpdatedAt = poolState.UpdatedAt
	}
}

func normalizeQuotaPools(status *MembershipStatus, state quotaState, pools []quotaPool, now time.Time) quotaState {
	for _, pool := range pools {
		state = normalizeQuotaStateInMemory(state, status, pool, now)
	}
	return state
}

func quotaSnapshotLocked(status *MembershipStatus, pool quotaPool, now time.Time) (QuotaSnapshot, error) {
	path, state, err := normalizeQuotaState(status, pool, now)
	if err != nil {
		return QuotaSnapshot{}, err
	}
	if err := saveQuotaState(path, state); err != nil {
		return QuotaSnapshot{}, err
	}
	return snapshotFromState(status, state, pool), nil
}

func snapshotFromState(status *MembershipStatus, state quotaState, pools ...quotaPool) QuotaSnapshot {
	pool := quotaPoolRegularDaily
	if len(pools) > 0 {
		pool = pools[0]
	}
	if status.UnlimitedRuntime {
		return QuotaSnapshot{
			Pool:                pool,
			Route:               quotaRouteRegular,
			TierName:            status.TierName,
			TierCode:            status.TierCode,
			BusinessDate:        quotaBusinessDate(time.Now()),
			PeriodLabel:         quotaPeriodLabel(pool),
			UnlimitedRuntime:    true,
			SponsorURL:          SponsorURL(status),
			RegularLimitSeconds: quotaLimitSeconds(status, quotaPoolRegularDaily),
			SpecialLimitSeconds: quotaLimitSeconds(status, quotaPoolSpecialPeriod),
		}
	}

	migrateLegacyQuotaState(&state)
	if state.Pools == nil {
		state.Pools = map[string]quotaPoolState{}
	}
	poolState := state.Pools[string(pool)]
	limit := quotaLimitSeconds(status, pool)
	used := poolState.UsedSeconds
	if used < 0 {
		used = 0
	}
	remaining := limit - used
	if remaining < 0 {
		remaining = 0
	}
	carriedDebt := poolState.CarriedDebtSeconds
	if carriedDebt < 0 || pool == quotaPoolSpecialPeriod {
		carriedDebt = 0
	}
	return QuotaSnapshot{
		Pool:               pool,
		Route:              quotaRouteRegular,
		PeriodKey:          poolState.PeriodKey,
		PeriodLabel:        quotaPeriodLabel(pool),
		TierName:           status.TierName,
		TierCode:           status.TierCode,
		LimitSeconds:       limit,
		UsedSeconds:        used,
		RemainingSeconds:   remaining,
		CarriedDebtSeconds: carriedDebt,
		BusinessDate:       poolState.PeriodKey,
		SponsorURL:         SponsorURL(status),
		UnlimitedRuntime:   false,
	}
}

func routeSnapshotFromState(status *MembershipStatus, state quotaState, route quotaRoute) QuotaSnapshot {
	regular := snapshotFromState(status, state, quotaPoolRegularDaily)
	special := snapshotFromState(status, state, quotaPoolSpecialPeriod)

	if status.UnlimitedRuntime {
		regular.Route = route
		regular.UnlimitedRuntime = true
		return regular
	}

	if route == quotaRouteSpecialThenRegular && special.RemainingSeconds > 0 {
		special.Route = route
		special.SpecialLimitSeconds = special.LimitSeconds
		special.SpecialUsedSeconds = special.UsedSeconds
		special.SpecialRemainingSeconds = special.RemainingSeconds
		special.RegularLimitSeconds = regular.LimitSeconds
		special.RegularUsedSeconds = regular.UsedSeconds
		special.RegularRemainingSeconds = regular.RemainingSeconds
		return special
	}

	regular.Route = route
	regular.FallbackToRegular = route == quotaRouteSpecialThenRegular && special.RemainingSeconds <= 0
	regular.SpecialLimitSeconds = special.LimitSeconds
	regular.SpecialUsedSeconds = special.UsedSeconds
	regular.SpecialRemainingSeconds = special.RemainingSeconds
	regular.RegularLimitSeconds = regular.LimitSeconds
	regular.RegularUsedSeconds = regular.UsedSeconds
	regular.RegularRemainingSeconds = regular.RemainingSeconds
	return regular
}

func GetQuotaSnapshot(status *MembershipStatus, pool quotaPool) (QuotaSnapshot, error) {
	quotaMu.Lock()
	defer quotaMu.Unlock()
	return quotaSnapshotLocked(status, pool, time.Now())
}

func AddQuotaUsage(status *MembershipStatus, delta time.Duration) (QuotaSnapshot, error) {
	if delta <= 0 {
		return GetQuotaSnapshot(status, quotaPoolRegularDaily)
	}
	seconds := int64(delta.Round(time.Second) / time.Second)
	if seconds <= 0 {
		seconds = 1
	}
	return AddQuotaUsageSeconds(status, quotaPoolRegularDaily, seconds)
}

func AddQuotaUsageSeconds(status *MembershipStatus, pool quotaPool, seconds int64) (QuotaSnapshot, error) {
	if seconds <= 0 {
		return GetQuotaSnapshot(status, pool)
	}
	quotaMu.Lock()
	defer quotaMu.Unlock()
	now := time.Now()
	path, state, err := normalizeQuotaState(status, pool, now)
	if err != nil {
		return QuotaSnapshot{}, err
	}
	if isRuntimeQuotaSubject(status) {
		poolKey := string(pool)
		poolState := state.Pools[poolKey]
		poolState.UsedSeconds += seconds
		if pool == quotaPoolSpecialPeriod && poolState.UsedSeconds > poolState.LimitSeconds {
			poolState.UsedSeconds = poolState.LimitSeconds
		}
		poolState.UpdatedAt = now.Format(time.RFC3339)
		state.Pools[poolKey] = poolState
	}
	if err := saveQuotaState(path, state); err != nil {
		return QuotaSnapshot{}, err
	}
	return snapshotFromState(status, state, pool), nil
}

func EnsureQuotaAvailable(status *MembershipStatus, pool quotaPool) (QuotaSnapshot, bool, error) {
	snapshot, err := GetQuotaSnapshot(status, pool)
	if err != nil {
		fallback := snapshotFromState(status, quotaState{Pools: map[string]quotaPoolState{}}, pool)
		return fallback, true, err
	}
	if snapshot.UnlimitedRuntime {
		return snapshot, true, nil
	}
	return snapshot, snapshot.RemainingSeconds > 0, nil
}

func EnsureQuotaRouteAvailable(status *MembershipStatus, route quotaRoute) (QuotaSnapshot, bool, error) {
	quotaMu.Lock()
	defer quotaMu.Unlock()
	now := time.Now()
	path, err := quotaStatePath()
	if err != nil {
		fallback := routeSnapshotFromState(status, quotaState{Pools: map[string]quotaPoolState{}}, route)
		return fallback, true, err
	}
	state := loadQuotaState(path)
	state = normalizeQuotaPools(status, state, []quotaPool{quotaPoolRegularDaily, quotaPoolSpecialPeriod}, now)
	if err := saveQuotaState(path, state); err != nil {
		return QuotaSnapshot{}, true, err
	}
	snapshot := routeSnapshotFromState(status, state, route)
	if snapshot.UnlimitedRuntime {
		return snapshot, true, nil
	}
	if route == quotaRouteSpecialThenRegular {
		return snapshot, snapshot.SpecialRemainingSeconds > 0 || snapshot.RegularRemainingSeconds > 0, nil
	}
	return snapshot, snapshot.RegularRemainingSeconds > 0, nil
}

func AddQuotaRouteUsageSeconds(status *MembershipStatus, route quotaRoute, seconds int64) (QuotaSnapshot, error) {
	if seconds <= 0 {
		snapshot, _, err := EnsureQuotaRouteAvailable(status, route)
		return snapshot, err
	}
	quotaMu.Lock()
	defer quotaMu.Unlock()
	now := time.Now()
	path, err := quotaStatePath()
	if err != nil {
		return QuotaSnapshot{}, err
	}
	state := loadQuotaState(path)
	state = normalizeQuotaPools(status, state, []quotaPool{quotaPoolRegularDaily, quotaPoolSpecialPeriod}, now)
	if isRuntimeQuotaSubject(status) {
		updatedAt := now.Format(time.RFC3339)
		regularState := state.Pools[string(quotaPoolRegularDaily)]
		specialState := state.Pools[string(quotaPoolSpecialPeriod)]
		regularCharge := seconds
		if route == quotaRouteSpecialThenRegular {
			specialRemaining := specialState.LimitSeconds - specialState.UsedSeconds
			if specialRemaining < 0 {
				specialRemaining = 0
			}
			specialCharge := seconds
			if specialCharge > specialRemaining {
				specialCharge = specialRemaining
			}
			if specialCharge > 0 {
				specialState.UsedSeconds += specialCharge
				specialState.UpdatedAt = updatedAt
				state.Pools[string(quotaPoolSpecialPeriod)] = specialState
			}
			regularCharge = seconds - specialCharge
		}
		if regularCharge > 0 {
			regularState.UsedSeconds += regularCharge
			regularState.UpdatedAt = updatedAt
			state.Pools[string(quotaPoolRegularDaily)] = regularState
		}
	}
	if err := saveQuotaState(path, state); err != nil {
		return QuotaSnapshot{}, err
	}
	return routeSnapshotFromState(status, state, route), nil
}

func FormatMinutes(seconds int64) int64 {
	if seconds <= 0 {
		return 0
	}
	return (seconds + 59) / 60
}
