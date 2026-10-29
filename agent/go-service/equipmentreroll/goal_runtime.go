package equipmentreroll

// independentGoalCosts 保留每套完整目标的独立续程估值，供日志展示当前状态偏向。
// 联合估值由 OR 目标规划器计算，不能用这些值逐词条拼成一个目标。
func independentGoalCosts(parts map[string]partScan, goals []map[string]int, material string) []float64 {
	costs := make([]float64, len(goals))
	for i, quota := range goals {
		costs[i] = expectedModulesForQuotaWithMaterial(parts, quota, material)
	}
	return costs
}

func validGoalQuotas(goals []map[string]int) bool {
	if len(goals) == 0 {
		return false
	}
	for _, quota := range goals {
		if !quotaIsValid(quota) {
			return false
		}
	}
	return true
}
