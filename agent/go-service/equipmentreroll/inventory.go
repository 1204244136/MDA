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
// 订制模块锁定是持续性锁（半永久，解除前不失效），自订密钥锁定只保护下一次效果变更。
// lockIndex 是本次新增锁在装备上所处的顺序位（从 0 开始）。
// 成本以客户端截图校准：0→1 需 2 模块 / 20 密钥，1→2 需 3 模块 / 30 密钥。
func (inv Inventory) CanAffordLock(material string, lockIndex int) bool {
	if lockIndex < 0 {
		lockIndex = 0
	}
	var cost int
	switch lockIndex {
	case 0:
		cost = 2
	case 1:
		cost = 3
	default:
		cost = 3
		if lockIndex > 1 {
			// 最多2锁，再加锁沿用3/30（防御性）
			cost = 3
		}
	}
	switch material {
	case "订制模块":
		if lockIndex == 0 && cost == 2 || lockIndex == 1 && cost == 3 {
			// 普通分支
		}
		return inv.CustomModules >= cost
	case "自订密钥":
		if lockIndex == 0 {
			cost = 20
		} else {
			cost = 30
		}
		return inv.CustomLockKeys >= cost
	default:
		return false
	}
}

// LockCost 返回指定材料在 lockIndex 位的消耗数量（用于日志/扣费）。
func LockCost(material string, lockIndex int) int {
	if lockIndex < 0 {
		lockIndex = 0
	}
	switch material {
	case "订制模块":
		if lockIndex == 0 {
			return 2
		}
		return 3
	case "自订密钥":
		if lockIndex == 0 {
			return 20
		}
		return 30
	default:
		return 0
	}
}

// ChooseLockMaterial 按“有密钥用密钥，不够再用模块”选择锁定材料。
// 返回选中材料名与是否可支付；若都不足返回("", false)。
func (inv Inventory) ChooseLockMaterial(lockIndex int) (string, bool) {
	if inv.CanAffordLock("自订密钥", lockIndex) {
		return "自订密钥", true
	}
	if inv.CanAffordLock("订制模块", lockIndex) {
		return "订制模块", true
	}
	return "", false
}
