package membership

import (
	"os"
	"testing"
	"time"
)

func TestPersistentMemberStatusSaveAndLoad(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "mda-member-cache-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	oldConfigDir := os.Getenv("APPDATA")
	os.Setenv("APPDATA", tempDir)
	defer os.Setenv("APPDATA", oldConfigDir)

	device := DeviceCodeV7{
		CPUHash:   "cpu-test-hash",
		UUIDHash:  "uuid-test-hash",
		BIOSHash:  "bios-test-hash",
		BoardHash: "board-test-hash",
		DiskHash:  "disk-test-hash",
		GUIDHash:  "guid-test-hash",
	}

	futureDate := time.Now().Add(30 * 24 * time.Hour).Format("20060102")
	response := &MemberStatusResponse{
		Matched:                     true,
		Score:                       100,
		IsMember:                    true,
		TierCode:                    "orange_plus",
		TierName:                    "Orange Plus",
		DailyRuntimeMinutes:         30,
		RegularDailyRuntimeMinutes:  30,
		SpecialPeriodRuntimeMinutes: 120,
		ExpiresOn:                   futureDate,
	}

	savePersistentMemberStatus(device, response)

	loaded, ok := loadPersistentMemberStatus(device)
	if !ok || loaded == nil {
		t.Fatalf("expected persistent member status to load, got ok=%v", ok)
	}

	if loaded.TierCode != "orange_plus" {
		t.Errorf("loaded.TierCode = %q, want orange_plus", loaded.TierCode)
	}
	if !loaded.VerificationUnavailable {
		t.Errorf("loaded.VerificationUnavailable = false, want true")
	}

	// 测试设备码不匹配
	differentDevice := DeviceCodeV7{
		CPUHash:  "different-cpu",
		UUIDHash: "different-uuid",
	}
	if _, ok := loadPersistentMemberStatus(differentDevice); ok {
		t.Errorf("expected mismatched device to be rejected")
	}
}

func TestPersistentMemberStatusGracePeriod(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "mda-member-grace-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	oldConfigDir := os.Getenv("APPDATA")
	os.Setenv("APPDATA", tempDir)
	defer os.Setenv("APPDATA", oldConfigDir)

	device := DeviceCodeV7{
		CPUHash:   "cpu-grace-hash",
		UUIDHash:  "uuid-grace-hash",
		BIOSHash:  "bios-grace-hash",
		BoardHash: "board-grace-hash",
		DiskHash:  "disk-grace-hash",
		GUIDHash:  "guid-grace-hash",
	}

	// 1. 到期 2 天前 (尚在宽限期 7 天内)
	expired2DaysAgo := time.Now().Add(-2 * 24 * time.Hour).Format("20060102")
	savePersistentMemberStatus(device, &MemberStatusResponse{
		Matched:   true,
		IsMember:  true,
		TierCode:  "orange_pro",
		TierName:  "Orange Pro",
		ExpiresOn: expired2DaysAgo,
	})

	loaded, ok := loadPersistentMemberStatus(device)
	if !ok || loaded == nil {
		t.Fatalf("expected status within 7-day grace period to load, got ok=%v", ok)
	}
	if loaded.TierCode != "orange_pro" {
		t.Errorf("loaded.TierCode = %q, want orange_pro", loaded.TierCode)
	}

	// 2. 到期 10 天前 (超过宽限期 7 天)
	expired10DaysAgo := time.Now().Add(-10 * 24 * time.Hour).Format("20060102")
	savePersistentMemberStatus(device, &MemberStatusResponse{
		Matched:   true,
		IsMember:  true,
		TierCode:  "orange_pro",
		TierName:  "Orange Pro",
		ExpiresOn: expired10DaysAgo,
	})

	if _, ok := loadPersistentMemberStatus(device); ok {
		t.Errorf("expected status expired beyond grace period to be rejected")
	}
}
