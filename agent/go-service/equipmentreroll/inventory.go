package equipmentreroll

// 本文件实现"洗词条"的材料库存与预算模型（与界面无关、可单元测试）。
//
// 材料用途边界（见策略文档 §2.1）：
//   - "效果变更"只能消耗订制模块；自订密钥不能代替洗词条费用；
//   - "效果锁定"才可以在订制模块和自订密钥之间二选一支付。
//
// 四优模板不锁定，因此每次效果变更固定消耗 1 个订制模块。

// Inventory 表示当前两种材料的库存数量。
type Inventory struct {
	// CustomModules 订制模块库存。
	CustomModules int `json:"custom_modules"`
	// CustomLockKeys 自订密钥库存。
	CustomLockKeys int `json:"custom_lock_keys"`
}

// RerollModuleCost 返回一次"效果变更"消耗的订制模块数量。
// 成本取决于本次操作时受保护（锁定）的栏位数：0 锁 1、1 锁 2、2 锁 3。
func RerollModuleCost(activeLocks int) int {
	if activeLocks < 0 {
		activeLocks = 0
	}
	if activeLocks > maxSlot-1 {
		activeLocks = maxSlot - 1
	}
	return 1 + activeLocks
}

// CanAffordReroll 判断订制模块是否足以支付一次效果变更。
// 即使自订密钥充足也不能代替订制模块支付洗词条费用。
func (inv Inventory) CanAffordReroll(activeLocks int) bool {
	return inv.CustomModules >= RerollModuleCost(activeLocks)
}

// EstimateSupportedEffectChanges 估算剩余订制模块在不新增锁定的前提下
// 还能支持多少次效果变更。四优模板（无锁）下即为库存数本身。
func (inv Inventory) EstimateSupportedEffectChanges() int {
	if inv.CustomModules <= 0 {
		return 0
	}
	return inv.CustomModules / RerollModuleCost(0)
}

// CanAffordLock 判断是否足以支付一次指定材料的锁定。
// 订制模块锁定是持续性锁，自订密钥锁定只保护下一次效果变更。
// lockIndex 是本次新增锁在装备上所处的顺序位（从 0 开始）。
//
// TODO(校准)：锁定材料的单次消耗数量由客户端确认后接入；四优模板不使用锁定。
func (inv Inventory) CanAffordLock(material string, lockIndex int) bool {
	if lockIndex < 0 {
		lockIndex = 0
	}
	cost := 1 + lockIndex
	switch material {
	case "订制模块":
		return inv.CustomModules >= cost
	case "自订密钥":
		return inv.CustomLockKeys >= cost
	default:
		return false
	}
}
