package crosschapter

import (
	"strconv"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

type chapterCard struct {
	number int
	box    maa.Rect
	locked bool
}

// The broad detector may see a faint chapter label with low confidence.
// It only proposes geometry; a narrow recognition must then read the number.
func fallbackCards(ctx *maa.Context, arg *maa.CustomRecognitionArg) []chapterCard {
	d, e := ctx.RunRecognition("CrossChapterCardNumber", arg.Img)
	if e != nil || d == nil || d.Results == nil {
		return nil
	}
	var cards []chapterCard
	for _, r := range d.Results.All {
		o, ok := r.AsOCR()
		if !ok || !numberRE.MatchString(strings.TrimSpace(o.Text)) {
			continue
		}
		x := o.Box[0]
		if !((x >= 455 && x < 478) || (x >= 680 && x < 699)) || o.Box[2] > 24 {
			continue
		}
		n, _ := strconv.Atoi(strings.TrimSpace(o.Text))
		if o.Score < 0.8 {
			over := map[string]any{"CrossChapterCardNumberLocal": map[string]any{"recognition": map[string]any{"param": map[string]any{"roi": maa.Rect{x - 6, o.Box[1] - 7, 18, 27}}}}}
			n = ocrNumber(recognize(ctx, arg.Img, "CrossChapterCardNumberLocal", over), false)
		}
		if n < 1 {
			continue
		}
		duplicate := false
		for _, c := range cards {
			if c.number == n && abs(c.box[0]-x) < 10 && abs(c.box[1]-o.Box[1]) < 10 {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		cards = append(cards, chapterCard{n, o.Box, recognize(ctx, arg.Img, "CrossChapterCardLock", offsetROI(ctx, "CrossChapterCardLock", o.Box)) != nil})
	}
	// Locked labels are often too faint for broad text detection. A real
	// padlock supplies card geometry, then narrow OCR still reads its number.
	if locks := recognize(ctx, arg.Img, "CrossChapterListLocks"); locks != nil && locks.Results != nil {
		for _, r := range locks.Results.Filtered {
			lock, ok := r.AsTemplateMatch()
			if !ok {
				continue
			}
			x := 457
			if lock.Box[0] > 640 {
				x = 679
			}
			roi := maa.Rect{x, lock.Box[1] - 12, 18, 27}
			over := map[string]any{"CrossChapterCardNumberLocal": map[string]any{"recognition": map[string]any{"param": map[string]any{"roi": roi}}}}
			n := ocrNumber(recognize(ctx, arg.Img, "CrossChapterCardNumberLocal", over), false)
			if n > 0 {
				present := false
				for _, c := range cards {
					if c.number == n && c.locked {
						present = true
						break
					}
				}
				if !present {
					cards = append(cards, chapterCard{n, roi, true})
				}
			}
		}
	}
	return cards
}

func latestUnlockedCard(cards []chapterCard) (chapterCard, bool) {
	var selected chapterCard
	count := 0
	for _, c := range cards {
		if c.locked {
			continue
		}
		if c.number > selected.number {
			selected, count = c, 1
		} else if c.number == selected.number {
			count++
		}
	}
	if count != 1 {
		return chapterCard{}, false
	}
	for _, c := range cards {
		if c.locked && c.number == selected.number+1 {
			return selected, true
		}
	}
	return chapterCard{}, false
}

type NormalFallbackChapterRecognition struct{}

func (*NormalFallbackChapterRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil || arg.Img == nil || recognize(ctx, arg.Img, "CrossChapterListTitle") == nil || recognize(ctx, arg.Img, "CrossChapterNormalList") == nil {
		return nil, false
	}
	cards := fallbackCards(ctx, arg)
	c, ok := latestUnlockedCard(cards)
	if !ok {
		return nil, false
	}
	return &maa.CustomRecognitionResult{Box: c.box, Detail: "normal_latest_unlocked:" + strconv.Itoa(c.number)}, true
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
