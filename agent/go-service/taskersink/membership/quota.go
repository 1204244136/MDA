package membership

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

type quotaPool string

type quotaRoute string

const (
	quotaStateVersion                = 5
	quotaPoolRegularDaily  quotaPool = "regular_daily"
	quotaPoolSpecialPeriod quotaPool = "special_period"
	quotaPoolEvent         quotaPool = "event"

	quotaRouteRegular            quotaRoute = "regular"
	quotaRouteSpecialThenRegular quotaRoute = "special_then_regular"
)

type quotaPoolState struct {
	PeriodKey    string `json:"period_key"`
	LimitSeconds int64  `json:"limit_seconds"`
	UsedSeconds  int64  `json:"used_seconds"`
	UpdatedAt    string `json:"updated_at"`
}

type quotaCouponRedemption struct {
	RedeemedAt string          `json:"redeemed_at"`
	RefillType QuotaRefillType `json:"refill_type"`
	DeviceHash string          `json:"device_hash,omitempty"`
}

// eventQuotaGrant 是一笔活动额度的发放记录。
// ExpiresAt 为空表示永久有效；否则为 RFC3339 时间戳，到期后该笔额度不再计入剩余、
// 也不再参与扣减，但记录会保留下来供「额度显示」提示用户。
type eventQuotaGrant struct {
	TaskEntry    string `json:"task_entry,omitempty"`
	ExpiresAt    string `json:"expires_at,omitempty"`
	LimitSeconds int64  `json:"limit_seconds"`
	UsedSeconds  int64  `json:"used_seconds"`
}

type quotaState struct {
	EventGrants     []eventQuotaGrant                `json:"event_grants,omitempty"`
	Version         int                              `json:"version,omitempty"`
	DeviceHash      string                           `json:"device_hash"`
	TierCode        string                           `json:"tier_code"`
	Pools           map[string]quotaPoolState        `json:"pools,omitempty"`
	RedeemedCoupons map[string]quotaCouponRedemption `json:"redeemed_coupons,omitempty"`

	BusinessDate string `json:"business_date,omitempty"`
	LimitSeconds int64  `json:"limit_seconds,omitempty"`
	UsedSeconds  int64  `json:"used_seconds,omitempty"`
	UpdatedAt    string `json:"updated_at,omitempty"`
}

type QuotaSnapshot struct {
	EventRemainingSeconds   int64
	Pool                    quotaPool
	Route                   quotaRoute
	PeriodKey               string
	PeriodLabel             string
	TierName                string
	TierCode                string
	LimitSeconds            int64
	UsedSeconds             int64
	RemainingSeconds        int64
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

var beijingLocation = time.FixedZone("Asia/Shanghai", 8*60*60)

func quotaBusinessDate(now time.Time) string {
	return now.In(beijingLocation).Add(-4 * time.Hour).Format("2006-01-02")
}

func quotaSpecialPeriodKey(status *MembershipStatus) string {
	if status == nil || status.StartsOn == "" || status.ExpiresOn == "" {
		return "no_subscription"
	}
	return status.StartsOn + ".." + status.ExpiresOn
}

func quotaStatePath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config directory: %w", err)
	}
	if dir == "" {
		return "", errors.New("user config directory is empty")
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

func loadQuotaState(path string) (quotaState, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return quotaState{}, nil
	}
	if err != nil {
		return quotaState{}, fmt.Errorf("read quota state: %w", err)
	}
	var state quotaState
	if err := json.Unmarshal(data, &state); err != nil {
		return quotaState{}, fmt.Errorf("parse quota state: %w", err)
	}
	return state, nil
}

func saveQuotaState(path string, state quotaState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".membership-quota-*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()

	// 清理临时文件：带重试应对 Windows 短暂文件锁；
	// 成功路径下 tempPath 已被 MoveFileEx 移走，仅当文件确实残留时才告警。
	cleanupTemp := func() {
		temp.Close()
		for i := 0; i < 3; i++ {
			if err := os.Remove(tempPath); err == nil {
				return
			}
			if i < 2 {
				time.Sleep(10 * time.Millisecond)
			}
		}
		if _, err := os.Stat(tempPath); err == nil {
			log.Warn().Str("temp_file", tempPath).Msg("failed to cleanup temporary quota state file")
		}
	}
	defer cleanupTemp()

	if err := temp.Chmod(0644); err != nil {
		return err
	}
	if _, err := temp.Write(data); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := replaceQuotaStateFile(tempPath, path); err != nil {
		return fmt.Errorf("failed to replace quota state file: %w", err)
	}
	return nil
}

func lockQuotaStateFile() (func() error, error) {
	path, err := quotaStatePath()
	if err != nil {
		return nil, err
	}
	return acquireQuotaFileLock(path + ".lock")
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

func addQuotaPoolUsage(poolState quotaPoolState, seconds int64) (quotaPoolState, bool) {
	if seconds <= 0 || poolState.LimitSeconds <= 0 {
		return poolState, false
	}
	limit := poolState.LimitSeconds
	if poolState.UsedSeconds >= limit {
		poolState.UsedSeconds = limit
		return poolState, true
	}
	if poolState.UsedSeconds+seconds >= limit {
		poolState.UsedSeconds = limit
		return poolState, true
	}
	poolState.UsedSeconds += seconds
	return poolState, false
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
	if isHighConsumptionEntry(entry) {
		return quotaRouteSpecialThenRegular
	}
	return quotaRouteRegular
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
	if state.BusinessDate == "" && state.LimitSeconds == 0 && state.UsedSeconds == 0 {
		return
	}
	state.Pools[string(quotaPoolRegularDaily)] = quotaPoolState{
		PeriodKey:    state.BusinessDate,
		LimitSeconds: state.LimitSeconds,
		UsedSeconds:  state.UsedSeconds,
		UpdatedAt:    state.UpdatedAt,
	}
	state.BusinessDate = ""
	state.LimitSeconds = 0
	state.UsedSeconds = 0
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
	state, err := loadQuotaState(path)
	if err != nil {
		return "", quotaState{}, err
	}
	state = normalizeQuotaStateInMemory(state, status, pool, now)
	return path, state, nil
}

func normalizeQuotaStateInMemory(state quotaState, status *MembershipStatus, pool quotaPool, now time.Time) quotaState {
	device := deviceHash(status.DeviceCode)
	tierCode := normalizeTierCode(status)
	updatedAt := now.Format(time.RFC3339)
	redeemedCoupons := state.RedeemedCoupons
	eventGrants := state.EventGrants
	if state.DeviceHash != device {
		eventGrants = nil
	}

	if state.DeviceHash != device || !isRuntimeQuotaSubject(status) {
		state = quotaState{
			Version:         quotaStateVersion,
			DeviceHash:      device,
			TierCode:        tierCode,
			Pools:           map[string]quotaPoolState{},
			RedeemedCoupons: redeemedCoupons,
			EventGrants:     eventGrants,
		}
	} else {
		migrateLegacyQuotaState(&state)
		if state.Pools == nil {
			state.Pools = map[string]quotaPoolState{}
		}
		state.Version = quotaStateVersion
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
		poolState.UsedSeconds = 0
		poolState.PeriodKey = periodKey
	}

	poolState.LimitSeconds = limit
	poolState.UpdatedAt = updatedAt
	if poolState.UsedSeconds < 0 {
		poolState.UsedSeconds = 0
	}
	if poolState.UsedSeconds > limit {
		poolState.UsedSeconds = limit
	}
	state.Pools[poolKey] = poolState
	if pool == quotaPoolRegularDaily {
		state.BusinessDate = poolState.PeriodKey
		state.LimitSeconds = poolState.LimitSeconds
		state.UsedSeconds = poolState.UsedSeconds
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
	unlock, err := lockQuotaStateFile()
	if err != nil {
		return QuotaSnapshot{}, err
	}
	defer unlock()

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
	return QuotaSnapshot{
		Pool:             pool,
		Route:            quotaRouteRegular,
		PeriodKey:        poolState.PeriodKey,
		PeriodLabel:      quotaPeriodLabel(pool),
		TierName:         status.TierName,
		TierCode:         status.TierCode,
		LimitSeconds:     limit,
		UsedSeconds:      used,
		RemainingSeconds: remaining,
		BusinessDate:     poolState.PeriodKey,
		SponsorURL:       SponsorURL(status),
		UnlimitedRuntime: false,
	}
}

// routeSnapshotFromState 组装指定路由的额度快照。活动额度不再优先扣减，因此它只作为
// 一个独立字段参与展示与可用性判断，不覆盖主池（常规/专项）的统计口径。
func routeSnapshotFromState(status *MembershipStatus, state quotaState, route quotaRoute, entries ...string) QuotaSnapshot {
	snapshot := baseRouteSnapshotFromState(status, state, route)
	snapshot.EventRemainingSeconds = eventQuotaRemaining(state, firstEntry(entries))
	return snapshot
}

func baseRouteSnapshotFromState(status *MembershipStatus, state quotaState, route quotaRoute) QuotaSnapshot {
	regular := snapshotFromState(status, state, quotaPoolRegularDaily)
	special := snapshotFromState(status, state, quotaPoolSpecialPeriod)

	if status.UnlimitedRuntime {
		regular.Route = route
		regular.UnlimitedRuntime = true
		return regular
	}

	// 扣减顺序为“常规 → 专项 → 活动”，常规额度因此是主池；只有常规额度已经用尽、
	// 而该路由仍可用专项额度时，才把专项额度作为当前正在扣减的池展示。
	if route == quotaRouteSpecialThenRegular && regular.RemainingSeconds <= 0 && special.RemainingSeconds > 0 {
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
	regular.SpecialLimitSeconds = special.LimitSeconds
	regular.SpecialUsedSeconds = special.UsedSeconds
	regular.SpecialRemainingSeconds = special.RemainingSeconds
	regular.RegularLimitSeconds = regular.LimitSeconds
	regular.RegularUsedSeconds = regular.UsedSeconds
	regular.RegularRemainingSeconds = regular.RemainingSeconds
	return regular
}

// quotaReserveSeconds 汇总该路由下所有额度池的剩余计费秒数，仅用于决定下一次额度 tick
// 的间隔：剩余越多，检查可以越稀疏。常规额度被用尽后仍可由活动额度支撑，因此不能只
// 看主池剩余，否则会退化成每 5 秒一次的密集读写。
func quotaReserveSeconds(snapshot QuotaSnapshot) int64 {
	if snapshot.UnlimitedRuntime {
		return 0
	}
	return snapshot.RegularRemainingSeconds + snapshot.SpecialRemainingSeconds + snapshot.EventRemainingSeconds
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
	unlock, err := lockQuotaStateFile()
	if err != nil {
		return QuotaSnapshot{}, err
	}
	defer unlock()

	now := time.Now()
	path, state, err := normalizeQuotaState(status, pool, now)
	if err != nil {
		return QuotaSnapshot{}, err
	}
	if isRuntimeQuotaSubject(status) {
		poolKey := string(pool)
		poolState := state.Pools[poolKey]
		poolState, _ = addQuotaPoolUsage(poolState, seconds)
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
		return fallback, false, err
	}
	if snapshot.UnlimitedRuntime {
		return snapshot, true, nil
	}
	return snapshot, snapshot.RemainingSeconds > 0, nil
}

func EnsureQuotaRouteAvailable(status *MembershipStatus, route quotaRoute, entries ...string) (QuotaSnapshot, bool, error) {
	quotaMu.Lock()
	defer quotaMu.Unlock()
	unlock, err := lockQuotaStateFile()
	if err != nil {
		fallback := routeSnapshotFromState(status, quotaState{Pools: map[string]quotaPoolState{}}, route)
		return fallback, false, err
	}
	defer unlock()

	now := time.Now()
	path, err := quotaStatePath()
	if err != nil {
		fallback := routeSnapshotFromState(status, quotaState{Pools: map[string]quotaPoolState{}}, route)
		return fallback, false, err
	}
	state, err := loadQuotaState(path)
	if err != nil {
		fallback := routeSnapshotFromState(status, quotaState{Pools: map[string]quotaPoolState{}}, route)
		return fallback, false, err
	}
	state = normalizeQuotaPools(status, state, []quotaPool{quotaPoolRegularDaily, quotaPoolSpecialPeriod}, now)
	if err := saveQuotaState(path, state); err != nil {
		return QuotaSnapshot{}, false, err
	}
	snapshot := routeSnapshotFromState(status, state, route, entries...)
	return snapshot, quotaAvailableForRoute(status, route, state, firstEntry(entries)), nil
}

func AddQuotaRouteUsageSeconds(status *MembershipStatus, route quotaRoute, seconds int64) (QuotaSnapshot, error) {
	snapshot, _, err := addQuotaRouteUsageSeconds(status, route, seconds)
	return snapshot, err
}

func addQuotaRouteUsageSeconds(status *MembershipStatus, route quotaRoute, seconds int64) (QuotaSnapshot, bool, error) {
	if seconds <= 0 {
		snapshot, _, err := EnsureQuotaRouteAvailable(status, route)
		return snapshot, false, err
	}
	quotaMu.Lock()
	defer quotaMu.Unlock()
	unlock, err := lockQuotaStateFile()
	if err != nil {
		return QuotaSnapshot{}, false, err
	}
	defer unlock()

	now := time.Now()
	path, err := quotaStatePath()
	if err != nil {
		return QuotaSnapshot{}, false, err
	}
	state, err := loadQuotaState(path)
	if err != nil {
		return QuotaSnapshot{}, false, err
	}
	state = normalizeQuotaPools(status, state, []quotaPool{quotaPoolRegularDaily, quotaPoolSpecialPeriod}, now)
	_, exhausted := chargeQuotaByPriority(status, "", route, seconds, false, &state, now)
	if err := saveQuotaState(path, state); err != nil {
		return QuotaSnapshot{}, false, err
	}
	return routeSnapshotFromState(status, state, route), exhausted, nil
}

// quotaPoolRemaining 返回额度池的剩余秒数（下限为 0）。
func quotaPoolRemaining(state quotaState, pool quotaPool) int64 {
	poolState := state.Pools[string(pool)]
	remaining := poolState.LimitSeconds - poolState.UsedSeconds
	if remaining < 0 {
		return 0
	}
	return remaining
}

// quotaReserveAvailable 判断该路由下是否还有常规额度之外的可用额度：
// 活动额度，或高级任务路由下的专项额度。这两类额度都按实际时长（1 倍）扣减。
func quotaReserveAvailable(route quotaRoute, state quotaState, entry string) bool {
	if eventQuotaRemaining(state, entry) > 0 {
		return true
	}
	return route == quotaRouteSpecialThenRegular && quotaPoolRemaining(state, quotaPoolSpecialPeriod) > 0
}

// quotaAvailableForRoute 判断该路由下是否还有任一可扣减的额度池。
// 与 EnsureQuotaRouteAvailable 的放行口径保持一致：常规额度被打满并不等于任务必须
// 停止，只要活动额度（或高级任务的专项额度）仍有剩余就应继续运行。
func quotaAvailableForRoute(status *MembershipStatus, route quotaRoute, state quotaState, entry string) bool {
	if !isRuntimeQuotaSubject(status) {
		return true
	}
	if quotaReserveAvailable(route, state, entry) {
		return true
	}
	return quotaPoolRemaining(state, quotaPoolRegularDaily) > 0
}

// billableSecondsToReal 把计费秒数按倍率还原为实际秒数（向下取整）。
// 用于常规额度被打满时，把溢出的计费额度交还给后续额度池。
func billableSecondsToReal(billable, permille int64) int64 {
	if billable <= 0 {
		return 0
	}
	if permille <= 0 {
		permille = multiplierScale
	}
	return billable * multiplierScale / permille
}

// chargeQuotaByPriority 按“最早失效优先”的顺序扣减 realSeconds，返回本次实际使用的
// 倍率，以及该路由下所有额度来源是否都已耗尽。
//
// 顺序由 quotaChargeOrder 统一决定：常规额度按业务日失效，专项额度按订阅周期失效，
// 每笔活动额度各按自己的期限失效，谁先失效谁先被消耗。这样会员每天的常规额度不会因为
// 手里攒着活动额度而被整日闲置，即将到期的活动额度也不会被更晚失效的专项额度挤掉。
//
// 倍率由实际扣减的额度池决定：高级任务消耗常规额度时按 5 倍计费，专项额度与活动额度
// 始终按实际时长（1 倍）扣减。
func chargeQuotaByPriority(status *MembershipStatus, entry string, route quotaRoute, realSeconds int64, flush bool, state *quotaState, now time.Time) (quotaMultiplier, bool) {
	if !isRuntimeQuotaSubject(status) || realSeconds <= 0 {
		return regularQuotaMultiplier(entry), false
	}

	updatedAt := now.Format(time.RFC3339)
	remainingReal := realSeconds
	// 本次实际使用的倍率：只有从常规额度扣费时才可能高于 1 倍。
	multiplier := unmultipliedQuotaMultiplier()

	for _, source := range quotaChargeOrder(status, route, *state, entry, now) {
		if remainingReal <= 0 {
			break
		}
		switch source.pool {
		case quotaPoolRegularDaily:
			// 常规额度按倍率计费（高级任务 5 倍）；打满时把溢出的计费额度还原成实际
			// 秒数，交给后面失效更晚的额度来源。
			regularMultiplier := regularQuotaMultiplier(entry)
			multiplier = regularMultiplier
			available := quotaPoolRemaining(*state, quotaPoolRegularDaily)
			regular := state.Pools[string(quotaPoolRegularDaily)]
			billable := regularMultiplier.billableSecondsFromReal(remainingReal, flush)
			if billable < available {
				regular.UsedSeconds += billable
				remainingReal = 0
			} else {
				regular.UsedSeconds = regular.LimitSeconds
				remainingReal = billableSecondsToReal(billable-available, regularMultiplier.totalPermille())
			}
			regular.UpdatedAt = updatedAt
			state.Pools[string(quotaPoolRegularDaily)] = regular
		case quotaPoolSpecialPeriod:
			special := state.Pools[string(quotaPoolSpecialPeriod)]
			charge := min(remainingReal, quotaPoolRemaining(*state, quotaPoolSpecialPeriod))
			special.UsedSeconds += charge
			special.UpdatedAt = updatedAt
			state.Pools[string(quotaPoolSpecialPeriod)] = special
			remainingReal -= charge
		case quotaPoolEvent:
			grant := &state.EventGrants[source.grantIndex]
			charge := min(remainingReal, grant.LimitSeconds-grant.UsedSeconds)
			grant.UsedSeconds += charge
			remainingReal -= charge
		}
	}

	return multiplier, !quotaAvailableForRoute(status, route, *state, entry)
}

// addQuotaRouteUsageRealSeconds 根据任务 entry 与当前额度池状态动态计算倍率，
// 并按“常规 → 专项 → 活动”的顺序在同一次文件锁内完成扣费，
// 避免每个 tick 重复读写额度状态文件。
// 注意：quotaMu 与文件锁必须按“quotaMu → 文件锁”的顺序同时持有、整体释放，
// 与其他配额路径保持一致；若持文件锁期间再等 quotaMu，会造成锁序倒置死锁。
func addQuotaRouteUsageRealSeconds(status *MembershipStatus, entry string, route quotaRoute, realSeconds int64, flush bool) (QuotaSnapshot, quotaMultiplier, bool, error) {
	if realSeconds <= 0 {
		snapshot, _, err := EnsureQuotaRouteAvailable(status, route, entry)
		return snapshot, quotaMultiplier{BasePermille: multiplierScale, ExtraPermille: multiplierScale}, false, err
	}

	quotaMu.Lock()
	defer quotaMu.Unlock()
	unlock, err := lockQuotaStateFile()
	if err != nil {
		fallback := routeSnapshotFromState(status, quotaState{Pools: map[string]quotaPoolState{}}, route)
		return fallback, quotaMultiplier{BasePermille: multiplierScale, ExtraPermille: multiplierScale}, false, err
	}
	defer unlock()

	now := time.Now()
	path, err := quotaStatePath()
	if err != nil {
		fallback := routeSnapshotFromState(status, quotaState{Pools: map[string]quotaPoolState{}}, route)
		return fallback, quotaMultiplier{BasePermille: multiplierScale, ExtraPermille: multiplierScale}, false, err
	}
	state, err := loadQuotaState(path)
	if err != nil {
		fallback := routeSnapshotFromState(status, quotaState{Pools: map[string]quotaPoolState{}}, route)
		return fallback, quotaMultiplier{BasePermille: multiplierScale, ExtraPermille: multiplierScale}, false, err
	}
	state = normalizeQuotaPools(status, state, []quotaPool{quotaPoolRegularDaily, quotaPoolSpecialPeriod}, now)

	multiplier, exhausted := chargeQuotaByPriority(status, entry, route, realSeconds, flush, &state, now)
	if err := saveQuotaState(path, state); err != nil {
		return QuotaSnapshot{}, multiplier, false, err
	}
	snapshot := routeSnapshotFromState(status, state, route, entry)
	return snapshot, multiplier, exhausted, nil
}

func FormatMinutes(seconds int64) int64 {
	if seconds <= 0 {
		return 0
	}
	return (seconds + 59) / 60
}

func firstEntry(entries []string) string {
	if len(entries) > 0 {
		return entries[0]
	}
	return ""
}

// eventGrantActive 判断一笔活动额度是否仍在有效期内。未设置到期时间表示永久有效；
// 到期时间无法解析时同样按永久处理，避免脏数据让用户平白丢失额度。
func eventGrantActive(grant eventQuotaGrant, now time.Time) bool {
	if grant.ExpiresAt == "" {
		return true
	}
	expiresAt, err := time.Parse(time.RFC3339, grant.ExpiresAt)
	if err != nil {
		return true
	}
	return now.Before(expiresAt)
}

func eventQuotaRemaining(state quotaState, entry string) int64 {
	return eventQuotaRemainingAt(state, entry, time.Now())
}

func eventQuotaRemainingAt(state quotaState, entry string, now time.Time) int64 {
	var remaining int64
	for _, grant := range state.EventGrants {
		if !eventGrantActive(grant, now) {
			continue
		}
		if (grant.TaskEntry == "" || grant.TaskEntry == entry) && grant.LimitSeconds > grant.UsedSeconds {
			remaining += grant.LimitSeconds - grant.UsedSeconds
		}
	}
	return remaining
}

// formatEventGrantExpiry 把发放项的到期时间格式化为北京时间文本；永久有效返回空串。
func formatEventGrantExpiry(grant eventQuotaGrant) string {
	if grant.ExpiresAt == "" {
		return ""
	}
	expiresAt, err := time.Parse(time.RFC3339, grant.ExpiresAt)
	if err != nil {
		return ""
	}
	return expiresAt.In(beijingLocation).Format("2006-01-02 15:04")
}

// eventGrantExpiry 返回用于排序的到期时间戳。永久有效的发放项返回 math.MaxInt64，
// 从而始终排在有时限的发放项之后。
func eventGrantExpiry(grant eventQuotaGrant) int64 {
	if grant.ExpiresAt == "" {
		return math.MaxInt64
	}
	expiresAt, err := time.Parse(time.RFC3339, grant.ExpiresAt)
	if err != nil {
		return math.MaxInt64
	}
	return expiresAt.Unix()
}

// eventGrantCandidates 返回本次可用于 entry 且尚未用尽的活动额度发放项下标。
// 每张券兑换来的额度都是独立的一笔（各自计时、各自过期），这里只做过滤，
// 与常规/专项额度之间的先后顺序统一交给 quotaChargeOrder。
func eventGrantCandidates(grants []eventQuotaGrant, entry string, now time.Time) []int {
	candidates := make([]int, 0, len(grants))
	for i, grant := range grants {
		if !eventGrantActive(grant, now) {
			continue
		}
		if grant.TaskEntry != "" && grant.TaskEntry != entry {
			continue
		}
		if grant.LimitSeconds <= grant.UsedSeconds {
			continue
		}
		candidates = append(candidates, i)
	}
	return candidates
}

// regularQuotaExpiry 返回当前业务日常规额度的失效时刻：下一个业务日的 4:00（北京时间）。
func regularQuotaExpiry(now time.Time) time.Time {
	beijing := now.In(beijingLocation)
	businessDay := beijing.Add(-4 * time.Hour)
	return time.Date(businessDay.Year(), businessDay.Month(), businessDay.Day(), 4, 0, 0, 0, beijingLocation).AddDate(0, 0, 1)
}

// specialQuotaExpiry 返回当前订阅周期专项额度的失效时刻（订阅到期日的次日 0 点）。
// 订阅信息缺失或无法解析时返回零值时间，由 quotaExpiryUnix 按“最晚失效”处理。
func specialQuotaExpiry(status *MembershipStatus) time.Time {
	if status == nil {
		return time.Time{}
	}
	expiresOn, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(status.ExpiresOn), beijingLocation)
	if err != nil {
		return time.Time{}
	}
	return expiresOn.AddDate(0, 0, 1)
}

// quotaExpiryUnix 把失效时刻转换为可比较的时间戳；零值时间按“最晚”处理。
func quotaExpiryUnix(expiry time.Time) int64 {
	if expiry.IsZero() {
		return math.MaxInt64
	}
	return expiry.Unix()
}

// quotaChargeSource 描述一个可扣减的额度来源。
type quotaChargeSource struct {
	pool       quotaPool
	grantIndex int
	expiry     int64
	rank       int
}

// quotaChargeOrder 汇总本次可用于该路由的额度来源，按“最早失效优先”排序。
//
// 顺序不再是固定的“常规 → 专项 → 活动”，而是统一按失效时刻升序排列：常规额度按业务日
// 失效，专项额度按订阅周期失效，每笔活动额度各按自己的期限失效。固定顺序会在“专项额度
// 还有一个月、而某笔活动额度三天后到期”时放过即将作废的那笔；反过来，当活动额度是永久
// 的、而专项额度本周期即将结束时，专项额度又会正确地排到活动额度之前。
//
// 失效时刻相同时按额度池排序（常规 → 专项 → 活动）；同为活动额度时，限定任务的额度优先
// 于通用额度——它只服务于当前任务，对本任务而言更“易浪费”。排序稳定，完全并列时保持
// 原有兑换顺序。
func quotaChargeOrder(status *MembershipStatus, route quotaRoute, state quotaState, entry string, now time.Time) []quotaChargeSource {
	sources := make([]quotaChargeSource, 0, len(state.EventGrants)+2)
	if quotaPoolRemaining(state, quotaPoolRegularDaily) > 0 {
		sources = append(sources, quotaChargeSource{
			pool:       quotaPoolRegularDaily,
			grantIndex: -1,
			expiry:     regularQuotaExpiry(now).Unix(),
			rank:       0,
		})
	}
	if route == quotaRouteSpecialThenRegular && quotaPoolRemaining(state, quotaPoolSpecialPeriod) > 0 {
		sources = append(sources, quotaChargeSource{
			pool:       quotaPoolSpecialPeriod,
			grantIndex: -1,
			expiry:     quotaExpiryUnix(specialQuotaExpiry(status)),
			rank:       1,
		})
	}
	for _, index := range eventGrantCandidates(state.EventGrants, entry, now) {
		rank := 3
		if state.EventGrants[index].TaskEntry != "" {
			rank = 2
		}
		sources = append(sources, quotaChargeSource{
			pool:       quotaPoolEvent,
			grantIndex: index,
			expiry:     eventGrantExpiry(state.EventGrants[index]),
			rank:       rank,
		})
	}
	sort.SliceStable(sources, func(a, b int) bool {
		if sources[a].expiry != sources[b].expiry {
			return sources[a].expiry < sources[b].expiry
		}
		return sources[a].rank < sources[b].rank
	})
	return sources
}
