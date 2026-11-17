package membership

import (
	"context"
	"encoding/xml"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

type macOSDeviceIdentifiers struct {
	UUID   string
	Serial string
}

// ioreg 的 plist 字典交替存放 key 和值；只读取同一顶层设备的两个字符串属性。
type macOSRegistryValue struct {
	XMLName xml.Name
	Text    string `xml:",chardata"`
}

type macOSRegistryArchive struct {
	XMLName xml.Name `xml:"plist"`
	Entries []struct {
		Values []macOSRegistryValue `xml:",any"`
	} `xml:"array>dict"`
}

var (
	macOSUUIDPattern   = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	macOSSerialPattern = regexp.MustCompile(`^[A-Z0-9]+$`)
)

func generateMacOSDeviceCodeV7() DeviceCodeV7 {
	started := time.Now()
	values, err := queryMacOSDeviceIdentifiers()
	if err != nil {
		log.Warn().Err(err).Dur("duration", time.Since(started)).Msg("macOS membership identity is unavailable; no fallback identity generated")
		return DeviceCodeV7{}
	}
	log.Debug().Dur("duration", time.Since(started)).Msg("Queried macOS device identifiers")
	return macOSDeviceCodeFromIdentifiers(values)
}

func queryMacOSDeviceIdentifiers() (macOSDeviceIdentifiers, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// 固定路径与 XML 输出避免 PATH 劫持、本地化文本及对子设备属性的误读。
	out, err := exec.CommandContext(ctx, "/usr/sbin/ioreg", "-a", "-r", "-d", "1", "-c", "IOPlatformExpertDevice").Output()
	if err != nil {
		return macOSDeviceIdentifiers{}, fmt.Errorf("query platform registry: %w", err)
	}
	return parseMacOSDeviceIdentifiers(out)
}

func parseMacOSDeviceIdentifiers(data []byte) (macOSDeviceIdentifiers, error) {
	var archive macOSRegistryArchive
	if err := xml.Unmarshal(data, &archive); err != nil {
		return macOSDeviceIdentifiers{}, fmt.Errorf("parse platform registry plist: %w", err)
	}
	if len(archive.Entries) != 1 {
		return macOSDeviceIdentifiers{}, fmt.Errorf("expected one platform registry entry, got %d", len(archive.Entries))
	}
	properties := archive.Entries[0].Values
	if len(properties)%2 != 0 {
		return macOSDeviceIdentifiers{}, fmt.Errorf("platform registry dictionary has an unpaired property")
	}
	var values macOSDeviceIdentifiers
	for i := 0; i < len(properties); i += 2 {
		key, value := properties[i], properties[i+1]
		if key.XMLName.Local != "key" {
			return macOSDeviceIdentifiers{}, fmt.Errorf("platform registry dictionary has an invalid key")
		}
		if key.Text != "IOPlatformUUID" && key.Text != "IOPlatformSerialNumber" {
			continue
		}
		if value.XMLName.Local != "string" {
			return macOSDeviceIdentifiers{}, fmt.Errorf("platform registry property %s is not a string", key.Text)
		}
		if key.Text == "IOPlatformUUID" {
			values.UUID = value.Text
		} else {
			values.Serial = value.Text
		}
	}
	return values, nil
}

func normalizeMacOSUUID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if !macOSUUIDPattern.MatchString(value) {
		return ""
	}
	compact := strings.ReplaceAll(value, "-", "")
	if strings.Trim(compact, "0") == "" || strings.Trim(compact, "f") == "" {
		return ""
	}
	return value
}

func normalizeMacOSSerial(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	if !macOSSerialPattern.MatchString(value) || !notPlaceholder(value) {
		return ""
	}
	switch value {
	case "NONE", "NULL", "NA", "NOTAVAILABLE", "NOTAPPLICABLE", "UNSPECIFIED", "DEFAULT", "SYSTEMSERIALNUMBER", "SERIALNUMBER":
		return ""
	}
	if strings.Trim(value, value[:1]) == "" {
		return ""
	}
	return value
}

func macOSMappingHash(tag, value string) string {
	if value == "" {
		return ""
	}
	// 该域与派生规则是会员绑定协议，升级时不可静默修改。
	return hashString("mda:macos:membership:v1\x00" + tag + "\x00" + value)
}

func macOSDeviceCodeFromIdentifiers(values macOSDeviceIdentifiers) DeviceCodeV7 {
	uuid := normalizeMacOSUUID(values.UUID)
	serial := normalizeMacOSSerial(values.Serial)
	if uuid == "" || serial == "" {
		log.Warn().Msg("macOS membership needs a valid platform UUID and serial; no fallback identity generated")
		return DeviceCodeV7{}
	}
	var device DeviceCodeV7
	// SN 占旧协议的 45+5 分，UUID 占 15+10+15+10 分；两根必须同时匹配才能达到 80 分。
	device.UUIDHash = macOSMappingHash("uuid:serial", serial)
	device.BIOSHash = macOSMappingHash("bios:serial-root", device.UUIDHash)
	device.CPUHash = macOSMappingHash("cpu:platform-uuid", uuid)
	device.BoardHash = macOSMappingHash("board:platform-root", device.CPUHash)
	device.DiskHash = macOSMappingHash("disk:platform-root", device.CPUHash)
	device.GUIDHash = macOSMappingHash("guid:platform-root", device.CPUHash)
	return device
}
