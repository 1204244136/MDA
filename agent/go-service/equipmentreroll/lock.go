package equipmentreroll

import (
	"encoding/json"
	"fmt"
	"image"
	"regexp"
	"strconv"
	"strings"
	"sync"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// 本文件实现自定义配额下的"效果锁定"流程：
// 点击槽位 → 进入效果锁定页（标题"效果锁定"）→ 选择消耗材料（SELECT）→ 点击确认 → 二次确认通知页点击确认 → 返回详情页。
// 材料策略：有自订密钥用自订密钥，不够再用订制模块（用户确认策略）。
// 锁定位：由 plan_dp.go 的策略迭代决定；策略上只考虑 2/3 号槽（1 号不锁——1 号 100% 易得、锁 1 追 23 代价高）。
//
// 文档索引：docs/zh_cn/nikke/EquipmentReroll/洗词条策略与Agent逻辑.md

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
	Part        string         `json:"part"`
	GlobalQuota map[string]int `json:"global_quota"`
	// Material 可选：本次锁定使用的材料（"自订密钥"默认 / "订制模块"）。
	// 当自订密钥耗尽、回退用订制模组时，应传入 "订制模块"，使锁定决策计入模块获取成本。
	Material string `json:"material"`
}

func (p *lockCheckParam) normalize() {
	p.Part = strings.TrimSpace(p.Part)
	p.Material = strings.TrimSpace(p.Material)
	if len(p.GlobalQuota) > 0 {
		p.GlobalQuota = normalizeQuota(p.GlobalQuota)
	}
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
	// 全部任务选项统一从承载点读取；一次 Run 只读一次，往下传给分支使用。
	cfg := loadCarrierConfig(ctx)
	if cfg.isSingle() {
		return r.lockCheckSingle(arg, params, cfg)
	}
	// 角色模式：按全局配额决定锁定（承载点 attach.quota_* 优先，回退本节点默认）。
	parts, ok := GetEquipmentSlotScans(arg.TaskID)
	if !ok {
		return nil, false
	}
	quota := cfg.resolveQuota(params.GlobalQuota)
	if !quotaIsValid(quota) {
		return nil, false
	}
	inv, inventoryReady := getInventory(arg.TaskID)
	if !inventoryReady {
		log.Warn().Str("component", "EquipmentReroll").Int64("task_id", arg.TaskID).Msg("lock planning requires initialized material inventory")
		return nil, false
	}
	scan, ok := parts[params.Part]
	if !ok {
		return nil, false
	}
	lockIndex := countLocks(scan)
	slot, material, affordable := desiredLockPlanForInventory(parts, params.Part, quota, inv, params.Material)
	if !affordable {
		log.Warn().Str("component", "EquipmentReroll").Int64("task_id", arg.TaskID).Int("lock_index", lockIndex).Msg("no affordable lock material")
		return nil, false
	}
	// 按实际可用材料规划；模块锁定会计入获取成本，密钥锁定不会。
	if slot == 0 {
		return nil, false
	}
	// 防御性检查：DesiredLockSlotForQuota 只会返回未锁的 2/3 号槽，走到这里说明策略层与
	// 快照不一致。用 Debug 记录——正常运行不该出现，也不值得当成告警干扰用户日志。
	if scan.Slots[slot-1].Lock != LockNone {
		log.Debug().Str("component", "EquipmentReroll").Str("part", params.Part).Int("lock_slot", slot).Msg("target lock slot already locked; skip lock")
		return nil, false
	}
	setPendingLock(arg.TaskID, slot, material)
	log.Info().Str("component", "EquipmentReroll").Str("part", params.Part).Str("material", material).Int("lock_slot", slot).Msg("part needs lock before reroll (custom quota)")
	return &maa.CustomRecognitionResult{Box: arg.Roi, Detail: "{}"}, true
}

// lockCheckSingle 单件模式锁定检查：按目标（effect -> 槽位限定）判断是否需要锁定某槽。
// 命中表示存在可锁定的 2/3 号槽（该槽已持有落在允许槽位的需求词条），路由到锁定流程。
func (r *EquipmentRerollLockCheckRecognition) lockCheckSingle(arg *maa.CustomRecognitionArg, params lockCheckParam, cfg carrierConfig) (*maa.CustomRecognitionResult, bool) {
	if !cfg.singleTargetOK() {
		log.Error().
			Str("component", "EquipmentReroll").
			Int64("task_id", arg.TaskID).
			Str("part", params.Part).
			Int("want_count", len(cfg.Target.Want)).
			Str("problem", cfg.TargetProblem).
			Msg("single equipment lock check requires a valid 1 to 3 affix target")
		return nil, false
	}
	t := cfg.Target
	part := params.Part
	if part == "" {
		part = cfg.Part
	}
	scan, ok := GetPartScan(arg.TaskID, part)
	if !ok {
		return nil, false
	}
	inv, inventoryReady := getInventory(arg.TaskID)
	if !inventoryReady {
		log.Warn().Str("component", "EquipmentReroll").Int64("task_id", arg.TaskID).Msg("single equipment lock planning requires initialized material inventory")
		return nil, false
	}
	lockIndex := countLocks(scan)
	material, ok := selectLockMaterial(inv, params.Material, lockIndex)
	if !ok {
		log.Warn().Str("component", "EquipmentReroll").Int64("task_id", arg.TaskID).Int("lock_index", lockIndex).Msg("single equipment no affordable lock material")
		return nil, false
	}
	slot, need := singleDesiredLockSlot(scan, t, material)
	if !need {
		return nil, false
	}
	// 防御性检查：singleDesiredLockSlot 只会返回未锁的 2/3 号槽，走到这里说明策略层与
	// 快照不一致。用 Debug 记录——正常运行不该出现，也不值得当成告警干扰用户日志。
	if scan.Slots[slot-1].Lock != LockNone {
		log.Debug().Str("component", "EquipmentReroll").Str("part", part).Int("lock_slot", slot).Msg("single equipment target lock slot already locked; skip lock")
		return nil, false
	}
	setPendingLock(arg.TaskID, slot, material)
	log.Info().Str("component", "EquipmentReroll").Str("part", part).Str("material", material).Int("lock_slot", slot).Msg("part needs lock before reroll (single equipment)")
	return &maa.CustomRecognitionResult{Box: arg.Roi, Detail: "{}"}, true
}

func desiredLockPlanForInventory(parts map[string]partScan, part string, quota map[string]int, inv Inventory, requested string) (int, string, bool) {
	scan, ok := parts[part]
	if !ok {
		return 0, "", false
	}
	material, ok := selectLockMaterial(inv, requested, countLocks(scan))
	if !ok {
		return 0, "", false
	}
	slot, need := DesiredLockSlotForQuota(parts, part, quota, material)
	if !need {
		return 0, material, false
	}
	return slot, material, true
}

// desiredLockSlotForCurrentMode 计算当前模式下的待锁槽位，用于 pending 丢失时回退。
// 读取一次承载点后委托给 desiredLockSlotForConfig。
func desiredLockSlotForCurrentMode(ctx *maa.Context, taskID int64, part string) (int, bool) {
	if ctx == nil {
		return 0, false
	}
	return desiredLockSlotForConfig(loadCarrierConfig(ctx), taskID, part)
}

// desiredLockSlotForConfig 按已读取的承载点配置计算待锁槽位：
//   - 单件模式：按 singleDesiredLockSlot（需求 2+ 才考虑锁槽，含槽位限定）；
//   - 角色模式：按 DesiredLockSlotForQuota（分配感知 + 先苦后甜）。
//
// 若已缓存库存（效果锁定页 OCR）且自订密钥不足以支付锁定，则按“订制模块”口径
// （计入获取成本）计算，使模块锁定时决策更保守。
func desiredLockSlotForConfig(cfg carrierConfig, taskID int64, part string) (int, bool) {
	scan, ok := GetPartScan(taskID, part)
	if !ok {
		return 0, false
	}
	material := lockMaterialForTask(taskID, part)
	if cfg.isSingle() {
		if !cfg.singleTargetOK() {
			return 0, false
		}
		return singleDesiredLockSlot(scan, cfg.Target, material)
	}
	if quotaTotal(cfg.Quota) == 0 {
		return 0, false
	}
	parts, ok := GetEquipmentSlotScans(taskID)
	if !ok {
		return 0, false
	}
	return DesiredLockSlotForQuota(parts, part, cfg.Quota, material)
}

// lockMaterialForTask 依据缓存库存决定本次锁定假设使用的材料：
// 自订密钥足以支付时用密钥（获取成本 0），否则用订制模块（计入获取成本）。未读到库存时默认密钥。
func lockMaterialForTask(taskID int64, part string) string {
	inv, ok := getInventory(taskID)
	if !ok {
		return "自订密钥"
	}
	lockIndex := 0
	if scan, sok := GetPartScan(taskID, part); sok {
		lockIndex = countLocks(scan)
	}
	if key, kok := inv.ChooseLockMaterial(lockIndex); kok {
		return key
	}
	return "订制模块"
}

// EquipmentRerollLockRouteSlotAction 根据待锁槽位路由到 Slot2/3 点击节点。
type EquipmentRerollLockRouteSlotAction struct{}

var _ maa.CustomActionRunner = &EquipmentRerollLockRouteSlotAction{}

func lockRouteTarget(slot int) string {
	switch slot {
	case 2:
		return "EquipmentRerollLockClickSlot2"
	case 3:
		return "EquipmentRerollLockClickSlot3"
	default:
		return "EquipmentRerollClickChangeEffect"
	}
}

func (a *EquipmentRerollLockRouteSlotAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		return false
	}
	slot, _, ok := getPendingLock(arg.TaskID)
	if !ok {
		if p, ok2 := currentEffectPart(arg.TaskID); ok2 {
			if s, need := desiredLockSlotForCurrentMode(ctx, arg.TaskID, p); need {
				slot = s
			}
		}
	}
	// 护栏：目标槽已锁，不能再“上锁”，转去效果变更。
	if slot == 2 || slot == 3 {
		if p, ok2 := currentEffectPart(arg.TaskID); ok2 {
			if scan, kok := GetPartScan(arg.TaskID, p); kok && scan.Slots[slot-1].Lock != LockNone {
				log.Warn().Str("component", "EquipmentReroll").Int("slot", slot).Msg("target slot already locked; go change effect instead of re-lock")
				if err := ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "EquipmentRerollClickChangeEffect"}}); err != nil {
					return false
				}
				return true
			}
		}
	}
	target := lockRouteTarget(slot)
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
	slot, pendingMaterial, _ := getPendingLock(arg.TaskID)

	titleDetail, err := ctx.RunRecognition("__EquipmentRerollLockTitle", arg.Img, nil)
	if err != nil || titleDetail == nil || !titleDetail.Hit {
		log.Debug().Str("component", "EquipmentReroll").Msg("lock page title not found")
		return nil, false
	}

	// 用前置“获取材料库存”初始化、并由行为扣减的库存余额决策材料（不再每次 OCR）。
	material := params.Material
	if material == "" {
		material = pendingMaterial
	}
	inv, inventoryReady := getInventory(arg.TaskID)
	if inventoryReady {
		// 只取当前部位的快照：本节点两种模式共用，而单件模式只扫了选定那一件，
		// 用要求四件齐全的 GetEquipmentSlotScans 会让单件模式在这里直接识别失败。
		scan, scanOK := GetPartScan(arg.TaskID, part)
		if !scanOK {
			return nil, false
		}
		lockIndex := countLocks(scan)
		selected, ok2 := selectLockMaterial(inv, material, lockIndex)
		if !ok2 {
			log.Warn().
				Str("component", "EquipmentReroll").
				Int64("task_id", arg.TaskID).
				Str("part", part).
				Int("modules_held", inv.CustomModules).
				Int("keys_held", inv.CustomLockKeys).
				Int("lock_index", lockIndex).
				Msg("cannot afford any lock material (behavior inventory)")
			return nil, false
		}
		materialChanged := selected != material
		material = selected
		if slot == 0 || materialChanged {
			// 单件/角色模式统一回退：从 EquipmentRerollLockNeed 读取目标配置重算待锁槽。
			if selectedSlot, need := desiredLockSlotForCurrentMode(ctx, arg.TaskID, part); need {
				slot = selectedSlot
			} else {
				return nil, false
			}
		}
	} else if material == "" {
		// 独立调试锁定流程没有库存快照时，保留旧的密钥默认行为。
		material = "自订密钥"
	}
	if slot == 0 {
		if s, need := desiredLockSlotForCurrentMode(ctx, arg.TaskID, part); need {
			slot = s
		} else {
			slot = 3
		}
	}
	setPendingLock(arg.TaskID, slot, material)

	materialCode := 2
	if material == "订制模块" {
		materialCode = 1
	}

	log.Info().
		Str("component", "EquipmentReroll").
		Str("material", material).
		Int("slot", slot).
		Msg("lock material selected (behavior inventory)")
	return &maa.CustomRecognitionResult{Box: arg.Roi, Detail: fmt.Sprintf(`{"material_code":%d}`, materialCode)}, true
}

// lastNonEmptyOCRText 返回识别结果中最后一条非空 OCR 文本（取最后一个匹配，稳健去空白）。
func lastNonEmptyOCRText(detail *maa.RecognitionDetail) string {
	var last string
	for _, result := range allResults(detail) {
		if result == nil {
			continue
		}
		ocr, ok := result.AsOCR()
		if !ok {
			continue
		}
		text := strings.Join(strings.Fields(ocr.Text), " ")
		if text != "" {
			last = text
		}
	}
	return last
}

// readLockHeldCount 读取效果锁定页“持有 N”数量：OCR 后取最后一个整数（如 “持有 760” → 760）。
// 返回 (数量, 是否识别到)；识别失败/未命中时数量为 0。
func readLockHeldCount(ctx *maa.Context, img image.Image, nodeName string) (int, bool) {
	detail, err := ctx.RunRecognition(nodeName, img, nil)
	if err != nil || detail == nil || !detail.Hit {
		log.Debug().Str("component", "EquipmentReroll").Str("node", nodeName).Msg("lock held count not recognized")
		return 0, false
	}
	text := lastNonEmptyOCRText(detail)
	if text == "" {
		return 0, false
	}
	re := regexp.MustCompile(`\d+`)
	matches := re.FindAllString(text, -1)
	if len(matches) == 0 {
		return 0, false
	}
	n, _ := strconv.Atoi(matches[len(matches)-1])
	return n, true
}

// EquipmentRerollLockSelectRouteAction 根据 LockSelectRecognition 的 Box 路由到模块/密钥 SELECT 点击。
type EquipmentRerollLockSelectRouteAction struct{}

var _ maa.CustomActionRunner = &EquipmentRerollLockSelectRouteAction{}

func lockSelectRouteTarget(materialCode int) string {
	if materialCode == 1 {
		return "EquipmentRerollLockSelectModule"
	}
	return "EquipmentRerollLockSelectKey"
}

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
	target := lockSelectRouteTarget(materialCode)
	if err := ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: target}}); err != nil {
		return false
	}
	log.Info().Str("component", "EquipmentReroll").Str("target", target).Msg("lock select routed")
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
			if scan, ok2 := GetPartScan(arg.TaskID, part); ok2 {
				lockIndex := countLocks(scan)
				recordLockMaterialCost(arg.TaskID, material, lockIndex)
				log.Info().Str("component", "EquipmentReroll").Str("part", part).Int("slot", slot).Str("material", material).Int("lock_index", lockIndex).Msg("recorded lock material cost")
			}
			applyLockToSnapshot(arg.TaskID, part, slot, material)
			log.Info().Str("component", "EquipmentReroll").Str("part", part).Int("slot", slot).Str("material", material).Msg("lock applied to snapshot on done")
		}
		clearPendingLock(arg.TaskID)

		// 支持至多 2 锁：若策略仍建议锁第二把，则仅写入 pending 待锁槽；
		// 由 LockDone 之后的 LockAfterRoute 按当前页面（确认页/详情页）分流到对应锁定入口，
		// 避免“在结果页/确认页却点了详情页坐标的锁图标”导致反复点已锁/点不到。
		if s, need := desiredLockSlotForCurrentMode(ctx, arg.TaskID, part); need {
			setPendingLock(arg.TaskID, s, "")
			log.Info().Str("component", "EquipmentReroll").Str("part", part).Int("next_lock_slot", s).Msg("set second lock pending; LockAfterRoute routes by page")
		}
	}
	return true
}

// EquipmentRerollKeepLockRouteSlotAction 在效果变更确认页完成锁定判定后，路由到确认页槽位点击节点。
// 复用 EquipmentRerollLockCheckRecognition 写入的 pending 槽位：2/3 号路由到确认页点击节点；
// 无需锁定（slot 缺失或 0）时直接路由到材料记录节点，随后进入确认页刷新。
type EquipmentRerollKeepLockRouteSlotAction struct{}

var _ maa.CustomActionRunner = &EquipmentRerollKeepLockRouteSlotAction{}

func keepLockRouteTarget(slot int) string {
	switch slot {
	case 2:
		return "EquipmentRerollKeepClickSlot2"
	case 3:
		return "EquipmentRerollKeepClickSlot3"
	default:
		return "EquipmentRerollPrepareRerollCost"
	}
}

func (a *EquipmentRerollKeepLockRouteSlotAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		return false
	}
	slot, _, ok := getPendingLock(arg.TaskID)
	if !ok {
		if p, ok2 := currentEffectPart(arg.TaskID); ok2 {
			if s, need := desiredLockSlotForCurrentMode(ctx, arg.TaskID, p); need {
				slot = s
			}
		}
	}
	// 护栏：目标槽已锁，则无需再锁，转去准备记录消耗（确认页刷新）。
	if slot == 2 || slot == 3 {
		if p, ok2 := currentEffectPart(arg.TaskID); ok2 {
			if scan, kok := GetPartScan(arg.TaskID, p); kok && scan.Slots[slot-1].Lock != LockNone {
				log.Warn().Str("component", "EquipmentReroll").Int("slot", slot).Msg("keep-lock target already locked; skip re-lock")
				slot = 0
			}
		}
	}
	target := keepLockRouteTarget(slot)
	if err := ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: target}}); err != nil {
		log.Error().Err(err).Str("component", "EquipmentReroll").Msg("failed to route keep lock slot")
		return false
	}
	log.Info().Str("component", "EquipmentReroll").Int("slot", slot).Str("target", target).Msg("keep lock slot routed")
	return true
}
