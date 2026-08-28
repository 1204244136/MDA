package customburst

import (
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// Register 注册 CustomBurst 特性的自定义识别与动作（底层框架识别 + 任务动作/流程门）。
// 注册失败时立即中断启动流程，避免部分注册成功导致运行时未定义行为。
func Register() {
	mustRegisterRecognition := func(name string, runner maa.CustomRecognitionRunner) {
		if err := maa.AgentServerRegisterCustomRecognition(name, runner); err != nil {
			log.Fatal().Err(err).Str("name", name).Msg("failed to register custom recognition")
		}
	}

	mustRegisterAction := func(name string, runner maa.CustomActionRunner) {
		if err := maa.AgentServerRegisterCustomAction(name, runner); err != nil {
			log.Fatal().Err(err).Str("name", name).Msg("failed to register custom action")
		}
	}

	mustRegisterRecognition("FastBurstPanelRecognition", &FastBurstPanelRecognition{})
	mustRegisterRecognition("CustomBurstSafetyGateRecognition", &CustomBurstSafetyGateRecognition{})
	mustRegisterRecognition("CustomBurstReturnToLowFrequencyRecognition", &CustomBurstReturnToLowFrequencyRecognition{})
	mustRegisterAction("CustomBurstRouteAction", &CustomBurstRouteAction{})
}
