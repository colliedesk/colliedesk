package intake

import (
	"sync"
	"time"
)

// After breakerFailures failed calls in a row, a workspace's intake stops asking the model for
// breakerPause: a provider that is down is not called for every card, and its cards get the rules'
// decision at once. The count is the process's; a restart starts it again.
const (
	breakerFailures = 5
	breakerPause    = 10 * time.Minute
)

// CodePaused is the error of an intake task that did not call the model because its breaker is open.
const CodePaused = "intake_paused"

type breaker struct {
	mu     sync.Mutex
	failed map[string]int
	until  map[string]time.Time
}

func newBreaker() *breaker {
	return &breaker{failed: map[string]int{}, until: map[string]time.Time{}}
}

// open says whether the workspace's intake is paused at now.
func (b *breaker) open(workspaceID string, now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return now.Before(b.until[workspaceID])
}

// fail counts a failed call; the last one of a run opens the breaker.
func (b *breaker) fail(workspaceID string, now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failed[workspaceID]++
	if b.failed[workspaceID] >= breakerFailures {
		b.until[workspaceID], b.failed[workspaceID] = now.Add(breakerPause), 0
	}
}

// succeed ends a run of failures.
func (b *breaker) succeed(workspaceID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.failed, workspaceID)
}
