package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Inbound rate limit per sender address and workspace.
const (
	InboundRateLimit  = 10
	InboundRateWindow = 10 * time.Minute
)

// ErrRateLimited means the sender exceeded InboundRateLimit messages in InboundRateWindow.
var ErrRateLimited = errors.New("inbound email rate limit reached")

// InboundResult says what happened to one inbound message.
type InboundResult struct {
	Status         string  `json:"status"`
	Reason         *string `json:"reason"`
	ConversationID *string `json:"conversation_id"`
	MessageID      *string `json:"message_id"`
	Authentication string  `json:"authentication"`
}

// IngestEmail files one parsed message into the workspace: duplicate, dropped, threaded or new.
func (s *Store) IngestEmail(ctx context.Context, workspaceID string, in email.Inbound, now time.Time) (InboundResult, error) {
	result, err := s.ingest(ctx, workspaceID, in, now)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		// A concurrent delivery of the same Message-ID won the race; report it as the duplicate it is.
		return s.ingest(ctx, workspaceID, in, now)
	}
	return result, err
}

func (s *Store) ingest(ctx context.Context, workspaceID string, in email.Inbound, now time.Time) (InboundResult, error) {
	result := InboundResult{Authentication: in.Authentication}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return result, fmt.Errorf("begin inbound email: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var existingID, existingKind, existingConversation string
	err = tx.QueryRow(ctx, `SELECT id::text,kind,conversation_id::text FROM messages WHERE workspace_id=$1 AND email_message_id=$2`,
		workspaceID, in.MessageID).Scan(&existingID, &existingKind, &existingConversation)
	switch {
	case err == nil && existingKind == "incoming":
		result.Status, result.MessageID, result.ConversationID = "duplicate", &existingID, &existingConversation
		return result, nil
	case err == nil:
		return dropped(result, email.DropOwnMessage), nil
	case !errors.Is(err, pgx.ErrNoRows):
		return result, fmt.Errorf("check inbound duplicate: %w", err)
	}
	var supportAddress string
	if err := tx.QueryRow(ctx, `SELECT support_address FROM email_channels WHERE workspace_id=$1`, workspaceID).Scan(&supportAddress); err != nil {
		return result, fmt.Errorf("load email channel: %w", err)
	}
	if in.FromAddress == supportAddress {
		return dropped(result, email.DropOwnAddress), nil
	}
	if in.DropReason != "" {
		return dropped(result, in.DropReason), nil
	}
	var recent int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM messages WHERE workspace_id=$1 AND kind='incoming' AND created_at>$2 AND email_meta->>'from'=$3`,
		workspaceID, now.Add(-InboundRateWindow), in.FromAddress).Scan(&recent); err != nil {
		return result, fmt.Errorf("check inbound rate: %w", err)
	}
	if recent >= InboundRateLimit {
		return result, ErrRateLimited
	}

	conversationID, err := findThread(ctx, tx, workspaceID, in)
	if err != nil {
		return result, err
	}
	result.Status = "threaded"
	if conversationID == "" {
		result.Status = "created"
		if conversationID, err = createEmailConversation(ctx, tx, workspaceID, in); err != nil {
			return result, err
		}
	} else if _, err := tx.Exec(ctx, `UPDATE conversations SET status=CASE WHEN status IN ('resolved','closed') THEN 'open' ELSE status END,
		updated_at=now() WHERE workspace_id=$1 AND id=$2`, workspaceID, conversationID); err != nil {
		return result, fmt.Errorf("reopen conversation: %w", err)
	}

	meta, err := json.Marshal(domain.IncomingEmail{From: in.FromAddress, Authentication: in.Authentication, Attachments: in.Attachments})
	if err != nil {
		return result, fmt.Errorf("encode email metadata: %w", err)
	}
	body := in.Body
	if strings.TrimSpace(body) == "" {
		body = "(no text content)"
	}
	var messageID string
	if err := tx.QueryRow(ctx, `INSERT INTO messages(workspace_id,conversation_id,kind,body,email_message_id,email_meta)
		VALUES($1,$2,'incoming',$3,$4,$5) RETURNING id::text`, workspaceID, conversationID, body, in.MessageID, meta).Scan(&messageID); err != nil {
		return result, fmt.Errorf("store inbound message: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return result, fmt.Errorf("commit inbound email: %w", err)
	}
	result.ConversationID, result.MessageID = &conversationID, &messageID
	return result, nil
}

func dropped(result InboundResult, reason string) InboundResult {
	result.Status, result.Reason = "dropped", &reason
	return result
}

// findThread matches In-Reply-To, then References (newest first), then the reply token; all within the workspace.
func findThread(ctx context.Context, tx pgx.Tx, workspaceID string, in email.Inbound) (string, error) {
	candidates := append([]string{}, in.InReplyTo...)
	for index := len(in.References) - 1; index >= 0; index-- {
		candidates = append(candidates, in.References[index])
	}
	for _, candidate := range candidates {
		var conversationID string
		err := tx.QueryRow(ctx, `SELECT conversation_id::text FROM messages WHERE workspace_id=$1 AND email_message_id=$2`, workspaceID, candidate).Scan(&conversationID)
		if err == nil {
			return conversationID, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return "", fmt.Errorf("match thread reference: %w", err)
		}
	}
	token := email.ReplyToken(in.Recipients)
	if token == "" {
		return "", nil
	}
	var conversationID string
	err := tx.QueryRow(ctx, `SELECT id::text FROM conversations WHERE workspace_id=$1 AND email_reply_token=$2`, workspaceID, token).Scan(&conversationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("match reply token: %w", err)
	}
	return conversationID, nil
}

func createEmailConversation(ctx context.Context, tx pgx.Tx, workspaceID string, in email.Inbound) (string, error) {
	name := in.FromName
	if name == "" {
		name = in.FromAddress[:strings.LastIndex(in.FromAddress, "@")]
	}
	var contactID string
	err := tx.QueryRow(ctx, `INSERT INTO contacts(workspace_id,name,email) VALUES($1,$2,$3)
		ON CONFLICT(workspace_id,email) DO UPDATE SET email=contacts.email RETURNING id::text`, workspaceID, name, in.FromAddress).Scan(&contactID)
	if err != nil {
		return "", fmt.Errorf("find or create contact: %w", err)
	}
	subject := in.Subject
	if subject == "" {
		subject = "(no subject)"
	}
	var conversationID string
	if err := tx.QueryRow(ctx, `INSERT INTO conversations(workspace_id,contact_id,subject) VALUES($1,$2,$3) RETURNING id::text`,
		workspaceID, contactID, subject).Scan(&conversationID); err != nil {
		return "", fmt.Errorf("create conversation: %w", err)
	}
	return conversationID, nil
}
