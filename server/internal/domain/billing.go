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
	Plan              string       `json:"plan"`
	Status            string       `json:"status"`
	CurrentPeriodEnd  *time.Time   `json:"current_period_end"`
	CancelAtPeriodEnd bool         `json:"cancel_at_period_end"`
	Limits            PlanLimits   `json:"limits"`
	Usage             BillingUsage `json:"usage"`
}

// NewBilling derives the effective plan from the stored subscription (nil for none).
func NewBilling(subscription *Subscription, usage BillingUsage, now time.Time) Billing {
	billing := Billing{Plan: PlanFree, Status: BillingStatusNone, Usage: usage}
	if subscription != nil {
		billing.Status = subscription.Status
		billing.CurrentPeriodEnd = UTCOptional(subscription.CurrentPeriodEnd)
		billing.CancelAtPeriodEnd = subscription.CancelAtPeriodEnd
		if subscription.Entitled(now) {
			billing.Plan = subscription.Plan
		}
	}
	billing.Limits = LimitsFor(billing.Plan)
	return billing
}
