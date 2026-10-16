package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const complaintReason = "The recipient marked this reply as spam."

// RelayBounce is a delivery failure the outbound relay reported for a reply sent from
// reply+<ReplyToken>@MAIL_FROM_DOMAIN. The relay may rewrite the Message-ID, so the reply is the one
// named by MessageID when that is ours, otherwise the latest one sent to a bounced recipient no later
// than SentBefore.
type RelayBounce struct {
	ReplyToken string
	MessageID  string
	Recipients []string
	SentBefore time.Time
	Complaint  bool
}

// MarkRelayBounce fails the delivery of the reply the relay reported, and reports whether one was
// found. A reply is sent only to the conversation's contact, so an event for any other recipient
// changes nothing.
func (s *Store) MarkRelayBounce(ctx context.Context, workspaceID string, bounce RelayBounce) (bool, error) {
	reason := bounceReason
	if bounce.Complaint {
		reason = complaintReason
	}
	recipients := make([]string, 0, len(bounce.Recipients))
	for _, recipient := range bounce.Recipients {
		recipients = append(recipients, strings.ToLower(strings.TrimSpace(recipient)))
	}
	updated, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (int64, error) {
		result, err := tx.Exec(ctx, `UPDATE email_outbox SET status='failed',last_error=$6,locked_until=NULL,claim_token=NULL,updated_at=now()
			WHERE workspace_id=$1 AND id=(
				SELECT o.id FROM email_outbox o
				JOIN messages m ON m.workspace_id=o.workspace_id AND m.id=o.message_id
				JOIN conversations c ON c.workspace_id=m.workspace_id AND c.id=m.conversation_id
				JOIN contacts ct ON ct.workspace_id=c.workspace_id AND ct.id=c.contact_id
				WHERE o.workspace_id=$1 AND c.email_reply_token=$2 AND m.kind='outgoing' AND o.status='sent'
					AND lower(ct.email)=ANY($3) AND (m.email_message_id=$4 OR o.sent_at<=$5)
				ORDER BY m.email_message_id IS NOT DISTINCT FROM $4 DESC, o.sent_at DESC LIMIT 1)`,
			workspaceID, bounce.ReplyToken, recipients, bounce.MessageID, bounce.SentBefore, reason)
		return result.RowsAffected(), err
	})
	if err != nil {
		return false, fmt.Errorf("record relay bounce: %w", err)
	}
	return updated == 1, nil
}
