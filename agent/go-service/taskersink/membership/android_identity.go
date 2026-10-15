package membership

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/rs/zerolog/log"
)

// 只接受正式会员映射所需的最小快照；未声明的诊断字段不会参与映射。
type androidIdentityRow struct {
	Name   string   `json:"name"`
	Status string   `json:"status"`
	Values []string `json:"values"`
}

type androidIdentitySnapshot struct {
	SchemaVersion int                  `json:"schema_version"`
	Rows          []androidIdentityRow `json:"rows"`
}

var androidHex = regexp.MustCompile(`^[0-9a-fA-F]+$`)

func androidMappingHash(field, source, value string) string {
	if value == "" {
		return ""
	}
	return hashString("mda:android:client-v1:" + field + ":" + source + "\x00" + value)
}

func normalizeAndroidID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	// 64 位整数的十六进制形式可不补前导零；归一成 16 位，拒绝零和已知占位值。
	if len(value) == 0 || len(value) > 16 || !androidHex.MatchString(value) || strings.Trim(value, "0") == "" || value == "9774d56d682e549c" {
		return ""
	}
	return strings.Repeat("0", 16-len(value)) + value
}

func androidDeviceCodeFromJSON(raw string) DeviceCodeV7 {
	device := DeviceCodeV7{}
	var report androidIdentitySnapshot
	if err := json.Unmarshal([]byte(raw), &report); err != nil || report.SchemaVersion != 1 {
		log.Warn().Msg("Android membership identity is unavailable; no placeholder identity generated")
		return device
	}
	values := map[string][]string{}
	for _, row := range report.Rows {
		if row.Status != "read" {
			continue
		}
		for _, value := range row.Values {
			if strings.Contains(value, "*") || (strings.HasPrefix(row.Name, "serial.") && !notPlaceholder(value)) {
				continue
			}
			if strings.HasPrefix(row.Name, "android_id.") {
				if normalized := normalizeAndroidID(value); normalized != "" {
					values[row.Name] = append(values[row.Name], normalized)
				}
			} else if strings.HasPrefix(row.Name, "serial.") {
				normalized := strings.TrimSpace(value)
				if normalized != "" && strings.Trim(normalized, "0") != "" && !strings.EqualFold(normalized, "9774d56d682e549c") {
					values[row.Name] = append(values[row.Name], normalized)
				}
			}
		}
	}
	one := func(name string) string {
		if len(values[name]) > 0 {
			return values[name][0]
		}
		return ""
	}
	serial := ""
	// 固定优先级合并为一个 SN 来源；重复编码的两字段合计沿用旧权重 50 分。
	for _, name := range []string{"serial.ro.boot.serialno", "serial.ro.serialno", "serial.api", "serial.policy"} {
		if serial = one(name); serial != "" {
			break
		}
	}
	privilegedID := one("android_id.privileged")
	if serial == "" || privilegedID == "" {
		log.Warn().Msg("Android membership needs valid serial and privileged Android ID; no shared fallback generated")
		return DeviceCodeV7{}
	}
	// 六项都是原接口接受的 SHA-256，不发送新协议字段。派生关系仅用于客户端识别本地额度身份。
	device.UUIDHash = androidMappingHash("uuid", "serial", serial)
	device.BIOSHash = androidMappingHash("bios", "serial-root", device.UUIDHash)
	device.CPUHash = androidMappingHash("cpu", "android-id-privileged", privilegedID)
	device.BoardHash = androidMappingHash("board", "privileged-root", device.CPUHash)
	device.DiskHash = androidMappingHash("disk", "privileged-root", device.CPUHash)
	if appID := one("android_id.app"); appID != "" {
		device.GUIDHash = androidMappingHash("guid", "android-id-app", appID)
	} else {
		device.GUIDHash = androidMappingHash("guid", "primary-fallback", device.UUIDHash+"\x00"+device.CPUHash)
	}
	return device
}

func validAndroidHash(value string) bool {
	return len(value) == 64 && androidHex.MatchString(value) && strings.Trim(value, "0") != ""
}

func isAndroidMappedDevice(device DeviceCodeV7) bool {
	for _, value := range []string{device.CPUHash, device.UUIDHash, device.BIOSHash, device.BoardHash, device.DiskHash, device.GUIDHash} {
		if !validAndroidHash(value) {
			return false
		}
	}
	return strings.EqualFold(device.BIOSHash, androidMappingHash("bios", "serial-root", strings.ToLower(device.UUIDHash))) &&
		strings.EqualFold(device.BoardHash, androidMappingHash("board", "privileged-root", strings.ToLower(device.CPUHash))) &&
		strings.EqualFold(device.DiskHash, androidMappingHash("disk", "privileged-root", strings.ToLower(device.CPUHash)))
}
