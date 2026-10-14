package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// messageSelect returns timeline rows with their email delivery (outgoing) and envelope (incoming).
const messageSelect = `SELECT m.id::text,m.conversation_id::text,m.author_agent_id::text,m.kind,m.body,m.created_at,m.email_meta,
	o.status,o.attempts,o.last_error,o.sent_at
	FROM messages m JOIN conversations c ON c.id=m.conversation_id AND c.workspace_id=m.workspace_id
	LEFT JOIN email_outbox o ON o.workspace_id=m.workspace_id AND o.message_id=m.id`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanMessage(row rowScanner) (domain.Message, error) {
	var message domain.Message
	var meta []byte
	var status, lastError *string
	var attempts *int32
	var sentAt *time.Time
	if err := row.Scan(&message.ID, &message.ConversationID, &message.AuthorAgentID, &message.Kind, &message.Body, &message.CreatedAt,
		&meta, &status, &attempts, &lastError, &sentAt); err != nil {
		return domain.Message{}, err
	}
	message.CreatedAt = domain.UTC(message.CreatedAt)
	if status != nil && attempts != nil {
		message.Delivery = &domain.MessageDelivery{Status: *status, Attempts: int(*attempts), LastError: lastError, SentAt: domain.UTCOptional(sentAt)}
	}
	if meta != nil {
		var envelope domain.IncomingEmail
		if err := json.Unmarshal(meta, &envelope); err != nil {
			return domain.Message{}, fmt.Errorf("decode email metadata: %w", err)
		}
		if envelope.Attachments == nil {
			envelope.Attachments = []domain.EmailAttachment{}
		}
		message.Email = &envelope
	}
	return message, nil
}

func (s *Store) ListMessages(ctx context.Context, workspaceID, conversationID string) ([]domain.Message, error) {
	rows, err := s.pool.Query(ctx, messageSelect+` WHERE m.workspace_id=$1 AND m.conversation_id=$2 ORDER BY m.created_at,m.id`, workspaceID, conversationID)
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	defer rows.Close()
	messages := make([]domain.Message, 0)
	for rows.Next() {
		message, err := scanMessage(rows)
		if err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate messages: %w", err)
	}
	return messages, nil
}

func getMessage(ctx context.Context, query querier, workspaceID, messageID string) (domain.Message, error) {
	message, err := scanMessage(query.QueryRow(ctx, messageSelect+` WHERE m.workspace_id=$1 AND m.id=$2`, workspaceID, messageID))
	if err != nil {
		return domain.Message{}, fmt.Errorf("get message: %w", err)
	}
	return message, nil
}

type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// CreateMessage stores a reply or note. A reply is queued for email when the workspace has an
// email channel; a note never is.
func (s *Store) CreateMessage(ctx context.Context, workspaceID, conversationID, agentID, kind, body string) (domain.Message, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Message{}, fmt.Errorf("begin message creation: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var messageID string
	err = tx.QueryRow(ctx, `INSERT INTO messages(workspace_id,conversation_id,author_agent_id,kind,body)
		SELECT $1,c.id,$3,$4,$5 FROM conversations c WHERE c.workspace_id=$1 AND c.id=$2
		RETURNING id::text`, workspaceID, conversationID, agentID, kind, body).Scan(&messageID)
	if err != nil {
		return domain.Message{}, fmt.Errorf("create message: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE conversations SET updated_at=now() WHERE workspace_id=$1 AND id=$2`, workspaceID, conversationID); err != nil {
		return domain.Message{}, fmt.Errorf("touch conversation: %w", err)
	}
	if kind == "outgoing" {
		if err := enqueueReply(ctx, tx, workspaceID, conversationID, messageID); err != nil {
			return domain.Message{}, err
		}
	}
	message, err := getMessage(ctx, tx, workspaceID, messageID)
	if err != nil {
		return domain.Message{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Message{}, fmt.Errorf("commit message creation: %w", err)
	}
	return message, nil
}
