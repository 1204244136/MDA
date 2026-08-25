package equipmentreroll

import (
	"sort"
	"strings"
)

// 本文件实现"洗单个装备词条"（单件模式）的纯决策逻辑（与界面无关、可单元测试）。
//
// 与角色模式（全局配额 DP 分配，见 plan.go / plan_dp.go）不同，单件模式：
//   - 只处理用户选定的一件装备（头部/臂部/身躯/腿部），不做跨装备的复杂搭配计算；
//   - 目标按"需求词条行"建模（借鉴 MaaEnd 任务选项的逐项 select 风格）：最多 3 行，
//     每行 = 一个需求词条 + 可选限定槽位（不限定则任意槽位均可），效果名直接作为选项；
//   - 词条数量限制不同：单件三槽、同效果不重复，因此需求词条数上限为 3（角色模式为 12）；
//   - 选择两条以上词条时，锁定决策复用 plan_dp.go 的槽位概率/期望成本模型（slot-aware DP），
//     比较"锁某槽 vs 不锁"的期望收益差距，选择期望剩余模块成本最低的锁定方案。
//
// 文档索引：docs/zh_cn/nikke/EquipmentReroll/洗词条策略与Agent逻辑.md（单件模式章节）

const (
	singleTargetMinCount = 1
	singleTargetMaxCount = maxSlot
)

// 单件目标非法原因码（英文短码，供日志与调用方分支使用；用户可见文案在动作层做 i18n）。
const (
	// singleTargetProblemDuplicatedAffix 同一效果被多行重复选择（单件三槽不可重复同效果）。
	singleTargetProblemDuplicatedAffix = "duplicated_affix"
	// singleTargetProblemCountOutOfRange 需求词条数不在 1~3 条。
	singleTargetProblemCountOutOfRange = "count_out_of_range"
	// singleTargetProblemUnknownAffix 词条名不是官方效果名。
	singleTargetProblemUnknownAffix = "unknown_affix"
	// singleTargetProblemSlotOutOfRange 槽位限定不在 0~3。
	singleTargetProblemSlotOutOfRange = "slot_out_of_range"
	// singleTargetProblemSlotConflict 多个词条被限定到同一槽位。
	singleTargetProblemSlotConflict = "slot_conflict"
)

// singleTarget 表示单件装备的词条目标。
type singleTarget struct {
	// Want 需求词条：effect -> 限定槽位（0=任意槽位，1/2/3=限定第 N 号槽，与任务选项槽位 select 的取值一致）。
	Want map[string]int
}

// singleTargetFromRows 把任务选项的"需求词条行"组装为 singleTarget 的 Want 映射。
// 每行 = (want 效果名, slot 限定号 0/1/2/3)；want 为空串表示该行不需求。
// 返回 false 表示同一效果被重复选择（单件同效果不重复，配置非法）。
func singleTargetFromRows(want1, want2, want3 string, slot1, slot2, slot3 int) (singleTarget, bool) {
	t := singleTarget{Want: make(map[string]int)}
	wants := [3]string{strings.TrimSpace(want1), strings.TrimSpace(want2), strings.TrimSpace(want3)}
	slots := [3]int{slot1, slot2, slot3}
	for i := 0; i < len(wants); i++ {
		if wants[i] == "" {
			continue
		}
		if _, dup := t.Want[wants[i]]; dup {
			return singleTarget{}, false
		}
		t.Want[wants[i]] = slots[i]
	}
	return t, true
}

// parseSingleTarget 直接按 effect -> 槽位（0=任意，1/2/3=限定）映射构造 singleTarget。
// 运行期目标由 singleTargetFromRows 从承载点的 want/slot 行组装；这个入口供测试与
// 已有 effect->slot 映射的场景直接构造，不做合法性校验（校验见 singleTargetProblem）。
func parseSingleTarget(raw map[string]int) singleTarget {
	t := singleTarget{Want: make(map[string]int, len(raw))}
	for e, slot := range raw {
		t.Want[e] = slot
	}
	return t
}

// singleTargetProblem 返回单件目标的非法原因码，返回空串表示合法：
//   - 需求词条数必须在 1~3（单件三槽、同效果不重复）；
//   - 词条名必须为官方效果名；
//   - 限定槽位必须在 0~3 且互不冲突（一个槽位只能放一个词条）。
func singleTargetProblem(t singleTarget) string {
	if len(t.Want) < singleTargetMinCount || len(t.Want) > singleTargetMaxCount {
		return singleTargetProblemCountOutOfRange
	}
	usedSlots := make(map[int]bool, len(t.Want))
	for _, effect := range sortedTargetEffects(t) {
		slot := t.Want[effect]
		if !isOfficialEffect(effect) {
			return singleTargetProblemUnknownAffix
		}
		if slot < 0 || slot > maxSlot {
			return singleTargetProblemSlotOutOfRange
		}
		if slot != 0 {
			if usedSlots[slot] {
				return singleTargetProblemSlotConflict
			}
			usedSlots[slot] = true
		}
	}
	return ""
}

// singleTargetValid 校验单件目标合法性（原因见 singleTargetProblem）。
func singleTargetValid(t singleTarget) bool {
	return singleTargetProblem(t) == ""
}

// isOfficialEffect 判断词条名是否为游戏内官方效果名。
func isOfficialEffect(e string) bool {
	for _, effect := range officialEffects {
		if e == effect {
			return true
		}
	}
	return false
}

// singleQuota 把单件目标转成 plan_dp.go 期望成本模型所需的配额（需求=1）。
func singleQuota(t singleTarget) map[string]int {
	quota := make(map[string]int, len(t.Want))
	for e := range t.Want {
		quota[e] = 1
	}
	return quota
}

// slotAllowMap 把单件目标转成槽位限定映射（effect -> 允许的 0 基槽位集合）。
// 没有限定槽位时返回 nil（表示任意槽位）。
func slotAllowMap(t singleTarget) map[string]map[int]bool {
	allow := make(map[string]map[int]bool, len(t.Want))
	for e, slot := range t.Want {
		if slot != 0 {
			allow[e] = map[int]bool{slot - 1: true}
		}
	}
	if len(allow) == 0 {
		return nil
	}
	return allow
}

// sortedTargetEffects 返回需求词条名的稳定排序（供缓存键与枚举使用）。
func sortedTargetEffects(t singleTarget) []string {
	names := make([]string, 0, len(t.Want))
	for e := range t.Want {
		names = append(names, e)
	}
	sort.Strings(names)
	return names
}

// effectInAllowedSlot 判断 effects 中 effect 是否出现在限定槽位（slot=0 表示任意槽位）。
func effectInAllowedSlot(effects [maxSlot]string, effect string, slot int) bool {
	for i, e := range effects {
		if e != effect {
			continue
		}
		if slot == 0 || slot == i+1 {
			return true
		}
	}
	return false
}

// singlePartSatisfied 判断单件是否满足目标：每个需求词条都出现在其允许槽位。
func singlePartSatisfied(scan partScan, t singleTarget) bool {
	effects := scan.Effects()
	for e, slot := range t.Want {
		if !effectInAllowedSlot(effects, e, slot) {
			return false
		}
	}
	return true
}

// singlePartNeedsReroll 判断单件是否仍需要洗词条。
func singlePartNeedsReroll(scan partScan, t singleTarget) bool {
	return !singlePartSatisfied(scan, t)
}

// singleEffectiveAffixCount 统计单件上"落在允许槽位"的需求词条数（物理有效词条数）。
// 用于结果页短期最优：宏观成本持平且有效词条数上升时接受。
func singleEffectiveAffixCount(scan partScan, t singleTarget) int {
	effects := scan.Effects()
	n := 0
	for e, slot := range t.Want {
		if effectInAllowedSlot(effects, e, slot) {
			n++
		}
	}
	return n
}

// singleDesiredLockSlot 决定单件当前是否需锁定某槽位：
//   - 需求 < 2（只有一条）→ 不锁（找到即达标，锁反而抬升重洗成本）；
//   - 需求 == 2 → 拿到就锁：用期望收益差距 DP 锁"已落地、允许槽位"的可锁槽中最优者；
//   - 需求 == 3（三槽全满，饱和）→ 先苦后甜：先便宜刷最难的 3 号，落地才锁。
//
// material 指定锁定材料（"自订密钥"/"订制模块"）；返回 0 表示不锁。
func singleDesiredLockSlot(scan partScan, t singleTarget, material string) (int, bool) {
	if len(t.Want) < 2 {
		return 0, false
	}
	quota := singleQuota(t)
	required := sortedTargetEffects(t)
	allow := slotAllowMap(t)
	if len(t.Want) >= maxSlot {
		slot := desiredLockSlotBitterSweetCore(scan, quota, required, nil, material, allow)
		return slot, slot != 0
	}
	slot, _ := bestLockSlotAndCostForTarget(scan, quota, required, nil, material, allow)
	return slot, slot != 0
}

// singleExpectedCost 单件在"最优锁定位"下的期望剩余订制模块成本。
// 复用了 plan_dp.go 的槽位概率/期望成本模型（含槽位限定与锁定排除）。
// 返回 >= costUnreachable 表示目标不可达（例如需求词条被永久锁在了不允许的槽位）。
func singleExpectedCost(scan partScan, t singleTarget) float64 {
	quota := singleQuota(t)
	required := sortedTargetEffects(t)
	allow := slotAllowMap(t)
	_, cost := bestLockSlotAndCostForTarget(scan, quota, required, nil, "", allow)
	return cost
}

// singleTargetUnreachable 判断单件目标在当前装备状态下是否已不可能达成。
//
// 典型成因：装备上有历史锁定（上次运行或手动锁的），而用户的槽位限定与之冲突——
// 比如 3 号槽已锁"攻击力增加"，却把"攻击力增加"限定到 2 号槽。锁定无法解除、
// 词条也不会跨槽移动，此时无论洗多少次都不可能达标。
//
// 必须在决策入口拦截：否则 singlePartSatisfied 恒 false（一直洗），而结果页
// 当前/候选成本同为 costUnreachable 恒判 Keep（一直不接受），会把用户的订制模块全部烧光。
func singleTargetUnreachable(scan partScan, t singleTarget) bool {
	return singleExpectedCost(scan, t) >= costUnreachable
}

// DecideResultPageSingle 单件结果页决策：候选状态的期望剩余模块成本严格更低才接受；
// 宏观成本持平且"允许槽位上的有效词条数"上升也接受（短期最优，不覆盖宏观更差）。
//
// changed 为结果页读到的候选三槽词条；currentScan 为当前部位快照（提供锁定状态，
// 其 Effects 即当前词条）。
func DecideResultPageSingle(changed [maxSlot]string, currentScan partScan, t singleTarget) ResultDecision {
	cand := currentScan
	for i := range cand.Slots {
		cand.Slots[i].Effect = changed[i]
	}
	curCost := singleExpectedCost(currentScan, t)
	candCost := singleExpectedCost(cand, t)
	if candCost < curCost-1e-6 {
		return ResultDecisionAccept
	}
	if candCost <= curCost+1e-6 && singleEffectiveAffixCount(cand, t) > singleEffectiveAffixCount(currentScan, t) {
		return ResultDecisionAccept
	}
	return ResultDecisionKeep
}
