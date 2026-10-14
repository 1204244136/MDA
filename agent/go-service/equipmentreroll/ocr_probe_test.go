package equipmentreroll

import "testing"

// TestOCRProbeHandlesMissingDetail 守住诊断摘要对空结果的容错：
// 识别彻底失败时 detail 为 nil，日志辅助函数不能因此 panic。
func TestOCRProbeHandlesMissingDetail(t *testing.T) {
	if got := ocrProbe(nil); got != "<none>" {
		t.Fatalf("ocrProbe(nil) = %q, want <none>", got)
	}
}
