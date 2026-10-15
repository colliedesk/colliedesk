package domain

import (
	"errors"
	"time"
)

// TrialLength is how long a new workspace has the full Team feature set before it is locked.
const TrialLength = 14 * 24 * time.Hour

// Access is what a workspace may do at one instant. A locked workspace can only sign in, pay and
// export its data.
type Access struct {
	Plan        string    `json:"plan"`
	TrialEndsAt time.Time `json:"trial_ends_at"`
	Locked      bool      `json:"locked"`
}

// NewAccess derives the plan in effect: the best subscription that is still paid, else the trial
// while now is before trialEndsAt, else PlanNone. At exactly trialEndsAt the trial is over.
func NewAccess(subscriptions []Subscription, trialEndsAt, now time.Time) Access {
	access := Access{TrialEndsAt: UTC(trialEndsAt)}
	var best *Subscription
	for index := range subscriptions {
		candidate := &subscriptions[index]
		if candidate.Entitled(now) && (best == nil || betterThan(*candidate, *best)) {
			best = candidate
		}
	}
	switch {
	case best != nil:
		access.Plan = best.Plan
	case now.Before(trialEndsAt):
		access.Plan = PlanTrial
	default:
		access.Plan, access.Locked = PlanNone, true
	}
	return access
}

// LockedError means the workspace's trial or subscription has ended and the action is not allowed.
type LockedError struct{}

func (*LockedError) Error() string {
	return "This workspace is locked because its trial or subscription ended. Choose a plan in Settings > Billing, or export your data."
}

// ErrMailboxClosed means a locked workspace no longer accepts new mail.
var ErrMailboxClosed = errors.New("mailbox closed: the workspace is locked")
