package equipmentreroll

import (
	"encoding/json"
	"fmt"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// EquipmentRerollMaterialCheckRecognition 在装备检测的“物资检测”流程中进入效果锁定页，
// 读取一次材料库存（订制模块/自订密钥 持有数量），用 setInventory 初始化任务级 Inventory 余额。
//
// 之后所有消耗都只靠“行为记录”扣减（recordRerollModuleCost / recordLockMaterialCost 调 decrementInventory），
// 不再每次检测库存；锁定材料选择（EquipmentRerollLockSelectRecognition）直接读该余额。
type EquipmentRerollMaterialCheckRecognition struct{}

var _ maa.CustomRecognitionRunner = &EquipmentRerollMaterialCheckRecognition{}

func (r *EquipmentRerollMaterialCheckRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil || arg.Img == nil {
		log.Error().Str("component", "EquipmentReroll").Msg("material check recognition arg is nil")
		return nil, false
	}

	moduleHeld, okModule := readLockHeldCount(ctx, arg.Img, "__EquipmentRerollLockModuleHeld")
	keyHeld, okKey := readLockHeldCount(ctx, arg.Img, "__EquipmentRerollLockKeyHeld")
	if !okModule || !okKey {
		log.Warn().
			Str("component", "EquipmentReroll").
			Bool("module_ocred", okModule).
			Bool("key_ocred", okKey).
			Msg("material OCR incomplete; skip initializing inventory")
		return nil, false
	}

	setInventory(arg.TaskID, Inventory{CustomModules: moduleHeld, CustomLockKeys: keyHeld})
	log.Info().
		Str("component", "EquipmentReroll").
		Int64("task_id", arg.TaskID).
		Int("modules", moduleHeld).
		Int("keys", keyHeld).
		Msg("material inventory initialized (material check)")
	// 把库存内容放进识别 detail 供 maafw.log 诊断；用户可见库存由最终摘要通过 focus 输出。
	return &maa.CustomRecognitionResult{Box: arg.Roi, Detail: buildMaterialCheckDetail(moduleHeld, keyHeld)}, true
}

// buildMaterialCheckDetail 构造材料库存的诊断 detail JSON。
func buildMaterialCheckDetail(modules, keys int) string {
	b, _ := json.Marshal(map[string]any{
		"custom_modules":   modules,
		"custom_lock_keys": keys,
		"message":          fmt.Sprintf("订制模块 %d / 自订密钥 %d", modules, keys),
	})
	return string(b)
}
