package equipmentreroll

import (
	"encoding/json"
	"fmt"
	"image"
	"strconv"
	"strings"

	"github.com/1204244136/MDA/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

func readRerollCost(ctx *maa.Context, img image.Image, scan partScan) (MaterialUsage, bool) {
	var cost MaterialUsage
	title, err := ctx.RunRecognition("__EquipmentRerollChangeEffectTitle", img, nil)
	if err != nil || title == nil || !title.Hit {
		return cost, false
	}
	module, err := ctx.RunRecognition("__EquipmentRerollCostModule", img, nil)
	if err != nil || module == nil || !module.Hit {
		return cost, false
	}
	count, err := ctx.RunRecognition("__EquipmentRerollCostModuleCount", img, nil)
	if err != nil || count == nil || !count.Hit {
		return cost, false
	}
	cost.CustomModules, err = strconv.Atoi(strings.TrimSpace(firstRawOCRText(count)))
	if err != nil || cost.CustomModules <= 0 {
		return cost, false
	}
	key, err := ctx.RunRecognition("__EquipmentRerollCostKey", img, nil)
	if err != nil || key == nil {
		return cost, false
	}
	wantKey := false
	for _, slot := range scan.Slots {
		wantKey = wantKey || slot.Lock == LockOneTime
	}
	if key.Hit != wantKey {
		return cost, false
	}
	if key.Hit {
		count, err = ctx.RunRecognition("__EquipmentRerollCostKeyCount", img, nil)
		if err != nil || count == nil || !count.Hit {
			return cost, false
		}
		cost.CustomLockKeys, err = strconv.Atoi(strings.TrimSpace(firstRawOCRText(count)))
		if err != nil || cost.CustomLockKeys <= 0 {
			return cost, false
		}
	}
	cost.RerollModules = RerollModuleCost(countLocks(scan))
	if cost.CustomModules < cost.RerollModules {
		return cost, false
	}
	cost.LockModules = cost.CustomModules - cost.RerollModules
	return cost, true
}

// EquipmentRerollCostRecognition 从确认页读取模组与密钥总费用。
type EquipmentRerollCostRecognition struct{}

var _ maa.CustomRecognitionRunner = &EquipmentRerollCostRecognition{}

func (r *EquipmentRerollCostRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil || arg.Img == nil {
		return nil, false
	}
	part, ok := currentEffectPart(arg.TaskID)
	if !ok {
		return nil, false
	}
	scan, ok := GetPartScan(arg.TaskID, part)
	if !ok {
		return nil, false
	}
	cost, ok := readRerollCost(ctx, arg.Img, scan)
	if !ok {
		return nil, false
	}
	data, err := json.Marshal(cost)
	if err != nil {
		return nil, false
	}
	return &maa.CustomRecognitionResult{Box: maa.Rect{0, 0, 1, 1}, Detail: string(data)}, true
}

// costGuardOutcome 是效果变更费用校验的结论。
type costGuardOutcome int

const (
	// costGuardProceed 余额足够，或库存尚未读到：继续执行本轮效果变更。
	costGuardProceed costGuardOutcome = iota
	// costGuardInsufficient 明确读到余额不足：结束任务。
	costGuardInsufficient
)

// guardRerollCost 判断本轮费用能否支付。
//
// 关键区别：**未读到库存不等于库存不足**。本轮无需锁定时流程不经过效果锁定页，
// 库存可能始终未知；那种情况先放行并由游戏侧校验。若确认后没有进入结果页，
// Pipeline 的有界重试会显式失败并提示用户，不再静默结束。
func guardRerollCost(taskID int64, cost MaterialUsage) costGuardOutcome {
	if modules, known := getModulesHeld(taskID); known && modules < cost.CustomModules {
		return costGuardInsufficient
	}
	if keys, known := getKeysHeld(taskID); known && keys < cost.CustomLockKeys {
		return costGuardInsufficient
	}
	return costGuardProceed
}

// EquipmentRerollPrepareRerollCostAction 校验费用并暂存。
// 只有明确读到余额不足才结束任务；未读到库存时照常继续，交由游戏侧和 Pipeline 失败分支兜底。
type EquipmentRerollPrepareRerollCostAction struct{}

var _ maa.CustomActionRunner = &EquipmentRerollPrepareRerollCostAction{}

func (a *EquipmentRerollPrepareRerollCostAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		return false
	}
	clearPendingRerollCost(arg.TaskID)
	var cost MaterialUsage
	if err := json.Unmarshal([]byte(customRecognitionDetail(arg)), &cost); err != nil || cost.CustomModules <= 0 || cost.CustomLockKeys < 0 {
		log.Error().Err(err).Msg("invalid effect change cost recognition")
		return false
	}
	modules, modulesKnown := getModulesHeld(arg.TaskID)
	if loadCarrierConfig(ctx).isValue() {
		plan, ok := currentValuePlan(arg.TaskID)
		if !ok || cost.CustomModules != plan.FirstModules || cost.CustomLockKeys != plan.FirstKeys {
			log.Error().Interface("plan", plan).Interface("actual_cost", cost).Msg("value cost differs from frozen plan; refusing consumption")
			return false
		}
	}
	keys, keysKnown := getKeysHeld(arg.TaskID)
	if guardRerollCost(arg.TaskID, cost) == costGuardInsufficient {
		var reason string
		if modulesKnown && modules < cost.CustomModules {
			reason = fmt.Sprintf("订制模块不足（持有 %d，需消耗 %d）", modules, cost.CustomModules)
		} else if keysKnown && keys < cost.CustomLockKeys {
			reason = fmt.Sprintf("自订密钥不足（持有 %d，需消耗 %d）", keys, cost.CustomLockKeys)
		} else {
			reason = "洗词条材料不足"
		}
		setAbortReason(arg.TaskID, reason)
		maafocus.Print(ctx, fmt.Sprintf("【洗词条结束】%s", reason))
		log.Warn().
			Str("component", "EquipmentReroll").
			Int64("task_id", arg.TaskID).
			Bool("modules_known", modulesKnown).
			Int("modules_held", modules).
			Bool("keys_known", keysKnown).
			Int("keys_held", keys).
			Interface("cost", cost).
			Str("reason", reason).
			Msg("insufficient materials for effect change")
		if err := routeEquipmentRerollEnd(ctx, arg.CurrentTaskName); err != nil {
			log.Error().Err(err).Str("component", "EquipmentReroll").Msg("failed to route end for insufficient materials")
			return false
		}
		return true
	}
	if !modulesKnown {
		log.Warn().
			Str("component", "EquipmentReroll").
			Int64("task_id", arg.TaskID).
			Interface("cost", cost).
			Msg("material inventory unknown; proceeding and letting the game enforce the cost")
	}
	resetTaskRetryGates(arg.TaskID)
	setPendingRerollCost(arg.TaskID, cost)
	log.Info().Interface("cost", cost).Msg("prepared effect change total cost")
	return true
}

// EquipmentRerollRecordRerollCostAction 仅在结果页已出现时记账一次。
type EquipmentRerollRecordRerollCostAction struct{}

var _ maa.CustomActionRunner = &EquipmentRerollRecordRerollCostAction{}

func (a *EquipmentRerollRecordRerollCostAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		return false
	}
	if !commitPendingRerollCost(arg.TaskID) {
		log.Error().Msg("effect change result has no pending cost; refusing to guess or charge twice")
		return false
	}
	return true
}
