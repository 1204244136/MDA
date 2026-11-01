//go:build !windows

package hdrcheck

// QueryColorState always reports nothing on non-Windows platforms
// Advanced color detection is only supported on Windows
func QueryColorState() (ColorState, error) {
	return ColorState{}, nil
}
