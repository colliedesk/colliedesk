package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
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
	// RelayBounceCopy recorded a bounce of Cc copies on a reply that still reached the contact.
	RelayBounceCopy RelayBounceResult = "copy"
	// RelayBounceOtherRecipient means the event names an address that is neither the contact nor a Cc
	// of the reply, or the contact next to others, so it cannot say which copy failed.
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

// MarkRelayBounce applies the relay event to the reply it names, once per webhook id. When the event
// names only the contact, the delivery fails; a reply still being sent (the relay may report a
// suppressed address before the worker records the send) fails too. When it names only Cc addresses
// of the reply, the customer still got it: the reply stays as it is and the bounced copies are added
// to its note, as for a delivery status report. sent_at is kept as the record that the relay took the
// reply, so the trial recipient cap still counts it; the hourly send limit counts only sent rows. A
// note already on a sent reply (copies refused at RCPT TO) is kept after the failure reason.
func (s *Store) MarkRelayBounce(ctx context.Context, workspaceID string, bounce RelayBounce) (RelayBounceResult, error) {
	reason := bounceReason
	if bounce.Complaint {
		reason = complaintReason
	}
	recipients := make([]string, 0, len(bounce.Recipients))
	for _, recipient := range bounce.Recipients {
		recipients = append(recipients, strings.ToLower(strings.TrimSpace(recipient)))
	}
	result, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (RelayBounceResult, error) {
		var messageID, contactEmail string
		var cc []string
		err := tx.QueryRow(ctx, `SELECT m.id::text,lower(ct.email),COALESCE(m.cc,'{}') FROM messages m
			JOIN conversations c ON c.workspace_id=m.workspace_id AND c.id=m.conversation_id
			JOIN contacts ct ON ct.workspace_id=c.workspace_id AND ct.id=c.contact_id
			WHERE m.workspace_id=$1 AND c.email_reply_token=$2 AND m.kind='outgoing'
				AND (($3<>'' AND left(m.email_message_id,15)='cd.'||$3) OR ($3='' AND $4<>'' AND m.email_message_id=$4))`,
			workspaceID, bounce.ReplyToken, bounce.ReplyID, bounce.MessageID).Scan(&messageID, &contactEmail, &cc)
		if errors.Is(err, pgx.ErrNoRows) {
			return RelayBounceNoReply, nil
		}
		if err != nil {
			return "", err
		}
		copies := allIn(recipients, cc)
		if !copies && !allIn(recipients, []string{contactEmail}) {
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
		if copies {
			_, err := tx.Exec(ctx, `UPDATE email_outbox SET last_error=concat_ws('; ',last_error,$3||array_to_string($4::text[],', ')),updated_at=now()
				WHERE workspace_id=$1 AND message_id=$2`, workspaceID, messageID, copyNote(bounce.Complaint), recipients)
			return RelayBounceCopy, err
		}
		failed, err := tx.Exec(ctx, `UPDATE email_outbox SET status='failed',
				last_error=CASE WHEN status='sent' THEN concat_ws('; ',$3::text,last_error) ELSE $3 END,
				locked_until=NULL,claim_token=NULL,updated_at=now()
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

func copyNote(complaint bool) string {
	if complaint {
		return "Marked as spam by copied address: "
	}
	return "Bounced from copied address: "
}

// allIn reports whether the event names at least one address and every one of them is in set.
func allIn(recipients, set []string) bool {
	if len(recipients) == 0 || len(set) == 0 {
		return false
	}
	for _, recipient := range recipients {
		if !slices.Contains(set, recipient) {
			return false
		}
	}
	return true
}
