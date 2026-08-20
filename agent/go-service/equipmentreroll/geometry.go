package equipmentreroll

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// 装备详情页三个槽位的固有几何（来自实测，作为「锚定固定标识 + offset」方案的基准）。
//
// 若将三个槽位视为一个整体矩形：
//   - 宽：300
//   - 高：70
//
// 每个槽位：
//   - 高：22
//   - 槽与槽之间的间距等分：3×22 + 2×2 = 70
//   - 因此槽位纵向步距（pitch）= 22 + 2 = 24
//
// 锚点：InspectFlag.png（装备详情页左侧 Flag 小图标）。
// 实测：当 Flag 识别框为 [580, 439, 14, 15] 时，第 1 槽位左上角为 (490, 484)。
// 因此 Flag 识别框左上角 → 第 1 槽位左上角的固定偏移为 (-90, 45)（距离恒定）。
//
// 注意：由于装备描述行数不同，换装备后整个槽位组在竖直方向可能整体偏移，
// 不能使用固定窄条带 ROI 逐个定位；应锚定 Flag 后按本几何做 offset。
const (
	// slotGroupWidth 三个槽位整体矩形的宽度。
	slotGroupWidth = 300
	// slotGroupHeight 三个槽位整体矩形的高度。
	slotGroupHeight = 70
	// slotHeight 单个槽位高度。
	slotHeight = 22
	// slotGap 相邻槽位之间的间隙（等分）。
	slotGap = 2
	// slotPitch 相邻槽位顶部之间的纵向步距（slotHeight + slotGap）。
	slotPitch = slotHeight + slotGap

	// inspectFlagTemplate 装备详情页锚点模板（Flag 小图标）。
	inspectFlagTemplate = "EquipmentReroll/InspectFlag.png"
	// flagToSlot1OffsetX Flag 识别框左上角 → 第 1 槽位左上角的水平偏移。
	flagToSlot1OffsetX = -90
	// flagToSlot1OffsetY Flag 识别框左上角 → 第 1 槽位左上角的垂直偏移。
	flagToSlot1OffsetY = 45
)

// slotRectFromFlag 由 Flag 锚点识别框推导第 slot 槽的完整矩形。
// 第 N 槽左上角 = Flag 左上角 + (flagToSlot1OffsetX, flagToSlot1OffsetY + (N-1)×slotPitch)。
func slotRectFromFlag(flag maa.Rect, slot int) maa.Rect {
	if slot < minSlot {
		slot = minSlot
	}
	if slot > maxSlot {
		slot = maxSlot
	}
	return maa.Rect{
		flag.X() + flagToSlot1OffsetX,
		flag.Y() + flagToSlot1OffsetY + (slot-minSlot)*slotPitch,
		slotGroupWidth,
		slotHeight,
	}
}
