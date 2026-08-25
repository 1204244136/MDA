package equipmentreroll

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"unicode"

	"github.com/1204244136/MDA/agent/go-service/pkg/i18n"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

const (
	minSlot = 1
	maxSlot = 3
)

// SlotLock 表示单个槽位的锁定状态。
type SlotLock int

const (
	// LockNone 无锁。
	LockNone SlotLock = iota
	// LockPermanent 永久锁（蓝色锁，订制模块锁定，持续到手动解除）。
	LockPermanent
	// LockOneTime 一次性锁（橙色锁，自订密钥锁定，仅保护下一次效果变更）。
	LockOneTime
)

func (l SlotLock) String() string {
	switch l {
	case LockPermanent:
		return "永久锁"
	case LockOneTime:
		return "一次性锁"
	default:
		return "无锁"
	}
}

func lockDisplayLabel(lock SlotLock) string {
	switch lock {
	case LockPermanent:
		return i18n.T("tasker.equipment_reroll.lock_permanent")
	case LockOneTime:
		return i18n.T("tasker.equipment_reroll.lock_one_time")
	default:
		return ""
	}
}

var equipmentParts = []string{"头部", "臂部", "身躯", "腿部"}

var officialEffects = []string{
	"优越代码伤害增加",
	"命中率增加",
	"最大装弹数增加",
	"攻击力增加",
	"蓄力伤害增加",
	"蓄力速度增加",
	"暴击率增加",
	"暴击伤害增加",
	"防御力增加",
}

type recordEffectParam struct {
	Slot   int    `json:"slot"`
	Part   string `json:"part"`
	IsLast bool   `json:"is_last"`
	// Value 数值区域 OCR 原文（可选，兼容旧的仅词条记录调用）。
	Value string `json:"value"`
	// Lock 锁定状态（可选，兼容旧的仅词条记录调用）。
	Lock SlotLock `json:"lock"`
}

// slotScanData 单个槽位扫描到的三要素：词条 / 数值 / 锁定状态。
type slotScanData struct {
	Effect string
	Value  string
	Lock   SlotLock
}

// partScan 一件装备 3 个槽位的完整扫描快照。
type partScan struct {
	Slots [maxSlot]slotScanData
}

// Effects 提取该部位三槽的词条名（供既有决策逻辑使用）。
func (p partScan) Effects() [maxSlot]string {
	var effects [maxSlot]string
	for i := range p.Slots {
		effects[i] = p.Slots[i].Effect
	}
	return effects
}

// MaterialUsage 记录任务执行期间累计消耗的材料数量。
type MaterialUsage struct {
	// CustomModules 订制模块总消耗（效果变更 + 订制模块锁定）。
	CustomModules int
	// CustomLockKeys 自订密钥消耗（自订密钥锁定）。
	CustomLockKeys int
	// RerollModules 其中“效果变更”消耗的订制模块数。
	RerollModules int
	// LockModules 其中“订制模组锁定”消耗的订制模块数。
	LockModules int
}

// monitorState 全量扫描的 task 级状态：当前部位的进行中数组 + 四部位完整快照 + 材料消耗。
type monitorState struct {
	Part                 string
	Effects              [maxSlot]string
	Values               [maxSlot]string
	Locks                [maxSlot]SlotLock
	Parts                map[string]partScan
	NextSlot             int
	Materials            MaterialUsage
	PendingRerollCost    int       // 已准备确认、尚未写入 Materials 的订制模块数
	Inventory            Inventory // 任务级材料余额（前置“获取材料库存”初始化，之后由行为扣减）
	InventoryInitialized bool      // Inventory 是否已被前置任务初始化
}

var (
	stateMu sync.Mutex
	states  = make(map[int64]monitorState)
)

// setInventory 初始化/更新任务级材料库存（前置“获取材料库存”OCR 到的一次性初始值）。
func setInventory(taskID int64, inv Inventory) {
	stateMu.Lock()
	defer stateMu.Unlock()
	state := states[taskID]
	state.Inventory = inv
	state.InventoryInitialized = true
	states[taskID] = state
}

// getInventory 读取任务级材料余额。只有前置任务已初始化过才返回 ok=true。
func getInventory(taskID int64) (Inventory, bool) {
	stateMu.Lock()
	defer stateMu.Unlock()
	state, ok := states[taskID]
	if !ok || !state.InventoryInitialized {
		return Inventory{}, false
	}
	return state.Inventory, true
}

// decrementInventory 在记录一次消耗时相应地扣减材料余额（按行为推导，不再 OCR）。
func decrementInventory(taskID int64, material string, cost int) {
	if cost <= 0 {
		return
	}
	stateMu.Lock()
	defer stateMu.Unlock()
	state := states[taskID]
	if !state.InventoryInitialized {
		return
	}
	switch material {
	case "订制模块":
		state.Inventory.CustomModules -= cost
		if state.Inventory.CustomModules < 0 {
			state.Inventory.CustomModules = 0
		}
	default:
		state.Inventory.CustomLockKeys -= cost
		if state.Inventory.CustomLockKeys < 0 {
			state.Inventory.CustomLockKeys = 0
		}
	}
	states[taskID] = state
}

// allResults 汇总识别结果（best + filtered + all）供遍历使用。
func allResults(detail *maa.RecognitionDetail) []*maa.RecognitionResult {
	if detail == nil || detail.Results == nil {
		return nil
	}
	results := make([]*maa.RecognitionResult, 0, 1+len(detail.Results.Filtered)+len(detail.Results.All))
	if detail.Results.Best != nil {
		results = append(results, detail.Results.Best)
	}
	results = append(results, detail.Results.Filtered...)
	results = append(results, detail.Results.All...)
	return results
}

func firstRecognizedEffect(detail *maa.RecognitionDetail) (string, string, bool) {
	firstRaw := ""
	for _, result := range allResults(detail) {
		if result == nil {
			continue
		}
		ocr, ok := result.AsOCR()
		if !ok || strings.TrimSpace(ocr.Text) == "" {
			continue
		}
		if firstRaw == "" {
			firstRaw = ocr.Text
		}
		if effect, recognized := normalizeEffect(ocr.Text); recognized {
			return effect, ocr.Text, true
		}
		if isUnobtainedEffect(ocr.Text) {
			return "", ocr.Text, true
		}
	}
	return "", firstRaw, false
}

// firstRawOCRText 返回识别结果中第一条非空 OCR 文本（去空白折叠），用于读取数值区域。
func firstRawOCRText(detail *maa.RecognitionDetail) string {
	for _, result := range allResults(detail) {
		if result == nil {
			continue
		}
		ocr, ok := result.AsOCR()
		if !ok || strings.TrimSpace(ocr.Text) == "" {
			continue
		}
		return strings.Join(strings.Fields(ocr.Text), " ")
	}
	return ""
}

// extractPercentValue 从 OCR 原文中提取最后一个百分数（如 "11.81%"）作为数值。
// 结果页槽位 OCR 通常是「【效果】11.81%」这类文本，取最后一个百分数作为实际数值。
func extractPercentValue(raw string) string {
	const pattern = `\d+(?:\.\d+)?%`
	re := regexp.MustCompile(pattern)
	matches := re.FindAllString(raw, -1)
	if len(matches) == 0 {
		return ""
	}
	return matches[len(matches)-1]
}

func clearMonitorState(taskID int64) {
	stateMu.Lock()
	delete(states, taskID)
	stateMu.Unlock()
}

// recordRerollModuleCost 累加一次“效果变更”消耗的订制模块（累计日志 + 行为扣减库存余额）。
func recordRerollModuleCost(taskID int64, modules int) {
	if modules <= 0 {
		return
	}
	stateMu.Lock()
	state := states[taskID]
	state.Materials.CustomModules += modules
	state.Materials.RerollModules += modules
	state.PendingRerollCost = 0
	states[taskID] = state
	stateMu.Unlock()
	decrementInventory(taskID, "订制模块", modules)
}

// setPendingRerollCost 在效果变更确认前记录“待消耗”的订制模块数。
func setPendingRerollCost(taskID int64, modules int) {
	if modules <= 0 {
		return
	}
	stateMu.Lock()
	defer stateMu.Unlock()
	state := states[taskID]
	state.PendingRerollCost = modules
	states[taskID] = state
}

// consumePendingRerollCost 读取并清空待记录的订制模块数。
func consumePendingRerollCost(taskID int64) int {
	stateMu.Lock()
	defer stateMu.Unlock()
	state := states[taskID]
	cost := state.PendingRerollCost
	state.PendingRerollCost = 0
	states[taskID] = state
	return cost
}

// flushPendingRerollCost 在成功结束前把未消费的待记录消耗补入 Materials。
func flushPendingRerollCost(taskID int64) {
	stateMu.Lock()
	defer stateMu.Unlock()
	state := states[taskID]
	if state.PendingRerollCost > 0 {
		state.Materials.CustomModules += state.PendingRerollCost
		state.Materials.RerollModules += state.PendingRerollCost
		state.PendingRerollCost = 0
		states[taskID] = state
	}
}

func clearPendingRerollCost(taskID int64) {
	stateMu.Lock()
	defer stateMu.Unlock()
	state := states[taskID]
	state.PendingRerollCost = 0
	states[taskID] = state
}

// recordLockMaterialCost 累加一次“效果锁定”消耗的材料（累计日志 + 行为扣减库存余额）。
// lockIndex 为该装备当前已有锁数量（0=第一把锁，1=第二把锁）。
func recordLockMaterialCost(taskID int64, material string, lockIndex int) {
	cost := LockCost(material, lockIndex)
	if cost <= 0 {
		return
	}
	stateMu.Lock()
	state := states[taskID]
	switch material {
	case "订制模块":
		state.Materials.CustomModules += cost
		state.Materials.LockModules += cost
	default:
		state.Materials.CustomLockKeys += cost
	}
	states[taskID] = state
	stateMu.Unlock()
	decrementInventory(taskID, material, cost)
}

// GetMaterialUsage 返回当前任务累计的材料消耗。
func GetMaterialUsage(taskID int64) (MaterialUsage, bool) {
	stateMu.Lock()
	defer stateMu.Unlock()
	state, ok := states[taskID]
	if !ok {
		return MaterialUsage{}, false
	}
	return state.Materials, true
}

func isEquipmentPart(part string) bool {
	for _, candidate := range equipmentParts {
		if candidate == part {
			return true
		}
	}
	return false
}

func beginScan(taskID int64, part string) error {
	if !isEquipmentPart(part) {
		return fmt.Errorf("unknown equipment part %q", part)
	}

	stateMu.Lock()
	defer stateMu.Unlock()
	state := states[taskID]
	if state.Parts == nil {
		state.Parts = make(map[string]partScan)
	}
	state.Part = part
	state.Effects = [maxSlot]string{}
	state.Values = [maxSlot]string{}
	state.Locks = [maxSlot]SlotLock{}
	state.NextSlot = minSlot
	states[taskID] = state
	return nil
}

func currentEffectPart(taskID int64) (string, bool) {
	stateMu.Lock()
	defer stateMu.Unlock()
	state, ok := states[taskID]
	if !ok || !isEquipmentPart(state.Part) {
		return "", false
	}
	return state.Part, true
}

// setCurrentPart 标记当前正在洗练的部位，供结果页决策刷新快照使用。
func setCurrentPart(taskID int64, part string) error {
	if !isEquipmentPart(part) {
		return fmt.Errorf("unknown equipment part %q", part)
	}
	stateMu.Lock()
	defer stateMu.Unlock()
	state := states[taskID]
	state.Part = part
	states[taskID] = state
	return nil
}

// updatePartEffects 用一次效果变更后的最新词条与数值刷新快照中的指定部位，
// 使后续决策可直接基于快照调度，无需重新全量扫描四件装备。
// 结果页按「之前的方案」读取变更槽位文本：词条名 + 数值；
// 锁定关系：接受变更后保留原锁，由 expireOneTimeLocks 处理一次性锁失效。
func updatePartEffects(taskID int64, part string, effects, values [maxSlot]string) {
	stateMu.Lock()
	defer stateMu.Unlock()
	state := states[taskID]
	if state.Parts == nil {
		state.Parts = make(map[string]partScan)
	}
	scan := state.Parts[part]
	for i := range scan.Slots {
		scan.Slots[i].Effect = effects[i]
		scan.Slots[i].Value = values[i]
		// 洗4优不增删锁，接受后保留原锁（永久/一次性）；后续由 expireOneTimeLocks 处理一次性锁失效。
	}
	state.Parts[part] = scan
	states[taskID] = state
}

// applyLockToSnapshot 在装备详情页成功上锁后，将指定槽位标记为对应锁定状态。
// material 为 "订制模块"（永久）或 "自订密钥"（一次性）；slot 为1-based。
func applyLockToSnapshot(taskID int64, part string, slot int, material string) {
	if slot < minSlot || slot > maxSlot {
		return
	}
	stateMu.Lock()
	defer stateMu.Unlock()
	state := states[taskID]
	if state.Parts == nil {
		state.Parts = make(map[string]partScan)
	}
	scan := state.Parts[part]
	var lk SlotLock
	switch material {
	case "订制模块":
		lk = LockPermanent
	case "自订密钥":
		lk = LockOneTime
	default:
		lk = LockNone
	}
	scan.Slots[slot-1].Lock = lk
	state.Parts[part] = scan
	states[taskID] = state
}

// expireOneTimeLocks 在一次效果变更完成后，使指定部位的一次性锁全部失效。
// 永久锁保持不变；自订密钥锁定的特性：无论结果页维持还是接受，下一轮都失效。
func expireOneTimeLocks(taskID int64, part string) {
	stateMu.Lock()
	defer stateMu.Unlock()
	state := states[taskID]
	scan, ok := state.Parts[part]
	if !ok {
		return
	}
	for i := range scan.Slots {
		if scan.Slots[i].Lock == LockOneTime {
			scan.Slots[i].Lock = LockNone
		}
	}
	state.Parts[part] = scan
	states[taskID] = state
}

// countLocks 返回该部位当前锁定数（永久+一次性）。
func countLocks(scan partScan) int {
	c := 0
	for _, s := range scan.Slots {
		if s.Lock != LockNone {
			c++
		}
	}
	return c
}

// GetPartScan 返回单部位完整快照（供锁定决策使用）。
func GetPartScan(taskID int64, part string) (partScan, bool) {
	stateMu.Lock()
	defer stateMu.Unlock()
	state, ok := states[taskID]
	if !ok {
		return partScan{}, false
	}
	scan, ok := state.Parts[part]
	return scan, ok
}

func displayEffectSlots(effects [maxSlot]string, emptyLabel string) [maxSlot]string {
	for i, effect := range effects {
		if strings.TrimSpace(effect) == "" {
			effects[i] = emptyLabel
		}
	}
	return effects
}

func emptySlotDisplayLabel() string {
	const key = "tasker.equipment_reroll.empty_slot"
	label := i18n.T(key)
	if label == key {
		return "（空槽位）"
	}
	return label
}

// formatSlotLine 生成单个槽位的扫描展示行：词条 + 数值 + 锁定标签。
func formatSlotLine(scan slotScanData, emptyLabel string, lockLabels map[SlotLock]string) string {
	if strings.TrimSpace(scan.Effect) == "" {
		return emptyLabel
	}
	line := scan.Effect
	if value := strings.TrimSpace(scan.Value); value != "" {
		if tier, calibrated, ok := resolveEffectTier(scan.Effect, value); ok {
			value = valueTierDisplay(calibrated, tier)
		}
		line += " " + value
	}
	if label := lockLabels[scan.Lock]; label != "" {
		line += " " + label
	}
	return line
}

// displayScanLines 生成一件装备三槽的扫描展示行（含数值与锁定状态）。
func displayScanLines(scan partScan, emptyLabel string) [maxSlot]string {
	lockLabels := map[SlotLock]string{
		LockPermanent: i18n.T("tasker.equipment_reroll.lock_permanent"),
		LockOneTime:   i18n.T("tasker.equipment_reroll.lock_one_time"),
	}
	return formatScanLines(scan, emptyLabel, lockLabels)
}

// formatScanLines 生成一件装备三槽的展示行。
// lockLabels 由调用方决定是否展示锁定状态，槽位、数值、档位和空槽位格式保持统一。
func formatScanLines(scan partScan, emptyLabel string, lockLabels map[SlotLock]string) [maxSlot]string {
	var lines [maxSlot]string
	for i, slot := range scan.Slots {
		lines[i] = formatSlotLine(slot, emptyLabel, lockLabels)
	}
	return lines
}

// partScanFromArrays 从三槽的并行数组中组装 partScan。
func partScanFromArrays(effects [maxSlot]string, values [maxSlot]string, locks [maxSlot]SlotLock) partScan {
	var scan partScan
	for i := 0; i < maxSlot; i++ {
		scan.Slots[i] = slotScanData{Effect: effects[i], Value: values[i], Lock: locks[i]}
	}
	return scan
}

// GetEquipmentSlotScans 返回四部位完整扫描快照（词条 + 数值 + 锁定状态）。
// 返回的 map 是副本，可安全供后续 Custom 组件读取。
// getScannedParts 返回目前已扫描到的部位快照（有几件返回几件）。
// 与 GetEquipmentSlotScans 的区别：后者要求四件齐全（角色模式决策的前提），
// 这里只用于摘要输出——单件模式只扫一件，仍应把那一件打印出来。
func getScannedParts(taskID int64) map[string]partScan {
	stateMu.Lock()
	defer stateMu.Unlock()

	state, ok := states[taskID]
	if !ok || len(state.Parts) == 0 {
		return nil
	}
	parts := make(map[string]partScan, len(state.Parts))
	for part, scan := range state.Parts {
		parts[part] = scan
	}
	return parts
}

func GetEquipmentSlotScans(taskID int64) (map[string]partScan, bool) {
	stateMu.Lock()
	defer stateMu.Unlock()

	state, ok := states[taskID]
	if !ok || len(state.Parts) != len(equipmentParts) {
		return nil, false
	}
	for _, part := range equipmentParts {
		if _, ok := state.Parts[part]; !ok {
			return nil, false
		}
	}

	parts := make(map[string]partScan, len(state.Parts))
	for part, scan := range state.Parts {
		parts[part] = scan
	}
	return parts, true
}

// currentPartScan 返回当前正在扫描部位的三槽扫描数据（进行中状态）。
func currentPartScan(taskID int64) (partScan, bool) {
	stateMu.Lock()
	defer stateMu.Unlock()
	state, ok := states[taskID]
	if !ok || !isEquipmentPart(state.Part) {
		return partScan{}, false
	}
	return partScanFromArrays(state.Effects, state.Values, state.Locks), true
}

// firstExistingLock 返回扫描快照中第一个已存在的锁。
// 前置扫描只允许发现无锁装备；脚本运行中新增的锁不会再次经过这条扫描路由。
func firstExistingLock(scan partScan) (int, SlotLock, bool) {
	for i, slot := range scan.Slots {
		if slot.Lock != LockNone {
			return i + 1, slot.Lock, true
		}
	}
	return 0, LockNone, false
}

func recordEffect(taskID int64, params recordEffectParam, effect string) (monitorState, error) {
	if params.Slot < minSlot || params.Slot > maxSlot {
		return monitorState{}, fmt.Errorf("invalid slot %d", params.Slot)
	}
	if params.IsLast != (params.Slot == maxSlot) {
		return monitorState{}, fmt.Errorf("is_last mismatch for slot %d", params.Slot)
	}

	stateMu.Lock()
	defer stateMu.Unlock()

	state, exists := states[taskID]
	if params.Slot == minSlot {
		if !exists {
			state = monitorState{Parts: make(map[string]partScan)}
		}
		if state.Parts == nil {
			state.Parts = make(map[string]partScan)
		}
		state.Part = params.Part
		state.Effects = [maxSlot]string{}
		state.Values = [maxSlot]string{}
		state.Locks = [maxSlot]SlotLock{}
		state.NextSlot = minSlot
	} else if !exists || state.Part != params.Part || state.NextSlot != params.Slot {
		delete(states, taskID)
		return monitorState{}, fmt.Errorf("slot %d has no preceding result for %s", params.Slot, params.Part)
	}
	state.Effects[params.Slot-1] = effect
	state.Values[params.Slot-1] = params.Value
	state.Locks[params.Slot-1] = params.Lock
	if params.IsLast {
		state.Parts[params.Part] = partScanFromArrays(state.Effects, state.Values, state.Locks)
		state.NextSlot = minSlot
	} else {
		state.NextSlot = params.Slot + 1
	}
	states[taskID] = state
	return state, nil
}

// specializedEffect 根据 OCR 中残留的关键字符做特异化识别。
// 适用于 OCR 严重截断但保留独有特征字的情况，例如“防增加】”应识别为“防御力增加”。
// 只使用在官方效果中具有排他性的字符/字符组合，避免误判。
func specializedEffect(raw string) (string, bool) {
	han := strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Han, r) {
			return r
		}
		return -1
	}, raw)
	switch {
	case strings.Contains(han, "防"):
		return "防御力增加", true
	case strings.Contains(han, "攻"):
		return TargetEffectAttackIncrease, true
	case strings.Contains(han, "优"):
		return TargetEffectElementalDamage, true
	case strings.Contains(han, "命"):
		return "命中率增加", true
	case strings.Contains(han, "装") || strings.Contains(han, "弹"):
		return "最大装弹数增加", true
	case strings.Contains(han, "速"):
		return "蓄力速度增加", true
	case strings.Contains(han, "暴") && strings.Contains(han, "率"):
		return "暴击率增加", true
	case strings.Contains(han, "暴") && strings.Contains(han, "伤"):
		return "暴击伤害增加", true
	case strings.Contains(han, "蓄") && strings.Contains(han, "伤"):
		return "蓄力伤害增加", true
	}
	return "", false
}

func normalizeEffect(raw string) (string, bool) {
	compact := strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, raw)
	for _, effect := range officialEffects {
		if strings.Contains(compact, effect) {
			return effect, true
		}
	}

	// OCR 严重截断但保留独有特征字时，用特异化规则直接识别。
	if effect, ok := specializedEffect(raw); ok {
		return effect, true
	}

	// OCR may replace one Chinese character. Compare only Chinese text so
	// brackets, percentages, and other row decorations do not affect matching.
	text := []rune(strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Han, r) {
			return r
		}
		return -1
	}, compact))
	for _, effect := range officialEffects {
		want := []rune(effect)
		for start := 0; start < len(text); start++ {
			for width := len(want) - 1; width <= len(want)+1; width++ {
				end := start + width
				if width < 1 || end > len(text) {
					continue
				}
				if runeEditDistance(text[start:end], want) <= 1 {
					return effect, true
				}
			}
		}
	}
	return "", false
}

func isUnobtainedEffect(raw string) bool {
	compact := strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, raw)
	// OCR 可能只识别出“未获得效”/“未获得”，只要包含“未获得”即视为空槽。
	return strings.Contains(compact, "未获得")
}

func runeEditDistance(a, b []rune) int {
	previous := make([]int, len(b)+1)
	for j := range previous {
		previous[j] = j
	}
	for i, ar := range a {
		current := make([]int, len(b)+1)
		current[0] = i + 1
		for j, br := range b {
			cost := 0
			if ar != br {
				cost = 1
			}
			current[j+1] = min3(current[j]+1, previous[j+1]+1, previous[j]+cost)
		}
		previous = current
	}
	return previous[len(b)]
}

func min3(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}
