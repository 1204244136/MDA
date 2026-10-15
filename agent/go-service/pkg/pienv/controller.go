package pienv

import (
	"encoding/json"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

// 统一 PI_CONTROLLER 与 MaaFramework GetInfo 返回的控制器类型。
const (
	ControllerKindUnknown       = ""
	ControllerKindAdb           = "adb"
	ControllerKindPlayCover     = "playcover"
	ControllerKindNativeAndroid = "native_android"
	ControllerKindWin32         = "win32"
	ControllerKindMacOS         = "macos"
	ControllerKindLinux         = "linux"
	ControllerKindGamepad       = "gamepad"
)

// NormalizeControllerKind 归一化已知类型和别名，未知类型返回 ControllerKindUnknown。
func NormalizeControllerKind(controllerType string) string {
	switch strings.ToLower(strings.TrimSpace(controllerType)) {
	case ControllerKindAdb:
		return ControllerKindAdb
	case ControllerKindPlayCover, "play_cover":
		return ControllerKindPlayCover
	case ControllerKindNativeAndroid:
		return ControllerKindNativeAndroid
	case ControllerKindWin32:
		return ControllerKindWin32
	case ControllerKindMacOS:
		return ControllerKindMacOS
	case ControllerKindLinux:
		return ControllerKindLinux
	case ControllerKindGamepad:
		return ControllerKindGamepad
	default:
		return ControllerKindUnknown
	}
}

// ControllerKind 返回 PI 环境中的控制器分类；ControllerType 仍提供未经修改的原始类型。
func ControllerKind() string {
	return NormalizeControllerKind(ControllerType())
}

// ResolveControllerKind 优先使用 PI 环境，缺失或未知时从已有控制器读取类型。
// ctrl 必须是本次回调已取得的句柄，不要为判断类型再次调用 GetController，
// 也不要跨回调保存句柄。PI 不可用且 ctrl 为 nil、读取失败或类型不明时，
// 返回 ControllerKindUnknown。
func ResolveControllerKind(ctrl *maa.Controller) string {
	var getInfo func() (string, error)
	if ctrl != nil {
		getInfo = ctrl.GetInfo
	}
	return resolveControllerKind(ControllerType(), getInfo)
}

func resolveControllerKind(controllerType string, getInfo func() (string, error)) string {
	if kind := NormalizeControllerKind(controllerType); kind != ControllerKindUnknown {
		return kind
	}
	if getInfo == nil {
		return ControllerKindUnknown
	}
	info, err := getInfo()
	if err != nil {
		return ControllerKindUnknown
	}
	var detail struct {
		Type string `json:"type"`
	}
	// GetInfo 的契约是 JSON；不从错误文本、设备名或路径猜测控制器类型。
	if err := json.Unmarshal([]byte(info), &detail); err != nil {
		return ControllerKindUnknown
	}
	return NormalizeControllerKind(detail.Type)
}
