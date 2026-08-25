package main

import "testing"

// TestAllocateAssignedCostSwap 验证 allocateAssigned 采用“成本交换”口径：
// 对稀缺效果（装弹1），它应选“补齐承载者 required 成本最低”的件（非对称状态下，已有装弹+攻、
// 更接近完整的件优先），而不是启发式地任由空件补。
func TestAllocateAssignedCostSwap(t *testing.T) {
	K := map[int]int{idxYou: 4, idxAtk: 4, idxAmmo: 1}
	// 构造非对称状态：
	//   piece0: 防御/攻/防御（攻@2 可锁）
	//   piece1: 装弹@1 + 攻@2（已带稀缺装弹，成本最低）——应为成本交换选中的承载者
	//   piece2/3: 全防御（空）
	pieces := [nPieces]piece{
		newPiece([3]int{idxDef, idxAtk, idxDef}),
		newPiece([3]int{idxAmmo, idxAtk, idxDef}),
		newPiece([3]int{idxDef, idxDef, idxDef}),
		newPiece([3]int{idxDef, idxDef, idxDef}),
	}
	assigned := allocateAssigned(pieces, K)

	carrier := -1
	for i := 0; i < nPieces; i++ {
		for _, e := range assigned[i] {
			if e == idxAmmo {
				carrier = i
			}
		}
	}
	// 满配额 优/攻 分给全部件。
	for i := 0; i < nPieces; i++ {
		if !sliceIntContains(assigned[i], idxYou) || !sliceIntContains(assigned[i], idxAtk) {
			t.Fatalf("piece %d should get full 优/攻; assigned=%v", i, assigned[i])
		}
	}
	if carrier != 1 {
		t.Fatalf("cost swap should pick piece 1 (has 装弹, lowest carrier cost), got %d; assigned=%v", carrier, assigned)
	}
	// 边界：容量 = 3 − 满配额数(2) = 1，因此装弹承载者至多 1 件。
	totalCarrier := 0
	for i := 0; i < nPieces; i++ {
		if sliceIntContains(assigned[i], idxAmmo) {
			totalCarrier++
		}
	}
	if totalCarrier != 1 {
		t.Fatalf("装弹1 should have exactly 1 carrier, got %d", totalCarrier)
	}
}
