package httpapi

import (
	"sync"
	"time"
)

const (
	defaultMaxExports         = 2
	exportRetryAfterSeconds   = "30"
	defaultExportWriteTimeout = 60
	defaultExportTotalTimeout = 2 * time.Hour
)

// exportLimiter admits a bounded number of exports at once, one per workspace. A slot is held from
// the first query to the last byte of the last attachment, so the per-workspace guard covers the
// whole export and a few big exports cannot occupy every connection of the server.
type exportLimiter struct {
	mu     sync.Mutex
	max    int
	active map[string]struct{}
}

type exportAdmission int

const (
	exportAdmitted exportAdmission = iota
	exportWorkspaceBusy
	exportServerBusy
)

func newExportLimiter(max int) *exportLimiter {
	if max <= 0 {
		max = defaultMaxExports
	}
	return &exportLimiter{max: max, active: map[string]struct{}{}}
}

// acquire admits the export of workspaceID or says why not. The returned func frees the slot.
func (l *exportLimiter) acquire(workspaceID string) (func(), exportAdmission) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, running := l.active[workspaceID]; running {
		return nil, exportWorkspaceBusy
	}
	if len(l.active) >= l.max {
		return nil, exportServerBusy
	}
	l.active[workspaceID] = struct{}{}
	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			delete(l.active, workspaceID)
			l.mu.Unlock()
		})
	}, exportAdmitted
}
