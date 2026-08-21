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
	if err := maa.AgentServerRegisterCustomAction("EquipmentRerollAfterAcceptRouteAction", &EquipmentRerollAfterAcceptRouteAction{}); err != nil {
		log.Error().Err(err).Msg("failed to register EquipmentRerollAfterAcceptRouteAction")
	}
	if err := maa.AgentServerRegisterCustomRecognition("EquipmentRerollLockCheckRecognition", &EquipmentRerollLockCheckRecognition{}); err != nil {
		log.Error().Err(err).Msg("failed to register EquipmentRerollLockCheckRecognition")
	}
	if err := maa.AgentServerRegisterCustomAction("EquipmentRerollLockRouteSlotAction", &EquipmentRerollLockRouteSlotAction{}); err != nil {
		log.Error().Err(err).Msg("failed to register EquipmentRerollLockRouteSlotAction")
	}
	if err := maa.AgentServerRegisterCustomAction("EquipmentRerollKeepLockRouteSlotAction", &EquipmentRerollKeepLockRouteSlotAction{}); err != nil {
		log.Error().Err(err).Msg("failed to register EquipmentRerollKeepLockRouteSlotAction")
	}
	if err := maa.AgentServerRegisterCustomRecognition("EquipmentRerollLockSelectRecognition", &EquipmentRerollLockSelectRecognition{}); err != nil {
		log.Error().Err(err).Msg("failed to register EquipmentRerollLockSelectRecognition")
	}
	if err := maa.AgentServerRegisterCustomAction("EquipmentRerollLockSelectRouteAction", &EquipmentRerollLockSelectRouteAction{}); err != nil {
		log.Error().Err(err).Msg("failed to register EquipmentRerollLockSelectRouteAction")
	}
	if err := maa.AgentServerRegisterCustomAction("EquipmentRerollLockConfirmAction", &EquipmentRerollLockConfirmAction{}); err != nil {
		log.Error().Err(err).Msg("failed to register EquipmentRerollLockConfirmAction")
	}
	if err := maa.AgentServerRegisterCustomAction("EquipmentRerollLockDoneAction", &EquipmentRerollLockDoneAction{}); err != nil {
		log.Error().Err(err).Msg("failed to register EquipmentRerollLockDoneAction")
	}
	maa.AgentServerAddTaskerSink(&taskLifecycle{})
}
