package hcanalytics

import (
	"context"
	"log/slog"
	"time"
)

// failing counts one workspace's failed sends in a row and, once there are PauseAfter of them,
// until when its page views are dropped.
type failing struct {
	count  int
	paused time.Time
}

// current reports whether item still goes to its workspace's target as loaded now, and that the
// workspace is not paused. A save that turns analytics off, removes the secret or changes the
// service makes every queued and waiting delivery of the old settings stale. s.mu must be held.
func (s *Sender) current(item *delivery) bool {
	target, ok := s.targets[item.target.WorkspaceID]
	return ok && target == item.target && !s.pausedLocked(item.target.WorkspaceID)
}

func (s *Sender) stillCurrent(item *delivery) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.current(item)
}

func (s *Sender) pausedLocked(workspaceID string) bool {
	state, ok := s.failing[workspaceID]
	return ok && s.options.Now().Before(state.paused)
}

// sent records how a send to item's target ended, unless the target changed meanwhile: a result
// of older settings would describe settings the team no longer has. PauseAfter failures in a row
// pause the workspace.
func (s *Sender) sent(item *delivery, failure string, final bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	workspaceID := item.target.WorkspaceID
	if target, ok := s.targets[workspaceID]; !ok || target != item.target {
		return
	}
	now := s.options.Now()
	if failure == "" {
		delete(s.failing, workspaceID)
		s.recordLocked(item.target, func(r *Result) { r.SentAt = now })
		return
	}
	state := s.failing[workspaceID]
	state.count++
	if state.count >= s.options.PauseAfter {
		state = failing{paused: now.Add(s.options.PauseFor)}
		slog.Warn("help center analytics: pausing a workspace whose service keeps failing",
			"workspace", workspaceID, "for", s.options.PauseFor, "error", failure)
		final = true
	}
	s.failing[workspaceID] = state
	if final {
		s.recordLocked(item.target, func(r *Result) { r.Error, r.ErrorAt = failure, now })
	}
}

func (s *Sender) recordLocked(target Target, change func(*Result)) {
	result := s.results[target.WorkspaceID]
	if result.Version != target.Version {
		result = Result{}
	}
	result.WorkspaceID, result.Version = target.WorkspaceID, target.Version
	change(&result)
	s.results[target.WorkspaceID] = result
}

// Reload reads the targets again: every workspace's for "", one workspace's otherwise (after a
// team changes its settings, so the change applies to the next page view). A workspace whose own
// read failed sends nothing until a later read succeeds.
func (s *Sender) Reload(ctx context.Context, workspaceID string) error {
	if workspaceID == "" {
		return s.reloadAll(ctx)
	}
	target, err := s.store.HelpCenterAnalyticsTarget(ctx, workspaceID)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.generation++
	s.changed[workspaceID] = s.generation
	if err != nil || target == nil || !target.Enabled || target.Problem != "" {
		s.replaceLocked(workspaceID, nil)
		return err
	}
	s.replaceLocked(workspaceID, target)
	return nil
}

// reloadAll swaps in every enabled target. A workspace one Reload changed after this read began
// keeps what that Reload loaded, since this read may hold its older settings. A target the store
// could not read keeps the one loaded; with none loaded, its problem becomes the last error.
func (s *Sender) reloadAll(ctx context.Context) error {
	s.mu.Lock()
	began := s.generation
	s.mu.Unlock()
	targets, err := s.store.HelpCenterAnalyticsTargets(ctx)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := make(map[string]*Target, len(targets))
	for _, target := range targets {
		if target.Problem == "" {
			next[target.WorkspaceID] = &target
			continue
		}
		if loaded, ok := s.targets[target.WorkspaceID]; ok {
			next[target.WorkspaceID] = &loaded
			continue
		}
		problem := target.Problem
		s.recordLocked(target, func(r *Result) { r.Error, r.ErrorAt = problem, s.options.Now() })
	}
	for workspaceID, stamp := range s.changed {
		if stamp <= began {
			delete(s.changed, workspaceID)
			continue
		}
		if loaded, ok := s.targets[workspaceID]; ok {
			next[workspaceID] = &loaded
		} else {
			delete(next, workspaceID)
		}
	}
	for workspaceID := range s.targets {
		if _, ok := next[workspaceID]; !ok {
			s.replaceLocked(workspaceID, nil)
		}
	}
	for workspaceID, target := range next {
		s.replaceLocked(workspaceID, target)
	}
	return nil
}

// replaceLocked loads target for workspaceID, or none when it is nil. New settings end a pause and
// drop the results gathered for the old ones. s.mu must be held.
func (s *Sender) replaceLocked(workspaceID string, target *Target) {
	old, had := s.targets[workspaceID]
	if target == nil {
		delete(s.targets, workspaceID)
	} else {
		s.targets[workspaceID] = *target
	}
	if had && target != nil && old == *target {
		return
	}
	delete(s.failing, workspaceID)
	if result, ok := s.results[workspaceID]; ok && (target == nil || result.Version != target.Version) {
		delete(s.results, workspaceID)
	}
}
