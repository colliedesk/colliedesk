package store

import (
	"context"
	"fmt"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

type ConversationUpdate struct {
	Status     *string
	Priority   *string
	AssigneeID **string
	TagIDs     *[]string
}

func (s *Store) UpdateConversation(ctx context.Context, workspaceID, id string, update ConversationUpdate) (domain.Conversation, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Conversation{}, fmt.Errorf("begin conversation update: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if update.TagIDs != nil {
		seen := make(map[string]bool, len(*update.TagIDs))
		for _, tagID := range *update.TagIDs {
			if seen[tagID] {
				continue
			}
			seen[tagID] = true
			var exists bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM tags WHERE workspace_id=$1 AND id=$2)`, workspaceID, tagID).Scan(&exists); err != nil {
				return domain.Conversation{}, fmt.Errorf("validate conversation tag: %w", err)
			}
			if !exists {
				return domain.Conversation{}, fmt.Errorf("validate conversation tag: unknown tag")
			}
		}
	}
	result, err := tx.Exec(ctx, `UPDATE conversations SET
		status=COALESCE($3,status),priority=COALESCE($4,priority),
		assignee_id=CASE WHEN $5 THEN $6::uuid ELSE assignee_id END,updated_at=now()
		WHERE workspace_id=$1 AND id=$2`, workspaceID, id, update.Status, update.Priority, update.AssigneeID != nil, nullableString(update.AssigneeID))
	if err != nil {
		return domain.Conversation{}, fmt.Errorf("update conversation: %w", err)
	}
	if result.RowsAffected() != 1 {
		return domain.Conversation{}, fmt.Errorf("update conversation: not found")
	}
	if update.TagIDs != nil {
		if _, err := tx.Exec(ctx, `DELETE FROM conversation_tags WHERE workspace_id=$1 AND conversation_id=$2`, workspaceID, id); err != nil {
			return domain.Conversation{}, fmt.Errorf("clear conversation tags: %w", err)
		}
		for _, tagID := range *update.TagIDs {
			if _, err := tx.Exec(ctx, `INSERT INTO conversation_tags(workspace_id,conversation_id,tag_id)
				SELECT $1,$2,t.id FROM tags t WHERE t.workspace_id=$1 AND t.id=$3`, workspaceID, id, tagID); err != nil {
				return domain.Conversation{}, fmt.Errorf("add conversation tag: %w", err)
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Conversation{}, fmt.Errorf("commit conversation update: %w", err)
	}
	return s.GetConversation(ctx, workspaceID, id)
}

func nullableString(value **string) any {
	if value == nil || *value == nil {
		return nil
	}
	return **value
}
