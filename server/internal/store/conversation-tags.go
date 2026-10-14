package store

import (
	"context"
	"fmt"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

func (s *Store) AddConversationTag(ctx context.Context, workspaceID, conversationID, tagID string) (domain.Conversation, error) {
	return s.setConversationTag(ctx, workspaceID, conversationID, tagID, true)
}

func (s *Store) RemoveConversationTag(ctx context.Context, workspaceID, conversationID, tagID string) (domain.Conversation, error) {
	return s.setConversationTag(ctx, workspaceID, conversationID, tagID, false)
}

func (s *Store) setConversationTag(ctx context.Context, workspaceID, conversationID, tagID string, add bool) (domain.Conversation, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.Conversation, error) {
		return setConversationTag(ctx, tx, workspaceID, conversationID, tagID, add)
	})
}

func setConversationTag(ctx context.Context, tx pgx.Tx, workspaceID, conversationID, tagID string, add bool) (domain.Conversation, error) {
	var conversationExists, tagExists bool
	err := tx.QueryRow(ctx, `SELECT
		EXISTS(SELECT 1 FROM conversations WHERE workspace_id=$1 AND id=$2),
		EXISTS(SELECT 1 FROM tags WHERE workspace_id=$1 AND id=$3)`, workspaceID, conversationID, tagID).
		Scan(&conversationExists, &tagExists)
	if err != nil {
		return domain.Conversation{}, fmt.Errorf("validate conversation tag update: %w", err)
	}
	if !conversationExists || !tagExists {
		return domain.Conversation{}, fmt.Errorf("update conversation tag: not found")
	}
	command := `DELETE FROM conversation_tags WHERE workspace_id=$1 AND conversation_id=$2 AND tag_id=$3`
	if add {
		command = `INSERT INTO conversation_tags(workspace_id,conversation_id,tag_id) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`
	}
	result, err := tx.Exec(ctx, command, workspaceID, conversationID, tagID)
	if err != nil {
		return domain.Conversation{}, fmt.Errorf("update conversation tag: %w", err)
	}
	if result.RowsAffected() > 0 {
		if _, err := tx.Exec(ctx, `UPDATE conversations SET updated_at=now() WHERE workspace_id=$1 AND id=$2`, workspaceID, conversationID); err != nil {
			return domain.Conversation{}, fmt.Errorf("touch tagged conversation: %w", err)
		}
	}
	return getConversation(ctx, tx, workspaceID, conversationID)
}
