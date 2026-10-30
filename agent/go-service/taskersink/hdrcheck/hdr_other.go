//go:build !windows

package hdrcheck

// ColorState 在非 Windows 平台恆為零值。
type ColorState struct {
	HDR      bool
	WCG      bool
	Displays int
}

// QueryColorState always reports nothing on non-Windows platforms
// Advanced color detection is only supported on Windows
func QueryColorState() (ColorState, error) {
	return ColorState{}, nil
}

// IsHDREnabled always returns false on non-Windows platforms
// HDR detection is only supported on Windows
func IsHDREnabled() (bool, error) {
	return false, nil
}
