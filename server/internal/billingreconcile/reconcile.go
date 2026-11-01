// Package billingreconcile compares the subscriptions Polar holds with the ones stored per
// workspace and repairs what a lost webhook left behind. It writes through the same store path
// and the same ordering rule as the webhook, so the two can never overwrite each other.
package billingreconcile

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/polar"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// Source lists the subscriptions of the Polar organization.
type Source interface {
	ListSubscriptions(ctx context.Context) ([]polar.Subscription, error)
}

// Data is the database side: a snapshot to compare with, and the webhook's write path.
type Data interface {
	BillingSnapshot(ctx context.Context) (store.BillingSnapshot, error)
	ApplySubscription(ctx context.Context, workspaceID, webhookID, eventType string, incoming domain.Subscription) (store.SubscriptionResult, error)
}

// Change is a stored subscription that differs from Polar's.
type Change struct {
	WorkspaceID  string
	Polar        domain.Subscription
	StoredStatus string // empty when nothing was stored
	// Result is what the store did with it; empty until an apply was tried (never set in a dry run).
	// ResultFailed marks the apply that returned an error.
	Result store.SubscriptionResult
}

// ResultFailed marks a change whose apply returned an error.
const ResultFailed store.SubscriptionResult = "failed"

// Unmatched is a paid Polar subscription that no workspace of this instance owns.
type Unmatched struct {
	Subscription polar.Subscription
	Reason       string
}

// Absent is a subscription stored as paid that Polar no longer lists.
type Absent struct {
	WorkspaceID string
	Stored      domain.Subscription
}

// Report is everything one run found. Applied counts the changes the store wrote.
type Report struct {
	Listed    int
	Changes   []Change
	Applied   int
	DryRun    bool
	Unmatched []Unmatched
	Absent    []Absent
}

// NeedsAttention reports whether the operator has something to read.
func (r Report) NeedsAttention() bool {
	return len(r.Changes) > 0 || len(r.Unmatched) > 0 || len(r.Absent) > 0
}

var workspaceIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Run reads all of Polar first and writes only afterwards, so a failed page changes nothing. With
// dryRun it computes the same report and writes nothing.
func Run(ctx context.Context, source Source, data Data, products polar.Products, now time.Time, dryRun bool) (Report, error) {
	listed, err := source.ListSubscriptions(ctx)
	if err != nil {
		return Report{}, err
	}
	snapshot, err := data.BillingSnapshot(ctx)
	if err != nil {
		return Report{}, err
	}
	report := Report{Listed: len(listed), DryRun: dryRun}
	seen := map[string]bool{}
	for _, sub := range listed {
		seen[sub.ID] = true
		workspaceID := sub.ExternalCustomerID
		if !workspaceIDPattern.MatchString(workspaceID) {
			report.noteUnmatched(sub, "customer has no workspace id as external id")
			continue
		}
		// Same order as the webhook: a product sold elsewhere is nobody's lost payment here.
		plan := products.PlanOf(sub.ProductID)
		if plan == "" {
			continue
		}
		if !snapshot.Workspaces[workspaceID] {
			report.noteUnmatched(sub, "workspace not found")
			continue
		}
		incoming := sub.Stored(plan)
		stored, have := snapshot.Subscriptions[workspaceID][sub.ID]
		switch {
		case have && incoming.ModifiedAt.Before(stored.ModifiedAt):
			continue
		// The same state is in sync once the row keeps its product; one stored before that is filled.
		case have && incoming.ModifiedAt.Equal(stored.ModifiedAt) && stored.ProductID != "":
			continue
		case !have && !incoming.Entitled(now):
			// Never stored and grants nothing: there is no access to restore.
			continue
		}
		report.Changes = append(report.Changes, Change{WorkspaceID: workspaceID, Polar: incoming, StoredStatus: stored.Status})
	}
	for workspaceID, subs := range snapshot.Subscriptions {
		for id, stored := range subs {
			if !seen[id] && stored.Entitled(now) {
				report.Absent = append(report.Absent, Absent{WorkspaceID: workspaceID, Stored: stored})
			}
		}
	}
	slices.SortFunc(report.Absent, func(a, b Absent) int { return strings.Compare(a.Stored.SubscriptionID, b.Stored.SubscriptionID) })
	if dryRun {
		return report, nil
	}
	for i := range report.Changes {
		change := &report.Changes[i]
		// The webhook id is derived from the subscription state and its product, so a repeated run is a
		// duplicate, and a state an earlier release reconciled without the product is written again.
		webhookID := fmt.Sprintf("reconcile:%s:%s:%s", change.Polar.SubscriptionID, change.Polar.ModifiedAt.UTC().Format(time.RFC3339Nano), change.Polar.ProductID)
		result, err := data.ApplySubscription(ctx, change.WorkspaceID, webhookID, "reconcile", change.Polar)
		if err != nil {
			change.Result = ResultFailed
			return report, fmt.Errorf("apply subscription %s: %w", change.Polar.SubscriptionID, err)
		}
		change.Result = result
		if result == store.SubscriptionApplied {
			report.Applied++
		}
	}
	return report, nil
}

func (r *Report) noteUnmatched(sub polar.Subscription, reason string) {
	if sub.Status == "active" || sub.Status == "past_due" {
		r.Unmatched = append(r.Unmatched, Unmatched{Subscription: sub, Reason: reason})
	}
}
