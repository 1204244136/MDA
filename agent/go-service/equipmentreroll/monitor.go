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

// monitorState 全量扫描的 task 级状态：当前部位的进行中数组 + 四部位完整快照。
type monitorState struct {
	Part     string
	Effects  [maxSlot]string
	Values   [maxSlot]string
	Locks    [maxSlot]SlotLock
	Parts    map[string]partScan
	NextSlot int
}

var (
	stateMu sync.Mutex
	states  = make(map[int64]monitorState)
)

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
// 锁定关系：洗4优保留原锁；四攻四优在接受变更后需处理一次性锁失效。
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

// formatSlotLine 生成单个槽位的扫描展示行：词条 + 数值 + 锁定标签。
func formatSlotLine(scan slotScanData, emptyLabel string, lockLabels map[SlotLock]string) string {
	if strings.TrimSpace(scan.Effect) == "" {
		return emptyLabel
	}
	line := scan.Effect
	if value := strings.TrimSpace(scan.Value); value != "" {
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

// GetEquipmentEffects returns the four-part snapshot accumulated for a task.
// The returned map is a copy and can be safely used by a later custom action.
func GetEquipmentEffects(taskID int64) (map[string][maxSlot]string, bool) {
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

	parts := make(map[string][maxSlot]string, len(state.Parts))
	for part, scan := range state.Parts {
		parts[part] = scan.Effects()
	}
	return parts, true
}

// GetEquipmentSlotScans 返回四部位完整扫描快照（词条 + 数值 + 锁定状态）。
// 当前洗四优决策只使用词条，但后续任务需要数值与锁定关系，因此保留完整快照读取入口。
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
