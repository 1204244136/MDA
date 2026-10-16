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

// readEquipmentAffixResult 在同一截图上读取结果页名称与数值，任一冲突都拒绝决策。
// 两条路径共用 OCR 模型，不把结果一致当作正确性证明。
func readEquipmentAffixResult(ctx *maa.Context, img image.Image, direct, detected string) (string, string, bool) {
	if ctx == nil || img == nil {
		return "", "", false
	}
	effect, known := "", false
	raw := ""
	for _, node := range []string{direct, detected} {
		detail, err := ctx.RunRecognition(node, img, nil)
		if err != nil {
			return "", raw, false
		}
		candidate, text, recognized, conflicting := firstRecognizedEffect(detail)
		if conflicting || isTransientUnlockText(text) {
			return "", text, false
		}
		if !recognized {
			if !known && raw == "" {
				raw = text
			}
			continue
		}
		if known && candidate != effect {
			return "", raw, false
		}
		if previous, value := extractPercentValue(raw), extractPercentValue(text); known && previous != "" && value != "" && previous != value {
			return "", raw, false
		}
		if !known || (extractPercentValue(raw) == "" && extractPercentValue(text) != "") {
			raw = text
		}
		effect, known = candidate, true
	}
	// 明确空槽不能同时带有数值。
	if known && effect == "" {
		if strings.IndexFunc(raw, unicode.IsDigit) >= 0 {
			return "", raw, false
		}
	}
	return effect, raw, known
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
