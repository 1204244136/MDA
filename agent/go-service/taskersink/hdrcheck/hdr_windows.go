//go:build windows

package hdrcheck

import (
	"unsafe"

	"github.com/rs/zerolog/log"
	"golang.org/x/sys/windows"
)

var (
	user32                          = windows.NewLazySystemDLL("user32.dll")
	procGetDisplayConfigBufferSizes = user32.NewProc("GetDisplayConfigBufferSizes")
	procQueryDisplayConfig          = user32.NewProc("QueryDisplayConfig")
	procDisplayConfigGetDeviceInfo  = user32.NewProc("DisplayConfigGetDeviceInfo")
)

// Display config flags
const (
	QDC_ONLY_ACTIVE_PATHS = 0x00000002
)

// DISPLAYCONFIG_DEVICE_INFO_TYPE
//
// 注意編號：GET_ADVANCED_COLOR_INFO_2 是 15，不是 11（11 是 GET_SDR_WHITE_LEVEL，
// 用錯型別只會拿到 ERROR_INVALID_PARAMETER=87）。
const (
	DISPLAYCONFIG_DEVICE_INFO_GET_ADVANCED_COLOR_INFO  = 9
	DISPLAYCONFIG_DEVICE_INFO_GET_ADVANCED_COLOR_INFO2 = 15
)

// DISPLAYCONFIG_ADVANCED_COLOR_MODE
const (
	advancedColorModeSDR uint32 = 0
	advancedColorModeWCG uint32 = 1
	advancedColorModeHDR uint32 = 2
)

// LUID represents a locally unique identifier
type LUID struct {
	LowPart  uint32
	HighPart int32
}

// DISPLAYCONFIG_PATH_SOURCE_INFO contains source information for a path
type DISPLAYCONFIG_PATH_SOURCE_INFO struct {
	AdapterId   LUID
	Id          uint32
	ModeInfoIdx uint32
	StatusFlags uint32
}

// DISPLAYCONFIG_PATH_TARGET_INFO contains target information for a path
type DISPLAYCONFIG_PATH_TARGET_INFO struct {
	AdapterId        LUID
	Id               uint32
	ModeInfoIdx      uint32
	OutputTechnology uint32
	Rotation         uint32
	Scaling          uint32
	RefreshRate      DISPLAYCONFIG_RATIONAL
	ScanLineOrdering uint32
	TargetAvailable  int32
	StatusFlags      uint32
}

// DISPLAYCONFIG_RATIONAL represents a rational number
type DISPLAYCONFIG_RATIONAL struct {
	Numerator   uint32
	Denominator uint32
}

// DISPLAYCONFIG_PATH_INFO contains information about a display path
type DISPLAYCONFIG_PATH_INFO struct {
	SourceInfo DISPLAYCONFIG_PATH_SOURCE_INFO
	TargetInfo DISPLAYCONFIG_PATH_TARGET_INFO
	Flags      uint32
}

// DISPLAYCONFIG_MODE_INFO contains mode information
type DISPLAYCONFIG_MODE_INFO struct {
	InfoType     uint32
	Id           uint32
	AdapterId    LUID
	ModeInfoData [64]byte // Union, we don't need to parse this
}

// DISPLAYCONFIG_DEVICE_INFO_HEADER is the header for device info requests
type DISPLAYCONFIG_DEVICE_INFO_HEADER struct {
	Type      uint32
	Size      uint32
	AdapterId LUID
	Id        uint32
}

// DISPLAYCONFIG_GET_ADVANCED_COLOR_INFO contains advanced color information
type DISPLAYCONFIG_GET_ADVANCED_COLOR_INFO struct {
	Header              DISPLAYCONFIG_DEVICE_INFO_HEADER
	Value               uint32 // Bit flags: advancedColorSupported, advancedColorEnabled, wideColorEnforced, advancedColorForceDisabled
	ColorEncoding       uint32
	BitsPerColorChannel uint32
}

// DISPLAYCONFIG_GET_ADVANCED_COLOR_INFO_2 reports the active HDR/WCG mode separately.
type DISPLAYCONFIG_GET_ADVANCED_COLOR_INFO_2 struct {
	Header              DISPLAYCONFIG_DEVICE_INFO_HEADER
	Value               uint32
	ColorEncoding       uint32
	BitsPerColorChannel uint32
	ActiveColorMode     uint32
}

// Windows error codes
const (
	ERROR_INSUFFICIENT_BUFFER = 122
)

// Maximum retry attempts for display config query
const maxRetries = 3

// QueryColorState queries color modes on active display paths.
//
// INFO_2 separates HDR from WCG. If it fails, the legacy advanced-color bit
// preserves compatibility but may still report WCG as HDR.
func QueryColorState() (ColorState, error) {
	var state ColorState

	// Check if required APIs are available (may not exist on older Windows)
	if err := procGetDisplayConfigBufferSizes.Find(); err != nil {
		return state, err
	}
	if err := procQueryDisplayConfig.Find(); err != nil {
		return state, err
	}
	if err := procDisplayConfigGetDeviceInfo.Find(); err != nil {
		return state, err
	}

	// Query display config with retry logic for topology changes
	pathArray, err := queryDisplayConfigWithRetry()
	if err != nil {
		return state, err
	}

	if len(pathArray) == 0 {
		return state, nil
	}

	// Check each active path
	for i := range pathArray {
		header := DISPLAYCONFIG_DEVICE_INFO_HEADER{
			AdapterId: pathArray[i].TargetInfo.AdapterId,
			Id:        pathArray[i].TargetInfo.Id,
		}

		colorInfo2 := DISPLAYCONFIG_GET_ADVANCED_COLOR_INFO_2{Header: header}
		colorInfo2.Header.Type = DISPLAYCONFIG_DEVICE_INFO_GET_ADVANCED_COLOR_INFO2
		colorInfo2.Header.Size = uint32(unsafe.Sizeof(DISPLAYCONFIG_GET_ADVANCED_COLOR_INFO_2{}))

		ret, _, _ := procDisplayConfigGetDeviceInfo.Call(uintptr(unsafe.Pointer(&colorInfo2)))
		if ret == 0 {
			state.Displays++
			switch colorInfo2.ActiveColorMode {
			case advancedColorModeHDR:
				state.HDR = true
			case advancedColorModeWCG:
				state.WCG = true
			}
			continue
		}

		log.Debug().
			Err(windows.Errno(ret)).
			Uint32("display_id", header.Id).
			Msg("Advanced color INFO_2 query failed; using legacy HDR detection")

		colorInfo := DISPLAYCONFIG_GET_ADVANCED_COLOR_INFO{Header: header}
		colorInfo.Header.Type = DISPLAYCONFIG_DEVICE_INFO_GET_ADVANCED_COLOR_INFO
		colorInfo.Header.Size = uint32(unsafe.Sizeof(DISPLAYCONFIG_GET_ADVANCED_COLOR_INFO{}))

		if ret, _, _ := procDisplayConfigGetDeviceInfo.Call(uintptr(unsafe.Pointer(&colorInfo))); ret != 0 {
			log.Debug().
				Err(windows.Errno(ret)).
				Uint32("display_id", header.Id).
				Msg("Failed to query legacy advanced color status")
			continue
		}
		state.Displays++

		// Preserve the legacy bit-1 check; it cannot reliably distinguish WCG from HDR.
		if (colorInfo.Value & 0x2) != 0 {
			state.HDR = true
		}
	}

	// If we couldn't query any display successfully, return error
	if state.Displays == 0 {
		return state, windows.ERROR_GEN_FAILURE
	}

	return state, nil
}

// queryDisplayConfigWithRetry queries display config with retry on ERROR_INSUFFICIENT_BUFFER
func queryDisplayConfigWithRetry() ([]DISPLAYCONFIG_PATH_INFO, error) {
	for attempt := 0; attempt < maxRetries; attempt++ {
		var numPathArrayElements, numModeInfoArrayElements uint32

		// Get buffer sizes
		ret, _, _ := procGetDisplayConfigBufferSizes.Call(
			uintptr(QDC_ONLY_ACTIVE_PATHS),
			uintptr(unsafe.Pointer(&numPathArrayElements)),
			uintptr(unsafe.Pointer(&numModeInfoArrayElements)),
		)
		if ret != 0 {
			return nil, windows.Errno(ret)
		}

		if numPathArrayElements == 0 {
			return nil, nil
		}

		// Allocate arrays
		pathArray := make([]DISPLAYCONFIG_PATH_INFO, numPathArrayElements)

		// Prepare pointers for QueryDisplayConfig
		var modeInfoPtr unsafe.Pointer
		if numModeInfoArrayElements > 0 {
			modeInfoArray := make([]DISPLAYCONFIG_MODE_INFO, numModeInfoArrayElements)
			modeInfoPtr = unsafe.Pointer(&modeInfoArray[0])
		}

		// Query display config
		ret, _, _ = procQueryDisplayConfig.Call(
			uintptr(QDC_ONLY_ACTIVE_PATHS),
			uintptr(unsafe.Pointer(&numPathArrayElements)),
			uintptr(unsafe.Pointer(&pathArray[0])),
			uintptr(unsafe.Pointer(&numModeInfoArrayElements)),
			uintptr(modeInfoPtr),
			0,
		)
		if ret == 0 {
			// Trim pathArray to actual count (may be less than allocated)
			return pathArray[:numPathArrayElements], nil
		}
		if ret != ERROR_INSUFFICIENT_BUFFER {
			return nil, windows.Errno(ret)
		}
		// Topology changed, retry with new buffer sizes
	}

	return nil, windows.ERROR_INSUFFICIENT_BUFFER
}
