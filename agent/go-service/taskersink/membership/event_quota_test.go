package membership

import (
	"errors"
	"testing"
	"time"
)

func TestEventCouponRedemptionAndPersistence(t *testing.T) {
	path := isolateQuotaState(t)
	status := testStatus(10, "event-device")
	now := time.Now()
	coupon := QuotaRefillCoupon{ID: testCouponID, IssuedOn: now.In(beijingLocation).Format("2006-01-02"), ValidDays: 7, RefillType: QuotaRefillTypeEvent, DurationSeconds: 120, TaskEntry: entryMapPushingFlow}
	redeem := func(c QuotaRefillCoupon) error {
		_, err := redeemQuotaRefillCouponAt(c, now, func() DeviceCodeV7 { return status.DeviceCode })
		return err
	}
	if err := redeem(coupon); err != nil {
		t.Fatal(err)
	}
	if err := redeem(coupon); !errors.Is(err, ErrRefillAlreadyRedeemed) {
		t.Fatalf("duplicate: %v", err)
	}
	coupon.ID = "ffeeddccbbaa99887766554433221100"
	coupon.TaskEntry = ""
	if err := redeem(coupon); err != nil {
		t.Fatal(err)
	}
	state := mustLoadQuotaState(t, path)
	if len(state.EventGrants) != 2 {
		t.Fatalf("grants: %+v", state.EventGrants)
	}
	status.TierCode = "orange_plus"
	status.StartsOn = "2027-01-01"
	status.ExpiresOn = "2027-02-01"
	state = normalizeQuotaPools(status, state, []quotaPool{quotaPoolRegularDaily, quotaPoolSpecialPeriod}, now.AddDate(1, 0, 0))
	if got := eventQuotaRemaining(state, entryMapPushingFlow); got != 240 {
		t.Fatalf("period reset lost event quota: %d", got)
	}
	status.UnlimitedRuntime = true
	state = normalizeQuotaPools(status, state, []quotaPool{quotaPoolRegularDaily, quotaPoolSpecialPeriod}, now)
	if len(state.EventGrants) != 2 {
		t.Fatal("unlimited status lost grants")
	}
}

func TestEventCouponRejectsInvalidDuration(t *testing.T) {
	for _, seconds := range []int64{0, -1, 315360001} {
		coupon := testRefillCoupon(QuotaRefillTypeEvent, "")
		coupon.DurationSeconds = seconds
		_, _, _, err := validateQuotaRefillCoupon(coupon, time.Date(2026, 6, 4, 0, 0, 0, 0, beijingLocation))
		if !errors.Is(err, ErrRefillInvalidCoupon) {
			t.Fatalf("duration %d: %v", seconds, err)
		}
	}
}

func TestQuotaChargeOrderKeepsEventQuotaAsReserve(t *testing.T) {
	path := isolateQuotaState(t)
	status := testStatus(1, "event-device") // 常规额度 60 秒
	status.SpecialPeriodRuntimeMinutes = 1  // 专项额度 60 秒
	now := time.Now()
	state := normalizeQuotaPools(status, quotaState{}, []quotaPool{quotaPoolRegularDaily, quotaPoolSpecialPeriod}, now)
	state.EventGrants = []eventQuotaGrant{
		{LimitSeconds: 20},
		{TaskEntry: entryMapPushingFlow, LimitSeconds: 30},
		{TaskEntry: entryEquipmentRerollMain, LimitSeconds: 100},
	}
	mustSaveQuotaState(t, path, state)

	// 第 1 段：常规额度兜住全部时长，专项与活动额度原封不动。
	snapshot, multiplier, exhausted, err := addQuotaRouteUsageRealSeconds(status, entryMapPushingFlow, quotaRouteSpecialThenRegular, 60, false)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.RegularUsedSeconds != 60 || snapshot.SpecialUsedSeconds != 0 || snapshot.EventRemainingSeconds != 50 {
		t.Fatalf("regular quota should be charged first: %+v", snapshot)
	}
	if multiplier.totalPermille() != multiplierScale {
		t.Fatalf("remaining event quota should keep the 1x shield: %+v", multiplier)
	}
	if exhausted {
		t.Fatal("special and event quota still remain, so the route must not report exhaustion")
	}

	// 第 2 段：常规额度打满后转入专项额度，活动额度仍然不动。
	snapshot, _, exhausted, err = addQuotaRouteUsageRealSeconds(status, entryMapPushingFlow, quotaRouteSpecialThenRegular, 60, false)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.SpecialUsedSeconds != 60 || snapshot.EventRemainingSeconds != 50 {
		t.Fatalf("special quota should be charged after regular quota: %+v", snapshot)
	}
	if exhausted {
		t.Fatal("event quota still remains, so the route must not report exhaustion")
	}

	// 第 3 段：常规与专项都打满后才动用活动额度；同任务的限定额度优先于通用额度。
	snapshot, _, exhausted, err = addQuotaRouteUsageRealSeconds(status, entryMapPushingFlow, quotaRouteSpecialThenRegular, 60, false)
	if err != nil {
		t.Fatal(err)
	}
	if !exhausted {
		t.Fatalf("all quota pools should be exhausted: %+v", snapshot)
	}
	if snapshot.EventRemainingSeconds != 0 {
		t.Fatalf("EventRemainingSeconds = %d, want 0", snapshot.EventRemainingSeconds)
	}
	state = mustLoadQuotaState(t, path)
	if state.EventGrants[1].UsedSeconds != 30 || state.EventGrants[0].UsedSeconds != 20 || state.EventGrants[2].UsedSeconds != 0 {
		t.Fatalf("wrong event grant priority: %+v", state.EventGrants)
	}
}

func TestEventQuotaAllowsTaskWhenRegularExhausted(t *testing.T) {
	path := isolateQuotaState(t)
	status := testStatus(10, "event-device")
	state := normalizeQuotaPools(status, quotaState{}, []quotaPool{quotaPoolRegularDaily, quotaPoolSpecialPeriod}, time.Now())
	pool := state.Pools[string(quotaPoolRegularDaily)]
	pool.UsedSeconds = pool.LimitSeconds
	state.Pools[string(quotaPoolRegularDaily)] = pool
	state.EventGrants = []eventQuotaGrant{{TaskEntry: entryMapPushingFlow, LimitSeconds: 20}}
	mustSaveQuotaState(t, path, state)
	if _, ok, err := EnsureQuotaRouteAvailable(status, quotaRouteSpecialThenRegular, entryMapPushingFlow); err != nil || !ok {
		t.Fatalf("eligible task denied: %v", err)
	}
	if _, ok, err := EnsureQuotaRouteAvailable(status, quotaRouteSpecialThenRegular, entryEquipmentRerollMain); err != nil || ok {
		t.Fatalf("unrelated task allowed: %v", err)
	}
	snapshot, _, _, err := addQuotaRouteUsageRealSeconds(status, entryMapPushingFlow, quotaRouteSpecialThenRegular, 20, true)
	if err != nil || snapshot.RemainingSeconds != 0 {
		t.Fatalf("last event seconds: %+v %v", snapshot, err)
	}
}

func TestEventQuotaShieldsRegularQuotaFromMultiplier(t *testing.T) {
	for _, entry := range HighConsumptionEntries() {
		t.Run(entry, func(t *testing.T) {
			path := isolateQuotaState(t)
			status := testStatus(10, "event-device") // 没有会员专项额度，活动额度仍为 1 倍。
			state := normalizeQuotaPools(status, quotaState{}, []quotaPool{quotaPoolRegularDaily, quotaPoolSpecialPeriod}, time.Now())
			state.EventGrants = []eventQuotaGrant{{TaskEntry: entry, LimitSeconds: 30}}
			mustSaveQuotaState(t, path, state)

			// 活动额度尚未用尽：常规额度按 1 倍扣减，活动额度完整保留。
			snapshot, multiplier, exhausted, err := addQuotaRouteUsageRealSeconds(status, entry, quotaRouteForEntry(entry), 20, false)
			if err != nil {
				t.Fatal(err)
			}
			if multiplier.totalPermille() != multiplierScale {
				t.Fatalf("regular quota multiplied while event quota remains: %+v", multiplier)
			}
			if snapshot.RegularUsedSeconds != 20 || snapshot.EventRemainingSeconds != 30 {
				t.Fatalf("event quota should stay untouched: %+v", snapshot)
			}
			if exhausted {
				t.Fatal("quota should not be reported exhausted")
			}

			// 活动额度用尽后，高级任务才按 5 倍计费常规额度。
			state = mustLoadQuotaState(t, path)
			state.EventGrants[0].UsedSeconds = state.EventGrants[0].LimitSeconds
			mustSaveQuotaState(t, path, state)
			snapshot, multiplier, _, err = addQuotaRouteUsageRealSeconds(status, entry, quotaRouteForEntry(entry), 2, false)
			if err != nil {
				t.Fatal(err)
			}
			if multiplier.totalPermille() != 5*multiplierScale {
				t.Fatalf("multiplier after event quota is gone = %d, want %d", multiplier.totalPermille(), 5*multiplierScale)
			}
			if snapshot.RegularUsedSeconds != 30 {
				t.Fatalf("RegularUsedSeconds = %d, want 30 (20 at 1x plus 2 real seconds at 5x)", snapshot.RegularUsedSeconds)
			}
		})
	}
}

// 会员每天有 60 秒常规额度（每日重置），手里还有 60 秒永不过期的活动额度。
// 旧顺序会先把活动额度烧光，之后常规额度只能按 5 倍计费；新顺序则先用当天会作废的
// 常规额度，活动额度跨日完整保留并持续充当 1 倍护盾。
func TestMemberKeepsEventQuotaAcrossBusinessDays(t *testing.T) {
	path := isolateQuotaState(t)
	status := testStatus(1, "event-device") // 每个业务日 60 秒常规额度
	status.IsMember = true
	status.TierCode = "orange_plus"
	status.TierName = "Orange Plus"

	firstDay := time.Now()
	state := normalizeQuotaPools(status, quotaState{}, []quotaPool{quotaPoolRegularDaily, quotaPoolSpecialPeriod}, firstDay)
	state.EventGrants = []eventQuotaGrant{{TaskEntry: entryMapPushingFlow, LimitSeconds: 60}}
	mustSaveQuotaState(t, path, state)

	// 第 1 天：60 秒时长全部由常规额度承担，活动额度一秒未动。
	snapshot, multiplier, exhausted, err := addQuotaRouteUsageRealSeconds(status, entryMapPushingFlow, quotaRouteSpecialThenRegular, 60, false)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.RegularUsedSeconds != 60 || snapshot.EventRemainingSeconds != 60 {
		t.Fatalf("regular quota should absorb the whole run: %+v", snapshot)
	}
	if multiplier.totalPermille() != multiplierScale {
		t.Fatalf("multiplier = %d, want %d", multiplier.totalPermille(), multiplierScale)
	}
	if exhausted {
		t.Fatal("event quota still remains, so the route must not report exhaustion")
	}

	// 跨业务日：常规额度按业务日重置，活动额度不受每日重置影响。
	state = normalizeQuotaPools(status, mustLoadQuotaState(t, path), []quotaPool{quotaPoolRegularDaily, quotaPoolSpecialPeriod}, firstDay.AddDate(0, 0, 1))
	if got := quotaPoolRemaining(state, quotaPoolRegularDaily); got != 60 {
		t.Fatalf("regular quota was not reset on the next business day: %d", got)
	}
	if got := eventQuotaRemaining(state, entryMapPushingFlow); got != 60 {
		t.Fatalf("event quota should survive the daily reset: %d", got)
	}
}

// 常规额度每天都会被自己的上限打满，但此时活动额度往往仍有剩余。
// 停止条件必须按“所有额度池都已用尽”判断，否则任务会在还有活动额度的当天被误停。
func TestConsumeTickKeepsRunningWhileEventQuotaRemains(t *testing.T) {
	path := isolateQuotaState(t)
	status := testStatus(10, "device-a") // 常规额度 600 秒
	state := normalizeQuotaPools(status, quotaState{}, []quotaPool{quotaPoolRegularDaily, quotaPoolSpecialPeriod}, time.Now())
	regular := state.Pools[string(quotaPoolRegularDaily)]
	regular.UsedSeconds = regular.LimitSeconds
	state.Pools[string(quotaPoolRegularDaily)] = regular
	state.EventGrants = []eventQuotaGrant{{TaskEntry: entryMapPushingFlow, LimitSeconds: 600}}
	mustSaveQuotaState(t, path, state)

	tracker := &RuntimeTracker{
		active:     true,
		generation: 1,
		entry:      entryMapPushingFlow,
		route:      quotaRouteSpecialThenRegular,
		status:     status,
		last:       time.Now().Add(-30 * time.Second),
		multiplier: quotaMultiplier{BasePermille: multiplierScale, ExtraPermille: multiplierScale},
		stopCh:     make(chan struct{}),
	}

	snapshot, done := tracker.consumeTick(status, quotaRouteSpecialThenRegular, 1)
	if done {
		t.Fatal("consumeTick() must not stop while event quota still remains")
	}
	if tracker.stopped {
		t.Fatal("stopped = true, want false (the task must keep running)")
	}
	if snapshot.EventRemainingSeconds != 570 {
		t.Fatalf("EventRemainingSeconds = %d, want 570", snapshot.EventRemainingSeconds)
	}
}

func TestQuotaReserveSecondsCountsAllPools(t *testing.T) {
	snapshot := QuotaSnapshot{
		RegularRemainingSeconds: 10,
		SpecialRemainingSeconds: 20,
		EventRemainingSeconds:   30,
	}
	if got := quotaReserveSeconds(snapshot); got != 60 {
		t.Fatalf("quotaReserveSeconds() = %d, want 60", got)
	}
	if got := quotaReserveSeconds(QuotaSnapshot{UnlimitedRuntime: true, RegularRemainingSeconds: 10}); got != 0 {
		t.Fatalf("quotaReserveSeconds() for unlimited runtime = %d, want 0", got)
	}
}
