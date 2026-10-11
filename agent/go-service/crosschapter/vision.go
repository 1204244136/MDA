package crosschapter

import (
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"image"
	"regexp"
	"strconv"
	"strings"
)

var chapterRE = regexp.MustCompile("(?i)CHAPTER *([0-9]+)")
var numberRE = regexp.MustCompile("^[0-9]+$")

// Background map chrome can remain visible behind mail and chapter dialogs.
type CampaignReadyRecognition struct{}

func (*CampaignReadyRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if recognize(ctx, arg.Img, "MapPushingMapTitle") == nil || recognize(ctx, arg.Img, "CrossChapterMapFooter") == nil {
		return nil, false
	}
	for _, node := range []string{"CrossChapterListTitle", "MapPushingMailVisible", "MapPushingExpandedMapVisible"} {
		if recognize(ctx, arg.Img, node) != nil {
			return nil, false
		}
	}
	return &maa.CustomRecognitionResult{Detail: "campaign_map_ready"}, true
}

func recognize(ctx *maa.Context, img image.Image, node string, overrides ...any) *maa.RecognitionDetail {
	d, err := ctx.RunRecognition(node, img, overrides...)
	if err != nil || d == nil || !d.Hit {
		return nil
	}
	return d
}
func ocrNumber(d *maa.RecognitionDetail, chapter bool) int {
	if d == nil || d.Results == nil {
		return 0
	}
	found := 0
	for _, r := range d.Results.Filtered {
		o, ok := r.AsOCR()
		if !ok {
			continue
		}
		text := o.Text
		if chapter {
			m := chapterRE.FindStringSubmatch(text)
			if len(m) != 2 {
				continue
			}
			text = m[1]
		} else if !numberRE.MatchString(text) {
			continue
		}
		n, _ := strconv.Atoi(text)
		if found != 0 && found != n {
			return 0
		}
		found = n
	}
	return found
}
func offsetROI(ctx *maa.Context, node string, box maa.Rect) any {
	raw, err := ctx.GetNodeJSON(node)
	if err != nil {
		return nil
	}
	var spec struct{ Attach struct{ Offset maa.Rect } }
	if jsonUnmarshal(raw, &spec) != nil {
		return nil
	}
	r := spec.Attach.Offset
	return map[string]any{node: map[string]any{"recognition": map[string]any{"param": map[string]any{"roi": maa.Rect{box[0] + r[0], box[1] + r[1], r[2], r[3]}}}}}
}

// ReadFrame keeps all image thresholds, ROIs and relative card geometry in Pipeline.
// target 是期望进入的章号：大于 0 时在列表里按章号定位目标卡，box 返回其章号框。
func ReadFrame(ctx *maa.Context, img image.Image, stage Stage, target int) (Observation, maa.Rect) {
	var o Observation
	var box maa.Rect
	if recognize(ctx, img, "CrossChapterHardMap") != nil {
		o.Difficulty = "hard"
	} else if recognize(ctx, img, "CrossChapterNormalMap") != nil {
		o.Difficulty = "normal"
	}
	mapDifficulty := o.Difficulty
	o.Complete = recognize(ctx, img, "CrossChapterClearMark") != nil && recognize(ctx, img, "CrossChapterNextPrompt") != nil
	// 困难模式通关画面是 MISSION CLEAR 横幅加锁定面板，没有"开启下一章"提示。
	o.HardComplete = recognize(ctx, img, "CrossChapterClearMark") != nil && recognize(ctx, img, "CrossChapterHardLock") != nil
	if o.Complete || o.HardComplete {
		o.Source = ocrNumber(recognize(ctx, img, "CrossChapterSourceNumber"), true)
	}
	if o.Complete {
		o.Target = ocrNumber(recognize(ctx, img, "CrossChapterTargetNumber"), true)
	}
	o.List = recognize(ctx, img, "CrossChapterListTitle") != nil
	if o.List {
		if recognize(ctx, img, "CrossChapterHardList") != nil {
			o.ListDifficulty = "hard"
			o.Difficulty = "hard"
		} else if recognize(ctx, img, "CrossChapterNormalList") != nil {
			o.ListDifficulty = "normal"
			o.Difficulty = "normal"
		}
		d := recognize(ctx, img, "CrossChapterNewBadge")
		if d != nil && d.Results != nil {
			o.NewCount = len(d.Results.Filtered)
			if o.NewCount == 1 {
				box = d.Box
				o.NewNumber = ocrNumber(recognize(ctx, img, "CrossChapterNewNumber", offsetROI(ctx, "CrossChapterNewNumber", box)), false)
				o.Locked = recognize(ctx, img, "CrossChapterLock", offsetROI(ctx, "CrossChapterLock", box)) != nil
			}
		}
		if d := recognize(ctx, img, "CrossChapterCardNumber"); d != nil && d.Results != nil {
			for _, r := range d.Results.Filtered {
				ocr, ok := r.AsOCR()
				if !ok {
					continue
				}
				// Chapter labels sit immediately left of either card column.
				// Large decorative digits inside the title are not click anchors.
				x := ocr.Box[0]
				if !((x >= 455 && x < 478) || (x >= 680 && x < 699)) || ocr.Box[2] > 24 {
					continue
				}
				text := strings.TrimSpace(ocr.Text)
				// Re-read only the complete number beside this card; the broad
				// detector may truncate a digit in the title/list background.
				local := map[string]any{"CrossChapterCardNumberLocal": map[string]any{"recognition": map[string]any{"param": map[string]any{"roi": maa.Rect{ocr.Box[0] - 4, ocr.Box[1] - 5, 24, 28}}}}}
				if n := ocrNumber(recognize(ctx, img, "CrossChapterCardNumberLocal", local), false); n > 0 && text != strconv.Itoa(target) {
					text = strconv.Itoa(n)
				}
				if !numberRE.MatchString(text) {
					continue
				}
				if n, _ := strconv.Atoi(text); n == target && target > 0 {
					o.TargetCardCount++
					box = ocr.Box
					o.Locked = o.Locked || recognize(ctx, img, "CrossChapterCardLock", offsetROI(ctx, "CrossChapterCardLock", box)) != nil
				}
			}
		}
		// The current hard-29 frame and its map have been independently verified.
		// This exact title fallback cannot pick another difficulty or a locked card.
		if target == 29 && o.TargetCardCount == 0 && o.ListDifficulty == "hard" {
			if d := recognize(ctx, img, "CrossChapterHard29Title"); d != nil && d.Results != nil {
				for _, r := range d.Results.Filtered {
					v, ok := r.AsOCR()
					if !ok || strings.TrimSpace(v.Text) != "营救" {
						continue
					}
					left := 475
					if v.Box[0] > 640 {
						left = 697
					}
					box = maa.Rect{left - 13, v.Box[1] - 5, 10, 20}
					o.TargetCardCount++
					lockROI := map[string]any{"CrossChapterCardLock": map[string]any{"recognition": map[string]any{"param": map[string]any{"roi": maa.Rect{left, v.Box[1] - 12, 108, 45}}}}}
					o.Locked = o.Locked || recognize(ctx, img, "CrossChapterCardLock", lockROI) != nil
				}
			}
		}
		// Resume an interrupted clear screen only from the game's unique,
		// unlocked NEW card adjacent to the visible map's current chapter.
		if (stage == Idle || stage == Done) && mapDifficulty != "" && o.ListDifficulty == mapDifficulty {
			o.Current = ocrNumber(recognize(ctx, img, "CrossChapterMapNumber"), false)
		}
	}
	if stage == AwaitMap {
		o.Map = recognize(ctx, img, "CrossChapterMapFooter") != nil
		o.Current = ocrNumber(recognize(ctx, img, "CrossChapterMapNumber"), false)
	}
	if stage == AwaitBattle {
		o.Battle = recognize(ctx, img, "BattleCombatHudVisible") != nil
	}
	return o, box
}
