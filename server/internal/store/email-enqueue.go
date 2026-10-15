package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/jackc/pgx/v5"
)

// ErrDeliveryNotFailed means a retry was requested for a delivery that is queued or already sent.
var ErrDeliveryNotFailed = errors.New("only a failed delivery can be retried")

// ErrOutboundNotReady means replies cannot be emailed now; the error text says why.
var ErrOutboundNotReady = errors.New("outgoing email is not ready")

// OutboundPolicy says whether replies can be emailed now and from which identity.
type OutboundPolicy struct {
	Enabled               bool
	RequireVerifiedDomain bool
	FallbackDomain        string
}

// WithOutbound returns a store that queues replies according to the policy.
func (s *Store) WithOutbound(policy OutboundPolicy) *Store {
	copy := *s
	copy.outbound = policy
	return &copy
}

// notReadyReason explains why a reply cannot be emailed, or returns "" when it can.
func (p OutboundPolicy) notReadyReason(domainVerified bool) string {
	switch {
	case !p.Enabled:
		return "Outgoing email is not configured on this server. Ask the administrator to set SMTP_HOST."
	case p.RequireVerifiedDomain && !domainVerified && p.FallbackDomain == "":
		return "Verify the support address domain in Settings before replies can be emailed."
	default:
		return ""
	}
}

// enqueueReply gives a reply its Message-ID and adds it to the outbox when the workspace has an email
// channel. The Message-ID is fixed here so every retry, including after a crash, carries the same one.
// When outbound email cannot work, the reply is recorded as failed with the reason instead of waiting.
func (s *Store) enqueueReply(ctx context.Context, tx pgx.Tx, workspaceID, conversationID, messageID string) error {
	var supportAddress string
	var verified bool
	err := tx.QueryRow(ctx, `SELECT support_address,domain_verified_at IS NOT NULL FROM email_channels WHERE workspace_id=$1`, workspaceID).
		Scan(&supportAddress, &verified)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load email channel: %w", err)
	}
	if err := s.checkTrialOutbound(ctx, tx, workspaceID, conversationID, verified); err != nil {
		return err
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
	status, reason := "queued", s.outbound.notReadyReason(verified)
	if reason != "" {
		status = "failed"
	}
	if _, err := tx.Exec(ctx, `INSERT INTO email_outbox(workspace_id,message_id,status,last_error,created_at) VALUES($1,$2,$3,NULLIF($4,''),$5)`,
		workspaceID, messageID, status, reason, s.now()); err != nil {
		return fmt.Errorf("queue reply: %w", err)
	}
	return nil
}

// RetryDelivery requeues a failed delivery of a reply in this workspace and conversation, if outbound
// email can work now.
func (s *Store) RetryDelivery(ctx context.Context, workspaceID, conversationID, messageID string) (domain.Message, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.Message, error) {
		return s.retryDelivery(ctx, tx, workspaceID, conversationID, messageID)
	})
}

func (s *Store) retryDelivery(ctx context.Context, tx pgx.Tx, workspaceID, conversationID, messageID string) (domain.Message, error) {
	message, err := getMessage(ctx, tx, workspaceID, messageID)
	if err != nil || message.ConversationID != conversationID || message.Delivery == nil {
		return domain.Message{}, fmt.Errorf("retry delivery: not found")
	}
	if message.Delivery.Status != "failed" {
		return domain.Message{}, ErrDeliveryNotFailed
	}
	var verified bool
	err = tx.QueryRow(ctx, `SELECT domain_verified_at IS NOT NULL FROM email_channels WHERE workspace_id=$1`, workspaceID).Scan(&verified)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Message{}, fmt.Errorf("%w: The email channel is not connected.", ErrOutboundNotReady)
	}
	if err != nil {
		return domain.Message{}, fmt.Errorf("load email channel: %w", err)
	}
	if reason := s.outbound.notReadyReason(verified); reason != "" {
		return domain.Message{}, fmt.Errorf("%w: %s", ErrOutboundNotReady, reason)
	}
	result, err := tx.Exec(ctx, `UPDATE email_outbox SET status='queued',attempts=0,next_attempt_at=now(),locked_until=NULL,
		claim_token=NULL,last_error=NULL,updated_at=now() WHERE workspace_id=$1 AND message_id=$2 AND status='failed'`, workspaceID, messageID)
	if err != nil {
		return domain.Message{}, fmt.Errorf("retry delivery: %w", err)
	}
	if result.RowsAffected() != 1 {
		return domain.Message{}, ErrDeliveryNotFailed
	}
	return getMessage(ctx, tx, workspaceID, messageID)
}
