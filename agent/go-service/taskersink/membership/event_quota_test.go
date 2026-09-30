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

	// 第 1 段：常规额度最先扣减，且高级任务在此按 5 倍计费——60 秒额度只覆盖 12 秒
	// 实际时长，溢出的 48 秒落到专项额度上；活动额度一秒未动。
	snapshot, multiplier, exhausted, err := addQuotaRouteUsageRealSeconds(status, entryMapPushingFlow, quotaRouteSpecialThenRegular, 60, false)
	if err != nil {
		t.Fatal(err)
	}
	if multiplier.totalPermille() != 5*multiplierScale {
		t.Fatalf("regular quota should be charged at 5x: %+v", multiplier)
	}
	if snapshot.RegularUsedSeconds != 60 || snapshot.SpecialUsedSeconds != 48 || snapshot.EventRemainingSeconds != 50 {
		t.Fatalf("wrong pool split: %+v", snapshot)
	}
	if exhausted {
		t.Fatal("special and event quota still remain, so the route must not report exhaustion")
	}

	// 第 2 段：常规额度打满后转入专项额度，专项按实际时长（1 倍）扣减。
	snapshot, multiplier, exhausted, err = addQuotaRouteUsageRealSeconds(status, entryMapPushingFlow, quotaRouteSpecialThenRegular, 12, false)
	if err != nil {
		t.Fatal(err)
	}
	if multiplier.totalPermille() != multiplierScale {
		t.Fatalf("special quota must not be multiplied: %+v", multiplier)
	}
	if snapshot.SpecialUsedSeconds != 60 || snapshot.EventRemainingSeconds != 50 {
		t.Fatalf("special quota should absorb the run: %+v", snapshot)
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

// 倍率是额度池的属性：只要扣的是常规额度就按 5 倍（高级任务），
// 活动额度与专项额度则始终按实际时长扣减。手里有活动额度不会让常规额度变得“更耐用”。
func TestMultiplierFollowsChargedPool(t *testing.T) {
	for _, entry := range HighConsumptionEntries() {
		t.Run(entry, func(t *testing.T) {
			path := isolateQuotaState(t)
			status := testStatus(10, "event-device") // 常规额度 600 秒
			state := normalizeQuotaPools(status, quotaState{}, []quotaPool{quotaPoolRegularDaily, quotaPoolSpecialPeriod}, time.Now())
			state.EventGrants = []eventQuotaGrant{{TaskEntry: entry, LimitSeconds: 60}}
			mustSaveQuotaState(t, path, state)

			// 常规额度优先扣减，且高级任务在此按 5 倍计费：20 秒实际时长吃掉 100 秒常规额度，
			// 活动额度一秒未动。
			snapshot, multiplier, exhausted, err := addQuotaRouteUsageRealSeconds(status, entry, quotaRouteForEntry(entry), 20, false)
			if err != nil {
				t.Fatal(err)
			}
			if multiplier.totalPermille() != 5*multiplierScale {
				t.Fatalf("regular quota should be charged at 5x: %+v", multiplier)
			}
			if snapshot.RegularUsedSeconds != 100 || snapshot.EventRemainingSeconds != 60 {
				t.Fatalf("wrong pool split: %+v", snapshot)
			}
			if exhausted {
				t.Fatal("quota should not be reported exhausted")
			}

			// 常规额度用尽后转活动额度：按实际时长扣减，倍率回到 1 倍。
			state = mustLoadQuotaState(t, path)
			regular := state.Pools[string(quotaPoolRegularDaily)]
			regular.UsedSeconds = regular.LimitSeconds
			state.Pools[string(quotaPoolRegularDaily)] = regular
			mustSaveQuotaState(t, path, state)

			snapshot, multiplier, _, err = addQuotaRouteUsageRealSeconds(status, entry, quotaRouteForEntry(entry), 20, false)
			if err != nil {
				t.Fatal(err)
			}
			if multiplier.totalPermille() != multiplierScale {
				t.Fatalf("event quota must not be multiplied: %+v", multiplier)
			}
			if snapshot.EventRemainingSeconds != 40 {
				t.Fatalf("EventRemainingSeconds = %d, want 40", snapshot.EventRemainingSeconds)
			}
		})
	}
}

// 会员每天有 60 秒常规额度（每日重置），手里还有 60 秒永不过期的活动额度。
// 常规额度优先扣减，因此每天的常规额度都会被用满而不是白白重置；活动额度只在常规
// 额度用尽后按实际时长补充，并且不受每日重置影响。
func TestMemberRegularQuotaIsUsedBeforeEventQuota(t *testing.T) {
	path := isolateQuotaState(t)
	status := testStatus(1, "event-device") // 每个业务日 60 秒常规额度
	status.IsMember = true
	status.TierCode = "orange_plus"
	status.TierName = "Orange Plus"

	firstDay := time.Now()
	state := normalizeQuotaPools(status, quotaState{}, []quotaPool{quotaPoolRegularDaily, quotaPoolSpecialPeriod}, firstDay)
	state.EventGrants = []eventQuotaGrant{{TaskEntry: entryMapPushingFlow, LimitSeconds: 60}}
	mustSaveQuotaState(t, path, state)

	// 第 1 天：常规额度先被打满（高级任务 5 倍，60 秒额度覆盖 12 秒实际时长），
	// 不足的 48 秒才落到活动额度上。
	snapshot, multiplier, exhausted, err := addQuotaRouteUsageRealSeconds(status, entryMapPushingFlow, quotaRouteSpecialThenRegular, 60, false)
	if err != nil {
		t.Fatal(err)
	}
	if multiplier.totalPermille() != 5*multiplierScale {
		t.Fatalf("regular quota should be charged at 5x: %+v", multiplier)
	}
	if snapshot.RegularUsedSeconds != 60 || snapshot.EventRemainingSeconds != 12 {
		t.Fatalf("regular quota should be drained first: %+v", snapshot)
	}
	if exhausted {
		t.Fatal("event quota still remains, so the route must not report exhaustion")
	}

	// 跨业务日：常规额度按业务日重置，活动额度不受每日重置影响。
	state = normalizeQuotaPools(status, mustLoadQuotaState(t, path), []quotaPool{quotaPoolRegularDaily, quotaPoolSpecialPeriod}, firstDay.AddDate(0, 0, 1))
	if got := quotaPoolRemaining(state, quotaPoolRegularDaily); got != 60 {
		t.Fatalf("regular quota was not reset on the next business day: %d", got)
	}
	if got := eventQuotaRemaining(state, entryMapPushingFlow); got != 12 {
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

func TestCrossPoolChargeReturnsUnmultipliedOnlyWhenRegularWasNotUsed(t *testing.T) {
	path := isolateQuotaState(t)
	status := testStatus(1, "device-a")
	state := normalizeQuotaPools(status, quotaState{}, []quotaPool{quotaPoolRegularDaily, quotaPoolSpecialPeriod}, time.Now())
	regular := state.Pools[string(quotaPoolRegularDaily)]
	regular.UsedSeconds = regular.LimitSeconds - 1
	state.Pools[string(quotaPoolRegularDaily)] = regular
	state.EventGrants = []eventQuotaGrant{{TaskEntry: entryMapPushingFlow, LimitSeconds: 10}}
	mustSaveQuotaState(t, path, state)
	snapshot, multiplier, _, err := addQuotaRouteUsageRealSeconds(status, entryMapPushingFlow, quotaRouteForEntry(entryMapPushingFlow), 2, false)
	if err != nil {
		t.Fatal(err)
	}
	if multiplier.totalPermille() != 5*multiplierScale {
		t.Fatalf("multiplier = %d, want 5000", multiplier.totalPermille())
	}
	if snapshot.RegularUsedSeconds != 60 || snapshot.EventRemainingSeconds != 8 {
		t.Fatalf("cross-pool charge lost runtime: %+v", snapshot)
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

// 每张券兑换出来的额度各自独立计时：填了时限的券写入自己的到期时间，没填的永久有效。
func TestEventCouponRedemptionAppliesValidityWindow(t *testing.T) {
	path := isolateQuotaState(t)
	status := testStatus(10, "event-device")
	now := time.Now()
	redeem := func(coupon QuotaRefillCoupon) (RefillResult, error) {
		return redeemQuotaRefillCouponAt(coupon, now, func() DeviceCodeV7 { return status.DeviceCode })
	}

	limited := QuotaRefillCoupon{
		ID:                testCouponID,
		IssuedOn:          now.In(beijingLocation).Format("2006-01-02"),
		ValidDays:         7,
		RefillType:        QuotaRefillTypeEvent,
		DurationSeconds:   600,
		EventValidSeconds: 12 * 60 * 60,
	}
	result, err := redeem(limited)
	if err != nil {
		t.Fatal(err)
	}
	wantExpiry := now.Add(12 * time.Hour).Format(time.RFC3339)
	if result.EventExpiresAt != wantExpiry {
		t.Fatalf("EventExpiresAt = %q, want %q", result.EventExpiresAt, wantExpiry)
	}

	permanent := limited
	permanent.ID = "ffeeddccbbaa99887766554433221100"
	permanent.EventValidSeconds = 0
	permanentResult, err := redeem(permanent)
	if err != nil {
		t.Fatal(err)
	}
	if permanentResult.EventExpiresAt != "" {
		t.Fatalf("permanent coupon EventExpiresAt = %q, want empty", permanentResult.EventExpiresAt)
	}

	state := mustLoadQuotaState(t, path)
	if len(state.EventGrants) != 2 {
		t.Fatalf("grants: %+v", state.EventGrants)
	}
	if state.EventGrants[0].ExpiresAt != wantExpiry {
		t.Fatalf("limited grant ExpiresAt = %q, want %q", state.EventGrants[0].ExpiresAt, wantExpiry)
	}
	if state.EventGrants[1].ExpiresAt != "" {
		t.Fatalf("permanent grant ExpiresAt = %q, want empty", state.EventGrants[1].ExpiresAt)
	}
}

func TestEventCouponRejectsInvalidValidity(t *testing.T) {
	for _, seconds := range []int64{-1, maxEventValiditySeconds + 1} {
		coupon := testRefillCoupon(QuotaRefillTypeEvent, "")
		coupon.EventValidSeconds = seconds
		_, _, _, err := validateQuotaRefillCoupon(coupon, time.Date(2026, 6, 4, 0, 0, 0, 0, beijingLocation))
		if !errors.Is(err, ErrRefillInvalidCoupon) {
			t.Fatalf("validity %d: %v", seconds, err)
		}
	}
}

// 过期的活动额度不再计入剩余，也不参与扣减，但仍保留在记录里供「额度显示」提示用户。
func TestExpiredEventQuotaIsIgnored(t *testing.T) {
	path := isolateQuotaState(t)
	status := testStatus(10, "event-device") // 常规额度 600 秒，无专项额度
	now := time.Now()
	state := normalizeQuotaPools(status, quotaState{}, []quotaPool{quotaPoolRegularDaily, quotaPoolSpecialPeriod}, now)
	regular := state.Pools[string(quotaPoolRegularDaily)]
	regular.UsedSeconds = regular.LimitSeconds
	state.Pools[string(quotaPoolRegularDaily)] = regular
	state.EventGrants = []eventQuotaGrant{
		{TaskEntry: entryMapPushingFlow, LimitSeconds: 600, ExpiresAt: now.Add(-time.Hour).Format(time.RFC3339)},
		{TaskEntry: entryMapPushingFlow, LimitSeconds: 300, ExpiresAt: now.Add(time.Hour).Format(time.RFC3339)},
	}
	mustSaveQuotaState(t, path, state)

	if got := eventQuotaRemaining(state, entryMapPushingFlow); got != 300 {
		t.Fatalf("eventQuotaRemaining() = %d, want 300 (expired grant excluded)", got)
	}

	if _, _, _, err := addQuotaRouteUsageRealSeconds(status, entryMapPushingFlow, quotaRouteSpecialThenRegular, 120, false); err != nil {
		t.Fatal(err)
	}
	state = mustLoadQuotaState(t, path)
	if state.EventGrants[0].UsedSeconds != 0 {
		t.Fatalf("expired grant was charged: %+v", state.EventGrants[0])
	}
	if state.EventGrants[1].UsedSeconds != 120 {
		t.Fatalf("active grant UsedSeconds = %d, want 120", state.EventGrants[1].UsedSeconds)
	}
}

// 每张券的额度各自独立，扣减必须先消耗最早失效的那一笔：若按兑换顺序扣，先兑换的额度
// 会在后兑换的额度被用光之前就白白过期。
func TestEventQuotaChargesEarliestExpiryFirst(t *testing.T) {
	path := isolateQuotaState(t)
	status := testStatus(10, "event-device")
	now := time.Now()
	state := normalizeQuotaPools(status, quotaState{}, []quotaPool{quotaPoolRegularDaily, quotaPoolSpecialPeriod}, now)
	regular := state.Pools[string(quotaPoolRegularDaily)]
	regular.UsedSeconds = regular.LimitSeconds
	state.Pools[string(quotaPoolRegularDaily)] = regular
	state.EventGrants = []eventQuotaGrant{
		{LimitSeconds: 100}, // 永久，最先兑换
		{LimitSeconds: 100, ExpiresAt: now.Add(48 * time.Hour).Format(time.RFC3339)},
		{LimitSeconds: 100, ExpiresAt: now.Add(time.Hour).Format(time.RFC3339)},
	}
	mustSaveQuotaState(t, path, state)

	if _, _, _, err := addQuotaRouteUsageRealSeconds(status, entryMapPushingFlow, quotaRouteSpecialThenRegular, 150, false); err != nil {
		t.Fatal(err)
	}
	state = mustLoadQuotaState(t, path)
	if state.EventGrants[2].UsedSeconds != 100 {
		t.Fatalf("one-hour grant UsedSeconds = %d, want 100 (charged first)", state.EventGrants[2].UsedSeconds)
	}
	if state.EventGrants[1].UsedSeconds != 50 {
		t.Fatalf("two-day grant UsedSeconds = %d, want 50", state.EventGrants[1].UsedSeconds)
	}
	if state.EventGrants[0].UsedSeconds != 0 {
		t.Fatalf("permanent grant should stay untouched: %+v", state.EventGrants[0])
	}
}

// 常规、专项、活动三个池统一按失效时刻排序：专项额度本周期即将结束时，
// 它必须排在永久的活动额度之前被消耗，而不是永远排在最后。
func TestSpecialQuotaChargedBeforePermanentEventQuota(t *testing.T) {
	path := isolateQuotaState(t)
	status := testStatus(10, "device-a") // 常规额度 600 秒
	status.SpecialPeriodRuntimeMinutes = 5
	status.ExpiresOn = time.Now().In(beijingLocation).Format("2006-01-02") // 本周期今天结束
	now := time.Now()
	state := normalizeQuotaPools(status, quotaState{}, []quotaPool{quotaPoolRegularDaily, quotaPoolSpecialPeriod}, now)
	state.EventGrants = []eventQuotaGrant{{LimitSeconds: 600}}
	mustSaveQuotaState(t, path, state)

	// 专项额度（次日 0 点失效）先于常规额度（次日 4 点失效）与永久活动额度被消耗。
	if _, _, _, err := addQuotaRouteUsageRealSeconds(status, entryMapPushingFlow, quotaRouteSpecialThenRegular, 400, false); err != nil {
		t.Fatal(err)
	}
	state = mustLoadQuotaState(t, path)
	if state.Pools[string(quotaPoolSpecialPeriod)].UsedSeconds != 300 {
		t.Fatalf("special quota should be drained first: %+v", state.Pools[string(quotaPoolSpecialPeriod)])
	}
	// 剩余 100 秒实际时长落到常规额度上（高级任务 5 倍）。
	if state.Pools[string(quotaPoolRegularDaily)].UsedSeconds != 500 {
		t.Fatalf("regular quota UsedSeconds = %d, want 500", state.Pools[string(quotaPoolRegularDaily)].UsedSeconds)
	}
	if state.EventGrants[0].UsedSeconds != 0 {
		t.Fatalf("permanent event quota should stay untouched: %+v", state.EventGrants[0])
	}
}

// 反过来，当某笔活动额度比专项额度更早失效时，它必须先于专项额度被消耗。
func TestEventQuotaChargedBeforeLaterSpecialQuota(t *testing.T) {
	path := isolateQuotaState(t)
	status := testStatus(10, "device-a") // 常规额度 600 秒
	status.SpecialPeriodRuntimeMinutes = 5
	now := time.Now()
	state := normalizeQuotaPools(status, quotaState{}, []quotaPool{quotaPoolRegularDaily, quotaPoolSpecialPeriod}, now)
	regular := state.Pools[string(quotaPoolRegularDaily)]
	regular.UsedSeconds = regular.LimitSeconds
	state.Pools[string(quotaPoolRegularDaily)] = regular
	state.EventGrants = []eventQuotaGrant{
		{TaskEntry: entryMapPushingFlow, LimitSeconds: 200, ExpiresAt: now.Add(time.Hour).Format(time.RFC3339)},
	}
	mustSaveQuotaState(t, path, state)

	if _, _, _, err := addQuotaRouteUsageRealSeconds(status, entryMapPushingFlow, quotaRouteSpecialThenRegular, 250, false); err != nil {
		t.Fatal(err)
	}
	state = mustLoadQuotaState(t, path)
	if state.EventGrants[0].UsedSeconds != 200 {
		t.Fatalf("event grant UsedSeconds = %d, want 200 (expires first)", state.EventGrants[0].UsedSeconds)
	}
	if state.Pools[string(quotaPoolSpecialPeriod)].UsedSeconds != 50 {
		t.Fatalf("special quota UsedSeconds = %d, want 50", state.Pools[string(quotaPoolSpecialPeriod)].UsedSeconds)
	}
}
