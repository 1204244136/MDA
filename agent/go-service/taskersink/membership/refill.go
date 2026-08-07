package membership

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	deviceMatchThreshold      = 80
	quotaRefillCouponIDBytes  = 16
	maxQuotaRefillValidityDay = 3650
)

var (
	ErrRefillDateMismatch    = errors.New("quota refill package is not valid today")
	ErrRefillNotYetValid     = errors.New("quota refill coupon is not valid yet")
	ErrRefillExpired         = errors.New("quota refill coupon has expired")
	ErrRefillDeviceMismatch  = errors.New("quota refill coupon is not valid for this device")
	ErrRefillStateMismatch   = errors.New("quota state belongs to a different device")
	ErrRefillAlreadyRedeemed = errors.New("quota refill coupon has already been redeemed")
	ErrRefillInvalidCoupon   = errors.New("quota refill coupon is invalid")
)

// QuotaRefillType identifies the quota pool reset by a coupon.
type QuotaRefillType string

const (
	QuotaRefillTypeDaily   QuotaRefillType = "daily"
	QuotaRefillTypeMonthly QuotaRefillType = "monthly"
)

// QuotaRefillCoupon is the immutable payload embedded in a refill executable.
type QuotaRefillCoupon struct {
	ID         string
	IssuedOn   string
	ValidDays  int
	RefillType QuotaRefillType
	SponsorURL string
}

// RefillResult describes a successful quota coupon redemption.
type RefillResult struct {
	Path         string
	CouponID     string
	DeviceHash   string
	BusinessDate string
	ValidThrough string
	RefillType   QuotaRefillType
}

func DeviceCodeFromSponsorURL(rawURL string) (DeviceCodeV7, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return DeviceCodeV7{}, err
	}
	if parsed.RawQuery == "" {
		return DeviceCodeV7{}, errors.New("sponsor URL has no device query")
	}
	values := parsed.Query()
	device := DeviceCodeV7{
		CPUHash:   values.Get("cpu"),
		UUIDHash:  values.Get("uuid"),
		BIOSHash:  values.Get("bios"),
		BoardHash: values.Get("board"),
		DiskHash:  values.Get("disk"),
		GUIDHash:  values.Get("guid"),
	}
	if weight := deviceCodeAvailableWeight(device); weight < deviceMatchThreshold {
		return DeviceCodeV7{}, fmt.Errorf("sponsor URL device fields only provide %d match weight; need at least %d", weight, deviceMatchThreshold)
	}
	return device, nil
}

func DeviceHashFromSponsorURL(rawURL string) (string, error) {
	device, err := DeviceCodeFromSponsorURL(rawURL)
	if err != nil {
		return "", err
	}
	return deviceHash(device), nil
}

func deviceCodeAvailableWeight(device DeviceCodeV7) int {
	weight := 0
	if device.CPUHash != "" {
		weight += v7Weights["cpu"]
	}
	if device.UUIDHash != "" {
		weight += v7Weights["uuid"]
	}
	if device.BIOSHash != "" {
		weight += v7Weights["bios"]
	}
	if device.BoardHash != "" {
		weight += v7Weights["board"]
	}
	if device.DiskHash != "" {
		weight += v7Weights["disk"]
	}
	if device.GUIDHash != "" {
		weight += v7Weights["guid"]
	}
	return weight
}

// QuotaRefillValidThrough returns the last valid Beijing calendar date.
func QuotaRefillValidThrough(issuedOn string, validDays int) (string, error) {
	issuedAt, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(issuedOn), beijingLocation)
	if err != nil {
		return "", fmt.Errorf("%w: issued date must be YYYY-MM-DD", ErrRefillInvalidCoupon)
	}
	if validDays < 1 || validDays > maxQuotaRefillValidityDay {
		return "", fmt.Errorf("%w: valid days must be between 1 and %d", ErrRefillInvalidCoupon, maxQuotaRefillValidityDay)
	}
	return issuedAt.AddDate(0, 0, validDays-1).Format("2006-01-02"), nil
}

// RedeemQuotaRefillCoupon validates and applies a refill coupon.
func RedeemQuotaRefillCoupon(coupon QuotaRefillCoupon) (RefillResult, error) {
	return redeemQuotaRefillCoupon(coupon, time.Now, GenerateDeviceCodeV7)
}

func redeemQuotaRefillCouponAt(
	coupon QuotaRefillCoupon,
	now time.Time,
	currentDevice func() DeviceCodeV7,
) (RefillResult, error) {
	return redeemQuotaRefillCoupon(coupon, func() time.Time { return now }, currentDevice)
}

func redeemQuotaRefillCoupon(
	coupon QuotaRefillCoupon,
	clock func() time.Time,
	currentDevice func() DeviceCodeV7,
) (RefillResult, error) {
	normalized, validThrough, pool, err := validateQuotaRefillCoupon(coupon, clock())
	if err != nil {
		return RefillResult{}, err
	}

	current := currentDevice()
	if normalized.SponsorURL != "" {
		target, err := DeviceCodeFromSponsorURL(normalized.SponsorURL)
		if err != nil {
			return RefillResult{}, fmt.Errorf("%w: %v", ErrRefillInvalidCoupon, err)
		}
		if MatchDeviceCodeV7(current, target) < deviceMatchThreshold {
			return RefillResult{}, ErrRefillDeviceMismatch
		}
	}

	quotaMu.Lock()
	defer quotaMu.Unlock()
	unlock, err := lockQuotaStateFile()
	if err != nil {
		return RefillResult{}, err
	}
	defer unlock()
	commitNow := clock()
	normalized, validThrough, pool, err = validateQuotaRefillCoupon(coupon, commitNow)
	if err != nil {
		return RefillResult{}, err
	}

	path, err := quotaStatePath()
	if err != nil {
		return RefillResult{}, err
	}
	state, err := loadQuotaState(path)
	if err != nil {
		return RefillResult{}, err
	}
	migrateLegacyQuotaState(&state)
	if state.RedeemedCoupons == nil {
		state.RedeemedCoupons = map[string]quotaCouponRedemption{}
	}
	if _, redeemed := state.RedeemedCoupons[normalized.ID]; redeemed {
		return RefillResult{}, ErrRefillAlreadyRedeemed
	}

	currentHash := state.DeviceHash
	if current != (DeviceCodeV7{}) || currentHash == "" {
		currentHash = deviceHash(current)
	}
	if state.DeviceHash != "" && state.DeviceHash != currentHash {
		return RefillResult{}, ErrRefillStateMismatch
	}
	if state.Pools == nil {
		state.Pools = map[string]quotaPoolState{}
	}
	state.Version = quotaStateVersion
	state.DeviceHash = currentHash

	businessDate := quotaBusinessDate(commitNow)
	updatedAt := commitNow.Format(time.RFC3339)
	resetQuotaPool(&state, pool, businessDate, updatedAt)
	state.RedeemedCoupons[normalized.ID] = quotaCouponRedemption{
		RedeemedAt: updatedAt,
		RefillType: normalized.RefillType,
		DeviceHash: currentHash,
	}
	if err := saveQuotaState(path, state); err != nil {
		return RefillResult{}, err
	}

	return RefillResult{
		Path:         path,
		CouponID:     normalized.ID,
		DeviceHash:   currentHash,
		BusinessDate: businessDate,
		ValidThrough: validThrough,
		RefillType:   normalized.RefillType,
	}, nil
}

func validateQuotaRefillCoupon(coupon QuotaRefillCoupon, now time.Time) (QuotaRefillCoupon, string, quotaPool, error) {
	id := strings.ToLower(strings.TrimSpace(coupon.ID))
	decodedID, err := hex.DecodeString(id)
	if err != nil || len(decodedID) != quotaRefillCouponIDBytes {
		return QuotaRefillCoupon{}, "", "", fmt.Errorf("%w: ID must be 32 hexadecimal characters", ErrRefillInvalidCoupon)
	}
	coupon.ID = id
	coupon.IssuedOn = strings.TrimSpace(coupon.IssuedOn)
	coupon.SponsorURL = strings.TrimSpace(coupon.SponsorURL)

	validThrough, err := QuotaRefillValidThrough(coupon.IssuedOn, coupon.ValidDays)
	if err != nil {
		return QuotaRefillCoupon{}, "", "", err
	}
	issuedAt, _ := time.ParseInLocation("2006-01-02", coupon.IssuedOn, beijingLocation)
	validThroughAt, _ := time.ParseInLocation("2006-01-02", validThrough, beijingLocation)
	todayText := now.In(beijingLocation).Format("2006-01-02")
	today, _ := time.ParseInLocation("2006-01-02", todayText, beijingLocation)
	if today.Before(issuedAt) {
		return QuotaRefillCoupon{}, "", "", ErrRefillNotYetValid
	}
	if today.After(validThroughAt) {
		return QuotaRefillCoupon{}, "", "", ErrRefillExpired
	}

	var pool quotaPool
	switch coupon.RefillType {
	case QuotaRefillTypeDaily:
		pool = quotaPoolRegularDaily
	case QuotaRefillTypeMonthly:
		pool = quotaPoolSpecialPeriod
	default:
		return QuotaRefillCoupon{}, "", "", fmt.Errorf("%w: unknown refill type %q", ErrRefillInvalidCoupon, coupon.RefillType)
	}
	if coupon.SponsorURL != "" {
		if _, err := DeviceCodeFromSponsorURL(coupon.SponsorURL); err != nil {
			return QuotaRefillCoupon{}, "", "", fmt.Errorf("%w: %v", ErrRefillInvalidCoupon, err)
		}
	}
	return coupon, validThrough, pool, nil
}

func resetQuotaPool(state *quotaState, pool quotaPool, businessDate string, updatedAt string) {
	poolKey := string(pool)
	poolState := state.Pools[poolKey]
	poolState.UsedSeconds = 0
	poolState.UpdatedAt = updatedAt
	if pool == quotaPoolRegularDaily {
		poolState.PeriodKey = businessDate
		state.BusinessDate = businessDate
		state.UsedSeconds = 0
		state.UpdatedAt = updatedAt
	}
	state.Pools[poolKey] = poolState
}

// RefillQuotaForSponsorURL retains the legacy same-day, all-pool refill API.
func RefillQuotaForSponsorURL(validDate string, sponsorURL string) (RefillResult, error) {
	return refillQuotaForSponsorURLAt(validDate, sponsorURL, time.Now(), GenerateDeviceCodeV7)
}

func refillQuotaForSponsorURLAt(validDate string, sponsorURL string, now time.Time, currentDevice func() DeviceCodeV7) (RefillResult, error) {
	validDate = strings.TrimSpace(validDate)
	if _, err := time.Parse("2006-01-02", validDate); err != nil {
		return RefillResult{}, err
	}
	businessDate := quotaBusinessDate(now)
	if businessDate != validDate {
		return RefillResult{}, ErrRefillDateMismatch
	}

	targetDevice, err := DeviceCodeFromSponsorURL(sponsorURL)
	if err != nil {
		return RefillResult{}, err
	}
	current := currentDevice()
	if MatchDeviceCodeV7(current, targetDevice) < deviceMatchThreshold {
		return RefillResult{}, ErrRefillDeviceMismatch
	}

	quotaMu.Lock()
	defer quotaMu.Unlock()
	unlock, err := lockQuotaStateFile()
	if err != nil {
		return RefillResult{}, err
	}
	defer unlock()

	path, err := quotaStatePath()
	if err != nil {
		return RefillResult{}, err
	}
	state, err := loadQuotaState(path)
	if err != nil {
		return RefillResult{}, err
	}
	migrateLegacyQuotaState(&state)
	if state.Pools == nil {
		state.Pools = map[string]quotaPoolState{}
	}
	updatedAt := now.Format(time.RFC3339)
	state.Version = quotaStateVersion
	state.DeviceHash = deviceHash(targetDevice)
	resetQuotaPool(&state, quotaPoolRegularDaily, businessDate, updatedAt)
	resetQuotaPool(&state, quotaPoolSpecialPeriod, businessDate, updatedAt)
	if err := saveQuotaState(path, state); err != nil {
		return RefillResult{}, err
	}
	return RefillResult{
		Path:         path,
		DeviceHash:   state.DeviceHash,
		BusinessDate: businessDate,
	}, nil
}
