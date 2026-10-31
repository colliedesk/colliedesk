package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// MaxBulkConversations is the most conversations one bulk update may change.
const MaxBulkConversations = 50

// BulkConversationUpdate holds the changes applied to every listed conversation. Tags in
// RemoveTagIDs are removed first, then tags in AddTagIDs are added.
type BulkConversationUpdate struct {
	Status       *string
	AssigneeID   **string
	AddTagIDs    []string
	RemoveTagIDs []string
	// ActorID is the agent making the change; it is recorded in the status history.
	ActorID string
	// Quiet changes the status without telling the customers (the channel's status notices).
	Quiet bool
}

// BulkUpdateConversations applies update to every conversation in ids in one transaction and
// returns how many it changed. An id that is missing or belongs to another workspace fails the
// whole update with a "not found" error and changes nothing.
func (s *Store) BulkUpdateConversations(ctx context.Context, workspaceID string, ids []string, update BulkConversationUpdate) (int, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (int, error) {
		var before map[string]string
		if update.Status != nil && !update.Quiet {
			var err error
			if before, err = statusesBefore(ctx, tx, workspaceID, ids); err != nil {
				return 0, err
			}
		}
		updated, err := bulkUpdateConversations(ctx, tx, workspaceID, ids, update)
		if err == nil && before != nil {
			err = s.sendStatusNotices(ctx, tx, workspaceID, before, *update.Status)
		}
		return updated, err
	})
}

func bulkUpdateConversations(ctx context.Context, tx pgx.Tx, workspaceID string, ids []string, update BulkConversationUpdate) (int, error) {
	if len(ids) > MaxBulkConversations {
		return 0, fmt.Errorf("bulk update conversations: too many conversations")
	}
	// Lock order, everywhere: agents row -> conversations (ORDER BY id) -> conversation_tags.
	// The assignee is checked once for the request, before any conversation row is locked.
	if update.AssigneeID != nil && *update.AssigneeID != nil {
		if err := ValidateAssignee(ctx, tx, workspaceID, **update.AssigneeID); err != nil {
			return 0, err
		}
	}
	// Holding the conversation rows next also keeps a concurrent delete from making the count wrong.
	locked, err := tx.Query(ctx, `SELECT id FROM conversations WHERE workspace_id=$1 AND id=ANY($2::uuid[])
		ORDER BY id FOR NO KEY UPDATE`, workspaceID, ids)
	if err != nil {
		return 0, fmt.Errorf("lock conversations: %w", err)
	}
	found := 0
	for locked.Next() {
		found++
	}
	locked.Close()
	if err := locked.Err(); err != nil {
		return 0, fmt.Errorf("lock conversations: %w", err)
	}
	if found != len(ids) {
		return 0, fmt.Errorf("bulk update conversations: not found")
	}
	tagIDs := distinct(append(append([]string{}, update.AddTagIDs...), update.RemoveTagIDs...))
	var tags int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM tags WHERE workspace_id=$1 AND id=ANY($2::uuid[])`, workspaceID, tagIDs).Scan(&tags); err != nil {
		return 0, fmt.Errorf("validate bulk conversation tags: %w", err)
	}
	if tags != len(tagIDs) {
		return 0, fmt.Errorf("validate conversation tag: unknown tag")
	}
	if update.Status != nil {
		if err := setStatusActor(ctx, tx, update.ActorID); err != nil {
			return 0, err
		}
	}
	if update.Status != nil || update.AssigneeID != nil {
		if _, err := tx.Exec(ctx, `UPDATE conversations SET
			status=COALESCE($3,status),
			assignee_id=CASE WHEN $4 THEN $5::uuid ELSE assignee_id END,updated_at=now()
			WHERE workspace_id=$1 AND id=ANY($2::uuid[])`,
			workspaceID, ids, update.Status, update.AssigneeID != nil, nullableString(update.AssigneeID)); err != nil {
			return 0, fmt.Errorf("bulk update conversations: %w", err)
		}
	}
	// A tag change moves updated_at only on the conversations whose tags really changed.
	if len(update.RemoveTagIDs) > 0 {
		if _, err := tx.Exec(ctx, `WITH removed AS (
				DELETE FROM conversation_tags
				WHERE workspace_id=$1 AND conversation_id=ANY($2::uuid[]) AND tag_id=ANY($3::uuid[])
				RETURNING conversation_id)
			UPDATE conversations SET updated_at=now()
			WHERE workspace_id=$1 AND id IN (SELECT conversation_id FROM removed)`,
			workspaceID, ids, update.RemoveTagIDs); err != nil {
			return 0, fmt.Errorf("bulk remove conversation tags: %w", err)
		}
	}
	if len(update.AddTagIDs) > 0 {
		if _, err := tx.Exec(ctx, `WITH added AS (
				INSERT INTO conversation_tags(workspace_id,conversation_id,tag_id)
				SELECT $1,c,t FROM unnest($2::uuid[]) AS c CROSS JOIN unnest($3::uuid[]) AS t
				ON CONFLICT DO NOTHING RETURNING conversation_id)
			UPDATE conversations SET updated_at=now()
			WHERE workspace_id=$1 AND id IN (SELECT conversation_id FROM added)`,
			workspaceID, ids, update.AddTagIDs); err != nil {
			return 0, fmt.Errorf("bulk add conversation tags: %w", err)
		}
	}
	return len(ids), nil
}

func distinct(values []string) []string {
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}
