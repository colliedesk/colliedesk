package store

import (
	"context"
	"fmt"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

func (s *Store) ListMessages(ctx context.Context, workspaceID, conversationID string) ([]domain.Message, error) {
	rows, err := s.pool.Query(ctx, `SELECT m.id::text,m.conversation_id::text,m.author_agent_id::text,m.author_name,m.kind,m.body,m.body_html,m.origin,m.created_at
		FROM messages m JOIN conversations c ON c.id=m.conversation_id AND c.workspace_id=m.workspace_id
		WHERE m.workspace_id=$1 AND m.conversation_id=$2 ORDER BY m.created_at,m.id`, workspaceID, conversationID)
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	defer rows.Close()
	messages := make([]domain.Message, 0)
	for rows.Next() {
		var message domain.Message
		if err := rows.Scan(&message.ID, &message.ConversationID, &message.AuthorAgentID, &message.AuthorName, &message.Kind, &message.Body, &message.BodyHTML, &message.Origin, &message.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		message.CreatedAt = domain.UTC(message.CreatedAt)
		message.Attachments = []domain.Attachment{}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate messages: %w", err)
	}
	rows.Close()
	return messages, s.attachConversationFiles(ctx, workspaceID, conversationID, messages)
}

func (s *Store) CreateMessage(ctx context.Context, workspaceID, conversationID, agentID, kind, body string) (domain.Message, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Message{}, fmt.Errorf("begin message creation: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var message domain.Message
	err = tx.QueryRow(ctx, `INSERT INTO messages(workspace_id,conversation_id,author_agent_id,kind,body)
		SELECT $1,c.id,$3,$4,$5 FROM conversations c WHERE c.workspace_id=$1 AND c.id=$2
		RETURNING id::text,conversation_id::text,author_agent_id::text,author_name,kind,body,created_at`, workspaceID, conversationID, agentID, kind, body).
		Scan(&message.ID, &message.ConversationID, &message.AuthorAgentID, &message.AuthorName, &message.Kind, &message.Body, &message.CreatedAt)
	if err != nil {
		return domain.Message{}, fmt.Errorf("create message: %w", err)
	}
	message.CreatedAt = domain.UTC(message.CreatedAt)
	message.Attachments = []domain.Attachment{}
	if _, err := tx.Exec(ctx, `UPDATE conversations SET updated_at=now() WHERE workspace_id=$1 AND id=$2`, workspaceID, conversationID); err != nil {
		return domain.Message{}, fmt.Errorf("touch conversation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Message{}, fmt.Errorf("commit message creation: %w", err)
	}
	return message, nil
}
