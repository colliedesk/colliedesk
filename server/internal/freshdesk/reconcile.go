package freshdesk

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"time"
)

// reconcile compares what Freshdesk holds with what the ledger and the
// database hold, and re-hashes every stored attachment of the run's tickets.
func (r *run) reconcile(ctx context.Context, report *Report) (*Report, error) {
	r.reportPeople(report)
	contactIDs := uniqueSorted(r.contactIDs)
	ticketIDs := r.expectedTickets()
	report.Contacts.Source = len(contactIDs)
	report.Tickets.Source = len(ticketIDs)
	report.Tickets.DeletedAtSource = uniqueSorted(r.deleted)
	report.Tickets.Trashed, report.Tickets.Spam = uniqueSorted(r.trashed), uniqueSorted(r.spam)
	report.Tickets.UnresolvedIDs = uniqueSorted(r.unresolved)
	report.Tickets.AbsentIDs, report.Tickets.Archived = r.absent, r.archived
	report.Tickets.HighestID, report.Tickets.HighestProbedID, report.Tickets.ProbedIDs = r.highestExisting, r.highestProbed, r.probed
	report.Verification = r.verification
	report.RateLimit = r.client.rateLimitStats()
	steps := []func(context.Context, *Report, []string, []string) error{r.countTickets, r.countChildren, r.collectFailures, r.countSavedReplies}
	if !r.opts.DryRun {
		// Verify first: a damaged file turns its ledger rows back to failed.
		steps = append([]func(context.Context, *Report, []string, []string) error{r.verifyAttachments}, steps...)
	}
	for _, step := range steps {
		if err := step(ctx, report, ticketIDs, contactIDs); err != nil {
			return nil, err
		}
	}
	report.Warnings = r.reportWarnings()
	report.Missing = missing(report)
	switch {
	case r.opts.DryRun:
		report.Status = StatusDryRun
	case len(report.Missing) > 0 || len(report.Failures) > 0:
		report.Status = StatusFailed
	case !report.Verification.Tickets.Verified || !report.Verification.Contacts.Verified || !report.Verification.SavedReplies.Verified:
		report.Status = StatusUnverified
	default:
		report.Status = StatusOK
	}
	report.FinishedAt = time.Now().UTC()
	return report, nil
}

func (r *run) reportPeople(report *Report) {
	report.Agents.Source, report.Agents.Matched = len(r.agents), len(r.agentTargets)
	report.Agents.Unmatched = []string{}
	for id, agent := range r.agents {
		if _, ok := r.agentTargets[id]; !ok {
			report.Agents.Unmatched = append(report.Agents.Unmatched, agent.Contact.Email)
		}
	}
	sort.Strings(report.Agents.Unmatched)
	report.Companies.Source, report.Companies.Attached = len(r.companies), len(r.usedCompanies)
	var unattached []string
	for id := range r.companies {
		if !r.usedCompanies[id] {
			unattached = append(unattached, strconv.FormatInt(id, 10))
		}
	}
	report.Companies.Unattached = uniqueSorted(unattached)
}

// expectedTickets are the tickets Freshdesk still holds and lists or archives.
func (r *run) expectedTickets() []string {
	gone := map[string]bool{}
	for _, id := range r.deleted {
		gone[id] = true
	}
	ids := append(append([]string{}, r.unclassified...), r.unchanged...)
	for _, ref := range r.refs {
		if id := strconv.FormatInt(ref.id, 10); !gone[id] {
			ids = append(ids, id)
		}
	}
	return uniqueSorted(ids)
}

func (r *run) countTickets(ctx context.Context, report *Report, ticketIDs, contactIDs []string) error {
	tx, err := r.begin(ctx)
	if err != nil {
		return fmt.Errorf("reconcile tickets: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // read only
	rows, err := tx.Query(ctx, `SELECT entity,status,meta FROM import_items WHERE workspace_id=$1 AND source=$2
		AND ((entity='ticket' AND source_id=ANY($3)) OR (entity='contact' AND source_id=ANY($4)))`,
		r.workspaceID, r.ledger.source, ticketIDs, contactIDs)
	if err != nil {
		return fmt.Errorf("reconcile tickets: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var entity, status string
		var raw []byte
		var meta ticketMeta
		if err := rows.Scan(&entity, &status, &raw); err != nil {
			return fmt.Errorf("reconcile tickets: %w", err)
		}
		target := &report.Contacts
		if entity == entityTicket {
			target = &report.Tickets.EntityCount
			if err := json.Unmarshal(raw, &meta); err != nil {
				return fmt.Errorf("reconcile ticket meta: %w", err)
			}
			report.Messages.Source += meta.Messages
			report.Attachments.Source += meta.Attachments
			report.Attachments.SourceBytes += meta.AttachmentBytes
			report.TimeEntries.Source += meta.TimeEntries
		}
		addStatus(target, status)
	}
	return rows.Err()
}

func (r *run) countChildren(ctx context.Context, report *Report, ticketIDs, _ []string) error {
	tx, err := r.begin(ctx)
	if err != nil {
		return fmt.Errorf("reconcile items: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // read only
	rows, err := tx.Query(ctx, `SELECT entity,status,count(*) FROM import_items
		WHERE workspace_id=$1 AND source=$2 AND parent_source_id=ANY($3) GROUP BY entity,status`, r.workspaceID, r.ledger.source, ticketIDs)
	if err != nil {
		return fmt.Errorf("reconcile items: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var entity, status string
		var count int
		if err := rows.Scan(&entity, &status, &count); err != nil {
			return fmt.Errorf("reconcile items: %w", err)
		}
		target := map[string]*EntityCount{entityDescription: &report.Messages, entityMessage: &report.Messages,
			entityAttachment: &report.Attachments.EntityCount, entityTimeEntry: &report.TimeEntries}[entity]
		for range count {
			addStatus(target, status)
		}
	}
	return rows.Err()
}

func addStatus(target *EntityCount, status string) {
	switch status {
	case ledgerDone:
		target.Imported++
	case ledgerFailed:
		target.Failed++
	}
}

func (r *run) collectFailures(ctx context.Context, report *Report, ticketIDs, contactIDs []string) error {
	tx, err := r.begin(ctx)
	if err != nil {
		return fmt.Errorf("reconcile failures: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // read only
	rows, err := tx.Query(ctx, `SELECT entity,source_id,COALESCE(parent_source_id,''),error FROM import_items
		WHERE workspace_id=$1 AND source=$2 AND status='failed' AND ((entity='ticket' AND source_id=ANY($3))
		OR parent_source_id=ANY($3) OR (entity='contact' AND source_id=ANY($4)))
		ORDER BY entity,length(source_id),source_id`, r.workspaceID, r.ledger.source, ticketIDs, contactIDs)
	if err != nil {
		return fmt.Errorf("reconcile failures: %w", err)
	}
	defer rows.Close()
	report.Failures = []Failure{}
	for rows.Next() {
		var failure Failure
		if err := rows.Scan(&failure.Entity, &failure.FreshdeskID, &failure.TicketID, &failure.Error); err != nil {
			return fmt.Errorf("reconcile failures: %w", err)
		}
		if failure.Entity == entityTicket {
			failure.TicketID = failure.FreshdeskID
		}
		report.Failures = append(report.Failures, failure)
	}
	return rows.Err()
}

func missing(report *Report) []string {
	out := []string{}
	for _, row := range []struct {
		name  string
		count EntityCount
	}{{"contacts", report.Contacts}, {"tickets", report.Tickets.EntityCount}, {"messages", report.Messages},
		{"attachments", report.Attachments.EntityCount}, {"time entries", report.TimeEntries}, {"saved replies", report.SavedReplies.EntityCount}} {
		if !report.DryRun && row.count.Imported < row.count.Source {
			out = append(out, fmt.Sprintf("%s: %d of %d not imported", row.name, row.count.Source-row.count.Imported, row.count.Source))
		}
	}
	if !report.DryRun && report.Attachments.ImportedBytes != report.Attachments.SourceBytes {
		out = append(out, fmt.Sprintf("attachment bytes: %d stored, %d reported by Freshdesk", report.Attachments.ImportedBytes, report.Attachments.SourceBytes))
	}
	if n := len(report.Attachments.ChecksumMismatches); n > 0 {
		out = append(out, fmt.Sprintf("attachments: %d stored files no longer match their recorded SHA-256", n))
	}
	return out
}

// countSavedReplies reports the canned responses the run saw against the ledger, and adds the
// failed ones (and unreadable folders) to the failures.
func (r *run) countSavedReplies(ctx context.Context, report *Report, _, _ []string) error {
	ids := uniqueSorted(r.cannedIDs)
	report.SavedReplies = SavedReplyCount{Folders: r.cannedFolders, NotAvailable: r.cannedUnavailable, EmptySkipped: uniqueSorted(r.cannedEmpty), PersonalNotImported: uniqueSorted(r.cannedPersonal),
		AttachmentsNotImported: r.cannedAttachments, ResponsesWithAttachments: uniqueSorted(r.cannedWithFiles), UnmappedPlaceholders: map[string][]string{}}
	report.SavedReplies.Source = len(ids)
	for name, used := range r.cannedPlaceholders {
		report.SavedReplies.UnmappedPlaceholders[name] = uniqueSorted(used)
	}
	if report.Failures == nil {
		report.Failures = []Failure{}
	}
	if r.cannedListError != "" {
		report.Failures = append(report.Failures, Failure{Entity: entityCannedFolder, FreshdeskID: "all", Error: r.cannedListError})
	}
	tx, err := r.begin(ctx)
	if err != nil {
		return fmt.Errorf("reconcile saved replies: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // read only
	rows, err := tx.Query(ctx, `SELECT entity,source_id,status,error FROM import_items WHERE workspace_id=$1 AND source=$2
		AND ((entity=$3 AND source_id=ANY($4)) OR (entity=$5 AND source_id=ANY($6) AND status='failed'))
		ORDER BY entity,length(source_id),source_id`, r.workspaceID, r.ledger.source, entitySavedReply, ids, entityCannedFolder, r.cannedUnreadable)
	if err != nil {
		return fmt.Errorf("reconcile saved replies: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var entity, id, status, message string
		if err := rows.Scan(&entity, &id, &status, &message); err != nil {
			return fmt.Errorf("reconcile saved replies: %w", err)
		}
		if entity == entitySavedReply {
			addStatus(&report.SavedReplies.EntityCount, status)
		}
		if status == ledgerFailed {
			report.Failures = append(report.Failures, Failure{Entity: entity, FreshdeskID: id, Error: message})
		}
	}
	return rows.Err()
}
