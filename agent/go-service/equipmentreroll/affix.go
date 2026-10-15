package equipmentreroll

import (
	"image"
	"strings"
	"unicode"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

// readEquipmentAffix 在同一截图上读取直接识别和检测识别的完整词条，冲突时拒绝猜测。
// 两条路径共用 OCR 模型，不把结果一致当作正确性证明。
func readEquipmentAffix(ctx *maa.Context, img image.Image, direct, detected string) (string, string, bool) {
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
