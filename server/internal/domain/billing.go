package domain

import (
	"fmt"
	"strings"
	"time"
)

// Plans a workspace can be on. A new workspace is on PlanTrial; once the trial ends with no paid
// subscription it is on PlanNone and locked.
const (
	PlanTrial    = "trial"
	PlanTeam     = "team"
	PlanFounding = "founding"
	PlanNone     = "none"
)

// BillingStatusNone is the status of a workspace that never had a subscription.
const BillingStatusNone = "none"

// Storage cap of the plans without a payment behind them (trial and locked): 2 GiB of attachment
// bytes, imported files excluded. StorageWarnPercent is where the web app starts to warn.
const (
	TrialStorageBytes  int64 = 2 << 30
	StorageWarnPercent       = 80
)

// PlanLimits are the caps of a plan.
type PlanLimits struct {
	Agents int `json:"agents"`
	// StorageBytes is the attachment storage cap, nil when the plan has none.
	StorageBytes *int64 `json:"storage_bytes"`
	// StorageWarnBytes is the usage at which the web app warns, nil when StorageBytes is nil.
	StorageWarnBytes *int64 `json:"storage_warn_bytes"`
	// RepliesPer24h is the cap on emailed replies in any 24 hours, nil when the plan has none (trial only).
	RepliesPer24h *int `json:"replies_per_24h"`
}

// StorageCapped reports whether attachment storage is capped on plan.
func StorageCapped(plan string) bool {
	return plan == PlanTrial || plan == PlanNone
}

// LimitsFor returns the caps of plan; a locked workspace (PlanNone) holds no seats.
func LimitsFor(plan string) PlanLimits {
	var limits PlanLimits
	switch plan {
	case PlanTrial, PlanTeam, PlanFounding:
		limits.Agents = 10
	}
	if plan == PlanTrial {
		replies := TrialRepliesPerDay
		limits.RepliesPer24h = &replies
	}
	if StorageCapped(plan) {
		limit, warn := TrialStorageBytes, TrialStorageBytes*StorageWarnPercent/100
		limits.StorageBytes, limits.StorageWarnBytes = &limit, &warn
	}
	return limits
}

// Subscription mirrors the latest Polar subscription event of a workspace.
type Subscription struct {
	Plan              string // team or founding: what the product bought
	SubscriptionID    string
	CustomerID        string
	Status            string // the Polar status
	CurrentPeriodEnd  *time.Time
	CancelAtPeriodEnd bool
	EndedAt           *time.Time
	ModifiedAt        time.Time // the subscription's own modified_at, for ordering events
}

// CompSubscriptionPrefix starts the subscription id of a complimentary plan, so it is never
// mistaken for a Polar subscription id.
const CompSubscriptionPrefix = "comp:"

// Complimentary reports whether the subscription is an operator-granted plan that nobody pays for.
func (s Subscription) Complimentary() bool {
	return strings.HasPrefix(s.SubscriptionID, CompSubscriptionPrefix)
}

// Entitled reports whether the subscription grants its paid plan at now.
// active and trialing pay; past_due stays paid while Polar retries the card; canceled pays until
// the period it already paid for ends; everything else (unpaid, incomplete, paused) does not.
// An ended subscription never pays, whatever its status says.
func (s Subscription) Entitled(now time.Time) bool {
	if s.EndedAt != nil && !s.EndedAt.After(now) {
		return false
	}
	switch s.Status {
	case "active", "trialing", "past_due":
		return true
	case "canceled":
		return s.CurrentPeriodEnd != nil && s.CurrentPeriodEnd.After(now)
	default:
		return false
	}
}

// BillingUsage is what a workspace has used of its plan limits now.
type BillingUsage struct {
	Agents int `json:"agents"`
	// PendingInvites are unused, unrevoked, unexpired invites; each holds an agent seat.
	PendingInvites int `json:"pending_invites"`
	// RepliesLast24h counts the emailed replies of the last 24 hours that count against the
	// trial's daily cap; it is 0 on every other plan.
	RepliesLast24h int `json:"replies_last_24h"`
	// StorageBytes is the attachment bytes the storage cap counts: every file except imported ones.
	StorageBytes int64 `json:"storage_bytes"`
}

// StorageLimitError means storing the new files would pass the plan's storage cap. Nothing was stored.
type StorageLimitError struct {
	Used, Adding, Limit int64
}

func (e *StorageLimitError) Error() string {
	return "This workspace has reached its 2 GiB attachment storage limit. Subscribe to remove the limit, or export your data."
}

// PlanLimitError means an action would exceed a limit of the workspace's plan.
type PlanLimitError struct {
	Plan   string
	Agents int // the agent cap of the plan
}

func (e *PlanLimitError) Error() string {
	name := "Team"
	switch e.Plan {
	case PlanFounding:
		name = "Founding"
	case PlanTrial:
		name = "trial"
	}
	if e.Plan == PlanTrial {
		return fmt.Sprintf("The trial allows %d agents, pending invites included. Revoke a pending invite to free a seat, or subscribe and contact us for more than %d agents.", e.Agents, e.Agents)
	}
	return fmt.Sprintf("The %s plan allows %d agents, pending invites included. Revoke a pending invite to free a seat, or contact us for more than %d agents.", name, e.Agents, e.Agents)
}

// Trial outbound controls: during the trial email goes only to customers who wrote first (unless
// the workspace verified its own sending domain), and at most TrialRepliesPerDay replies leave in
// any rolling 24 hours.
const (
	TrialRepliesPerDay = 200
	TrialReplyWindow   = 24 * time.Hour
)

// TrialOutboundError refuses a reply the trial does not allow. Nothing was stored.
type TrialOutboundError struct {
	// RetryAfter is set when the daily cap is the reason: the time until a slot frees.
	RetryAfter time.Duration
}

// Limited reports whether the daily cap, not the first-contact rule, refused the reply.
func (e *TrialOutboundError) Limited() bool { return e.RetryAfter > 0 }

func (e *TrialOutboundError) Error() string {
	if e.Limited() {
		return fmt.Sprintf("The trial sends up to %d replies in 24 hours. Try again later, or upgrade to remove this limit.", TrialRepliesPerDay)
	}
	return "During the trial, email goes only to customers who wrote to you first. Verify your sending domain in Settings or upgrade to start new conversations."
}

// Billing is what an admin sees about their workspace's plan.
type Billing struct {
	Plan string `json:"plan"`
	// TrialEndsAt is when the workspace's free trial ends or ended; it is set whatever the plan.
	TrialEndsAt time.Time `json:"trial_ends_at"`
	// Locked is true when the workspace has no paid plan and its trial is over.
	Locked            bool       `json:"locked"`
	Status            string     `json:"status"`
	CurrentPeriodEnd  *time.Time `json:"current_period_end"`
	CancelAtPeriodEnd bool       `json:"cancel_at_period_end"`
	// ActiveSubscriptions counts subscriptions that grant a paid plan now; more than one is a
	// duplicate the admin should cancel in the portal.
	ActiveSubscriptions int `json:"active_subscriptions"`
	// Complimentary is true when the plan in effect is a grant by HC Labs, not a Polar subscription:
	// there is no billing account, so no portal and no checkout. ComplimentaryUntil is its end, nil
	// when it has none.
	Complimentary      bool         `json:"complimentary"`
	ComplimentaryUntil *time.Time   `json:"complimentary_until"`
	Limits             PlanLimits   `json:"limits"`
	Usage              BillingUsage `json:"usage"`
}

// NewBilling derives the plan in effect from every subscription of the workspace. The best
// subscription that is still paid decides the plan (founding before team, then the later period
// end); with none paid, the most recently changed one explains the state. ActiveSubscriptions
// counts the paid ones so a duplicate is visible.
func NewBilling(subscriptions []Subscription, trialEndsAt time.Time, usage BillingUsage, now time.Time) Billing {
	billing := Billing{Status: BillingStatusNone, Usage: usage}
	var best, latest *Subscription
	for index := range subscriptions {
		candidate := &subscriptions[index]
		// A complimentary grant has no billing account behind it, so once it ran out there is
		// nothing to explain or manage.
		if !candidate.Complimentary() && (latest == nil || candidate.ModifiedAt.After(latest.ModifiedAt)) {
			latest = candidate
		}
		if candidate.Entitled(now) {
			billing.ActiveSubscriptions++
			if best == nil || betterThan(*candidate, *best) {
				best = candidate
			}
		}
	}
	shown := best
	if best == nil {
		shown = latest
	}
	access := NewAccess(subscriptions, trialEndsAt, now)
	billing.Plan, billing.TrialEndsAt, billing.Locked = access.Plan, access.TrialEndsAt, access.Locked
	if shown != nil {
		billing.Status = shown.Status
		billing.CurrentPeriodEnd = UTCOptional(shown.CurrentPeriodEnd)
		billing.CancelAtPeriodEnd = shown.CancelAtPeriodEnd
	}
	if best != nil && best.Complimentary() {
		billing.Complimentary, billing.ComplimentaryUntil = true, UTCOptional(best.CurrentPeriodEnd)
	}
	billing.Limits = LimitsFor(billing.Plan)
	return billing
}

func betterThan(a, b Subscription) bool {
	if a.Plan != b.Plan {
		return a.Plan == PlanFounding
	}
	if a.CurrentPeriodEnd == nil || b.CurrentPeriodEnd == nil {
		return a.CurrentPeriodEnd != nil
	}
	return a.CurrentPeriodEnd.After(*b.CurrentPeriodEnd)
}

// KnownStatuses are the Polar subscription statuses this product understands. Anything else is
// stored as StatusUnknown and never grants a plan.
var KnownStatuses = map[string]bool{
	"incomplete": true, "incomplete_expired": true, "trialing": true, "active": true,
	"past_due": true, "canceled": true, "unpaid": true, "paused": true,
}

const StatusUnknown = "unknown"
