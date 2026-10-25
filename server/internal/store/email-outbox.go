package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

const maxReplyReferences = 20

// ErrLeaseLost means another worker reclaimed the delivery; this worker's result is discarded.
var ErrLeaseLost = errors.New("delivery lease was lost")

// Delivery is one claimed outbox row with everything needed to compose the email.
type Delivery struct {
	OutboxID, WorkspaceID, MessageID string
	// ChannelID is the email channel the reply leaves through.
	ChannelID  string
	ClaimToken string
	Attempts   int
	// SenderName is the channel's name in From and Reply-To; empty only when the channel is gone.
	SenderName, SupportAddress string
	DomainVerified             bool
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
	// Mailbox is the Gmail account connected to the channel now; nil when none is connected.
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

func loadDelivery(ctx context.Context, tx pgx.Tx, item *Delivery) error {
	var conversationID string
	var createdAt time.Time
	err := tx.QueryRow(ctx, `SELECT COALESCE(ch.sender_name,''),COALESCE(ch.support_address,''),COALESCE(ch.verified,false),w.email_paused_at IS NOT NULL,
			ct.name,ct.email,c.subject,m.body,COALESCE(m.email_message_id,''),COALESCE(c.email_reply_token,''),c.id::text,m.created_at,m.cc
		FROM messages m JOIN conversations c ON c.workspace_id=m.workspace_id AND c.id=m.conversation_id
		JOIN contacts ct ON ct.workspace_id=c.workspace_id AND ct.id=c.contact_id
		JOIN workspaces w ON w.id=m.workspace_id
		LEFT JOIN (SELECT e.workspace_id,e.channel_id,e.sender_name,e.support_address,d.domain_verified_at IS NOT NULL AS verified
			FROM email_channels e`+emailDomainJoin+`) ch ON ch.workspace_id=m.workspace_id AND ch.channel_id::text=$3
		WHERE m.workspace_id=$1 AND m.id=$2`, item.WorkspaceID, item.MessageID, item.ChannelID).
		Scan(&item.SenderName, &item.SupportAddress, &item.DomainVerified, &item.EmailPaused, &item.ContactName, &item.ContactEmail, &item.Subject, &item.Body,
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
	err = tx.QueryRow(ctx, `SELECT address,status,COALESCE(error_code,''),token_key_id,refresh_token_sealed FROM mailbox_connections WHERE workspace_id=$1 AND channel_id::text=$2`, item.WorkspaceID, item.ChannelID).
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

// EmailDelivery loads what the email sender needs of a claimed reply of the outbox.
func (s *Store) EmailDelivery(ctx context.Context, workspaceID, outboxID string, attempts int) (Delivery, error) {
	item := Delivery{OutboxID: outboxID, WorkspaceID: workspaceID, Attempts: attempts}
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT message_id::text,COALESCE(channel_id::text,''),COALESCE(transport,'smtp'),COALESCE(claim_token::text,'') FROM channel_outbox WHERE workspace_id=$1 AND id=$2`,
			workspaceID, outboxID).Scan(&item.MessageID, &item.ChannelID, &item.Transport, &item.ClaimToken); err != nil {
			return fmt.Errorf("load delivery %s: %w", outboxID, err)
		}
		return loadDelivery(ctx, tx, &item)
	})
	return item, err
}
