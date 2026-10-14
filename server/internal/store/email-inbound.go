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

// Inbound limits: a loop guard per sender address and an abuse ceiling on new conversations per
// workspace. The sender is the header From, which a sender controls, so the workspace ceiling is the
// bound that holds when From rotates.
const (
	InboundRateLimit            = 10
	InboundNewConversationLimit = 200
	InboundRateWindow           = 10 * time.Minute
	bounceReason                = "The recipient's mail server bounced this reply."
)

// ErrRateLimited means a sender or the workspace exceeded an inbound limit in InboundRateWindow.
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
	// Serialize deliveries from one sender so the rate count and duplicate check see each other.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "email-inbound:"+workspaceID+":"+in.FromAddress); err != nil {
		return result, fmt.Errorf("lock inbound sender: %w", err)
	}

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
	if in.DropReason == email.DropMailerDaemon && len(in.BouncedMessageIDs) > 0 {
		if err := markBounced(ctx, tx, workspaceID, in.BouncedMessageIDs); err != nil {
			return result, err
		}
		if err := tx.Commit(ctx); err != nil {
			return result, fmt.Errorf("commit bounce: %w", err)
		}
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
		var created int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM conversations WHERE workspace_id=$1 AND created_at>$2`,
			workspaceID, now.Add(-InboundRateWindow)).Scan(&created); err != nil {
			return result, fmt.Errorf("check new conversation ceiling: %w", err)
		}
		if created >= InboundNewConversationLimit {
			return result, ErrRateLimited
		}
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
