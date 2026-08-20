package equipmentreroll

import (
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// Register registers EquipmentReroll custom actions and recognitions.
func Register() {
	if err := maa.AgentServerRegisterCustomAction("EquipmentRerollScanBeginAction", &ScanBeginAction{}); err != nil {
		log.Error().Err(err).Msg("failed to register EquipmentRerollScanBeginAction")
	}
	if err := maa.AgentServerRegisterCustomRecognition("EquipmentRerollScanSlotRecognition", &EquipmentRerollScanSlotRecognition{}); err != nil {
		log.Error().Err(err).Msg("failed to register EquipmentRerollScanSlotRecognition")
	}
	if err := maa.AgentServerRegisterCustomAction("EquipmentRerollScanRouteAction", &EquipmentRerollScanRouteAction{}); err != nil {
		log.Error().Err(err).Msg("failed to register EquipmentRerollScanRouteAction")
	}
	if err := maa.AgentServerRegisterCustomRecognition("EquipmentRerollPartNeedRecognition", &EquipmentRerollPartNeedRecognition{}); err != nil {
		log.Error().Err(err).Msg("failed to register EquipmentRerollPartNeedRecognition")
	}
	if err := maa.AgentServerRegisterCustomRecognition("EquipmentRerollResultDecideRecognition", &EquipmentRerollResultDecideRecognition{}); err != nil {
		log.Error().Err(err).Msg("failed to register EquipmentRerollResultDecideRecognition")
	}
	if err := maa.AgentServerRegisterCustomAction("EquipmentRerollResultRouteAction", &EquipmentRerollResultRouteAction{}); err != nil {
		log.Error().Err(err).Msg("failed to register EquipmentRerollResultRouteAction")
	}
	maa.AgentServerAddTaskerSink(&taskLifecycle{})
}
