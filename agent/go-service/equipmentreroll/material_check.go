package equipmentreroll

import (
	"encoding/json"
	"fmt"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// EquipmentRerollMaterialCheckRecognition 在**进入效果锁定页时顺便**读取一次材料库存
// （订制模块/自订密钥 持有数量），用 setInventory 初始化任务级 Inventory 余额。
//
// 该节点挂在 EquipmentRerollLockPageEntered 之后：旧版本靠“详情页点词条进锁定页”的前置
// 物资检测读库存，新版客户端装备详情页词条已不可点击，那条路会一直点不动而卡死，因此改为
// 顺着锁定流程顺手读一次。
//
// 后续在确认页读取实际费用，结果页出现后由 commitPendingRerollCost 扣减；
// 锁定材料选择读取该余额，选择/确认锁定本身不消耗库存。
type EquipmentRerollMaterialCheckRecognition struct{}

var _ maa.CustomRecognitionRunner = &EquipmentRerollMaterialCheckRecognition{}

func (r *EquipmentRerollMaterialCheckRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil || arg.Img == nil {
		log.Error().Str("component", "EquipmentReroll").Msg("material check recognition arg is nil")
		return nil, false
	}

	moduleHeld, okModule := readHeldCount(ctx, arg.Img, "__EquipmentRerollLockModuleHeld")
	keyHeld, okKey := readHeldCount(ctx, arg.Img, "__EquipmentRerollLockKeyHeld")
	if !okModule || !okKey {
		log.Warn().
			Str("component", "EquipmentReroll").
			Bool("module_ocred", okModule).
			Bool("key_ocred", okKey).
			Msg("material OCR incomplete; skip initializing inventory")
		// 读库存只是“顺便看一眼”，读不到不能阻断锁定流程，否则会退化成无限重试。
		// 这里仍然命中，让流程继续进入材料选择；缺库存时由锁定决策走默认材料兜底。
		return &maa.CustomRecognitionResult{Box: arg.Roi, Detail: `{"inventory_ready":false}`}, true
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

// EquipmentRerollSyncInventoryRecognition 识别效果变更确认页的订制模块持有量并同步到任务状态。
//
// 背景：库存原先只在进入效果锁定页时读取，但本轮无需锁定（或走一键载入历史锁）时
// 流程根本不经过锁定页，确认页的费用校验便拿不到库存，直接把任务结束掉。
// 确认页的「持有材料」行始终显示本轮费用涉及的订制模块持有量，因此这里补一次同步。
//
// 页面归属交给 Pipeline 表达，Go 不重复判定：本节点挂在确认页哨兵
// EquipmentRerollKeepLockGate 之后，且读数节点 __EquipmentRerollConfirmHeldModule
// 以「持有材料」文字为锚点，不在确认页时不会命中。Go 只做「读数 + 写任务级状态」。
//
// 只同步订制模块：确认页在无密钥费用时并不显示自订密钥持有量。
// 历史加载会跳过效果锁定页，所以仅在此前已读全库存时才允许复用；
// 密钥未知时走普通逐槽锁定流程，由 MaterialCheck 尝试补齐库存。
//
// 识别不到持有量时不命中，流程自然落到哨兵的后续节点；费用校验会按「库存未知」
// 放行，由游戏侧的材料校验兜底，不会退化成无限重试。
type EquipmentRerollSyncInventoryRecognition struct{}

var _ maa.CustomRecognitionRunner = &EquipmentRerollSyncInventoryRecognition{}

func (r *EquipmentRerollSyncInventoryRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil || arg.Img == nil {
		return nil, false
	}
	// roi 引用不会自动执行锚点，必须在同一截图上先定位标签。
	label, err := ctx.RunRecognition("__EquipmentRerollHeldLabel", arg.Img, nil)
	if err != nil || label == nil || !label.Hit {
		return nil, false
	}
	modules, ok := readHeldCount(ctx, arg.Img, "__EquipmentRerollConfirmHeldModule")
	if !ok {
		log.Debug().Str("component", "EquipmentReroll").Msg("confirm page held module count not recognized")
		return nil, false
	}
	setInventoryModules(arg.TaskID, modules)
	log.Info().
		Str("component", "EquipmentReroll").
		Int64("task_id", arg.TaskID).
		Int("modules", modules).
		Msg("material inventory synced from change effect page")
	return &maa.CustomRecognitionResult{
		Box:    arg.Roi,
		Detail: fmt.Sprintf(`{"inventory_synced":true,"custom_modules":%d}`, modules),
	}, true
}
