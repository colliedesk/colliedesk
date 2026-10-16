package billingreconcile

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/email"
)

func orDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

// outcome says what happened to a change: the store's answer, or that nothing was tried.
func (c Change) outcome(dryRun bool) string {
	switch {
	case dryRun:
		return "would apply"
	case c.Result == "":
		return "not attempted"
	}
	return string(c.Result)
}

// Lines are the findings, one per line, with no customer data beyond ids.
func (r Report) Lines() []string {
	var lines []string
	for _, change := range r.Changes {
		lines = append(lines, fmt.Sprintf("%s workspace=%s subscription=%s plan=%s status=%s -> %s", change.outcome(r.DryRun), change.WorkspaceID,
			change.Polar.SubscriptionID, change.Polar.Plan, orDash(change.StoredStatus), change.Polar.Status))
	}
	for _, item := range r.Unmatched {
		lines = append(lines, fmt.Sprintf("unmatched subscription=%s status=%s product=%s customer=%s reason=%q",
			item.Subscription.ID, item.Subscription.Status, item.Subscription.ProductID, item.Subscription.CustomerID, item.Reason))
	}
	for _, item := range r.Absent {
		lines = append(lines, fmt.Sprintf("absent at Polar workspace=%s subscription=%s status=%s", item.WorkspaceID, item.Stored.SubscriptionID, item.Stored.Status))
	}
	return lines
}

// Summary is the closing line of the command; its counts are the ones of the lines above.
func (r Report) Summary() string {
	if !r.NeedsAttention() {
		return fmt.Sprintf("reconcile: in sync (%d Polar subscriptions)", r.Listed)
	}
	if r.DryRun {
		return fmt.Sprintf("reconcile (dry run): %d to apply, %d unmatched, %d absent at Polar", len(r.Changes), len(r.Unmatched), len(r.Absent))
	}
	return fmt.Sprintf("reconcile: %d applied, %d not applied, %d unmatched, %d absent at Polar", r.Applied, len(r.Changes)-r.Applied, len(r.Unmatched), len(r.Absent))
}

// Print writes the findings and the summary.
func (r Report) Print(output io.Writer) error {
	for _, line := range r.Lines() {
		if _, err := fmt.Fprintln(output, line); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(output, r.Summary())
	return err
}

// Mail composes the operator's summary. The buyer's address is included for unmatched payments,
// because it is the only handle the operator has to find the workspace.
func (r Report) Mail(from, to string, now time.Time) ([]byte, error) {
	messageID, err := email.NewMessageID(domainOf(from))
	if err != nil {
		return nil, err
	}
	var body strings.Builder
	body.WriteString("The billing reconcile compared Polar with the workspaces and found the items below.\n\n")
	if len(r.Changes) > 0 {
		body.WriteString("Differences from Polar (a webhook was lost or arrived out of order), with what the store did:\n")
		for _, change := range r.Changes {
			fmt.Fprintf(&body, "- %s: workspace %s, subscription %s: plan %s, status %s -> %s\n", change.outcome(r.DryRun), change.WorkspaceID, change.Polar.SubscriptionID,
				change.Polar.Plan, orDash(change.StoredStatus), change.Polar.Status)
		}
		body.WriteString("\n")
	}
	if len(r.Unmatched) > 0 {
		body.WriteString("Paid in Polar but owned by no workspace (the buyer paid and received nothing). Create or find the workspace and apply the plan (collie plan), or refund in Polar:\n")
		for _, item := range r.Unmatched {
			sub := item.Subscription
			fmt.Fprintf(&body, "- subscription %s, status %s, product %s, customer %s, buyer %s: %s\n", sub.ID, sub.Status, sub.ProductID, sub.CustomerID, orDash(sub.CustomerEmail), item.Reason)
		}
		body.WriteString("\n")
	}
	if len(r.Absent) > 0 {
		body.WriteString("Stored as paid but not listed by Polar any more (nothing was changed; check the subscription in Polar):\n")
		for _, item := range r.Absent {
			fmt.Fprintf(&body, "- workspace %s, subscription %s, status %s\n", item.WorkspaceID, item.Stored.SubscriptionID, item.Stored.Status)
		}
	}
	body.WriteString("\n" + r.Summary() + "\n")
	return email.Compose(email.Outgoing{FromName: "Colliedesk", FromAddress: from, ToAddress: to,
		Subject: fmt.Sprintf("Colliedesk: billing reconcile found %d item(s)", len(r.Changes)+len(r.Unmatched)+len(r.Absent)),
		Body:    body.String(), MessageID: messageID, Date: now, Standalone: true})
}

func domainOf(address string) string {
	_, domain, _ := strings.Cut(address, "@")
	return domain
}
