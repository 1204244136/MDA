package membership

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

type runtimeTrackingLease struct {
	release func() error
	once    sync.Once
	err     error
}

var runtimeLeaseParentPID = os.Getppid

func runtimeTrackingLeaseScope(detail maa.TaskerTaskDetail) string {
	taskerID := strings.TrimSpace(detail.UUID)
	if taskerID == "" {
		taskerID = fmt.Sprintf("fallback:%s:%d", detail.Hash, detail.TaskID)
	}
	return fmt.Sprintf("parent:%d;tasker:%s", runtimeLeaseParentPID(), taskerID)
}

func runtimeTrackingLeasePath(detail maa.TaskerTaskDetail) (string, string, error) {
	quotaPath, err := quotaStatePath()
	if err != nil {
		return "", "", err
	}
	scope := runtimeTrackingLeaseScope(detail)
	sum := sha256.Sum256([]byte(scope))
	name := "membership-runtime-" + hex.EncodeToString(sum[:16]) + ".lock"
	return filepath.Join(filepath.Dir(quotaPath), name), scope, nil
}

func tryAcquireRuntimeTrackingLease(detail maa.TaskerTaskDetail) (*runtimeTrackingLease, bool, error) {
	path, _, err := runtimeTrackingLeasePath(detail)
	if err != nil {
		return nil, false, err
	}
	release, acquired, err := tryAcquireQuotaFileLock(path)
	if err != nil || !acquired {
		return nil, acquired, err
	}
	return &runtimeTrackingLease{release: release}, true, nil
}

func (l *runtimeTrackingLease) Release() error {
	if l == nil {
		return nil
	}
	l.once.Do(func() {
		if l.release != nil {
			l.err = l.release()
		}
	})
	return l.err
}
