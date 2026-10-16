package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// Querier is satisfied by a transaction, so callers such as importers can record
// an attachment in the same transaction as their ledger.
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// NewAttachment is the metadata of bytes already written to attachment storage.
type NewAttachment struct {
	MessageID   string
	Filename    string
	ContentType string
	Object      attachments.Object
}

const attachmentColumns = `id::text,message_id::text,filename,content_type,size,sha256,created_at`

// InsertAttachment records an attachment on a message of the same workspace.
func InsertAttachment(ctx context.Context, q Querier, workspaceID string, input NewAttachment) (domain.Attachment, error) {
	contentType := input.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	var item domain.Attachment
	// clock_timestamp, not the transaction's now(): files added in one transaction keep their order.
	err := q.QueryRow(ctx, `INSERT INTO attachments(workspace_id,message_id,filename,content_type,size,sha256,storage_key,created_at)
		SELECT $1,m.id,$3,$4,$5,$6,$7,clock_timestamp() FROM messages m WHERE m.workspace_id=$1 AND m.id=$2
		RETURNING `+attachmentColumns, workspaceID, input.MessageID, input.Filename, contentType,
		input.Object.Size, input.Object.SHA256, input.Object.Key).
		Scan(&item.ID, &item.MessageID, &item.Filename, &item.ContentType, &item.Size, &item.SHA256, &item.CreatedAt)
	if err != nil {
		return domain.Attachment{}, fmt.Errorf("create attachment: %w", err)
	}
	item.CreatedAt = domain.UTC(item.CreatedAt)
	return item, nil
}

func (s *Store) CreateAttachment(ctx context.Context, workspaceID string, input NewAttachment) (domain.Attachment, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.Attachment, error) {
		return InsertAttachment(ctx, tx, workspaceID, input)
	})
}

// GetAttachment returns attachment metadata and its storage key within one workspace.
func (s *Store) GetAttachment(ctx context.Context, workspaceID, id string) (domain.Attachment, string, error) {
	var item domain.Attachment
	var key string
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT `+attachmentColumns+`,storage_key FROM attachments WHERE workspace_id=$1 AND id=$2`, workspaceID, id).
			Scan(&item.ID, &item.MessageID, &item.Filename, &item.ContentType, &item.Size, &item.SHA256, &item.CreatedAt, &key)
	})
	if err != nil {
		return domain.Attachment{}, "", fmt.Errorf("get attachment: %w", err)
	}
	item.CreatedAt = domain.UTC(item.CreatedAt)
	return item, key, nil
}

// rowsQuerier is satisfied by a transaction.
type rowsQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func attachConversationFiles(ctx context.Context, q rowsQuerier, workspaceID, conversationID string, messages []domain.Message) error {
	return attachFiles(ctx, q, messages, `SELECT a.id::text,a.message_id::text,a.filename,a.content_type,a.size,a.sha256,a.created_at
		FROM attachments a JOIN messages m ON m.workspace_id=a.workspace_id AND m.id=a.message_id
		WHERE a.workspace_id=$1 AND m.conversation_id=$2 ORDER BY a.created_at,a.id`, workspaceID, conversationID)
}

func attachMessageFiles(ctx context.Context, q rowsQuerier, workspaceID string, message *domain.Message) error {
	messages := []domain.Message{*message}
	if err := attachFiles(ctx, q, messages, `SELECT id::text,message_id::text,filename,content_type,size,sha256,created_at
		FROM attachments WHERE workspace_id=$1 AND message_id=$2 ORDER BY created_at,id`, workspaceID, message.ID); err != nil {
		return err
	}
	message.Attachments = messages[0].Attachments
	return nil
}

func attachFiles(ctx context.Context, q rowsQuerier, messages []domain.Message, sql string, args ...any) error {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("list attachments: %w", err)
	}
	defer rows.Close()
	index := make(map[string]int, len(messages))
	for position, message := range messages {
		index[message.ID] = position
	}
	for rows.Next() {
		var item domain.Attachment
		if err := rows.Scan(&item.ID, &item.MessageID, &item.Filename, &item.ContentType, &item.Size, &item.SHA256, &item.CreatedAt); err != nil {
			return fmt.Errorf("scan attachment: %w", err)
		}
		item.CreatedAt = domain.UTC(item.CreatedAt)
		if position, ok := index[item.MessageID]; ok {
			messages[position].Attachments = append(messages[position].Attachments, item)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate attachments: %w", err)
	}
	return nil
}

// ReplaceAttachmentBytes points an existing attachment at freshly stored bytes,
// for example after its stored file was lost. It reports false when the
// attachment no longer exists on that message.
func ReplaceAttachmentBytes(ctx context.Context, q Querier, workspaceID, id string, input NewAttachment) (bool, error) {
	var replaced string
	err := q.QueryRow(ctx, `UPDATE attachments SET filename=$4,content_type=$5,size=$6,sha256=$7,storage_key=$8
		WHERE workspace_id=$1 AND id=$2 AND message_id=$3 RETURNING id::text`, workspaceID, id, input.MessageID,
		input.Filename, input.ContentType, input.Object.Size, input.Object.SHA256, input.Object.Key).Scan(&replaced)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("replace attachment bytes: %w", err)
	}
	return true, nil
}
