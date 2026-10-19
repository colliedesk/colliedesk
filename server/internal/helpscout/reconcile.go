package helpscout

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// reconcile compares what Help Scout holds with what the ledger and the database hold, and
// re-hashes every stored file of the run's conversations.
func (r *run) reconcile(ctx context.Context, report *Report) error {
	conversationIDs, contactSet := []string{}, map[string]bool{}
	for _, ref := range r.refs {
		conversationIDs = append(conversationIDs, strconv.FormatInt(ref.ID, 10))
		contactSet[strconv.FormatInt(ref.PrimaryCustomer.ID, 10)] = true
	}
	contactIDs := []string{}
	for id := range contactSet {
		contactIDs = append(contactIDs, id)
	}
	contactIDs = uniqueSorted(contactIDs)
	r.reportAgents(report)
	report.Conversations.Source, report.Contacts.Source = len(conversationIDs), len(contactIDs)
	report.Conversations.Spam, report.Conversations.DeletedAtSource = uniqueSorted(r.spam), uniqueSorted(r.deleted)
	report.Conversations.Drafts = uniqueSorted(r.drafts)
	report.Verification, report.RateLimit = r.verification, r.client.rateLimitStats()
	report.Attachments.ChecksumMismatches, report.InlineImages.ChecksumMismatches = []string{}, []string{}
	steps := []func(context.Context, *Report, []string, []string) error{r.countConversations, r.countChildren, r.countTags, r.collectFailures, r.collectGoneImages}
	if !r.opts.DryRun {
		// Verify first: a damaged file turns its ledger rows back to failed.
		steps = append([]func(context.Context, *Report, []string, []string) error{r.verifyAttachments}, steps...)
	}
	for _, step := range steps {
		if err := step(ctx, report, conversationIDs, contactIDs); err != nil {
			return err
		}
	}
	report.Warnings = r.reportWarnings()
	report.Missing = missing(report)
	switch {
	case r.opts.DryRun:
		report.Status = StatusDryRun
	case len(report.Missing) > 0 || len(report.Failures) > 0:
		report.Status = StatusFailed
	case !report.Verification.Conversations.Verified:
		report.Status = StatusUnverified
	default:
		report.Status = StatusOK
	}
	report.FinishedAt = time.Now().UTC()
	return nil
}

func (r *run) reportAgents(report *Report) {
	report.Agents = AgentCount{Source: len(r.users), Matched: len(r.agentTargets), Unmatched: []string{}}
	for id, user := range r.users {
		if _, ok := r.agentTargets[id]; !ok {
			report.Agents.Unmatched = append(report.Agents.Unmatched, strings.ToLower(strings.TrimSpace(user.Email)))
		}
	}
	sort.Strings(report.Agents.Unmatched)
}

func (r *run) reportWarnings() []Warning {
	out := make([]Warning, 0, len(r.warnings))
	for _, warning := range r.warnings {
		out = append(out, Warning{Code: warning.Code, Message: warning.Message, HelpScoutIDs: uniqueSorted(warning.HelpScoutIDs)})
	}
	return out
}

// countConversations counts conversations and contacts by ledger status, and adds up the Help Scout
// side of every child count from the meta each finished conversation recorded.
func (r *run) countConversations(ctx context.Context, report *Report, conversationIDs, contactIDs []string) error {
	tx, err := r.begin(ctx)
	if err != nil {
		return fmt.Errorf("reconcile conversations: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // read only
	rows, err := tx.Query(ctx, `SELECT entity,source_id,status,meta FROM import_items WHERE workspace_id=$1 AND source=$2
		AND ((entity='ticket' AND source_id=ANY($3)) OR (entity='contact' AND source_id=ANY($4)))`,
		r.workspaceID, r.ledger.source, conversationIDs, contactIDs)
	if err != nil {
		return fmt.Errorf("reconcile conversations: %w", err)
	}
	defer rows.Close()
	report.Messages.DraftThreads, report.Attachments.Virus = []string{}, []string{}
	var short []string
	for rows.Next() {
		var entity, id, status string
		var raw []byte
		if err := rows.Scan(&entity, &id, &status, &raw); err != nil {
			return fmt.Errorf("reconcile conversations: %w", err)
		}
		if entity == entityContact {
			addStatus(&report.Contacts, status)
			continue
		}
		var meta itemMeta
		if err := json.Unmarshal(raw, &meta); err != nil {
			return fmt.Errorf("reconcile conversation meta: %w", err)
		}
		addStatus(&report.Conversations.EntityCount, status)
		if meta.ThreadsRead < meta.Threads {
			short = append(short, id)
		}
		report.Messages.Source += meta.Messages
		report.Messages.LineItems += meta.LineItems
		report.Messages.DraftThreads = append(report.Messages.DraftThreads, meta.Drafts...)
		report.Attachments.Source += meta.Attachments
		report.Attachments.Virus = append(report.Attachments.Virus, meta.Virus...)
		report.InlineImages.Source += meta.InlineImages
		report.Tags.Source += len(meta.Tags)
	}
	report.Messages.DraftThreads, report.Attachments.Virus = uniqueSorted(report.Messages.DraftThreads), uniqueSorted(report.Attachments.Virus)
	if len(short) > 0 {
		report.Verification.Conversations.Verified = false
		report.Verification.Conversations.Detail += fmt.Sprintf(" Conversations whose threads endpoint returned fewer threads than the conversation lists: %s.", strings.Join(uniqueSorted(short), ", "))
	}
	return rows.Err()
}

func (r *run) countChildren(ctx context.Context, report *Report, conversationIDs, _ []string) error {
	tx, err := r.begin(ctx)
	if err != nil {
		return fmt.Errorf("reconcile items: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // read only
	rows, err := tx.Query(ctx, `SELECT entity,status,count(*) FROM import_items WHERE workspace_id=$1 AND source=$2
		AND entity IN ('thread','attachment','inline_image') AND parent_source_id=ANY($3) GROUP BY entity,status`, r.workspaceID, r.ledger.source, conversationIDs)
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
		target := map[string]*EntityCount{entityThread: &report.Messages.EntityCount, entityAttachment: &report.Attachments.EntityCount,
			entityInlineImage: &report.InlineImages.EntityCount}[entity]
		for range count {
			addStatus(target, status)
		}
	}
	return rows.Err()
}

// countTags counts the Help Scout tags of each conversation that its Colliedesk conversation holds.
func (r *run) countTags(ctx context.Context, report *Report, conversationIDs, _ []string) error {
	tx, err := r.begin(ctx)
	if err != nil {
		return fmt.Errorf("reconcile tags: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // read only
	err = tx.QueryRow(ctx, `SELECT count(*) FROM import_items i
		CROSS JOIN LATERAL jsonb_array_elements_text(COALESCE(i.meta->'tags','[]'::jsonb)) AS t(name)
		WHERE i.workspace_id=$1 AND i.source=$2 AND i.entity='ticket' AND i.source_id=ANY($3) AND EXISTS (
			SELECT 1 FROM conversation_tags ct JOIN tags g ON g.workspace_id=ct.workspace_id AND g.id=ct.tag_id
			WHERE ct.workspace_id=i.workspace_id AND ct.conversation_id=i.target_id AND g.name=t.name)`,
		r.workspaceID, r.ledger.source, conversationIDs).Scan(&report.Tags.Imported)
	if err != nil {
		return fmt.Errorf("reconcile tags: %w", err)
	}
	return nil
}

func addStatus(target *EntityCount, status string) {
	switch status {
	case ledgerDone:
		target.Imported++
	case ledgerFailed:
		target.Failed++
	}
}

func missing(report *Report) []string {
	out := []string{}
	if report.DryRun {
		return out
	}
	for _, row := range []struct {
		name  string
		count EntityCount
	}{{"conversations", report.Conversations.EntityCount}, {"contacts", report.Contacts}, {"messages", report.Messages.EntityCount},
		{"attachments", report.Attachments.EntityCount}, {"inline images", report.InlineImages.recoverable()}, {"tags", report.Tags}} {
		if row.count.Imported < row.count.Source {
			out = append(out, fmt.Sprintf("%s: %d of %d not imported", row.name, row.count.Source-row.count.Imported, row.count.Source))
		}
	}
	if n := len(report.Attachments.ChecksumMismatches); n > 0 {
		out = append(out, fmt.Sprintf("attachments: %d stored files no longer match their recorded SHA-256", n))
	}
	if n := len(report.InlineImages.ChecksumMismatches); n > 0 {
		out = append(out, fmt.Sprintf("inline images: %d stored files no longer match their recorded SHA-256", n))
	}
	return out
}

// recoverable is the count a run is judged on: images Help Scout still had.
func (c InlineImageCount) recoverable() EntityCount {
	count := c.EntityCount
	count.Source -= c.Gone
	return count
}
