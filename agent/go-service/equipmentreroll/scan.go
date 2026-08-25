package equipmentreroll

import (
	"encoding/json"
	"fmt"
	"image"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// 本文件实现"洗词条"全量扫描的 Go 业务层：
// 槽位定位、词条/数值 OCR、锁定颜色判定全部由 Pipeline 完成，
// Go 只负责按槽位复用 Pipeline 子识别节点、解释结果、写入快照并记录日志。

// scanSlotParam 是 EquipmentRerollScanSlotRecognition 的参数。
type scanSlotParam struct {
	Slot   int  `json:"slot"`
	IsLast bool `json:"is_last"`
}

// slotScanResult 单个槽位扫描得到的词条 / 数值 / 锁定 / 档位。
type slotScanResult struct {
	Effect    string // 规范化后的官方效果名称（空 = 空槽 / 未获得效果）
	Value     string // 数值（OCR 校准后，精确到档位表数值）
	Tier      int    // 效果数值档位（1~15，来自 mapping；0=未确认）
	Lock      SlotLock
	RawEffect string // 词条区域 OCR 原文
	RawValue  string // 数值区域 OCR 原文
}

// EquipmentRerollScanSlotRecognition 全量扫描单个槽位三要素（词条 / 数值 / 锁定）。
// 它复用 Pipeline 定义的子识别节点：
//   - __EquipmentRerollSlotN{AffixOCR,ValueOCR,LockBlue,LockOrange}
//
// Go 只做结果解释与业务记录，不承载识别参数。
type EquipmentRerollScanSlotRecognition struct{}

var _ maa.CustomRecognitionRunner = &EquipmentRerollScanSlotRecognition{}

func (r *EquipmentRerollScanSlotRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil || arg.Img == nil {
		log.Error().Str("component", "EquipmentReroll").Msg("invalid slot scan context")
		return nil, false
	}

	var params scanSlotParam
	if err := json.Unmarshal([]byte(arg.CustomRecognitionParam), &params); err != nil {
		log.Error().Err(err).Str("component", "EquipmentReroll").Msg("failed to parse slot scan param")
		return nil, false
	}
	if params.Slot < minSlot || params.Slot > maxSlot {
		log.Error().Str("component", "EquipmentReroll").Int("slot", params.Slot).Msg("invalid slot scan index")
		return nil, false
	}

	part, ok := currentEffectPart(arg.TaskID)
	if !ok {
		log.Error().Str("component", "EquipmentReroll").Int64("task_id", arg.TaskID).Msg("slot scan part is not initialized")
		return nil, false
	}

	scan := scanSlotByPipeline(ctx, arg.Img, params.Slot)

	if isTransientUnlockText(scan.RawEffect) {
		log.Warn().
			Str("component", "EquipmentReroll").
			Int64("task_id", arg.TaskID).
			Str("part", part).
			Int("slot", params.Slot).
			Str("raw_effect", scan.RawEffect).
			Msg("slot is in transient unlock state; retry scan")
		return nil, false
	}

	if _, err := recordEffect(arg.TaskID, recordEffectParam{
		Slot:   params.Slot,
		Part:   part,
		IsLast: params.IsLast,
		Value:  scan.Value,
		Lock:   scan.Lock,
	}, scan.Effect); err != nil {
		log.Error().
			Err(err).
			Str("component", "EquipmentReroll").
			Int64("task_id", arg.TaskID).
			Int("slot", params.Slot).
			Msg("equipment slot scan state is incomplete")
		return nil, false
	}

	// 结构化识别 detail 和日志用于诊断；用户可见摘要由 EquipmentRerollScanRouteAction 通过 focus 输出。
	log.Debug().
		Str("component", "EquipmentReroll").
		Int64("task_id", arg.TaskID).
		Str("part", part).
		Int("slot", params.Slot).
		Str("effect", scan.Effect).
		Str("value", valueTierDisplay(scan.Value, scan.Tier)). // 带档位显示（前端/用户）
		Int("tier", scan.Tier).
		Str("value_tier", valueTierDisplay(scan.Value, scan.Tier)).
		Str("lock", scan.Lock.String()).
		Str("raw_effect", scan.RawEffect).
		Str("raw_value", scan.RawValue).
		Msg("equipment slot scanned")

	// 把“带档位的数值”作为结构化识别 detail 返回，供 maafw.log 诊断。
	return &maa.CustomRecognitionResult{Box: arg.Roi, Detail: buildScanSlotDetail(part, params.Slot, scan)}, true
}

// buildScanSlotDetail 构造槽位扫描的诊断 detail JSON（含档位）。
// value 字段带档位（如 11.81%（T11）），raw_value 保留 OCR 原值。
func buildScanSlotDetail(part string, slot int, scan slotScanResult) string {
	display := valueTierDisplay(scan.Value, scan.Tier)
	msg := "空槽位"
	if scan.Effect != "" {
		msg = fmt.Sprintf("%s %s", scan.Effect, display)
	}
	detail := map[string]any{
		"part":       part,
		"slot":       slot,
		"effect":     scan.Effect,
		"value":      display,
		"tier":       scan.Tier,
		"value_tier": display,
		"raw_value":  scan.RawValue,
		"lock":       scan.Lock.String(),
		"message":    msg,
	}
	b, _ := json.Marshal(detail)
	return string(b)
}

// scanSlotByPipeline 复用 Pipeline 子识别节点读取一个槽位的词条 / 数值 / 锁定。
// 识别参数（ROI、ColorMatch 颜色区间、count 阈值）都在 Pipeline JSON 中维护。
func scanSlotByPipeline(ctx *maa.Context, img image.Image, slot int) slotScanResult {
	var result slotScanResult
	if ctx == nil || img == nil {
		return result
	}

	affixDetail, err := ctx.RunRecognition(fmt.Sprintf("__EquipmentRerollSlot%dAffixOCR", slot), img, nil)
	if err == nil && affixDetail != nil {
		result.Effect, result.RawEffect, _ = firstRecognizedEffect(affixDetail)
	}

	valueDetail, err := ctx.RunRecognition(fmt.Sprintf("__EquipmentRerollSlot%dValueOCR", slot), img, nil)
	if err == nil && valueDetail != nil {
		result.Value = firstRawOCRText(valueDetail)
		result.RawValue = result.Value
		// 档位校准：用映射表修正 OCR 数值（输出精确档位值）。
		if tier, calibrated, ok := resolveEffectTier(result.Effect, result.Value); ok {
			result.Value = calibrated
			result.Tier = tier
		}
	}

	blueDetail, err := ctx.RunRecognition(fmt.Sprintf("__EquipmentRerollSlot%dLockBlue", slot), img, nil)
	if err == nil && blueDetail != nil && blueDetail.Hit {
		result.Lock = LockPermanent
		return result
	}

	orangeDetail, err := ctx.RunRecognition(fmt.Sprintf("__EquipmentRerollSlot%dLockOrange", slot), img, nil)
	if err == nil && orangeDetail != nil && orangeDetail.Hit {
		result.Lock = LockOneTime
		return result
	}

	result.Lock = LockNone
	return result
}

// valueTierDisplay 返回 "11.81%（T11）" 形式的展示串（无档位时只返回数值）。
func valueTierDisplay(value string, tier int) string {
	if tier <= 0 {
		return value
	}
	return fmt.Sprintf("%s（T%d）", value, tier)
}
