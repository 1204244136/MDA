package equipmentreroll

import "testing"

func TestValueLocksMatchPlan(t *testing.T) {
	// 三槽共 27 种实际状态与 27 种规划交叉比较：任何槽改变都不能跳过核验。
	for actual := 0; actual < 27; actual++ {
		var scan partScan
		v := actual
		for i := range scan.Slots {
			scan.Slots[i].Lock = SlotLock(v % 3)
			v /= 3
		}
		for desired := 0; desired < 27; desired++ {
			var plan valuePlan
			v = desired
			for i := range plan.Locks {
				plan.Locks[i] = SlotLock(v % 3)
				v /= 3
			}
			if valueLocksMatchPlan(scan, plan) != (actual == desired) {
				t.Fatalf("actual=%d desired=%d", actual, desired)
			}
		}
	}
}
