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

func TestEventQuotaTaskIsolationAndPriority(t *testing.T) {
	path := isolateQuotaState(t)
	status := testStatus(10, "event-device")
	status.SpecialPeriodRuntimeMinutes = 1
	now := time.Now()
	state := normalizeQuotaPools(status, quotaState{}, []quotaPool{quotaPoolRegularDaily, quotaPoolSpecialPeriod}, now)
	state.EventGrants = []eventQuotaGrant{{LimitSeconds: 20}, {TaskEntry: entryMapPushingFlow, LimitSeconds: 30}, {TaskEntry: entryEquipmentRerollMain, LimitSeconds: 100}}
	mustSaveQuotaState(t, path, state)
	// 先耗尽本任务的 30 秒，再用通用 10 秒，其他任务和专项额度不变。
	snapshot, _, _, err := addQuotaRouteUsageRealSeconds(status, entryMapPushingFlow, quotaRouteSpecialThenRegular, 40, false)
	if err != nil {
		t.Fatal(err)
	}
	state = mustLoadQuotaState(t, path)
	if state.EventGrants[0].UsedSeconds != 10 || state.EventGrants[1].UsedSeconds != 30 || state.EventGrants[2].UsedSeconds != 0 || snapshot.SpecialUsedSeconds != 0 {
		t.Fatalf("wrong priority: %+v / %+v", state.EventGrants, snapshot)
	}
	// 剩余活动 10 秒 + 专项 60 秒 + 常规实际 10 秒（50 秒额度）。
	snapshot, _, _, err = addQuotaRouteUsageRealSeconds(status, entryMapPushingFlow, quotaRouteSpecialThenRegular, 80, false)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.EventRemainingSeconds != 0 || snapshot.SpecialUsedSeconds != 60 || snapshot.RegularUsedSeconds != 50 {
		t.Fatalf("wrong spillover: %+v", snapshot)
	}
	state = mustLoadQuotaState(t, path)
	if state.EventGrants[2].UsedSeconds != 0 {
		t.Fatal("charged unrelated task")
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

func TestOnlyRegularQuotaUsesMultiplier(t *testing.T) {
	for _, entry := range HighConsumptionEntries() {
		t.Run(entry, func(t *testing.T) {
			path := isolateQuotaState(t)
			status := testStatus(10, "event-device") // 没有会员专项额度，活动额度仍为 1 倍。
			state := normalizeQuotaPools(status, quotaState{}, []quotaPool{quotaPoolRegularDaily, quotaPoolSpecialPeriod}, time.Now())
			state.EventGrants = []eventQuotaGrant{{TaskEntry: entry, LimitSeconds: 30}}
			mustSaveQuotaState(t, path, state)
			snapshot, multiplier, _, err := addQuotaRouteUsageRealSeconds(status, entry, quotaRouteForEntry(entry), 20, false)
			if err != nil {
				t.Fatal(err)
			}
			if snapshot.EventRemainingSeconds != 10 || snapshot.RegularUsedSeconds != 0 || multiplier.totalPermille() != multiplierScale {
				t.Fatalf("event quota multiplied: %+v %+v", snapshot, multiplier)
			}
			snapshot, _, _, err = addQuotaRouteUsageRealSeconds(status, entry, quotaRouteForEntry(entry), 20, true)
			if err != nil {
				t.Fatal(err)
			}
			if snapshot.EventRemainingSeconds != 0 || snapshot.RegularUsedSeconds != 50 {
				t.Fatalf("only remaining 10 seconds should be multiplied: %+v", snapshot)
			}
		})
	}
}
