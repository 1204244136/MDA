package membership

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/rs/zerolog/log"
)

const (
	cachedStatusFileName  = "membership-status-cache.json"
	membershipGracePeriod = 7 * 24 * time.Hour
)

type persistentCachedStatus struct {
	DeviceCode DeviceCodeV7         `json:"device_code"`
	Response   MemberStatusResponse `json:"response"`
	CachedAt   time.Time            `json:"cached_at"`
}

func persistentCachedStatusPath() (string, error) {
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
	return filepath.Join(path, cachedStatusFileName), nil
}

func savePersistentMemberStatus(deviceCode DeviceCodeV7, response *MemberStatusResponse) {
	if response == nil || !response.Matched || !response.IsMember {
		return
	}

	path, err := persistentCachedStatusPath()
	if err != nil {
		log.Warn().Err(err).Msg("failed to resolve persistent cached status path")
		return
	}

	record := persistentCachedStatus{
		DeviceCode: deviceCode,
		Response:   *response,
		CachedAt:   time.Now(),
	}

	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		log.Warn().Err(err).Msg("failed to marshal persistent cached status")
		return
	}

	temp, err := os.CreateTemp(filepath.Dir(path), ".membership-cache-*.tmp")
	if err != nil {
		log.Warn().Err(err).Msg("failed to create temp file for persistent cached status")
		return
	}
	tempPath := temp.Name()
	defer func() {
		temp.Close()
		_ = os.Remove(tempPath)
	}()

	if _, err := temp.Write(data); err != nil {
		return
	}
	if err := temp.Sync(); err != nil {
		return
	}
	if err := temp.Close(); err != nil {
		return
	}

	if err := replaceQuotaStateFile(tempPath, path); err != nil {
		log.Warn().Err(err).Msg("failed to replace persistent cached status file")
	}
}

func loadPersistentMemberStatus(currentDeviceCode DeviceCodeV7) (*MembershipStatus, bool) {
	path, err := persistentCachedStatusPath()
	if err != nil {
		return nil, false
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}

	var record persistentCachedStatus
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, false
	}

	// 1. 验证设备码匹配度
	if MatchDeviceCodeV7(currentDeviceCode, record.DeviceCode) < 80 {
		return nil, false
	}

	// 2. 验证有效期
	expiresOn := record.Response.ExpiresOn
	if expiresOn == "" {
		return nil, false
	}

	var expiryTime time.Time
	if len(expiresOn) == 8 {
		t, err := time.ParseInLocation("20060102", expiresOn, beijingLocation)
		if err == nil {
			expiryTime = t.Add(24*time.Hour - time.Second)
		}
	} else {
		t, err := time.ParseInLocation("2006-01-02", expiresOn, beijingLocation)
		if err == nil {
			expiryTime = t.Add(24*time.Hour - time.Second)
		}
	}

	if expiryTime.IsZero() {
		return nil, false
	}

	now := time.Now()
	// 如果在有效期内，或者在到期后的宽限期内
	if now.After(expiryTime.Add(membershipGracePeriod)) {
		return nil, false
	}

	status := statusFromResponse(&record.Response, currentDeviceCode)
	status.VerificationUnavailable = true
	return status, true
}
