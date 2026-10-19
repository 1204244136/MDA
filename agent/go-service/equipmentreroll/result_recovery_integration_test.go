package equipmentreroll

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

// Exercise the real framework's timeout/on_error semantics with inert screenshots
// and synthetic OCR availability. No device is connected and clicks are rejected.
func TestEquipmentResultRecoveryOffline(t *testing.T) {
	lib := os.Getenv("MDA_MAA_LIB")
	if lib == "" {
		t.Skip("set MDA_MAA_LIB for offline pipeline recovery")
	}
	if err := maa.Init(maa.WithLibDir(lib), maa.WithJSONEncoder(json.Marshal), maa.WithJSONDecoder(json.Unmarshal)); err != nil {
		t.Fatal(err)
	}
	defer maa.Release()
	res, err := maa.NewResource()
	if err != nil {
		t.Fatal(err)
	}
	defer res.Destroy()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	if !res.PostBundle(filepath.Join(root, "assets/resource")).Wait().Success() {
		t.Fatal("resource load failed")
	}
	probe := &resultRecoveryProbe{}
	if err := res.RegisterCustomRecognition("TestResultRecoveryOCR", probe); err != nil {
		t.Fatal(err)
	}
	for name, action := range map[string]maa.CustomActionRunner{
		"EquipmentRerollResultRouteAction": &EquipmentRerollResultRouteAction{},
		"EquipmentRerollFailAction":        &resultRecoveryFailure{probe},
		"TestResultRecoveryKeep":           &resultRecoveryKeep{probe},
		"TestResultRecoveryForbidden":      &resultRecoveryForbidden{probe},
	} {
		if err := res.RegisterCustomAction(name, action); err != nil {
			t.Fatal(err)
		}
	}
	controllerProbe := &resultRecoveryController{}
	ctrl, err := maa.NewCustomController(controllerProbe)
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.Destroy()
	if !ctrl.PostConnect().Wait().Success() {
		t.Fatal("offline controller connection failed")
	}
	tasker, err := maa.NewTasker()
	if err != nil {
		t.Fatal(err)
	}
	defer tasker.Destroy()
	if err := tasker.BindResource(res); err != nil {
		t.Fatal(err)
	}
	if err := tasker.BindController(ctrl); err != nil {
		t.Fatal(err)
	}
	override := map[string]any{
		"__EquipmentRerollResultButtonsVisible": map[string]any{
			"recognition": map[string]any{"type": "DirectHit"},
		},
		"EquipmentRerollResultPage": map[string]any{
			"recognition": map[string]any{"type": "Custom", "param": map[string]any{"custom_recognition": "TestResultRecoveryOCR"}},
		},
		"EquipmentRerollResultClickKeep": map[string]any{
			"recognition": map[string]any{"type": "DirectHit"},
			"action":      map[string]any{"type": "Custom", "param": map[string]any{"custom_action": "TestResultRecoveryKeep"}},
			"pre_delay":   0, "post_delay": 0, "post_wait_freezes": 0,
			"next": []string{}, "on_error": []string{},
		},
	}
	for _, name := range []string{"EquipmentRerollConfirmChangeEffect", "EquipmentRerollRecordRerollCost"} {
		override[name] = map[string]any{
			"recognition": map[string]any{"type": "DirectHit"},
			"action":      map[string]any{"type": "Custom", "param": map[string]any{"custom_action": "TestResultRecoveryForbidden"}},
			"pre_delay":   0, "post_delay": 0, "post_wait_freezes": 0,
			"next": []string{}, "on_error": []string{},
		}
	}
	for _, tc := range []struct {
		name   string
		misses int32
		rounds int32
		wantOK bool
	}{
		{"immediate", 0, 1, true},
		{"transient misses recover", 2, 1, true},
		{"persistent misses stop", -1, 0, false},
		{"new rounds recover independently", 2, 2, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			probe.misses, probe.rounds = tc.misses, tc.rounds
			probe.attempts.Store(0)
			probe.totalAttempts.Store(0)
			probe.keeps.Store(0)
			probe.failures.Store(0)
			probe.forbidden.Store(0)
			started := time.Now()
			job := tasker.PostTask("__EquipmentRerollResultButtonsVisible", override).Wait()
			elapsed := time.Since(started)
			if job.Success() != tc.wantOK {
				t.Fatalf("success=%v, want %v; error=%v", job.Success(), tc.wantOK, job.Error())
			}
			if probe.keeps.Load() != tc.rounds {
				t.Fatalf("keep actions=%d, want %d", probe.keeps.Load(), tc.rounds)
			}
			if tc.wantOK {
				if probe.failures.Load() != 0 || probe.totalAttempts.Load() != (tc.misses+1)*tc.rounds {
					t.Fatalf("recovery failed: failures=%d attempts=%d", probe.failures.Load(), probe.totalAttempts.Load())
				}
			} else {
				if probe.failures.Load() != 1 || probe.totalAttempts.Load() < 2 {
					t.Fatalf("persistent misses did not reach explicit failure after polling: failures=%d attempts=%d", probe.failures.Load(), probe.totalAttempts.Load())
				}
				if elapsed < 5*time.Second || elapsed > 8*time.Second {
					t.Fatalf("unreadable result did not stop at the configured five-second wait: %s", elapsed)
				}
			}
			if controllerProbe.clicks.Load() != 0 || probe.forbidden.Load() != 0 {
				t.Fatal("result recovery attempted a click or re-entered consumption/recording")
			}
			detail, err := job.GetDetail()
			if err != nil {
				t.Fatal(err)
			}
			defer clearMonitorState(detail.ID)
			t.Logf("attempts=%d keeps=%d failures=%d elapsed=%s", probe.totalAttempts.Load(), probe.keeps.Load(), probe.failures.Load(), elapsed)
		})
	}
}

type resultRecoveryController struct {
	maa.BlankController
	clicks atomic.Int32
}

func (c *resultRecoveryController) Click(int32, int32) bool {
	c.clicks.Add(1)
	return false
}

type resultRecoveryProbe struct {
	misses, rounds int32
	attempts       atomic.Int32
	totalAttempts  atomic.Int32
	keeps          atomic.Int32
	failures       atomic.Int32
	forbidden      atomic.Int32
}

func (p *resultRecoveryProbe) Run(_ *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	p.totalAttempts.Add(1)
	if p.attempts.Add(1) <= p.misses || p.misses < 0 {
		return nil, false
	}
	return &maa.CustomRecognitionResult{Box: arg.Roi, Detail: resultDecisionDetail(ResultDecisionKeep)}, true
}

type resultRecoveryKeep struct{ probe *resultRecoveryProbe }

func (a *resultRecoveryKeep) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	var next []maa.NextItem
	if a.probe.keeps.Add(1) < a.probe.rounds {
		a.probe.attempts.Store(0)
		next = []maa.NextItem{{Name: "__EquipmentRerollResultButtonsVisible"}}
	}
	return ctx.OverrideNext(arg.CurrentTaskName, next) == nil
}

type resultRecoveryFailure struct{ probe *resultRecoveryProbe }

func (a *resultRecoveryFailure) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	a.probe.failures.Add(1)
	return (&EquipmentRerollFailAction{}).Run(ctx, arg)
}

type resultRecoveryForbidden struct{ probe *resultRecoveryProbe }

func (a *resultRecoveryForbidden) Run(*maa.Context, *maa.CustomActionArg) bool {
	a.probe.forbidden.Add(1)
	return false
}
