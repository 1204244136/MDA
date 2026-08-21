package equipmentreroll

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// 本文件实现四攻四优的"效果锁定"流程：
// 点击槽位 → 进入效果锁定页（标题"效果锁定"）→ 选择消耗材料（SELECT）→ 点击确认 → 二次确认通知页点击确认 → 返回详情页。
// 材料策略：有自订密钥用自订密钥，不够再用订制模块（用户确认策略）。
// 锁定位：仅对 3→2号中已出现单一目标且未锁定的槽位执行锁定，1号不锁。

var (
	lockPendingMu       sync.Mutex
	lockPendingSlot     = make(map[int64]int)
	lockPendingMaterial = make(map[int64]string)
)

func setPendingLock(taskID int64, slot int, material string) {
	lockPendingMu.Lock()
	lockPendingSlot[taskID] = slot
	lockPendingMaterial[taskID] = material
	lockPendingMu.Unlock()
}

func getPendingLock(taskID int64) (int, string, bool) {
	lockPendingMu.Lock()
	defer lockPendingMu.Unlock()
	slot, ok := lockPendingSlot[taskID]
	if !ok {
		return 0, "", false
	}
	mat := lockPendingMaterial[taskID]
	return slot, mat, true
}

func clearPendingLock(taskID int64) {
	lockPendingMu.Lock()
	delete(lockPendingSlot, taskID)
	delete(lockPendingMaterial, taskID)
	lockPendingMu.Unlock()
}

// lockCheckParam 是 EquipmentRerollLockCheckRecognition 的参数。
type lockCheckParam struct {
	Part     string `json:"part"`
	Template string `json:"template"`
}

func (p *lockCheckParam) normalize() {
	p.Part = strings.TrimSpace(p.Part)
	p.Template = strings.TrimSpace(p.Template)
	if p.Template == "" {
		p.Template = "FourElementalDamage"
	}
}

// isFourAtkTemplateFromNodeJSON 从节点 JSON 中解析 EquipmentRerollLockNeed 的模板覆盖。
func isFourAtkTemplateFromNodeJSON(raw string) bool {
	if raw == "" {
		return false
	}
	var data struct {
		Recognition struct {
			Param struct {
				CustomRecognitionParam struct {
					Template string `json:"template"`
				} `json:"custom_recognition_param"`
			} `json:"param"`
		} `json:"recognition"`
	}
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return false
	}
	t := strings.TrimSpace(data.Recognition.Param.CustomRecognitionParam.Template)
	return t == "FourAttackFourElementalDamage" || t == "FourAtkFourElem"
}

// isFourAtkTemplateFromLockNeed 读取当前任务中 EquipmentRerollLockNeed 节点的生效覆盖，
// 用于在 Keep 分支的 EquipmentRerollKeepLockCheck 未单独覆盖模板时，推断当前是否为四攻四优任务。
// 这是对旧任务资源/未加载完整覆盖的兼容回退；四优任务下该节点覆盖为 FourElementalDamage，因此不会误锁。
func isFourAtkTemplateFromLockNeed(ctx *maa.Context) bool {
	if ctx == nil {
		return false
	}
	raw, err := ctx.GetNodeJSON("EquipmentRerollLockNeed")
	if err != nil {
		return false
	}
	return isFourAtkTemplateFromNodeJSON(raw)
}

// EquipmentRerollLockCheckRecognition 判断当前详情页的指定部位是否需要先锁定再洗。
// 若命中，表示该部位存在可锁定槽（3/2号单一目标未锁），路由到锁定流程；否则直进效果变更。
type EquipmentRerollLockCheckRecognition struct{}

var _ maa.CustomRecognitionRunner = &EquipmentRerollLockCheckRecognition{}

func (r *EquipmentRerollLockCheckRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if arg == nil {
		return nil, false
	}
	var params lockCheckParam
	if err := json.Unmarshal([]byte(arg.CustomRecognitionParam), &params); err != nil {
		return nil, false
	}
	params.normalize()
	if params.Part == "" {
		if p, ok := currentEffectPart(arg.TaskID); ok {
			params.Part = p
		}
	}
	isFourAtk := params.Template == "FourAttackFourElementalDamage" || params.Template == "FourAtkFourElem"
	if !isFourAtk {
		// 兼容：Keep 分支节点未被任务资源覆盖时，从 LockNeed 的有效覆盖推断当前模板。
		isFourAtk = isFourAtkTemplateFromLockNeed(ctx)
	}
	if !isFourAtk {
		return nil, false
	}
	scan, ok := GetPartScan(arg.TaskID, params.Part)
	if !ok {
		return nil, false
	}
	slot, need := DesiredLockSlotForFourAtkFourElem(scan)
	if !need {
		return nil, false
	}
	setPendingLock(arg.TaskID, slot, "")
	log.Info().Str("component", "EquipmentReroll").Str("part", params.Part).Int("lock_slot", slot).Msg("part needs lock before reroll")
	return &maa.CustomRecognitionResult{Box: arg.Roi, Detail: "{}"}, true
}

// EquipmentRerollLockRouteSlotAction 根据待锁槽位路由到 Slot2/3 点击节点。
type EquipmentRerollLockRouteSlotAction struct{}

var _ maa.CustomActionRunner = &EquipmentRerollLockRouteSlotAction{}

func (a *EquipmentRerollLockRouteSlotAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		return false
	}
	slot, _, ok := getPendingLock(arg.TaskID)
	if !ok {
		if p, ok2 := currentEffectPart(arg.TaskID); ok2 {
			if scan, ok3 := GetPartScan(arg.TaskID, p); ok3 {
				if s, need := DesiredLockSlotForFourAtkFourElem(scan); need {
					slot = s
				}
			}
		}
	}
	if slot == 0 {
		slot = 3
	}
	target := "EquipmentRerollLockClickSlot3"
	if slot == 2 {
		target = "EquipmentRerollLockClickSlot2"
	}
	if err := ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: target}}); err != nil {
		log.Error().Err(err).Str("component", "EquipmentReroll").Msg("failed to route lock slot")
		return false
	}
	log.Info().Str("component", "EquipmentReroll").Int("slot", slot).Str("target", target).Msg("lock slot routed")
	return true
}

// EquipmentRerollLockSelectRecognition 在效果锁定页选择材料。
// 策略：有自订密钥用密钥，否则用订制模块。返回 Detail 携带 material_code，
// 由 LockSelectRouteAction 路由到 Pipeline 的 SELECT 点击节点（坐标只维护在 Pipeline）。
type EquipmentRerollLockSelectRecognition struct{}

var _ maa.CustomRecognitionRunner = &EquipmentRerollLockSelectRecognition{}

type lockSelectParam struct {
	Part     string `json:"part"`
	Slot     int    `json:"slot"`
	Material string `json:"material"` // 可选强制材料，未传则按策略自动选
}

func (r *EquipmentRerollLockSelectRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil || arg.Img == nil {
		return nil, false
	}
	var params lockSelectParam
	_ = json.Unmarshal([]byte(arg.CustomRecognitionParam), &params)

	part := params.Part
	if part == "" {
		if p, ok := currentEffectPart(arg.TaskID); ok {
			part = p
		}
	}
	slot, _, _ := getPendingLock(arg.TaskID)
	if slot == 0 {
		if p, ok := GetPartScan(arg.TaskID, part); ok {
			if s, need := DesiredLockSlotForFourAtkFourElem(p); need {
				slot = s
			}
		}
		if slot == 0 {
			slot = 3
		}
	}

	titleDetail, err := ctx.RunRecognition("__EquipmentRerollLockTitle", arg.Img, nil)
	if err != nil || titleDetail == nil || !titleDetail.Hit {
		log.Debug().Str("component", "EquipmentReroll").Msg("lock page title not found")
		return nil, false
	}

	material := params.Material
	if material == "" {
		// 默认策略：自订密钥优先
		// 若后续能通过 OCR 读取"持有"数量，可动态回退；当前先按优先策略
		material = "自订密钥"
	}
	setPendingLock(arg.TaskID, slot, material)

	materialCode := 2
	if material == "订制模块" {
		materialCode = 1
	}

	log.Info().Str("component", "EquipmentReroll").Str("material", material).Int("slot", slot).Msg("lock material selected")
	return &maa.CustomRecognitionResult{Box: arg.Roi, Detail: fmt.Sprintf(`{"material_code":%d}`, materialCode)}, true
}

// EquipmentRerollLockSelectRouteAction 根据 LockSelectRecognition 的 Box 路由到模块/密钥 SELECT 点击。
type EquipmentRerollLockSelectRouteAction struct{}

var _ maa.CustomActionRunner = &EquipmentRerollLockSelectRouteAction{}

func (a *EquipmentRerollLockSelectRouteAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		return false
	}
	materialCode := 2
	detail := customRecognitionDetail(arg)
	if detail != "" {
		var d struct {
			MaterialCode int `json:"material_code"`
		}
		if err := json.Unmarshal([]byte(detail), &d); err == nil && d.MaterialCode == 1 {
			materialCode = 1
		}
	}
	// 兼容兜底：自定义 Detail 缺失时从 pending 读取材料
	if detail == "" {
		_, mat, ok := getPendingLock(arg.TaskID)
		if ok && mat == "订制模块" {
			materialCode = 1
		}
	}
	target := "EquipmentRerollLockSelectKey"
	if materialCode == 1 {
		target = "EquipmentRerollLockSelectModule"
	}
	if err := ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: target}}); err != nil {
		return false
	}
	log.Info().Str("component", "EquipmentReroll").Str("target", target).Msg("lock select routed")
	return true
}

// EquipmentRerollLockConfirmAction 在锁定页点击"确认"后，等待二次确认弹窗并乐观更新快照。
type EquipmentRerollLockConfirmAction struct{}

var _ maa.CustomActionRunner = &EquipmentRerollLockConfirmAction{}

func (a *EquipmentRerollLockConfirmAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		return false
	}
	part, ok := currentEffectPart(arg.TaskID)
	if !ok {
		return true
	}
	slot, material, ok := getPendingLock(arg.TaskID)
	if !ok {
		if scan, ok2 := GetPartScan(arg.TaskID, part); ok2 {
			if s, need := DesiredLockSlotForFourAtkFourElem(scan); need {
				slot = s
				material = "自订密钥"
			}
		}
	}
	if slot != 0 {
		if material == "" {
			material = "自订密钥"
		}
		applyLockToSnapshot(arg.TaskID, part, slot, material)
		log.Info().Str("component", "EquipmentReroll").Str("part", part).Int("slot", slot).Str("material", material).Msg("lock applied to snapshot (optimistic)")
	}
	return true
}

// EquipmentRerollLockDoneAction 上锁完成：乐观更新快照并清理 pending。
type EquipmentRerollLockDoneAction struct{}

var _ maa.CustomActionRunner = &EquipmentRerollLockDoneAction{}

func (a *EquipmentRerollLockDoneAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		return false
	}
	part, ok := currentEffectPart(arg.TaskID)
	if ok {
		slot, material, has := getPendingLock(arg.TaskID)
		if has && slot != 0 {
			if material == "" {
				material = "自订密钥"
			}
			applyLockToSnapshot(arg.TaskID, part, slot, material)
			log.Info().Str("component", "EquipmentReroll").Str("part", part).Int("slot", slot).Str("material", material).Msg("lock applied to snapshot on done")
		}
		clearPendingLock(arg.TaskID)
	}
	return true
}

// EquipmentRerollKeepLockRouteSlotAction 在效果变更确认页完成锁定判定后，路由到确认页槽位点击节点。
// 复用 EquipmentRerollLockCheckRecognition 写入的 pending 槽位：2/3 号路由到确认页点击节点；
// 无需锁定（slot 缺失或 0）时直接路由到确认页刷新（EquipmentRerollConfirmChangeEffect）。
type EquipmentRerollKeepLockRouteSlotAction struct{}

var _ maa.CustomActionRunner = &EquipmentRerollKeepLockRouteSlotAction{}

func (a *EquipmentRerollKeepLockRouteSlotAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		return false
	}
	slot, _, ok := getPendingLock(arg.TaskID)
	if !ok {
		if p, ok2 := currentEffectPart(arg.TaskID); ok2 {
			if scan, ok3 := GetPartScan(arg.TaskID, p); ok3 {
				if s, need := DesiredLockSlotForFourAtkFourElem(scan); need {
					slot = s
				}
			}
		}
	}
	target := "EquipmentRerollConfirmChangeEffect" // 无需锁定：确认页直接刷新
	if slot == 2 {
		target = "EquipmentRerollKeepClickSlot2"
	} else if slot == 3 {
		target = "EquipmentRerollKeepClickSlot3"
	}
	if err := ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: target}}); err != nil {
		log.Error().Err(err).Str("component", "EquipmentReroll").Msg("failed to route keep lock slot")
		return false
	}
	log.Info().Str("component", "EquipmentReroll").Int("slot", slot).Str("target", target).Msg("keep lock slot routed")
	return true
}
