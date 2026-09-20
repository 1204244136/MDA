package equipmentreroll

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// 读取真实配置，回归“旧按钮消失后成功页不在候选列表”的实机卡死。
func TestResultTransitionsRetainSuccessWhileWaiting(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	data, err := os.ReadFile(filepath.Join(filepath.Dir(file), "../../../assets/resource/pipeline/EquipmentReroll/EquipmentReroll.json"))
	if err != nil {
		t.Fatal(err)
	}
	var nodes map[string]struct {
		Next   []string `json:"next"`
		Action struct {
			Param struct {
				Custom retryGateParam `json:"custom_action_param"`
			} `json:"param"`
		} `json:"action"`
	}
	if err := json.Unmarshal(data, &nodes); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ click, success, gate string }{
		{"EquipmentRerollResultClickKeep", "EquipmentRerollKeepLockGate", "__EquipmentRerollRetryGateResultClickKeep"},
		{"EquipmentRerollResultClickAccept", "EquipmentRerollAfterAccept", "__EquipmentRerollRetryGateResultClickAccept"},
		{"EquipmentRerollLossMaxTierConfirm", "EquipmentRerollAfterAccept", "__EquipmentRerollRetryGateLossMaxTierConfirm"},
	} {
		t.Run(tc.click, func(t *testing.T) {
			firstVisible := func(names []string, successVisible bool) string {
				for _, name := range names {
					if name == tc.gate || (successVisible && name == tc.success) {
						return name
					}
				}
				return ""
			}
			if got := firstVisible(nodes[tc.click].Next, true); got != tc.success {
				t.Fatalf("immediate success routed to %q", got)
			}
			if got := firstVisible(nodes[tc.click].Next, false); got != tc.gate {
				t.Fatalf("animation not sent to wait: %q", got)
			}
			param := nodes[tc.gate].Action.Param.Custom
			next := retryGateNext(param, tc.gate, false)
			names := make([]string, len(next))
			for i := range next {
				names[i] = next[i].Name
			}
			if got := firstVisible(names, true); got != tc.success {
				t.Fatalf("delayed success lost after gate: %q", got)
			}
			if got := firstVisible(names, false); got != tc.gate {
				t.Fatalf("missing button cannot re-enter wait: %q", got)
			}
			if param.GiveUp != "" {
				t.Fatal("retry exhaustion must not imply result success")
			}
		})
	}
}

func TestRetryResetIsScopedToTask(t *testing.T) {
	clearRetryGates()
	t.Cleanup(clearRetryGates)
	retryGate["10|result_click_keep"] = retryGateState{count: 3, last: time.Now()}
	retryGate["11|result_click_keep"] = retryGateState{count: 2, last: time.Now()}
	resetTaskRetryGates(10)
	if _, ok := retryGate["10|result_click_keep"]; ok {
		t.Fatal("new round retained retries")
	}
	if retryGate["11|result_click_keep"].count != 2 {
		t.Fatal("reset affected another task")
	}
}
