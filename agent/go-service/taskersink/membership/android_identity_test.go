package membership

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func androidFixture(t *testing.T, extra ...androidIdentityRow) DeviceCodeV7 {
	t.Helper()
	report := androidIdentitySnapshot{SchemaVersion: 1, Rows: append([]androidIdentityRow{
		{Name: "serial.ro.boot.serialno", Status: "read", Values: []string{"TEST-SN-ALPHA"}},
		{Name: "android_id.privileged", Status: "read", Values: []string{"1234567890abcdef"}},
	}, extra...)}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	return androidDeviceCodeFromJSON(string(data))
}

func TestAndroidMappingUsesOriginalSixFieldsAndWeights(t *testing.T) {
	device := androidFixture(t,
		androidIdentityRow{Name: "android_id.app", Status: "read", Values: []string{"0000000000000000"}},
		androidIdentityRow{Name: "serial.api", Status: "read", Values: []string{"TEST-SN-ALPHA"}},
	)
	if !isAndroidMappedDevice(device) {
		t.Fatal("mapping is not self-consistent")
	}
	data, _ := json.Marshal(device)
	var fields map[string]string
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 6 {
		t.Fatal("wire format must contain only original six fields")
	}
	for _, value := range fields {
		if !validAndroidHash(value) {
			t.Fatal("legacy server requires six valid hashes")
		}
	}
	if MatchDeviceCodeV7(device, device) != 100 {
		t.Fatal("original full match score changed")
	}
	changedSN := device
	changedSN.UUIDHash = hashString("different serial")
	changedSN.BIOSHash = hashString("different serial child")
	if MatchDeviceCodeV7(changedSN, device) != 50 {
		t.Fatal("serial source must carry original UUID+BIOS weights: 50")
	}
	changedID := device
	changedID.CPUHash, changedID.BoardHash, changedID.DiskHash = hashString("cpu"), hashString("board"), hashString("disk")
	if MatchDeviceCodeV7(changedID, device) != 60 {
		t.Fatal("privileged ID must carry original CPU+board+disk weights: 40")
	}
	desktop := deviceCodeFromIdentifiers(deviceIdentifierValues{CPU: []string{"1234567890abcdef"}, UUID: []string{"TEST-SN-ALPHA"}})
	if MatchDeviceCodeV7(device, desktop) != 0 {
		t.Fatal("Android domain must differ from plain Windows hashes")
	}
}

func TestAndroidAppIdentityChangeKeepsMembershipAndQuota(t *testing.T) {
	base := androidFixture(t)
	full := androidFixture(t, androidIdentityRow{Name: "android_id.app", Status: "read", Values: []string{"abcdef1234567890"}})
	changed := androidFixture(t, androidIdentityRow{Name: "android_id.app", Status: "read", Values: []string{"1234567890abcdef"}})
	if MatchDeviceCodeV7(full, base) != 90 || MatchDeviceCodeV7(full, changed) != 90 {
		t.Fatal("App ID/fallback changes must only affect 10 points")
	}
	if deviceHash(base) != deviceHash(full) || deviceHash(base) != deviceHash(changed) {
		t.Fatal("App ID change reset local quota/coupons")
	}
	upper := base
	upper.CPUHash, upper.UUIDHash, upper.BIOSHash = strings.ToUpper(base.CPUHash), strings.ToUpper(base.UUIDHash), strings.ToUpper(base.BIOSHash)
	upper.BoardHash, upper.DiskHash, upper.GUIDHash = strings.ToUpper(base.BoardHash), strings.ToUpper(base.DiskHash), strings.ToUpper(base.GUIDHash)
	if deviceHash(base) != deviceHash(upper) {
		t.Fatal("hash casing reset quota identity")
	}
	url := SponsorURL(&MembershipStatus{DeviceCode: full})
	parsed, err := DeviceCodeFromSponsorURL(url)
	if err != nil || parsed != full {
		t.Fatalf("legacy sponsor/refill URL round trip failed: %v", err)
	}
	if strings.Contains(url, "device_schema") {
		t.Fatal("no new server parameter is permitted")
	}
	if deviceHash(parsed) != deviceHash(full) {
		t.Fatal("URL parsing lost local quota identity")
	}
}

func TestAndroidMappingIgnoresPhoneAndNetworkIdentifiers(t *testing.T) {
	base := androidFixture(t)
	extra := androidFixture(t,
		androidIdentityRow{Name: "imei.slot0", Status: "read", Values: []string{"123456789012345"}},
		androidIdentityRow{Name: "meid.slot0", Status: "read", Values: []string{"1234567890ABCD"}},
		androidIdentityRow{Name: "mac.factory", Status: "read", Values: []string{"AA:BB:CC:DD:EE:FF"}},
	)
	if base != extra {
		t.Fatal("unused optional phone/network identifiers affected mapping")
	}
	if normalizeAndroidID("0000000000000000") != "" || normalizeAndroidID("abc") != "0000000000000abc" {
		t.Fatal("Android numeric ID normalization failed")
	}
	for _, raw := range []string{`{}`, `{"schema_version":1,"rows":[{"name":"serial.api","status":"read","values":["TEST-SN"]}]}`, `{"schema_version":1,"rows":[{"name":"android_id.privileged","status":"read","values":["1234567890abcdef"]}]}`} {
		if got := androidDeviceCodeFromJSON(raw); got != (DeviceCodeV7{}) {
			t.Fatal("missing core source created a shared fallback identity")
		}
	}
}

func TestAndroidMappingLegacyContractVectors(t *testing.T) {
	data, err := os.ReadFile("../../../../tools/verify/fixtures/android-membership-vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors []struct {
		Name     string          `json:"name"`
		Snapshot json.RawMessage `json:"snapshot"`
		Expected DeviceCodeV7    `json:"expected"`
	}
	if err := json.Unmarshal(data, &vectors); err != nil {
		t.Fatal(err)
	}
	for _, vector := range vectors {
		t.Run(vector.Name, func(t *testing.T) {
			if got := androidDeviceCodeFromJSON(string(vector.Snapshot)); got != vector.Expected {
				t.Fatalf("mapping does not match legacy contract vector: %s", vector.Name)
			}
		})
	}
}
