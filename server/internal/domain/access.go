package domain

import (
	"errors"
	"fmt"
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
	// TrialExtended is true when a Freshdesk import already extended this workspace's trial.
	TrialExtended bool `json:"trial_extended"`
	// AgentsOver is how many active agents the workspace has beyond its plan's agent cap, after a
	// move to a smaller plan; 0 when it fits. Set where the agents are counted (authentication).
	AgentsOver int `json:"agents_over"`
	// SeatPaused is true for an agent beyond the cap: the most recently added agents are paused
	// first and the owner never is. Such an agent can only see why and sign out.
	SeatPaused bool `json:"seat_paused"`
	// storageBytes is the largest storage cap among the paid plans held, which can be above the cap
	// of Plan: Founding outranks Small on agents but Small stores more. 0 means StorageCap(Plan).
	storageBytes int64
}

// StorageLimit is the attachment storage cap in effect: the largest cap among the paid plans the
// workspace holds, or the cap of Plan.
func (a Access) StorageLimit() int64 {
	if a.storageBytes > 0 {
		return a.storageBytes
	}
	return StorageCap(a.Plan)
}

// NewAccess derives the plan in effect: the best subscription that is still paid, else the trial
// while now is before trialEndsAt, else PlanNone. At exactly trialEndsAt the trial is over.
func NewAccess(subscriptions []Subscription, trialEndsAt, now time.Time) Access {
	access := Access{TrialEndsAt: UTC(trialEndsAt)}
	var best *Subscription
	for index := range subscriptions {
		candidate := &subscriptions[index]
		if !candidate.Entitled(now) {
			continue
		}
		if best == nil || betterThan(*candidate, *best) {
			best = candidate
		}
		access.storageBytes = max(access.storageBytes, StorageCap(candidate.Plan))
	}
	switch {
	case best != nil:
		access.Plan = best.Plan
	case now.Before(trialEndsAt):
		access.Plan, access.storageBytes = PlanTrial, 0
	default:
		access.Plan, access.Locked, access.storageBytes = PlanNone, true, 0
	}
	return access
}

// SeatPausedError refuses an agent paused because the workspace has more active agents than its
// plan allows (see Access.SeatPaused).
type SeatPausedError struct {
	Plan   string
	Agents int
}

func (e *SeatPausedError) Error() string {
	return fmt.Sprintf("Your access is paused. The %s plan allows %s and this workspace has more, so the most recently added agents are paused. It opens again when a workspace admin deactivates agents or upgrades the plan.", PlanName(e.Plan), agentCount(e.Agents))
}

// LockedError means the workspace's trial or subscription has ended and the action is not allowed.
type LockedError struct{}

func (*LockedError) Error() string {
	return "This workspace is locked because its trial or subscription ended. Choose a plan in Settings > Billing, or export your data."
}

// ErrMailboxClosed means a locked workspace no longer accepts new mail.
var ErrMailboxClosed = errors.New("mailbox closed: the workspace is locked")

// ErrWebFormClosed means the public web form is off, or the workspace is locked.
var ErrWebFormClosed = errors.New("web form closed")
