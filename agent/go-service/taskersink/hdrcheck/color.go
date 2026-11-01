package hdrcheck

// ColorState summarizes color modes across active display paths.
type ColorState struct {
	HDR      bool // Any queried display is classified as HDR.
	WCG      bool // Any queried display reports WCG through INFO_2.
	Displays int  // Number of successfully queried display paths.
}

// IsHDREnabled checks whether any active display is classified as HDR.
func IsHDREnabled() (bool, error) {
	state, err := QueryColorState()
	if err != nil {
		return false, err
	}
	return state.HDR, nil
}
