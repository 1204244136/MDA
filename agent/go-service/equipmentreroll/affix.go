package equipmentreroll

import (
	"image"
	"strings"
	"unicode"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

// readEquipmentAffixName 仅确认完整名称；详情页数值由专用 Pipeline 节点读取与校准。
// 名称区域附带的数字不能否定名称，也不能替代专用节点的数值证据。
func readEquipmentAffixName(ctx *maa.Context, img image.Image, direct, detected string) (string, string, bool) {
	if ctx == nil || img == nil {
		return "", "", false
	}
	effect, raw, known := "", "", false
	for _, node := range []string{direct, detected} {
		detail, err := ctx.RunRecognition(node, img, nil)
		if err != nil {
			return "", raw, false
		}
		results := filteredAffixOCR(detail)
		candidate, text, names, conflicting := recognizeAffixNameOCR(results)
		if conflicting {
			return "", text, false
		}
		if len(names) == 0 {
			if !known && raw == "" {
				raw = text
			}
			continue
		}
		if candidate == "" {
			// 空槽不能携带百分比或数字，仍须检查同槽的矛盾证据。
			_, text, valid, conflicting := recognizeAffixOCR(results)
			if !valid || conflicting || strings.IndexFunc(text, unicode.IsDigit) >= 0 {
				return "", text, false
			}
		}
		if known && candidate != effect {
			return "", raw, false
		}
		if !known {
			raw = text
		}
		effect, known = candidate, true
	}
	return effect, raw, known
}

// affixReading 累积 readEquipmentAffixResult 在两条 OCR 路径之间的读取状态。
// effect 是已确认的官方词条名（空串表示槽位为空），raw 是对外展示的 OCR 原文，
// known 表示已有至少一条读数确认过该槽位。
type affixReading struct {
	effect string
	raw    string
	known  bool
}

// merge 并入一条 OCR 读数；ok=false 表示两条路径给出无法调和的证据，本次识别必须失败重试。
//
// 数值不一致时先按官方档位表仲裁：整行识别（only_rec）会吞掉小字号的小数位，同一帧
// 可能读出 "6.6%" 与 "6.64%"、"11%" 与 "11.11%" 这类只差精度的结果（见
// MDA-logs-v1.12.2-20261101-120023）。取校准值继续，只有落到**不同**官方档位才是
// 真冲突（另一个合法数值），此时仍拒绝决策。
func (r affixReading) merge(candidate, text string, recognized, conflicting bool) (affixReading, bool) {
	if conflicting || isTransientUnlockText(text) {
		r.raw = text
		return r, false
	}
	if !recognized {
		if !r.known && r.raw == "" {
			r.raw = text
		}
		return r, true
	}
	if r.known && candidate != r.effect {
		return r, false
	}
	if previous, value := extractPercentValue(r.raw), extractPercentValue(text); r.known && previous != "" && value != "" && previous != value {
		reconciled, ok := reconcileAffixValue(r.effect, previous, value)
		if !ok {
			return r, false
		}
		r.raw = strings.Replace(r.raw, previous, reconciled, 1)
	}
	if !r.known || (extractPercentValue(r.raw) == "" && extractPercentValue(text) != "") {
		r.raw = text
	}
	r.effect, r.known = candidate, true
	return r, true
}

// reconcileAffixValue 用官方档位表仲裁两条 OCR 路径的数值差异，返回应采用（已校准）的数值。
// 两条读数落到同一档位、或只有一条能落到档位时视为同源读数；落到不同档位说明是另一个
// 合法数值，属真冲突，返回 ok=false 交由调用方拒绝决策。
func reconcileAffixValue(effect, a, b string) (string, bool) {
	tierA, calibratedA, okA := resolveEffectTier(effect, a)
	tierB, calibratedB, okB := resolveEffectTier(effect, b)
	switch {
	case okA && okB:
		if tierA != tierB {
			return "", false
		}
		return calibratedA, true
	case okA:
		return calibratedA, true
	case okB:
		return calibratedB, true
	default:
		return "", false
	}
}

// readEquipmentAffixResult 在同一截图上读取结果页名称与数值，任一冲突都拒绝决策。
// 两条路径共用 OCR 模型，不把结果一致当作正确性证明。
func readEquipmentAffixResult(ctx *maa.Context, img image.Image, direct, detected string) (string, string, bool) {
	if ctx == nil || img == nil {
		return "", "", false
	}
	var state affixReading
	for _, node := range []string{direct, detected} {
		detail, err := ctx.RunRecognition(node, img, nil)
		if err != nil {
			return "", state.raw, false
		}
		candidate, text, recognized, conflicting := firstRecognizedEffect(detail)
		next, ok := state.merge(candidate, text, recognized, conflicting)
		if !ok {
			return "", next.raw, false
		}
		state = next
	}
	// 明确空槽不能同时带有数值。
	if state.known && state.effect == "" && strings.IndexFunc(state.raw, unicode.IsDigit) >= 0 {
		return "", state.raw, false
	}
	return state.effect, state.raw, state.known
}

// recognizeAffixOCR 解释单槽内已通过框架过滤的 OCR，名称和百分比允许分框。
// 只关联完整名称右侧同一行的独立百分比，不借用残缺名称路径中的数值。
func recognizeAffixOCR(results []*maa.OCRResult) (string, string, bool, bool) {
	effect, raw, names, conflicting := recognizeAffixNameOCR(results)
	if conflicting || len(names) == 0 {
		return "", raw, false, conflicting
	}

	value := ""
	for _, ocr := range results {
		candidate, recognized := normalizeEffect(ocr.Text)
		belongs := (recognized && candidate == effect) || (effect == "" && isUnobtainedEffect(ocr.Text))
		if !belongs && strings.TrimSpace(ocr.Text) == extractPercentValue(ocr.Text) {
			for _, name := range names {
				if affixValueFollowsName(name.Box, ocr.Box) {
					belongs = true
					break
				}
			}
		}
		if !belongs {
			continue
		}
		for _, percent := range percentValuePattern.FindAllString(ocr.Text, -1) {
			if effect == "" || (value != "" && value != percent) {
				return "", raw, false, true
			}
			value = percent
		}
	}
	if value != "" && extractPercentValue(raw) == "" {
		raw += " " + value
	}
	return effect, raw, true, false
}

// recognizeAffixNameOCR 同时供详情页和结果页使用，只判断名称及页面矛盾。
// 返回的名称框供结果页关联百分比；数字冲突不在此处决定名称有效性。
func recognizeAffixNameOCR(results []*maa.OCRResult) (string, string, []*maa.OCRResult, bool) {
	effect, raw, known := "", "", false
	var names []*maa.OCRResult
	for _, ocr := range results {
		if raw == "" {
			raw = strings.Join(strings.Fields(ocr.Text), " ")
		}
		if isTransientUnlockText(ocr.Text) || len(matchingEffectNames(ocr.Text)) > 1 {
			return "", ocr.Text, nil, true
		}
		candidate, recognized := normalizeEffect(ocr.Text)
		empty := isUnobtainedEffect(ocr.Text)
		if !recognized && !empty {
			continue
		}
		if (recognized && empty) || (known && candidate != effect) {
			return "", raw, nil, true
		}
		if !known || extractPercentValue(raw) == "" {
			raw = ocr.Text
		}
		effect, known = candidate, true
		names = append(names, ocr)
	}
	return effect, raw, names, false
}

func affixValueFollowsName(name, value maa.Rect) bool {
	if name[2] <= 0 || name[3] <= 0 || value[2] <= 0 || value[3] <= 0 || value[0] < name[0]+name[2] {
		return false
	}
	// 两个框的垂直中心须互相落在对方高度内，避免关联相邻槽位的数值。
	nameCenter, valueCenter := 2*name[1]+name[3], 2*value[1]+value[3]
	return nameCenter >= 2*value[1] && nameCenter < 2*(value[1]+value[3]) &&
		valueCenter >= 2*name[1] && valueCenter < 2*(name[1]+name[3])
}
