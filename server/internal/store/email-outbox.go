package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/jackc/pgx/v5"
)

const maxReplyReferences = 20

// ErrDeliveryNotFailed means a retry was requested for a delivery that is queued or already sent.
var ErrDeliveryNotFailed = errors.New("only a failed delivery can be retried")

// Delivery is one claimed outbox row with everything needed to compose the email.
type Delivery struct {
	OutboxID, WorkspaceID, MessageID string
	Attempts                         int
	WorkspaceName, SupportAddress    string
	ContactName, ContactEmail        string
	Subject, Body                    string
	EmailMessageID, ReplyToken       string
	InReplyTo                        string
	References                       []string
}

// enqueueReply gives a reply its Message-ID and queues it when the workspace has an email channel.
// The Message-ID is fixed here so every retry, including after a crash, carries the same one.
func enqueueReply(ctx context.Context, tx pgx.Tx, workspaceID, conversationID, messageID string) error {
	var supportAddress string
	err := tx.QueryRow(ctx, `SELECT support_address FROM email_channels WHERE workspace_id=$1`, workspaceID).Scan(&supportAddress)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load email channel: %w", err)
	}
	emailMessageID, err := email.NewMessageID(email.Domain(supportAddress))
	if err != nil {
		return err
	}
	token, err := email.NewToken()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE messages SET email_message_id=$3 WHERE workspace_id=$1 AND id=$2`, workspaceID, messageID, emailMessageID); err != nil {
		return fmt.Errorf("assign Message-ID: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE conversations SET email_reply_token=COALESCE(email_reply_token,$3) WHERE workspace_id=$1 AND id=$2`,
		workspaceID, conversationID, token); err != nil {
		return fmt.Errorf("assign reply token: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO email_outbox(workspace_id,message_id) VALUES($1,$2)`, workspaceID, messageID); err != nil {
		return fmt.Errorf("queue reply: %w", err)
	}
	return nil
}

// ClaimDueDeliveries leases due rows and counts the attempt. A lease that expires without a result
// (crash, timeout) makes the row due again: delivery is at least once, deduplicated by Message-ID.
func (s *Store) ClaimDueDeliveries(ctx context.Context, now time.Time, limit int, lease time.Duration) ([]Delivery, error) {
	rows, err := s.pool.Query(ctx, `WITH due AS (
			SELECT id FROM email_outbox WHERE status='queued' AND next_attempt_at<=$1 AND (locked_until IS NULL OR locked_until<=$1)
			ORDER BY next_attempt_at LIMIT $2 FOR UPDATE SKIP LOCKED)
		UPDATE email_outbox o SET locked_until=$3,attempts=o.attempts+1,updated_at=$1 FROM due WHERE o.id=due.id
		RETURNING o.id::text,o.workspace_id::text,o.message_id::text,o.attempts`, now, limit, now.Add(lease))
	if err != nil {
		return nil, fmt.Errorf("claim deliveries: %w", err)
	}
	claimed, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Delivery, error) {
		var item Delivery
		err := row.Scan(&item.OutboxID, &item.WorkspaceID, &item.MessageID, &item.Attempts)
		return item, err
	})
	if err != nil {
		return nil, fmt.Errorf("scan claimed deliveries: %w", err)
	}
	for index := range claimed {
		if err := s.loadDelivery(ctx, &claimed[index]); err != nil {
			return nil, err
		}
	}
	return claimed, nil
}

func (s *Store) loadDelivery(ctx context.Context, item *Delivery) error {
	var conversationID string
	var createdAt time.Time
	err := s.pool.QueryRow(ctx, `SELECT w.name,COALESCE(ch.support_address,''),ct.name,ct.email,c.subject,m.body,
			COALESCE(m.email_message_id,''),COALESCE(c.email_reply_token,''),c.id::text,m.created_at
		FROM messages m JOIN conversations c ON c.workspace_id=m.workspace_id AND c.id=m.conversation_id
		JOIN contacts ct ON ct.workspace_id=c.workspace_id AND ct.id=c.contact_id
		JOIN workspaces w ON w.id=m.workspace_id
		LEFT JOIN email_channels ch ON ch.workspace_id=m.workspace_id
		WHERE m.workspace_id=$1 AND m.id=$2`, item.WorkspaceID, item.MessageID).
		Scan(&item.WorkspaceName, &item.SupportAddress, &item.ContactName, &item.ContactEmail, &item.Subject, &item.Body,
			&item.EmailMessageID, &item.ReplyToken, &conversationID, &createdAt)
	if err != nil {
		return fmt.Errorf("load delivery %s: %w", item.OutboxID, err)
	}
	rows, err := s.pool.Query(ctx, `SELECT email_message_id,kind FROM messages
		WHERE workspace_id=$1 AND conversation_id=$2 AND email_message_id IS NOT NULL AND id<>$3 AND created_at<=$4
		ORDER BY created_at,id`, item.WorkspaceID, conversationID, item.MessageID, createdAt)
	if err != nil {
		return fmt.Errorf("load thread references: %w", err)
	}
	defer rows.Close()
	var lastIncoming string
	for rows.Next() {
		var id, kind string
		if err := rows.Scan(&id, &kind); err != nil {
			return fmt.Errorf("scan thread reference: %w", err)
		}
		item.References = append(item.References, id)
		if kind == "incoming" {
			lastIncoming = id
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate thread references: %w", err)
	}
	// Reply to the customer's latest message; without one, continue our own thread.
	item.InReplyTo = lastIncoming
	if item.InReplyTo == "" && len(item.References) > 0 {
		item.InReplyTo = item.References[len(item.References)-1]
	}
	if len(item.References) > maxReplyReferences {
		item.References = item.References[len(item.References)-maxReplyReferences:]
	}
	return nil
}

// MarkDeliverySent records a successful hand-off to the SMTP relay.
func (s *Store) MarkDeliverySent(ctx context.Context, item Delivery, now time.Time) error {
	return s.finishDelivery(ctx, item, `status='sent',sent_at=$3,last_error=NULL`, now)
}

// MarkDeliveryRetry keeps the row queued and schedules the next attempt.
func (s *Store) MarkDeliveryRetry(ctx context.Context, item Delivery, next time.Time, reason string) error {
	return s.finishDelivery(ctx, item, `next_attempt_at=$3,last_error=$4`, next, reason)
}

// MarkDeliveryFailed stops automatic retries; an agent can requeue with RetryDelivery.
func (s *Store) MarkDeliveryFailed(ctx context.Context, item Delivery, reason string) error {
	return s.finishDelivery(ctx, item, `status='failed',last_error=$3`, reason)
}

func (s *Store) finishDelivery(ctx context.Context, item Delivery, set string, args ...any) error {
	_, err := s.pool.Exec(ctx, `UPDATE email_outbox SET `+set+`,locked_until=NULL,updated_at=now() WHERE workspace_id=$1 AND id=$2`,
		append([]any{item.WorkspaceID, item.OutboxID}, args...)...)
	if err != nil {
		return fmt.Errorf("update delivery %s: %w", item.OutboxID, err)
	}
	return nil
}

// RetryDelivery requeues a failed delivery of a reply in this workspace and conversation.
func (s *Store) RetryDelivery(ctx context.Context, workspaceID, conversationID, messageID string) (domain.Message, error) {
	result, err := s.pool.Exec(ctx, `UPDATE email_outbox o SET status='queued',attempts=0,next_attempt_at=now(),locked_until=NULL,
			last_error=NULL,updated_at=now()
		FROM messages m WHERE o.workspace_id=$1 AND o.message_id=$3 AND o.status='failed'
		AND m.workspace_id=o.workspace_id AND m.id=o.message_id AND m.conversation_id=$2`, workspaceID, conversationID, messageID)
	if err != nil {
		return domain.Message{}, fmt.Errorf("retry delivery: %w", err)
	}
	message, err := getMessage(ctx, s.pool, workspaceID, messageID)
	if err != nil || message.ConversationID != conversationID || message.Delivery == nil {
		return domain.Message{}, fmt.Errorf("retry delivery: not found")
	}
	if result.RowsAffected() != 1 {
		return domain.Message{}, ErrDeliveryNotFailed
	}
	return message, nil
}
