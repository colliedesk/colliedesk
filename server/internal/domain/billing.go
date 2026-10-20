package domain

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// Plans a workspace can be on. A new workspace is on PlanTrial; once the trial ends with no paid
// subscription it is on PlanNone and locked.
const (
	PlanTrial    = "trial"
	PlanSolo     = "solo"
	PlanSmall    = "small"
	PlanTeam     = "team"
	PlanTeam25   = "team25"
	PlanFounding = "founding"
	PlanNone     = "none"
)

// BillingStatusNone is the status of a workspace that never had a subscription.
const BillingStatusNone = "none"

// FoundingSlots is how many Founding subscriptions can exist at once; the plan closes when they are taken.
const FoundingSlots = 10

// Storage caps. The trial (and a locked workspace) gets 2 GiB of attachment bytes, imported files
// excluded. Paid plans count imported files too: Solo gets 10 GiB, Small 30 GiB, Founding 20 GiB,
// Team 100 GiB, Team 25 250 GiB. StorageWarnPercent is where the web app starts to warn.
const (
	TrialStorageBytes    int64 = 2 << 30
	SoloStorageBytes     int64 = 10 << 30
	SmallStorageBytes    int64 = 30 << 30
	FoundingStorageBytes int64 = 20 << 30
	TeamStorageBytes     int64 = 100 << 30
	Team25StorageBytes   int64 = 250 << 30
	StorageWarnPercent         = 80
)

// PlanLimits are the caps of a plan.
type PlanLimits struct {
	Agents int `json:"agents"`
	// StorageBytes is the attachment storage cap, nil when the plan has none.
	StorageBytes *int64 `json:"storage_bytes"`
	// StorageWarnBytes is the usage at which the web app warns, nil when StorageBytes is nil.
	StorageWarnBytes *int64 `json:"storage_warn_bytes"`
	// RepliesPer24h is the cap on people emailed in any 24 hours (each reply counts its contact and every
	// Cc address), nil when the plan has none (trial only).
	RepliesPer24h *int `json:"replies_per_24h"`
}

// PlanName is the plan as a person reads it: "Team 25", not the stored id "team25".
func PlanName(plan string) string {
	switch plan {
	case PlanSolo:
		return "Solo"
	case PlanSmall:
		return "Small"
	case PlanTeam:
		return "Team"
	case PlanTeam25:
		return "Team 25"
	case PlanFounding:
		return "Founding"
	case PlanTrial:
		return "trial"
	}
	return plan
}

// StorageCap returns the attachment storage cap of plan in bytes, or 0 when the plan has none.
func StorageCap(plan string) int64 {
	switch plan {
	case PlanTrial, PlanNone:
		return TrialStorageBytes
	case PlanSolo:
		return SoloStorageBytes
	case PlanSmall:
		return SmallStorageBytes
	case PlanFounding:
		return FoundingStorageBytes
	case PlanTeam:
		return TeamStorageBytes
	case PlanTeam25:
		return Team25StorageBytes
	}
	return 0
}

// StorageCapped reports whether attachment storage is capped on plan.
func StorageCapped(plan string) bool { return StorageCap(plan) > 0 }

// StorageCountsImports reports whether imported files count toward the cap of plan.
// Every plan with a cap other than the trial's counts them.
func StorageCountsImports(plan string) bool {
	return StorageCapped(plan) && plan != PlanTrial && plan != PlanNone
}

// LimitsFor returns the caps of plan; a locked workspace (PlanNone) holds no seats.
func LimitsFor(plan string) PlanLimits {
	return LimitsForStorage(plan, StorageCap(plan))
}

// LimitsForStorage is LimitsFor with an explicit storage cap (0 for none), for a server or test
// that changes the Founding cap.
func LimitsForStorage(plan string, storageCap int64) PlanLimits {
	var limits PlanLimits
	switch plan {
	case PlanSolo:
		limits.Agents = 1
	case PlanSmall:
		limits.Agents = 3
	case PlanTrial, PlanTeam, PlanFounding:
		limits.Agents = 10
	case PlanTeam25:
		limits.Agents = 25
	}
	if plan == PlanTrial {
		recipients := TrialRecipientsPerDay
		limits.RepliesPer24h = &recipients
	}
	if storageCap > 0 {
		limit, warn := storageCap, storageCap*StorageWarnPercent/100
		limits.StorageBytes, limits.StorageWarnBytes = &limit, &warn
	}
	return limits
}

// Subscription mirrors the latest Polar subscription event of a workspace.
type Subscription struct {
	Plan              string // solo, small, team, team25 or founding: what the product bought
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
	// RepliesLast24h counts the people the emailed replies of the last 24 hours reached (the contact
	// plus every Cc address of each reply), which is what the trial's daily cap limits; it is 0 on
	// every other plan.
	RepliesLast24h int `json:"replies_last_24h"`
	// StorageBytes is the attachment bytes the storage cap counts: every file except imported ones,
	// or every file including imported ones on a paid plan.
	StorageBytes int64 `json:"storage_bytes"`
}

// StorageLimitError means storing the new files would pass the plan's storage cap. Nothing was stored.
type StorageLimitError struct {
	Plan                string
	Used, Adding, Limit int64
}

func (e *StorageLimitError) Error() string {
	limit := FormatBytes(e.Limit)
	switch e.Plan {
	case PlanSolo, PlanSmall:
		return fmt.Sprintf("This workspace has reached the %s attachment storage limit of the %s plan. Nothing is deleted, customer email is still stored, and you can still read, reply and export. Uploads and imports are refused. Upgrade to the %s plan under Billing > Manage billing for a higher limit, or export your data.", limit, PlanName(e.Plan), PlanName(NextPlan(e.Plan)))
	case PlanFounding:
		return fmt.Sprintf("This workspace has reached the %s attachment storage limit of the Founding plan. Nothing is deleted, customer email is still stored, and you can still read, reply and export. Uploads and imports are refused. Upgrade to the Team plan under Billing > Manage billing for a higher limit, or export your data.", limit)
	case PlanTeam, PlanTeam25:
		return fmt.Sprintf("This workspace has reached the %s attachment storage limit of its plan. Nothing is deleted, customer email is still stored, and you can still read, reply and export. Uploads and imports are refused. Contact us for more storage, or export your data.", limit)
	}
	return fmt.Sprintf("This workspace has reached its %s attachment storage limit. Subscribe to remove the limit, or export your data.", limit)
}

// FormatBytes writes a byte count for a person: "20 GiB" when it is a whole number of GiB or MiB,
// otherwise a figure with one decimal and the exact bytes in brackets, such as "1.5 GiB (1610612736 bytes)".
func FormatBytes(n int64) string {
	switch {
	case n >= 1<<30 && n%(1<<30) == 0:
		return fmt.Sprintf("%d GiB", n>>30)
	case n >= 1<<20 && n%(1<<20) == 0:
		return fmt.Sprintf("%d MiB", n>>20)
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GiB (%d bytes)", float64(n)/(1<<30), n)
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB (%d bytes)", float64(n)/(1<<20), n)
	}
	return fmt.Sprintf("%d bytes", n)
}

// upgradeLadder is the order a growing team moves through the plans; each step gives more agents.
// Founding is a one-off offer outside it.
var upgradeLadder = []string{PlanSolo, PlanSmall, PlanTeam, PlanTeam25}

// NextPlan is the step after plan on the upgrade ladder (Solo, Small, Team, Team 25), or "" for the
// top step or a plan outside the ladder.
func NextPlan(plan string) string {
	for index, step := range upgradeLadder[:len(upgradeLadder)-1] {
		if step == plan {
			return upgradeLadder[index+1]
		}
	}
	return ""
}

// UpgradeFor is the first plan above plan on the ladder that onSale lists, or "" when there is none,
// so a refusal never offers a plan the server cannot sell.
func UpgradeFor(plan string, onSale []string) string {
	for next := NextPlan(plan); next != ""; next = NextPlan(next) {
		if slices.Contains(onSale, next) {
			return next
		}
	}
	return ""
}

// PlanLimitError means an action would exceed a limit of the workspace's plan.
type PlanLimitError struct {
	Plan   string
	Agents int // the agent cap of the plan
	// Upgrade is the plan the refusal points to (see UpgradeFor), "" when the server sells none above.
	Upgrade string
}

func (e *PlanLimitError) Error() string {
	allows := fmt.Sprintf("%d agents", e.Agents)
	if e.Agents == 1 {
		allows = "1 agent"
	}
	if e.Plan == PlanTrial {
		return fmt.Sprintf("The trial allows %s, pending invites included. Revoke a pending invite to free a seat, or subscribe and contact us for more than %d agents.", allows, e.Agents)
	}
	if e.Upgrade != "" {
		return fmt.Sprintf("The %s plan allows %s, pending invites included. Revoke a pending invite to free a seat, or upgrade to %s (up to %d agents) under Manage billing.", PlanName(e.Plan), allows, PlanName(e.Upgrade), LimitsFor(e.Upgrade).Agents)
	}
	return fmt.Sprintf("The %s plan allows %s, pending invites included. Revoke a pending invite to free a seat, or contact us for more than %d agents.", PlanName(e.Plan), allows, e.Agents)
}

// Trial outbound controls: during the trial email goes only to customers who wrote first (unless
// the workspace verified its own sending domain), and replies reach at most TrialRecipientsPerDay
// people in any rolling 24 hours, counting the contact and every Cc address of each reply.
const (
	TrialRecipientsPerDay = 200
	TrialReplyWindow      = 24 * time.Hour
)

// TrialOutboundError refuses a reply the trial does not allow. Nothing was stored.
type TrialOutboundError struct {
	// RetryAfter is set when the daily cap is the reason: the time until a slot frees.
	RetryAfter time.Duration
	// RefusedCC names the Cc addresses the trial does not allow, when they are the reason.
	RefusedCC []string
	// ReplyTo is set when the customer wrote first, but from an address other than the contact's (a
	// contact merged into this one): it is the address the reply would go to, which never wrote.
	ReplyTo string
}

// ReasonContactAddressDiffers is the reason of a refusal whose customer wrote from another address.
const ReasonContactAddressDiffers = "contact_address_differs"

// Limited reports whether the daily cap, not the first-contact rule, refused the reply.
func (e *TrialOutboundError) Limited() bool { return e.RetryAfter > 0 }

func (e *TrialOutboundError) Error() string {
	if e.Limited() {
		return fmt.Sprintf("The trial emails up to %d people in 24 hours, counting everyone copied. Try again later, or upgrade to remove this limit.", TrialRecipientsPerDay)
	}
	if len(e.RefusedCC) > 0 {
		return "During the trial, a reply can copy only people the customer wrote from or copied. Not allowed: " + strings.Join(e.RefusedCC, ", ") +
			". Remove them, or verify your sending domain in Settings or upgrade."
	}
	if e.ReplyTo != "" {
		return "During the trial, a reply goes only to an address that wrote to you first. This customer wrote from another address, merged into this contact, and the reply would go to " +
			e.ReplyTo + ". Verify your sending domain in Settings or upgrade to reply."
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
	// CheckoutPlans are the plans this server can sell, in the order they are offered. The web app
	// shows a purchase button for each and no other.
	CheckoutPlans []string `json:"checkout_plans"`
}

// NewBilling derives the plan in effect from every subscription of the workspace. The best
// subscription that is still paid decides the plan (by planRank, then the later period
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

// planRank orders the paid plans by what they give, so a workspace that holds two gets the better
// one: agents first, then storage. Team 25 has the most agents; Team and Founding have the same
// agents but Team has the larger storage cap; Small and Solo have fewer agents than Founding.
func planRank(plan string) int {
	switch plan {
	case PlanTeam25:
		return 5
	case PlanTeam:
		return 4
	case PlanFounding:
		return 3
	case PlanSmall:
		return 2
	case PlanSolo:
		return 1
	}
	return 0
}

func betterThan(a, b Subscription) bool {
	if a.Plan != b.Plan {
		return planRank(a.Plan) > planRank(b.Plan)
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
