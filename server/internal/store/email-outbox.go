package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/jackc/pgx/v5"
)

const maxReplyReferences = 20

// ErrLeaseLost means another worker reclaimed the delivery; this worker's result is discarded.
var ErrLeaseLost = errors.New("delivery lease was lost")

// Delivery is one claimed outbox row with everything needed to compose the email.
type Delivery struct {
	OutboxID, WorkspaceID, MessageID string
	ClaimToken                       string
	Attempts                         int
	WorkspaceName, SupportAddress    string
	DomainVerified                   bool
	// EmailPaused is set while the workspace's sending is paused after spam complaints.
	EmailPaused                bool
	ContactName, ContactEmail  string
	Subject, Body              string
	EmailMessageID, ReplyToken string
	InReplyTo                  string
	References                 []string
	// Opening is the first email of a conversation the team started: nothing came before it, so it is a
	// new thread and not a reply.
	Opening bool
	// CC is the list stored with the reply; a retry sends to the same addresses.
	CC    []string
	Files []DeliveryFile
	// Transport is what the reply was accepted for: "smtp", or "gmail" through Mailbox.
	Transport string
	// Mailbox is the workspace's connected Gmail account now; nil when none is connected.
	Mailbox *DeliveryMailbox
}

// DeliveryMailbox is the connected Gmail account a reply leaves through, with its sealed grant.
type DeliveryMailbox struct {
	Address, Status, ErrorCode string
	Grant                      SealedGrant
}

// DeliveryFile is a file attached to the reply, kept in attachment storage under StorageKey.
type DeliveryFile struct {
	Filename, ContentType, StorageKey, SHA256 string
}

// ClaimDueDeliveries leases due rows, counts the attempt and stamps a claim token that every finish
// must present. A lease that expires without a result makes the row due again: delivery is at least
// once, deduplicated by Message-ID. A row whose data cannot be loaded fails alone.
func (s *Store) ClaimDueDeliveries(ctx context.Context, now time.Time, limit int, lease time.Duration) ([]Delivery, error) {
	// Only the lease crosses workspaces; each claimed row is then loaded and finished in its own workspace.
	var claimed []Delivery
	err := s.db.Privileged(ctx, database.PrivilegeOutboxClaim, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `WITH due AS (
				SELECT id FROM email_outbox WHERE status='queued' AND next_attempt_at<=$1 AND (locked_until IS NULL OR locked_until<=$1)
				ORDER BY next_attempt_at LIMIT $2 FOR UPDATE SKIP LOCKED)
			UPDATE email_outbox o SET locked_until=$3,attempts=o.attempts+1,claim_token=gen_random_uuid(),updated_at=$1 FROM due WHERE o.id=due.id
			RETURNING o.id::text,o.workspace_id::text,o.message_id::text,o.claim_token::text,o.attempts,o.transport`, now, limit, now.Add(lease))
		if err != nil {
			return fmt.Errorf("claim deliveries: %w", err)
		}
		claimed, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Delivery, error) {
			var item Delivery
			err := row.Scan(&item.OutboxID, &item.WorkspaceID, &item.MessageID, &item.ClaimToken, &item.Attempts, &item.Transport)
			return item, err
		})
		if err != nil {
			return fmt.Errorf("scan claimed deliveries: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	loaded := claimed[:0]
	var errs []error
	for _, item := range claimed {
		if err := s.loadDelivery(ctx, &item); err != nil {
			errs = append(errs, err, s.MarkDeliveryFailed(ctx, item, "The reply could not be loaded for sending."))
			continue
		}
		loaded = append(loaded, item)
	}
	return loaded, errors.Join(errs...)
}

func (s *Store) loadDelivery(ctx context.Context, item *Delivery) error {
	return s.db.InWorkspace(ctx, item.WorkspaceID, func(tx pgx.Tx) error { return loadDelivery(ctx, tx, item) })
}

func loadDelivery(ctx context.Context, tx pgx.Tx, item *Delivery) error {
	var conversationID string
	var createdAt time.Time
	err := tx.QueryRow(ctx, `SELECT w.name,COALESCE(ch.support_address,''),COALESCE(ch.domain_verified_at IS NOT NULL,false),w.email_paused_at IS NOT NULL,
			ct.name,ct.email,c.subject,m.body,COALESCE(m.email_message_id,''),COALESCE(c.email_reply_token,''),c.id::text,m.created_at,m.cc
		FROM messages m JOIN conversations c ON c.workspace_id=m.workspace_id AND c.id=m.conversation_id
		JOIN contacts ct ON ct.workspace_id=c.workspace_id AND ct.id=c.contact_id
		JOIN workspaces w ON w.id=m.workspace_id
		LEFT JOIN email_channels ch ON ch.workspace_id=m.workspace_id
		WHERE m.workspace_id=$1 AND m.id=$2`, item.WorkspaceID, item.MessageID).
		Scan(&item.WorkspaceName, &item.SupportAddress, &item.DomainVerified, &item.EmailPaused, &item.ContactName, &item.ContactEmail, &item.Subject, &item.Body,
			&item.EmailMessageID, &item.ReplyToken, &conversationID, &createdAt, &item.CC)
	if err != nil {
		return fmt.Errorf("load delivery %s: %w", item.OutboxID, err)
	}
	rows, err := tx.Query(ctx, `SELECT email_message_id,kind FROM messages
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
	if len(item.References) == 0 {
		if err := tx.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM messages WHERE workspace_id=$1 AND conversation_id=$2 AND kind='incoming')`,
			item.WorkspaceID, conversationID).Scan(&item.Opening); err != nil {
			return fmt.Errorf("check thread start: %w", err)
		}
	}
	var mailbox DeliveryMailbox
	err = tx.QueryRow(ctx, `SELECT address,status,COALESCE(error_code,''),token_key_id,refresh_token_sealed FROM mailbox_connections WHERE workspace_id=$1`, item.WorkspaceID).
		Scan(&mailbox.Address, &mailbox.Status, &mailbox.ErrorCode, &mailbox.Grant.KeyID, &mailbox.Grant.Sealed)
	switch {
	case err == nil:
		item.Mailbox = &mailbox
	case !errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("load mailbox connection: %w", err)
	}
	// Reply to the customer's latest message; without one, continue our own thread.
	item.InReplyTo = lastIncoming
	if item.InReplyTo == "" && len(item.References) > 0 {
		item.InReplyTo = item.References[len(item.References)-1]
	}
	if len(item.References) > maxReplyReferences {
		item.References = item.References[len(item.References)-maxReplyReferences:]
	}
	fileRows, err := tx.Query(ctx, `SELECT filename,content_type,storage_key,sha256 FROM attachments
		WHERE workspace_id=$1 AND message_id=$2 ORDER BY created_at,id`, item.WorkspaceID, item.MessageID)
	if err != nil {
		return fmt.Errorf("load reply files: %w", err)
	}
	item.Files, err = pgx.CollectRows(fileRows, func(row pgx.CollectableRow) (DeliveryFile, error) {
		var file DeliveryFile
		err := row.Scan(&file.Filename, &file.ContentType, &file.StorageKey, &file.SHA256)
		return file, err
	})
	if err != nil {
		return fmt.Errorf("scan reply files: %w", err)
	}
	return nil
}

// CountSentSince counts replies the workspace handed to the relay since the given time.
func (s *Store) CountSentSince(ctx context.Context, workspaceID string, since time.Time) (int, error) {
	count, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (int, error) {
		var count int
		err := tx.QueryRow(ctx, `SELECT count(*) FROM email_outbox WHERE workspace_id=$1 AND status='sent' AND sent_at>$2`,
			workspaceID, since).Scan(&count)
		return count, err
	})
	if err != nil {
		return 0, fmt.Errorf("count sent replies: %w", err)
	}
	return count, nil
}

// MarkDeliverySent records a successful hand-off to the SMTP relay. note, kept as the delivery's
// last error, names copied addresses the reply did not reach; empty when it reached everyone.
func (s *Store) MarkDeliverySent(ctx context.Context, item Delivery, sentAt time.Time, note string) error {
	return s.finishDelivery(ctx, item, `status='sent',sent_at=$4,last_error=NULLIF($5,'')`, sentAt, note)
}

// MarkDeliveryRetry keeps the row queued and schedules the next attempt.
func (s *Store) MarkDeliveryRetry(ctx context.Context, item Delivery, next time.Time, reason string) error {
	return s.finishDelivery(ctx, item, `next_attempt_at=$4,last_error=$5`, next, reason)
}

// DeferDelivery postpones a claimed row without spending the attempt it was claimed with.
func (s *Store) DeferDelivery(ctx context.Context, item Delivery, next time.Time, reason string) error {
	return s.finishDelivery(ctx, item, `next_attempt_at=$4,last_error=$5,attempts=GREATEST(attempts-1,0)`, next, reason)
}

// DeferDeliveryForPause postpones a claimed row because the workspace's sending is paused, without spending
// the attempt. The row is marked so that lifting the pause releases it and nothing else.
func (s *Store) DeferDeliveryForPause(ctx context.Context, item Delivery, next time.Time, reason string) error {
	return s.finish(ctx, item, true, `next_attempt_at=$4,last_error=$5,attempts=GREATEST(attempts-1,0)`, next, reason)
}

// MarkDeliveryFailed stops automatic retries; an agent can requeue with RetryDelivery.
func (s *Store) MarkDeliveryFailed(ctx context.Context, item Delivery, reason string) error {
	return s.finishDelivery(ctx, item, `status='failed',last_error=$4`, reason)
}

// finishDelivery applies a result only while this worker still holds the claim, so a worker whose
// lease expired cannot overwrite the result of the worker that reclaimed the row.
func (s *Store) finishDelivery(ctx context.Context, item Delivery, set string, args ...any) error {
	return s.finish(ctx, item, false, set, args...)
}

func (s *Store) finish(ctx context.Context, item Delivery, heldByPause bool, set string, args ...any) error {
	updated, err := scoped(ctx, s, item.WorkspaceID, func(tx pgx.Tx) (int64, error) {
		result, err := tx.Exec(ctx, `UPDATE email_outbox SET `+set+fmt.Sprintf(",held_by_pause=%t", heldByPause)+`,locked_until=NULL,claim_token=NULL,updated_at=now()
			WHERE workspace_id=$1 AND id=$2 AND claim_token=$3::uuid AND status='queued'`,
			append([]any{item.WorkspaceID, item.OutboxID, item.ClaimToken}, args...)...)
		return result.RowsAffected(), err
	})
	if err != nil {
		return fmt.Errorf("update delivery %s: %w", item.OutboxID, err)
	}
	if updated != 1 {
		return fmt.Errorf("update delivery %s: %w", item.OutboxID, ErrLeaseLost)
	}
	return nil
}
