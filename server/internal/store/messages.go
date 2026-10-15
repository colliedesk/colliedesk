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
const messageSelect = `SELECT m.id::text,m.conversation_id::text,m.author_agent_id::text,m.author_name,m.kind,m.body,m.body_html,m.origin,m.created_at,m.email_meta,
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
	if err := row.Scan(&message.ID, &message.ConversationID, &message.AuthorAgentID, &message.AuthorName, &message.Kind, &message.Body,
		&message.BodyHTML, &message.Origin, &message.CreatedAt, &meta, &status, &attempts, &lastError, &sentAt); err != nil {
		return domain.Message{}, err
	}
	message.CreatedAt = domain.UTC(message.CreatedAt)
	message.Attachments = []domain.Attachment{}
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
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]domain.Message, error) {
		return listMessages(ctx, tx, workspaceID, conversationID)
	})
}

func listMessages(ctx context.Context, tx pgx.Tx, workspaceID, conversationID string) ([]domain.Message, error) {
	rows, err := tx.Query(ctx, messageSelect+` WHERE m.workspace_id=$1 AND m.conversation_id=$2 ORDER BY m.created_at,m.id`, workspaceID, conversationID)
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
	rows.Close()
	return messages, attachConversationFiles(ctx, tx, workspaceID, conversationID, messages)
}

type messageQuerier interface {
	Querier
	rowsQuerier
}

func getMessage(ctx context.Context, query messageQuerier, workspaceID, messageID string) (domain.Message, error) {
	message, err := scanMessage(query.QueryRow(ctx, messageSelect+` WHERE m.workspace_id=$1 AND m.id=$2`, workspaceID, messageID))
	if err != nil {
		return domain.Message{}, fmt.Errorf("get message: %w", err)
	}
	if err := attachMessageFiles(ctx, query, workspaceID, &message); err != nil {
		return domain.Message{}, err
	}
	return message, nil
}

// CreateMessage stores a reply or note with files already written to attachment storage (only
// Filename, ContentType and Object are read). A reply is queued for email when the workspace has an
// email channel; a note never is. When it fails, the caller owns the stored bytes.
func (s *Store) CreateMessage(ctx context.Context, workspaceID, conversationID, agentID, kind, body string, files ...NewAttachment) (domain.Message, error) {
	tx, err := s.db.BeginWorkspace(ctx, workspaceID)
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
	if err := s.insertMessageFiles(ctx, tx, workspaceID, messageID, files); err != nil {
		return domain.Message{}, err
	}
	if kind == "outgoing" {
		if err := s.enqueueReply(ctx, tx, workspaceID, conversationID, messageID); err != nil {
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

func (s *Store) insertMessageFiles(ctx context.Context, tx pgx.Tx, workspaceID, messageID string, files []NewAttachment) error {
	if len(files) == 0 {
		return nil
	}
	var total int64
	for _, file := range files {
		total += file.Object.Size
	}
	if err := s.CheckStorage(ctx, tx, workspaceID, total, s.now()); err != nil {
		return err
	}
	for _, file := range files {
		file.MessageID = messageID
		if _, err := InsertAttachment(ctx, tx, workspaceID, file); err != nil {
			return err
		}
	}
	return nil
}
