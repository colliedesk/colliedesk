package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

const complaintReason = "The recipient marked this reply as spam."

// RelayBounceResult says what a relay event did.
type RelayBounceResult string

const (
	// RelayBounceApplied failed the reply the event names.
	RelayBounceApplied RelayBounceResult = "applied"
	// RelayBounceDuplicate is a redelivery of an event already applied.
	RelayBounceDuplicate RelayBounceResult = "duplicate"
	// RelayBounceNoReply means the event names no reply of this conversation.
	RelayBounceNoReply RelayBounceResult = "no reply"
	// RelayBounceOtherRecipient means the event names someone besides the conversation's contact, the
	// only recipient of a reply, so it cannot say that the contact's copy failed.
	RelayBounceOtherRecipient RelayBounceResult = "other recipient"
	// RelayBounceAlreadyFailed means the reply is already failed.
	RelayBounceAlreadyFailed RelayBounceResult = "already failed"
)

// RelayBounce is a delivery failure the outbound relay reported for a reply sent from
// reply+<ReplyToken>[-<ReplyID>]@MAIL_FROM_DOMAIN. ReplyID names the reply; the older address without
// it names only the conversation, so then the reply is found only by our MessageID, which a relay may
// have rewritten. Nothing is guessed from timing.
type RelayBounce struct {
	WebhookID  string
	EventType  string
	ReplyToken string
	ReplyID    string
	MessageID  string
	Recipients []string
	Complaint  bool
}

// MarkRelayBounce fails the delivery of the reply the relay event names, once per webhook id. A reply
// still being sent (the relay may report a suppressed address before the worker records the send) is
// failed too.
func (s *Store) MarkRelayBounce(ctx context.Context, workspaceID string, bounce RelayBounce) (RelayBounceResult, error) {
	reason := bounceReason
	if bounce.Complaint {
		reason = complaintReason
	}
	result, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (RelayBounceResult, error) {
		var messageID, contactEmail string
		err := tx.QueryRow(ctx, `SELECT m.id::text,lower(ct.email) FROM messages m
			JOIN conversations c ON c.workspace_id=m.workspace_id AND c.id=m.conversation_id
			JOIN contacts ct ON ct.workspace_id=c.workspace_id AND ct.id=c.contact_id
			WHERE m.workspace_id=$1 AND c.email_reply_token=$2 AND m.kind='outgoing'
				AND (($3<>'' AND left(m.email_message_id,15)='cd.'||$3) OR ($3='' AND $4<>'' AND m.email_message_id=$4))`,
			workspaceID, bounce.ReplyToken, bounce.ReplyID, bounce.MessageID).Scan(&messageID, &contactEmail)
		if errors.Is(err, pgx.ErrNoRows) {
			return RelayBounceNoReply, nil
		}
		if err != nil {
			return "", err
		}
		if !onlyRecipient(bounce.Recipients, contactEmail) {
			return RelayBounceOtherRecipient, nil
		}
		recorded, err := tx.Exec(ctx, `INSERT INTO relay_webhook_events(workspace_id,webhook_id,event_type,message_id) VALUES($1,$2,$3,$4)
			ON CONFLICT DO NOTHING`, workspaceID, bounce.WebhookID, bounce.EventType, messageID)
		if err != nil {
			return "", err
		}
		if recorded.RowsAffected() == 0 {
			return RelayBounceDuplicate, nil
		}
		failed, err := tx.Exec(ctx, `UPDATE email_outbox SET status='failed',last_error=$3,locked_until=NULL,claim_token=NULL,updated_at=now()
			WHERE workspace_id=$1 AND message_id=$2 AND status IN ('sent','queued')`, workspaceID, messageID, reason)
		if err != nil {
			return "", err
		}
		if failed.RowsAffected() == 0 {
			return RelayBounceAlreadyFailed, nil
		}
		return RelayBounceApplied, nil
	})
	if err != nil {
		return "", fmt.Errorf("record relay bounce: %w", err)
	}
	return result, nil
}

// onlyRecipient reports whether the event names the contact and nobody else.
func onlyRecipient(recipients []string, contact string) bool {
	if len(recipients) == 0 {
		return false
	}
	for _, recipient := range recipients {
		if strings.ToLower(strings.TrimSpace(recipient)) != contact {
			return false
		}
	}
	return true
}
