package collectible

import (
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// Register 注册收藏品预热的自定义组件。
// 注册失败直接中断启动：动作名在 Pipeline 里是硬引用，缺一个就会在跑到那一步时才炸，
// 那种失败现场很难定位（和 customburst 的处理保持一致）。
func Register() {
	mustRegisterAction := func(name string, runner maa.CustomActionRunner) {
		if err := maa.AgentServerRegisterCustomAction(name, runner); err != nil {
			log.Fatal().Err(err).Str("name", name).Msg("failed to register custom action")
		}
	}

	mustRegisterAction("CharacterPrepCollectibleDecide", &DecideAction{})
	mustRegisterAction("CharacterPrepCollectibleSetGrade", &SetGradeAction{})
	mustRegisterAction("CharacterPrepCollectibleSetRepeat", &SetRepeatAction{})
	mustRegisterAction("CharacterPrepCollectibleTierCheck", &TierCheckAction{})
}
