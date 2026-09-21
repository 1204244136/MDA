package equipmentreroll

import (
	"fmt"
	"math"

	"github.com/1204244136/MDA/agent/go-service/pkg/i18n"
)

type rerollEstimate struct {
	Modules float64
	Config  carrierConfig
	Valid   bool
	Flavor  int // 同一任务固定台词，摘要重试不重新抽取。
}

const luckFlavorCount = 3

// luckTier 按实际/初始期望的模块成本比评级，不代表概率分位数。
func luckTier(ratio float64) string {
	switch {
	case ratio <= 0.1:
		return "divine"
	case ratio <= 0.2:
		return "lifespan"
	case ratio <= 0.35:
		return "insider"
	case ratio <= 0.5:
		return "vip"
	case ratio <= 0.7:
		return "coupon"
	case ratio <= 0.9:
		return "escape"
	case ratio <= 1.1:
		return "standard"
	case ratio <= 1.4:
		return "detour"
	case ratio <= 2:
		return "donor"
	case ratio <= 3:
		return "shareholder"
	case ratio <= 5:
		return "pity"
	default:
		return "outlier"
	}
}

func rerollTargetSatisfied(parts map[string]partScan, cfg carrierConfig) bool {
	if cfg.validateOperation() != nil {
		return false
	}
	for _, part := range cfg.valueScope() {
		if _, ok := parts[part]; !ok {
			return false
		}
	}
	if cfg.isValue() {
		progress, err := evaluateValueScope(parts, cfg)
		return err == nil && progress.Pending == 0
	}
	if cfg.isSingle() {
		return cfg.singleTargetOK() && singleTargetValid(cfg.Target) && singlePartSatisfied(parts[cfg.Part], cfg.Target)
	}
	quota := cfg.resolveQuota(nil)
	return quotaIsValid(quota) && AllPartsSatisfiedQuota(parts, quota)
}

func buildLuckMessage(taskID int64, parts map[string]partScan, usage MaterialUsage) string {
	stateMu.Lock()
	estimate := states[taskID].InitialEstimate
	stateMu.Unlock()
	if estimate == nil {
		return ""
	}
	if !estimate.Valid || estimate.Modules <= 0 || math.IsNaN(estimate.Modules) || math.IsInf(estimate.Modules, 0) {
		return ""
	}
	if !rerollTargetSatisfied(parts, estimate.Config) {
		return ""
	}
	if usage.CustomModules <= 0 {
		return ""
	}
	const prefix = "tasker.equipment_reroll.luck_"
	ratio := float64(usage.CustomModules) / estimate.Modules
	tierKey := prefix + luckTier(ratio)
	quip := i18n.T(fmt.Sprintf("%s_%d", tierKey, estimate.Flavor+1))
	message := i18n.T(prefix+"result", i18n.T(tierKey), quip, estimate.Modules, usage.CustomModules, ratio*100)
	if usage.CustomLockKeys > 0 {
		message += "\n" + i18n.T(prefix+"keys", usage.CustomLockKeys)
	}
	return message
}
