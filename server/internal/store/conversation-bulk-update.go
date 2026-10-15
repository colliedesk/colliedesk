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
}

// BulkUpdateConversations applies update to every conversation in ids in one transaction and
// returns how many it changed. An id that is missing or belongs to another workspace fails the
// whole update with a "not found" error and changes nothing.
func (s *Store) BulkUpdateConversations(ctx context.Context, workspaceID string, ids []string, update BulkConversationUpdate) (int, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (int, error) {
		return bulkUpdateConversations(ctx, tx, workspaceID, distinct(ids), update)
	})
}

func bulkUpdateConversations(ctx context.Context, tx pgx.Tx, workspaceID string, ids []string, update BulkConversationUpdate) (int, error) {
	if len(ids) > MaxBulkConversations {
		return 0, fmt.Errorf("bulk update conversations: too many conversations")
	}
	tagIDs := distinct(append(append([]string{}, update.AddTagIDs...), update.RemoveTagIDs...))
	var conversations, tags int
	if err := tx.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM conversations WHERE workspace_id=$1 AND id=ANY($2::uuid[])),
		(SELECT count(*) FROM tags WHERE workspace_id=$1 AND id=ANY($3::uuid[]))`,
		workspaceID, ids, tagIDs).Scan(&conversations, &tags); err != nil {
		return 0, fmt.Errorf("validate bulk conversation update: %w", err)
	}
	if conversations != len(ids) {
		return 0, fmt.Errorf("bulk update conversations: not found")
	}
	if tags != len(tagIDs) {
		return 0, fmt.Errorf("validate conversation tag: unknown tag")
	}
	if _, err := tx.Exec(ctx, `UPDATE conversations SET
		status=COALESCE($3,status),
		assignee_id=CASE WHEN $4 THEN $5::uuid ELSE assignee_id END,updated_at=now()
		WHERE workspace_id=$1 AND id=ANY($2::uuid[])`,
		workspaceID, ids, update.Status, update.AssigneeID != nil, nullableString(update.AssigneeID)); err != nil {
		return 0, fmt.Errorf("bulk update conversations: %w", err)
	}
	if len(update.RemoveTagIDs) > 0 {
		if _, err := tx.Exec(ctx, `DELETE FROM conversation_tags
			WHERE workspace_id=$1 AND conversation_id=ANY($2::uuid[]) AND tag_id=ANY($3::uuid[])`,
			workspaceID, ids, update.RemoveTagIDs); err != nil {
			return 0, fmt.Errorf("bulk remove conversation tags: %w", err)
		}
	}
	if len(update.AddTagIDs) > 0 {
		if _, err := tx.Exec(ctx, `INSERT INTO conversation_tags(workspace_id,conversation_id,tag_id)
			SELECT $1,c,t FROM unnest($2::uuid[]) AS c CROSS JOIN unnest($3::uuid[]) AS t
			ON CONFLICT DO NOTHING`, workspaceID, ids, update.AddTagIDs); err != nil {
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
