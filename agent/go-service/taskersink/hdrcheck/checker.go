package hdrcheck

import (
	"github.com/1204244136/MDA/agent/go-service/pkg/i18n"
	"github.com/1204244136/MDA/agent/go-service/pkg/maafocus"
	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// HDRChecker checks if HDR is enabled on any display before task execution
type HDRChecker struct {
	// warned tracks whether we've already warned in this session
	// to avoid spamming the user with repeated warnings
	warned bool
}

// OnTaskerTask handles tasker task events
func (c *HDRChecker) OnTaskerTask(tasker *maa.Tasker, event maa.EventStatus, detail maa.TaskerTaskDetail) {
	// Only check on task starting
	if event != maa.EventStatusStarting {
		return
	}

	// Skip if we've already warned
	if c.warned {
		return
	}

	log.Debug().
		Uint64("task_id", detail.TaskID).
		Str("entry", detail.Entry).
		Msg("Checking HDR status before task execution")

	state, err := QueryColorState()
	if err != nil {
		log.Warn().Err(err).Msg("Failed to check HDR status")
		return
	}

	switch {
	case state.HDR:
		log.Warn().Int("displays", state.Displays).Msg("HDR is enabled! This may cause issues with image recognition.")

		// Print warning message (HTML formatted for MXU display)
		maafocus.PrintLargeContentTrimNewline(
			i18n.RenderHTML("tasker.hdr_warning", nil),
		)

		// Mark as warned to avoid repeated warnings
		c.warned = true
	case state.WCG:
		// WCG/ACM is advanced color, but should not trigger the HDR warning.
		log.Debug().Int("displays", state.Displays).
			Msg("HDR check passed: advanced color (WCG/ACM) is on, HDR is off")
	default:
		log.Debug().Msg("HDR check passed: HDR is not enabled")
	}
}
