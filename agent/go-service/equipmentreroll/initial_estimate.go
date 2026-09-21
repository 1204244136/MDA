package equipmentreroll

import (
	"fmt"
	"math"
	"math/rand/v2"

	"github.com/1204244136/MDA/agent/go-service/pkg/i18n"
	"github.com/rs/zerolog/log"
)

// shouldReportInitialEstimate 在所选范围扫描结束后提示；调试扫描不估算。
func shouldReportInitialEstimate(standalone bool, part string, cfg carrierConfig) bool {
	if standalone {
		return false
	}
	if cfg.isSingle() {
		return part == cfg.Part && isEquipmentPart(part)
	}
	return part == equipmentParts[len(equipmentParts)-1]
}

// initialModuleEstimate 只读初始快照，不修改任务状态或锁方案。
// 扫描阶段尚未确认材料库存，统一按不用密钥估算模组。
func initialModuleEstimate(parts map[string]partScan, cfg carrierConfig) (float64, error) {
	if err := cfg.validateOperation(); err != nil {
		return 0, err
	}
	for _, p := range cfg.valueScope() {
		if _, ok := parts[p]; !ok {
			return 0, fmt.Errorf("missing snapshot: %s", p)
		}
	}
	var cost float64
	if cfg.isValue() {
		plan, err := planValueRerollWithInventory(parts, cfg, Inventory{CustomModules: 1000000}, "")
		if err != nil {
			return 0, err
		}
		cost = plan.ExpectedTotalModules
	} else if cfg.isSingle() {
		if !isEquipmentPart(cfg.Part) || !cfg.singleTargetOK() || !singleTargetValid(cfg.Target) {
			return 0, fmt.Errorf("invalid single target")
		}
		_, cost = bestLockSlotAndCostForTarget(parts[cfg.Part], singleQuota(cfg.Target), sortedTargetEffects(cfg.Target), nil, "订制模块", slotAllowMap(cfg.Target))
	} else {
		quota := cfg.resolveQuota(nil)
		if !quotaIsValid(quota) {
			return 0, fmt.Errorf("invalid quota")
		}
		if !AllPartsSatisfiedQuota(parts, quota) {
			assigned := allocateQuotaRequired(parts, quota)
			for _, part := range equipmentParts {
				cost += partExpectedCostForRequired(parts[part], quota, assigned[part], "订制模块")
			}
		}
	}
	if math.IsNaN(cost) || math.IsInf(cost, 0) || cost < 0 || cost >= costUnreachable {
		return 0, fmt.Errorf("no finite estimate")
	}
	return cost, nil
}

func initialEstimateMessage(taskID int64, parts map[string]partScan, cfg carrierConfig) string {
	cost, err := initialModuleEstimate(parts, cfg)
	stateMu.Lock()
	state := states[taskID]
	if state.InitialEstimate == nil {
		state.InitialEstimate = &rerollEstimate{Modules: cost, Config: cfg, Valid: err == nil, Flavor: rand.IntN(luckFlavorCount)}
		states[taskID] = state
	}
	stateMu.Unlock()
	if err != nil {
		log.Warn().Err(err).Str("component", "EquipmentReroll").
			Str("operation", string(cfg.Operation)).Str("mode", string(cfg.Mode)).
			Msg("initial module estimate unavailable")
		return i18n.T("tasker.equipment_reroll.initial_estimate_unavailable")
	}
	if cost == 0 {
		return i18n.T("tasker.equipment_reroll.initial_estimate_satisfied")
	}
	return i18n.T("tasker.equipment_reroll.initial_estimate", math.Ceil(cost))
}
