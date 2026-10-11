package crosschapter

import (
	"fmt"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"sync"
	"time"
)

// 困难连败降级：同一任务内战败计数达到 2 次，视为当前难度打不过，
// 打开章节列表切到普通页签，从"正在执行作战"的章节继续推图。
// 计数按任务隔离，重开任务自动清零；5 秒内重复命中只记一次。

type failState struct {
	fails     int
	lastFail  time.Time
	lastClick time.Time
	lastBox   maa.Rect
	hadClick  bool
	nodeFails int
	failBox   maa.Rect
}

var (
	failMu     sync.Mutex
	failByTask = map[int64]*failState{}
	// 被判"打不过"的节点位置：战败 2 次的点击框，找怪时跳过其附近。
	exclByTask = map[int64][]maa.Rect{}
)

const (
	fallbackFailThreshold = 2
	battleNodeFailLimit   = 2
)

func recordMonsterClick(task int64, box maa.Rect) {
	failMu.Lock()
	st := failByTask[task]
	if st == nil {
		st = &failState{}
		failByTask[task] = st
	}
	st.lastBox, st.hadClick, st.lastClick = box, true, time.Now()
	failMu.Unlock()
}

func battleNodeExclusions(task int64) []maa.Rect {
	failMu.Lock()
	defer failMu.Unlock()
	return append([]maa.Rect(nil), exclByTask[task]...)
}

func clearBattleNodeExclusions(task int64) {
	failMu.Lock()
	delete(exclByTask, task)
	failMu.Unlock()
}

// Screen rectangles cannot identify the same node after a camera change.
func invalidateBattlePositions(task int64) {
	failMu.Lock()
	delete(exclByTask, task)
	if st := failByTask[task]; st != nil {
		st.lastBox, st.failBox = maa.Rect{}, maa.Rect{}
		st.hadClick, st.nodeFails = false, 0
	}
	failMu.Unlock()
}

type BattleFailCountAction struct{}

func (*BattleFailCountAction) Run(_ *maa.Context, arg *maa.CustomActionArg) bool {
	if arg == nil {
		return false
	}
	failMu.Lock()
	st := failByTask[arg.TaskID]
	if st == nil {
		st = &failState{}
		failByTask[arg.TaskID] = st
	}
	now := time.Now()
	if !st.lastFail.IsZero() && now.Sub(st.lastFail) <= 5*time.Second {
		failMu.Unlock()
		return true
	}
	st.fails++
	// 归因到最近一次点怪的框：同框战败累计到上限就把该节点拉黑，
	// 找怪跳过它去清其余目标，避免打不过的怪被无限重试。
	excluded := ""
	if st.hadClick && now.Sub(st.lastClick) < 3*time.Minute {
		if nearRect(st.lastBox, st.failBox) {
			st.nodeFails++
		} else {
			st.nodeFails, st.failBox = 1, st.lastBox
		}
		if st.nodeFails >= battleNodeFailLimit {
			exclByTask[arg.TaskID] = append(exclByTask[arg.TaskID], st.failBox)
			excluded = fmt.Sprintf("%v", st.failBox)
			st.nodeFails, st.failBox = 0, maa.Rect{}
		}
	} else if !nearRect(st.lastBox, st.failBox) {
		st.nodeFails, st.failBox = 0, maa.Rect{}
	}
	st.lastFail = now
	n := st.fails
	msg := map[string]any{"fails": n}
	if excluded != "" {
		msg["node_excluded_box"] = excluded
	}
	failMu.Unlock()
	recordMapEvent(arg.TaskID, "battle_fail_counted", msg)
	return true
}

func nearRect(a, b maa.Rect) bool {
	if a == (maa.Rect{}) || b == (maa.Rect{}) {
		return false
	}
	ax, ay := a[0]+a[2]/2, a[1]+a[3]/2
	bx, by := b[0]+b[2]/2, b[1]+b[3]/2
	dx, dy := ax-bx, ay-by
	return dx*dx+dy*dy < 120*120
}

type HardFallbackReady struct{}

func (*HardFallbackReady) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil || arg.Img == nil || !mapSurface(ctx, arg.Img) {
		return nil, false
	}
	failMu.Lock()
	st := failByTask[arg.TaskID]
	armed := st != nil && st.fails >= fallbackFailThreshold
	failMu.Unlock()
	if !armed {
		return nil, false
	}
	if recognize(ctx, arg.Img, "CrossChapterHardMap") == nil {
		return nil, false
	}
	return &maa.CustomRecognitionResult{Box: maa.Rect{0, 0, 1280, 720}, Detail: "hard_fail_fallback_armed"}, true
}

type HardFallbackArmAction struct{}

func (*HardFallbackArmAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		return false
	}
	failMu.Lock()
	st := failByTask[arg.TaskID]
	if st == nil {
		st = &failState{}
		failByTask[arg.TaskID] = st
	}
	st.fails = 0
	delete(exclByTask, arg.TaskID)
	failMu.Unlock()
	sweepMu.Lock()
	delete(sweepByTask, arg.TaskID)
	sweepMu.Unlock()
	d, err := ctx.RunAction("CrossChapterOpenListClick", maa.Rect{1104, 668, 132, 26}, "")
	ok := err == nil && d != nil && d.Success
	recordMapEvent(arg.TaskID, "hard_fallback_started", map[string]any{"list_open_clicked": ok})
	return ok
}
