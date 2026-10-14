package freshdesk

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

// writeConversation creates (or, for a ticket changed since the last run,
// updates) the CollieDesk conversation with Freshdesk's own timestamps.
func (r *run) writeConversation(ctx context.Context, ticket fdTicket, contactID string, existing *string, archived bool) (string, error) {
	key := strconv.FormatInt(ticket.ID, 10)
	status, ok := mapStatus(ticket.Status)
	if !ok {
		r.warn("custom_status_mapped", "Ticket has a custom Freshdesk status; it was imported as pending.", key)
	}
	priority, ok := mapPriority(ticket.Priority)
	if !ok {
		r.warn("unknown_priority_mapped", "Ticket has an unknown Freshdesk priority; it was imported as normal.", key)
	}
	var assignee *string
	if ticket.ResponderID != nil {
		if id, matched := r.agentTargets[*ticket.ResponderID]; matched {
			assignee = &id
		}
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin conversation import: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var id string
	if existing != nil {
		err = tx.QueryRow(ctx, `UPDATE conversations SET subject=$3,status=$4,priority=$5,assignee_id=$6,updated_at=$7,origin=$8
			WHERE workspace_id=$1 AND id=$2 RETURNING id::text`, r.workspaceID, *existing,
			subjectOrDefault(ticket.Subject), status, priority, assignee, ticket.UpdatedAt.UTC(), ticketOrigin(ticket)).Scan(&id)
	}
	if existing == nil || errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `INSERT INTO conversations(workspace_id,contact_id,subject,status,priority,assignee_id,created_at,updated_at,origin)
			SELECT $1,c.id,$3,$4,$5,$6,$7,$8,$9 FROM contacts c WHERE c.workspace_id=$1 AND c.id=$2 RETURNING id::text`,
			r.workspaceID, contactID, subjectOrDefault(ticket.Subject), status, priority, assignee,
			ticket.CreatedAt.UTC(), ticket.UpdatedAt.UTC(), ticketOrigin(ticket)).Scan(&id)
		if err == nil {
			err = r.ledger.mark(ctx, tx, ledgerMark{entity: entityTicket, sourceID: key, targetID: &id, status: ledgerFailed,
				errText: "incomplete: the import stopped before this ticket finished", meta: &ticketMeta{Archived: archived}})
		}
	}
	if err != nil {
		return "", fmt.Errorf("write conversation: %w", err)
	}
	for _, name := range ticket.Tags {
		if name = strings.TrimSpace(name); name == "" {
			continue
		}
		if err := r.tagConversation(ctx, tx, id, name); err != nil {
			return "", err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit conversation import: %w", err)
	}
	return id, nil
}

// importMessage writes one timeline message and its attachments; it returns
// the number of items that failed (already recorded in the ledger).
func (r *run) importMessage(ctx context.Context, ticketKey, conversationID, entity, sourceID string, message newMessage, files []fdAttachment, refresh func(context.Context, int64) (fdAttachment, error)) int {
	messageID, ok, err := r.ledger.done(ctx, entity, sourceID)
	if err == nil && !ok {
		messageID, err = r.insertMessage(ctx, ticketKey, conversationID, entity, sourceID, message)
	}
	if err != nil {
		r.itemFailed(ctx, entity, sourceID, ticketKey, err)
		return 1 + len(files)
	}
	failed := 0
	for _, file := range files {
		if err := r.importAttachment(ctx, ticketKey, messageID, file, refresh); err != nil {
			if errors.Is(err, errDiskFull) {
				// Every later download would fail too: stop the run.
				if r.internalErr == nil {
					r.internalErr = err
				}
				return failed + 1
			}
			r.itemFailed(ctx, entityAttachment, strconv.FormatInt(file.ID, 10), ticketKey, err)
			failed++
		}
	}
	return failed
}

func (r *run) insertMessage(ctx context.Context, ticketKey, conversationID, entity, sourceID string, m newMessage) (string, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin message import: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var id string
	err = tx.QueryRow(ctx, `INSERT INTO messages(workspace_id,conversation_id,author_agent_id,author_name,kind,body,body_html,origin,created_at)
		SELECT $1,c.id,$3,$4,$5,$6,$7,$8,$9 FROM conversations c WHERE c.workspace_id=$1 AND c.id=$2 RETURNING id::text`,
		r.workspaceID, conversationID, m.authorAgentID, m.authorName, m.kind, m.body, m.html, m.origin, m.createdAt).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("write message: %w", err)
	}
	if err := r.ledger.mark(ctx, tx, ledgerMark{entity: entity, sourceID: sourceID, parentID: ticketKey, targetID: &id, status: ledgerDone}); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit message import: %w", err)
	}
	return id, nil
}

func (r *run) importTimeEntry(ctx context.Context, ticketKey, conversationID string, entry fdTimeEntry) int {
	key := strconv.FormatInt(entry.ID, 10)
	if err := r.writeTimeEntry(ctx, ticketKey, conversationID, key, entry); err != nil {
		r.itemFailed(ctx, entityTimeEntry, key, ticketKey, err)
		return 1
	}
	return 0
}

func (r *run) writeTimeEntry(ctx context.Context, ticketKey, conversationID, key string, entry fdTimeEntry) error {
	if _, ok, err := r.ledger.done(ctx, entityTimeEntry, key); err != nil || ok {
		return err
	}
	minutes, err := parseTimeSpent(entry.TimeSpent)
	if err != nil {
		return err
	}
	var agentID *string
	agentName := ""
	if entry.AgentID != nil {
		agentID, agentName = r.author(*entry.AgentID)
	}
	executed := entry.CreatedAt.UTC()
	if entry.ExecutedAt != nil {
		executed = entry.ExecutedAt.UTC()
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin time entry import: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var id string
	err = tx.QueryRow(ctx, `INSERT INTO time_entries(workspace_id,conversation_id,agent_id,agent_name,minutes,note,billable,executed_at,created_at)
		SELECT $1,c.id,$3,$4,$5,$6,$7,$8,$9 FROM conversations c WHERE c.workspace_id=$1 AND c.id=$2 RETURNING id::text`,
		r.workspaceID, conversationID, agentID, agentName, minutes, entry.Note, entry.Billable, executed, orNow(entry.CreatedAt)).Scan(&id)
	if err != nil {
		return fmt.Errorf("write time entry: %w", err)
	}
	if err := r.ledger.mark(ctx, tx, ledgerMark{entity: entityTimeEntry, sourceID: key, parentID: ticketKey, targetID: &id, status: ledgerDone}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit time entry import: %w", err)
	}
	return nil
}

// tagConversation adds a tag by name, creating it when needed; a tag the
// import created is recorded so an undo can remove it again.
func (r *run) tagConversation(ctx context.Context, tx pgx.Tx, conversationID, name string) error {
	var tagID string
	var inserted bool
	err := tx.QueryRow(ctx, `INSERT INTO tags(workspace_id,name) VALUES($1,$2)
		ON CONFLICT (workspace_id,name) DO UPDATE SET name=EXCLUDED.name RETURNING id::text,(xmax=0)`, r.workspaceID, name).Scan(&tagID, &inserted)
	if err != nil {
		return fmt.Errorf("tag conversation: %w", err)
	}
	if inserted {
		if err := r.ledger.mark(ctx, tx, ledgerMark{entity: entityTag, sourceID: name, targetID: &tagID, status: ledgerDone, meta: &ticketMeta{Created: true}}); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO conversation_tags(workspace_id,conversation_id,tag_id) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`,
		r.workspaceID, conversationID, tagID); err != nil {
		return fmt.Errorf("tag conversation: %w", err)
	}
	return nil
}
