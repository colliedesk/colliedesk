package helpscout

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// writeConversation creates (or, for a conversation changed since the last run, updates) the
// Colliedesk conversation with Help Scout's own timestamps, and adds its tags.
func (r *run) writeConversation(ctx context.Context, ref hsConversation, contactID string, existing *string, meta itemMeta) (string, error) {
	key := strconv.FormatInt(ref.ID, 10)
	status := r.mapStatus(ref.Status, key)
	assignee, keepAssignee := r.assignee(ref, key)
	origin := conversationOrigin(ref, r.mailbox)
	tx, err := r.begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin conversation import: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var id string
	if existing != nil {
		err = tx.QueryRow(ctx, `UPDATE conversations SET subject=$3,status=$4,
			assigned_at=CASE WHEN NOT $8 AND assignee_id IS DISTINCT FROM $5::uuid THEN NULL ELSE assigned_at END,
			assigned_by_agent_id=CASE WHEN NOT $8 AND assignee_id IS DISTINCT FROM $5::uuid THEN NULL ELSE assigned_by_agent_id END,
			assignee_id=CASE WHEN $8 THEN assignee_id ELSE $5::uuid END,updated_at=$6,origin=$7 WHERE workspace_id=$1 AND id=$2 RETURNING id::text`,
			r.workspaceID, *existing, subjectOrDefault(ref.Subject), status, assignee, ref.updatedAt(), origin, keepAssignee).Scan(&id)
	}
	if existing == nil || errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `INSERT INTO conversations(workspace_id,contact_id,subject,status,priority,assignee_id,created_at,updated_at,origin,channel)
			SELECT $1,c.id,$3,$4,'normal',$5,$6,$7,$8,$9 FROM contacts c WHERE c.workspace_id=$1 AND c.id=$2 RETURNING id::text`,
			r.workspaceID, contactID, subjectOrDefault(ref.Subject), status, assignee, ref.CreatedAt.UTC(), ref.updatedAt(), origin, domain.ChannelImported).Scan(&id)
		if err == nil {
			err = r.ledger.mark(ctx, tx, ledgerMark{entity: entityTicket, sourceID: key, targetID: &id, status: ledgerFailed,
				errText: "incomplete: the import stopped before this conversation finished", meta: &meta})
		}
	}
	if err != nil {
		return "", fmt.Errorf("write conversation: %w", err)
	}
	for _, name := range meta.Tags {
		if err := r.tagConversation(ctx, tx, id, name); err != nil {
			return "", err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit conversation import: %w", err)
	}
	return id, nil
}

// tagConversation adds a tag by name, creating it when needed; a tag the import created is recorded
// so an undo can remove it again.
func (r *run) tagConversation(ctx context.Context, tx pgx.Tx, conversationID, name string) error {
	var tagID string
	var inserted bool
	err := tx.QueryRow(ctx, `INSERT INTO tags(workspace_id,name) VALUES($1,$2)
		ON CONFLICT (workspace_id,name) DO UPDATE SET name=EXCLUDED.name RETURNING id::text,(xmax=0)`, r.workspaceID, name).Scan(&tagID, &inserted)
	if err != nil {
		return fmt.Errorf("tag conversation: %w", err)
	}
	if inserted {
		if err := r.ledger.mark(ctx, tx, ledgerMark{entity: entityTag, sourceID: name, targetID: &tagID, status: ledgerDone, meta: &itemMeta{Created: true}}); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO conversation_tags(workspace_id,conversation_id,tag_id) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`,
		r.workspaceID, conversationID, tagID); err != nil {
		return fmt.Errorf("tag conversation: %w", err)
	}
	return nil
}
