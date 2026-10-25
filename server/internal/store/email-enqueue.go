package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/colliedesk/colliedesk/server/internal/channels"
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
	// GmailEnabled is whether this server has its Google settings; without them a stored Gmail connection
	// cannot send, and its replies fail at once instead of waiting.
	GmailEnabled bool
}

// WithOutbound returns a store that queues replies according to the policy.
func (s *Store) WithOutbound(policy OutboundPolicy) *Store {
	copy := *s
	copy.outbound = policy
	return &copy
}

// notReadyReason explains why a reply cannot be emailed, or returns "" when it can. A connected Gmail
// account sends by itself: it needs no relay and no verified domain, and a broken one never hands its
// replies to the relay instead.
func (p OutboundPolicy) notReadyReason(domainVerified bool, mailbox *mailboxState) string {
	switch {
	case mailbox != nil && mailbox.status == domain.MailboxFailed:
		return domain.MailboxFailureText(mailbox.errorCode)
	case mailbox != nil && !p.GmailEnabled:
		return domain.MailboxNotConfiguredNote
	case mailbox != nil:
		return ""
	case !p.Enabled:
		return "Outgoing email is not configured on this server. Ask the administrator to set SMTP_HOST."
	case p.RequireVerifiedDomain && !domainVerified && p.FallbackDomain == "":
		return "Verify the support address domain in Settings before replies can be emailed."
	default:
		return ""
	}
}

// PrepareEmailReply is the email channel's part of queuing a reply, in the transaction that stores
// it: the reply gets its Message-ID and the conversation its reply token, fixed now so every retry,
// including after a crash, carries the same ones; the trial's limits refuse it; and when outbound
// email cannot work, it is queued as failed with the reason instead of waiting. A retry chooses the
// transport again and checks the trial's cap from now.
func (s *Store) PrepareEmailReply(ctx context.Context, tx pgx.Tx, request channels.PrepareRequest) (channels.Prepared, error) {
	workspaceID, conversationID := request.WorkspaceID, request.ConversationID
	var supportAddress string
	var verified bool
	err := tx.QueryRow(ctx, `SELECT e.support_address,d.verified_at IS NOT NULL FROM email_channels e`+emailDomainJoin+`
		WHERE e.workspace_id=$1 AND e.channel_id::text=$2`, workspaceID, request.Channel.ID).
		Scan(&supportAddress, &verified)
	if errors.Is(err, pgx.ErrNoRows) {
		return channels.Prepared{}, fmt.Errorf("%w: The email channel is not connected.", ErrOutboundNotReady)
	}
	if err != nil {
		return channels.Prepared{}, fmt.Errorf("load email channel: %w", err)
	}
	mailbox, err := loadMailboxState(ctx, tx, workspaceID, request.Channel.ID)
	if err != nil {
		return channels.Prepared{}, err
	}
	recipients := 1 + len(request.CC)
	prepared := channels.Prepared{Status: channels.DeliveryQueued, Recipients: recipients, Transport: mailbox.transport()}
	if request.Retry {
		if reason := s.outbound.notReadyReason(verified, mailbox); reason != "" {
			return channels.Prepared{Status: channels.DeliveryFailed, Reason: reason}, nil
		}
		// A reply first queued while its channel was off has no Message-ID and token yet.
		var assigned bool
		if err := tx.QueryRow(ctx, `SELECT COALESCE(starts_with(external_id,'email:'),false) FROM messages WHERE workspace_id=$1 AND id=$2`, workspaceID, request.MessageID).Scan(&assigned); err != nil {
			return channels.Prepared{}, fmt.Errorf("load reply: %w", err)
		}
		if !assigned {
			if err := s.assignEmailIdentity(ctx, tx, workspaceID, conversationID, supportAddress, &prepared); err != nil {
				return channels.Prepared{}, err
			}
		}
		now := s.now()
		if trial, err := s.onTrial(ctx, tx, workspaceID, now); err != nil {
			return channels.Prepared{}, err
		} else if trial {
			if err := s.checkTrialCap(ctx, tx, workspaceID, now, recipients); err != nil {
				return channels.Prepared{}, err
			}
		}
		return prepared, nil
	}
	if err := s.checkTrialOutbound(ctx, tx, workspaceID, conversationID, verified, recipients); err != nil {
		return channels.Prepared{}, err
	}
	if err := s.checkTrialCC(ctx, tx, workspaceID, conversationID, verified, request.CC); err != nil {
		return channels.Prepared{}, err
	}
	if err := s.assignEmailIdentity(ctx, tx, workspaceID, conversationID, supportAddress, &prepared); err != nil {
		return channels.Prepared{}, err
	}
	if reason := s.outbound.notReadyReason(verified, mailbox); reason != "" {
		prepared.Status, prepared.Reason = channels.DeliveryFailed, reason
	}
	return prepared, nil
}

// assignEmailIdentity gives a reply its Message-ID and its conversation a reply token when it has none.
func (s *Store) assignEmailIdentity(ctx context.Context, tx pgx.Tx, workspaceID, conversationID, supportAddress string, prepared *channels.Prepared) error {
	var err error
	if prepared.ExternalID, err = email.NewMessageID(email.Domain(supportAddress)); err != nil {
		return err
	}
	token, err := email.NewToken()
	if err != nil {
		return err
	}
	// The conversation's row lock keeps two replies at once from giving it two tokens.
	if _, err := tx.Exec(ctx, `SELECT 1 FROM conversations WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, workspaceID, conversationID); err != nil {
		return fmt.Errorf("lock conversation: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO conversation_thread_keys(workspace_id,conversation_id,channel_type,key)
		SELECT $1,$2,'email','reply-token:'||$3 WHERE NOT EXISTS (SELECT 1 FROM conversation_thread_keys
			WHERE workspace_id=$1 AND conversation_id=$2 AND channel_type='email' AND starts_with(key,'reply-token:'))`,
		workspaceID, conversationID, token); err != nil {
		return fmt.Errorf("assign reply token: %w", err)
	}
	return nil
}

// mailboxState is the Gmail account connected to an email channel as the outbound rules see it.
type mailboxState struct {
	status, errorCode string
}

// transport is what a reply queued now is sent through.
func (m *mailboxState) transport() string {
	if m == nil {
		return "smtp"
	}
	return "gmail"
}

func loadMailboxState(ctx context.Context, tx pgx.Tx, workspaceID, channelID string) (*mailboxState, error) {
	var state mailboxState
	err := tx.QueryRow(ctx, `SELECT status,COALESCE(error_code,'') FROM mailbox_connections WHERE workspace_id=$1 AND channel_id::text=$2`, workspaceID, channelID).
		Scan(&state.status, &state.errorCode)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load mailbox connection: %w", err)
	}
	return &state, nil
}
