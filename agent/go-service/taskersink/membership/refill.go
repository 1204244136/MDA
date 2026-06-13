package membership

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const deviceMatchThreshold = 80

var (
	ErrRefillDateMismatch   = errors.New("quota refill package is not valid today")
	ErrRefillDeviceMismatch = errors.New("quota refill package is not valid for this device")
)

type RefillResult struct {
	Path         string
	DeviceHash   string
	BusinessDate string
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

	path, err := quotaStatePath()
	if err != nil {
		return RefillResult{}, err
	}
	state := loadQuotaState(path)
	migrateLegacyQuotaState(&state)
	if state.Pools == nil {
		state.Pools = map[string]quotaPoolState{}
	}

	deviceHash := deviceHash(targetDevice)
	updatedAt := now.Format(time.RFC3339)
	state.Version = 2
	state.DeviceHash = deviceHash
	for poolKey, poolState := range state.Pools {
		poolState.UsedSeconds = 0
		poolState.CarriedDebtSeconds = 0
		poolState.UpdatedAt = updatedAt
		if poolKey == string(quotaPoolRegularDaily) {
			poolState.PeriodKey = businessDate
		}
		state.Pools[poolKey] = poolState
	}
	state.BusinessDate = businessDate
	state.UsedSeconds = 0
	state.CarriedDebtSeconds = 0
	state.UpdatedAt = updatedAt

	if err := saveQuotaState(path, state); err != nil {
		return RefillResult{}, err
	}
	return RefillResult{
		Path:         path,
		DeviceHash:   deviceHash,
		BusinessDate: businessDate,
	}, nil
}
