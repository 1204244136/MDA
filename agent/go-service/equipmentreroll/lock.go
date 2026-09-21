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
// 点击槽位 → 效果锁定页 → 选择材料 → 蓝色确认 → 返回原页面；实际变更时才扣费。
// 材料策略：有自订密钥用自订密钥，不够再用订制模块（用户确认策略）。
// 锁定位：由 plan_dp.go 的策略迭代决定；策略上只考虑 2/3 号槽（1 号不锁——1 号 100% 易得、锁 1 追 23 代价高）。
//
// 文档索引：docs/zh_cn/nikke/EquipmentReroll/洗词条策略与Agent逻辑.md

var (
	lockPendingMu       sync.Mutex
	lockPendingSlot     = make(map[int64]int)
	lockPendingMaterial = make(map[int64]string)
)

// optimisticLockInventory 是“尚未读到真实库存”时用于锁定规划的乐观估计。
//
// 库存现在由 EquipmentRerollMaterialCheckRecognition 在**进入效果锁定页时**顺便读取，
// 因此装备详情页的锁定规划阶段通常还没有库存。这里按材料充足乐观估计，让锁定决策不被
// “还没读库存”阻断；真实持有量在锁定页读到后由 LockSelectRecognition 修正材料选择，
// 若实际不足则由锁定页的重试闸门放弃锁定、退回效果变更。
// 取值只需覆盖最大单次锁定成本（自订密钥 30 / 订制模块 3）。
var optimisticLockInventory = Inventory{CustomModules: 999, CustomLockKeys: 999}

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
	if cfg.isValue() {
		// 数值模式只在确认页冻结及执行完整锁方案，不在详情页提前加锁。
		return nil, false
	}
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
		// 详情页阶段通常还没读到库存（库存改为进入效果锁定页时顺便读），
		// 按乐观估计继续规划，避免“读不到库存 = 永不锁定”。
		inv = optimisticLockInventory
	}
	scan, ok := parts[params.Part]
	if !ok {
		return nil, false
	}
	lockIndex := countLocks(scan)
	slot, material, outcome := desiredLockPlanForInventory(parts, params.Part, quota, inv, params.Material)
	if outcome != lockPlanLock {
		logLockPlanSkipped(params.Part, arg.TaskID, lockIndex, outcome)
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
		// 同 lockCheck：详情页阶段按乐观估计规划，真实库存进锁定页后修正。
		inv = optimisticLockInventory
	}
	lockIndex := countLocks(scan)
	material, ok := selectLockMaterial(inv, params.Material, lockIndex)
	if !ok {
		log.Warn().Str("component", "EquipmentReroll").Int64("task_id", arg.TaskID).Int("lock_index", lockIndex).Msg("single equipment no affordable lock material")
		return nil, false
	}
	slot, need := singleDesiredLockSlot(scan, t, material)
	if !need {
		log.Info().Str("component", "EquipmentReroll").Int64("task_id", arg.TaskID).Str("part", part).Msg("no lock needed for this part under current target")
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

// lockPlanOutcome 说明一次锁定规划的结论。
// 「策略判定不需要锁」与「材料不足」都会导致本轮不锁定，但排查方向完全相反，
// 因此必须分开返回，不能合并成一个 bool 后统一报成材料不足。
type lockPlanOutcome int

const (
	// lockPlanLock 需要且可以锁定，slot/material 有效。
	lockPlanLock lockPlanOutcome = iota
	// lockPlanNotNeeded 当前配额下该部位没有值得锁的槽位。
	lockPlanNotNeeded
	// lockPlanUnaffordable 有想锁的槽位，但两种锁定材料都付不起。
	lockPlanUnaffordable
	// lockPlanUnknown 部位快照缺失，无法规划。
	lockPlanUnknown
)

// logLockPlanSkipped 按规划结论分别记录日志，避免把「策略不需要锁」误报成材料不足。
func logLockPlanSkipped(part string, taskID int64, lockIndex int, outcome lockPlanOutcome) {
	logger := log.With().
		Str("component", "EquipmentReroll").
		Int64("task_id", taskID).
		Str("part", part).
		Int("lock_index", lockIndex).
		Logger()
	switch outcome {
	case lockPlanNotNeeded:
		logger.Info().Msg("no lock needed for this part under current quota")
	case lockPlanUnaffordable:
		logger.Warn().Msg("no affordable lock material")
	default:
		logger.Warn().Msg("lock plan unavailable; skipping lock")
	}
}

func desiredLockPlanForInventory(parts map[string]partScan, part string, quota map[string]int, inv Inventory, requested string) (int, string, lockPlanOutcome) {
	scan, ok := parts[part]
	if !ok {
		return 0, "", lockPlanUnknown
	}
	material, ok := selectLockMaterial(inv, requested, countLocks(scan))
	if !ok {
		return 0, "", lockPlanUnaffordable
	}
	slot, need := DesiredLockSlotForQuota(parts, part, quota, material)
	if !need {
		return 0, material, lockPlanNotNeeded
	}
	return slot, material, lockPlanLock
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
	if cfg.isValue() {
		slot, _, ok := valueLockSelection(taskID)
		return slot, ok
	}
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

// lockRouteTarget 返回锁定入口节点。
//
// 新版客户端装备详情页的词条行已不可点击，旧路径（EquipmentRerollLockClickSlot2/3
// 点击详情页词条进入锁定页）会一直点不动而卡死，因此锁定统一改走
// “点效果变更 → 确认页”，再由确认页的 KeepLock 分支完成锁定。
// slot 参数保留用于日志与后续可能的按槽分流。
func lockRouteTarget(slot int) string {
	_ = slot
	return "EquipmentRerollClickChangeEffect"
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
	if slot >= minSlot && slot <= maxSlot {
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
	// 统一进入确认页；历史锁准入由确认页加载节点在点击前校验。
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

	if loadCarrierConfig(ctx).isValue() {
		selectedSlot, selectedMaterial, ok := valueLockSelection(arg.TaskID)
		if !ok {
			log.Error().Msg("value lock plan is no longer affordable; stop before consuming materials")
			return &maa.CustomRecognitionResult{Box: arg.Roi, Detail: `{"material_code":0}`}, true
		}
		if slot != selectedSlot {
			return nil, false
		}
		code := 2
		if selectedMaterial == "订制模块" {
			code = 1
		}
		setPendingLock(arg.TaskID, slot, selectedMaterial)
		return &maa.CustomRecognitionResult{Box: arg.Roi, Detail: fmt.Sprintf(`{"material_code":%d}`, code)}, true
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
				Msg("cannot afford any lock material (behavior inventory); give up locking")
			// 材料不足不能返回 false：本节点带自循环兜底，返回 false 会变成无限重试。
			// 改为命中并让 LockSelectRouteAction 把流程带去“放弃锁定 → 效果变更”。
			return &maa.CustomRecognitionResult{Box: arg.Roi, Detail: `{"material_code":0}`}, true
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

// readHeldCount 读取页面「持有 N」的数量：OCR 后取最后一个整数（如 “持有 760” → 760）。
// 同时服务于效果锁定页（两种材料持有量）与效果变更确认页（订制模块持有量）。
// 返回 (数量, 是否识别到)；识别失败/未命中时数量为 0。
func readHeldCount(ctx *maa.Context, img image.Image, nodeName string) (int, bool) {
	detail, err := ctx.RunRecognition(nodeName, img, nil)
	if err != nil || detail == nil || !detail.Hit {
		log.Debug().Str("component", "EquipmentReroll").Str("node", nodeName).Msg("lock held count not recognized")
		return 0, false
	}
	text := matchedHeldText(detail)
	if text == "" {
		return 0, false
	}
	text = strings.NewReplacer(",", "", "，", "").Replace(text)
	re := regexp.MustCompile(`\d+`)
	matches := re.FindAllString(text, -1)
	if len(matches) == 0 {
		return 0, false
	}
	n, err := strconv.Atoi(matches[len(matches)-1])
	return n, err == nil
}

// matchedHeldText 只使用 expected/replace 后的最佳结果，不能让 all 中未命中的文本覆盖库存。
func matchedHeldText(detail *maa.RecognitionDetail) string {
	if detail == nil || !detail.Hit || detail.Results == nil || detail.Results.Best == nil {
		return ""
	}
	ocr, ok := detail.Results.Best.AsOCR()
	if !ok {
		return ""
	}
	return strings.Join(strings.Fields(ocr.Text), " ")
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
		if err := json.Unmarshal([]byte(detail), &d); err == nil {
			// material_code=0 表示两种锁定材料都付不起：放弃锁定，退回效果变更，
			// 而不是继续在锁定页点 SELECT（那会变成无限重试）。
			if d.MaterialCode == 0 {
				if err := ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "EquipmentRerollLockAbort"}}); err != nil {
					log.Error().Err(err).Str("component", "EquipmentReroll").Msg("failed to route lock abort")
					return false
				}
				log.Info().Str("component", "EquipmentReroll").Msg("no affordable lock material; abort locking and go change effect")
				return true
			}
			if d.MaterialCode == 1 {
				materialCode = 1
			}
		}
	} else {
		// 兼容兜底：自定义 Detail 缺失时从 pending 读取材料
		if _, mat, ok := getPendingLock(arg.TaskID); ok && mat == "订制模块" {
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
	resetTaskRetryGates(arg.TaskID)
	part, ok := currentEffectPart(arg.TaskID)
	if ok {
		slot, material, has := getPendingLock(arg.TaskID)
		if has && slot != 0 {
			if material == "" {
				material = "自订密钥"
			}
			// 这里只设置锁状态，不扣库存；确认页读取本轮两种材料总费用。
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
	case 1:
		return "EquipmentRerollKeepClickSlot1"
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
	if slot >= minSlot && slot <= maxSlot {
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
