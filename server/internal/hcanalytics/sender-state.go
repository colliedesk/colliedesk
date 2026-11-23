package hcanalytics

import (
	"context"
	"log/slog"
	"time"
)

// failing counts one help center's failed sends in a row and, once there are PauseAfter of them,
// until when its page views are dropped.
type failing struct {
	count  int
	paused time.Time
}

// current reports whether item still goes to its workspace's target as loaded now, and that the
// workspace is not paused. A save that turns analytics off, removes the secret or changes the
// service makes every queued and waiting delivery of the old settings stale. s.mu must be held.
func (s *Sender) current(item *delivery) bool {
	target, ok := s.targets[item.target.key()]
	return ok && target == item.target && !s.pausedLocked(item.target.key())
}

func (s *Sender) stillCurrent(item *delivery) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.current(item)
}

func (s *Sender) pausedLocked(center centerKey) bool {
	state, ok := s.failing[center]
	return ok && s.options.Now().Before(state.paused)
}

// sent records how a send to item's target ended, unless the target changed meanwhile: a result
// of older settings would describe settings the team no longer has. PauseAfter failures in a row
// pause the workspace.
func (s *Sender) sent(item *delivery, failure string, final bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	center := item.target.key()
	if target, ok := s.targets[center]; !ok || target != item.target {
		return
	}
	now := s.options.Now()
	if failure == "" {
		delete(s.failing, center)
		s.recordLocked(item.target, func(r *Result) { r.SentAt = now })
		return
	}
	state := s.failing[center]
	state.count++
	if state.count >= s.options.PauseAfter {
		state = failing{paused: now.Add(s.options.PauseFor)}
		slog.Warn("help center analytics: pausing a workspace whose service keeps failing",
			"workspace", center.workspace, "help_center", center.helpCenter, "for", s.options.PauseFor, "error", failure)
		final = true
	}
	s.failing[center] = state
	if final {
		s.recordLocked(item.target, func(r *Result) { r.Error, r.ErrorAt = failure, now })
	}
}

func (s *Sender) recordLocked(target Target, change func(*Result)) {
	result := s.results[target.key()]
	if result.Version != target.Version {
		result = Result{}
	}
	result.WorkspaceID, result.HelpCenterID, result.Version = target.WorkspaceID, target.HelpCenterID, target.Version
	change(&result)
	s.results[target.key()] = result
}

// Reload reads the targets again: every help center's for "", the primary help center's of one
// workspace otherwise. ReloadIn reads one help center's.
func (s *Sender) Reload(ctx context.Context, workspaceID string) error {
	if workspaceID == "" {
		return s.reloadAll(ctx)
	}
	return s.ReloadIn(ctx, workspaceID, "")
}

// ReloadIn reads the target of one help center ("" for the workspace's primary) again, after a team
// changes its settings, so the change applies to the next page view. A help center whose own read
// failed sends nothing until a later read succeeds.
func (s *Sender) ReloadIn(ctx context.Context, workspaceID, helpCenterID string) error {
	target, err := s.store.HelpCenterAnalyticsTargetIn(ctx, workspaceID, helpCenterID)
	center := centerKey{workspaceID, helpCenterID}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.generation++
	s.changed[center] = s.generation
	if err != nil || target == nil || !target.Enabled || target.Problem != "" {
		s.replaceLocked(center, nil)
		return err
	}
	s.replaceLocked(center, target)
	return nil
}

// reloadAll swaps in every enabled target. A help center one ReloadIn changed after this read began
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
	next := make(map[centerKey]*Target, len(targets))
	for _, target := range targets {
		if target.Problem == "" {
			next[target.key()] = &target
			continue
		}
		if loaded, ok := s.targets[target.key()]; ok {
			next[target.key()] = &loaded
			continue
		}
		problem := target.Problem
		s.recordLocked(target, func(r *Result) { r.Error, r.ErrorAt = problem, s.options.Now() })
	}
	for center, stamp := range s.changed {
		if stamp <= began {
			delete(s.changed, center)
			continue
		}
		if loaded, ok := s.targets[center]; ok {
			next[center] = &loaded
		} else {
			delete(next, center)
		}
	}
	for center := range s.targets {
		if _, ok := next[center]; !ok {
			s.replaceLocked(center, nil)
		}
	}
	for center, target := range next {
		s.replaceLocked(center, target)
	}
	return nil
}

// replaceLocked loads target for a help center, or none when it is nil. New settings end a pause and
// drop the results gathered for the old ones. s.mu must be held.
func (s *Sender) replaceLocked(center centerKey, target *Target) {
	old, had := s.targets[center]
	if target == nil {
		delete(s.targets, center)
	} else {
		s.targets[center] = *target
	}
	if had && target != nil && old == *target {
		return
	}
	delete(s.failing, center)
	if result, ok := s.results[center]; ok && (target == nil || result.Version != target.Version) {
		delete(s.results, center)
	}
}
