// Package retention deletes workspaces that stayed locked for a year, after telling their admins.
package retention

import (
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// Action is what the job does with one workspace now.
type Action string

const (
	ActionNone   Action = "none"
	ActionNotice Action = "notice"
	ActionDelete Action = "delete"
	// ActionReset clears a notice that no longer applies: the workspace has access again, or the
	// notice belongs to an earlier lock.
	ActionReset Action = "reset"
)

// Decide chooses the action for a workspace. lockedSince is meaningful only when locked. A notice
// counts only if it was sent during the current lock, and deletion needs it to be at least
// RetentionNoticeLead old as well as the lock to be a year old.
func Decide(locked bool, lockedSince time.Time, noticeSentAt *time.Time, now time.Time) Action {
	if !locked {
		if noticeSentAt != nil {
			return ActionReset
		}
		return ActionNone
	}
	noticeAt, deleteAt := domain.RetentionDates(lockedSince)
	current := noticeSentAt != nil && !noticeSentAt.Before(lockedSince)
	switch {
	case current:
		if !now.Before(deleteAt) && !now.Before(noticeSentAt.Add(domain.RetentionNoticeLead)) {
			return ActionDelete
		}
		return ActionNone
	case !now.Before(noticeAt):
		return ActionNotice
	case noticeSentAt != nil:
		return ActionReset
	}
	return ActionNone
}

// earliestDeletion is the first moment a workspace noticed at noticedAt may be deleted.
func earliestDeletion(lockedSince, noticedAt time.Time) time.Time {
	_, deleteAt := domain.RetentionDates(lockedSince)
	if floor := noticedAt.Add(domain.RetentionNoticeLead); floor.After(deleteAt) {
		return floor
	}
	return deleteAt
}
