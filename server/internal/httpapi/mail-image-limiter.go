package httpapi

import (
	"context"
	"sync"
	"time"
)

const (
	defaultMailImageSlots          = 8
	defaultMailImageWorkspaceSlots = 4
	defaultMailImageQueueWait      = 10 * time.Second
)

// mailImageLimiter bounds the image fetches running at once: a global number of slots, and a smaller
// number per workspace so one tenant's slow images cannot take them all. A request that finds no free
// slot waits (a newsletter sends all its images at once and an <img> never retries), and gives up only
// after the queue wait, when the caller answers 503 with Retry-After.
type mailImageLimiter struct {
	global       chan struct{}
	perWorkspace int
	wait         time.Duration

	mu         sync.Mutex
	workspaces map[string]*workspaceSlots
}

type workspaceSlots struct {
	slots chan struct{}
	users int // requests holding or waiting for a slot, so an idle workspace's entry can go
}

func newMailImageLimiter(global, perWorkspace int, wait time.Duration) *mailImageLimiter {
	if global <= 0 {
		global = defaultMailImageSlots
	}
	if perWorkspace <= 0 {
		perWorkspace = defaultMailImageWorkspaceSlots
	}
	if wait <= 0 {
		wait = defaultMailImageQueueWait
	}
	return &mailImageLimiter{global: make(chan struct{}, global), perWorkspace: perWorkspace, wait: wait, workspaces: map[string]*workspaceSlots{}}
}

// acquire waits for a slot of the workspace and then a global one, for at most the queue wait. The
// workspace slot comes first, so a workspace that is over its share queues alone and never holds a
// global slot while it waits. It returns false when the wait ran out or the request was cancelled.
func (l *mailImageLimiter) acquire(ctx context.Context, workspaceID string) (release func(), ok bool) {
	l.mu.Lock()
	entry := l.workspaces[workspaceID]
	if entry == nil {
		entry = &workspaceSlots{slots: make(chan struct{}, l.perWorkspace)}
		l.workspaces[workspaceID] = entry
	}
	entry.users++
	l.mu.Unlock()
	leave := func() {
		l.mu.Lock()
		if entry.users--; entry.users == 0 {
			delete(l.workspaces, workspaceID)
		}
		l.mu.Unlock()
	}
	timer := time.NewTimer(l.wait)
	defer timer.Stop()
	select {
	case entry.slots <- struct{}{}:
	case <-timer.C:
		leave()
		return nil, false
	case <-ctx.Done():
		leave()
		return nil, false
	}
	select {
	case l.global <- struct{}{}:
	case <-timer.C:
		<-entry.slots
		leave()
		return nil, false
	case <-ctx.Done():
		<-entry.slots
		leave()
		return nil, false
	}
	return func() {
		<-l.global
		<-entry.slots
		leave()
	}, true
}
