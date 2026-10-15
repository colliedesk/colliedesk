package domain

import "time"

// Plans a workspace can be on. A workspace with no subscription is on PlanFree.
const (
	PlanFree     = "free"
	PlanTeam     = "team"
	PlanFounding = "founding"
)

// BillingStatusNone is the status of a workspace that never had a subscription.
const BillingStatusNone = "none"

// PlanLimits are the caps of a plan. A nil ConversationsPerMonth means no cap.
type PlanLimits struct {
	Agents                int  `json:"agents"`
	ConversationsPerMonth *int `json:"conversations_per_month"`
}

// LimitsFor returns the caps of plan; an unknown plan gets the free caps.
func LimitsFor(plan string) PlanLimits {
	switch plan {
	case PlanTeam, PlanFounding:
		return PlanLimits{Agents: 10}
	default:
		monthly := 100
		return PlanLimits{Agents: 3, ConversationsPerMonth: &monthly}
	}
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

type BillingUsage struct {
	Agents                 int `json:"agents"`
	ConversationsThisMonth int `json:"conversations_this_month"`
}

// Billing is what an admin sees about their workspace's plan.
type Billing struct {
	Plan              string     `json:"plan"`
	Status            string     `json:"status"`
	CurrentPeriodEnd  *time.Time `json:"current_period_end"`
	CancelAtPeriodEnd bool       `json:"cancel_at_period_end"`
	// ActiveSubscriptions counts subscriptions that grant a paid plan now; more than one is a
	// duplicate the admin should cancel in the portal.
	ActiveSubscriptions int          `json:"active_subscriptions"`
	Limits              PlanLimits   `json:"limits"`
	Usage               BillingUsage `json:"usage"`
}

// NewBilling derives the plan in effect from every subscription of the workspace. The best
// subscription that is still paid decides the plan (founding before team, then the later period
// end); with none paid, the most recently changed one explains the state. ActiveSubscriptions
// counts the paid ones so a duplicate is visible.
func NewBilling(subscriptions []Subscription, usage BillingUsage, now time.Time) Billing {
	billing := Billing{Plan: PlanFree, Status: BillingStatusNone, Usage: usage}
	var best, latest *Subscription
	for index := range subscriptions {
		candidate := &subscriptions[index]
		if latest == nil || candidate.ModifiedAt.After(latest.ModifiedAt) {
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
	if best != nil {
		billing.Plan = best.Plan
	} else {
		shown = latest
	}
	if shown != nil {
		billing.Status = shown.Status
		billing.CurrentPeriodEnd = UTCOptional(shown.CurrentPeriodEnd)
		billing.CancelAtPeriodEnd = shown.CancelAtPeriodEnd
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
