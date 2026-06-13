package centerpriority

import (
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// Register registers center-priority custom recognitions.
func Register() {
	if err := maa.AgentServerRegisterCustomRecognition("CenterPriorityRecognition", &CenterPriorityRecognition{}); err != nil {
		log.Error().Err(err).Msg("failed to register CenterPriorityRecognition")
	}
}
