package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// Delivery codes the frame gives.
const (
	DeliveryCodeChannelOff     = "channel_off"
	DeliveryCodeChannelRemoved = "channel_removed"
	DeliveryCodeNoRecipient    = "no_recipient"
	DeliveryCodeGaveUp         = "gave_up"
)

// ReplyRoute is the channel a reply on a conversation leaves through.
type ReplyRoute struct {
	// Channel is nil when no channel can carry it; Removed then names the conversation's own channel
	// that was removed, if that is why.
	Channel *channels.Channel
	Removed *channels.Channel
}

// replyRoute finds the channel that carries replies of a conversation: its own when its type sends,
// else the one of the type its type names, else the type that carries other replies. A removed
// channel of a singleton type gives way to the live channel of that type (email disconnected and
// connected again still answers its old conversations).
func (s *Store) replyRoute(ctx context.Context, tx pgx.Tx, workspaceID, conversationID string) (ReplyRoute, error) {
	if s.channels == nil {
		return ReplyRoute{}, channels.ErrNoRegistry
	}
	var origin string
	var channelID *string
	if err := tx.QueryRow(ctx, `SELECT channel,channel_id::text FROM conversations WHERE workspace_id=$1 AND id=$2`, workspaceID, conversationID).
		Scan(&origin, &channelID); err != nil {
		return ReplyRoute{}, fmt.Errorf("load conversation channel: %w", err)
	}
	replyType, ok := s.channels.ReplyType(channels.Type(origin))
	if !ok {
		return ReplyRoute{}, nil
	}
	var own *channels.Channel
	if channelID != nil {
		channel, err := s.channelByID(ctx, tx, workspaceID, *channelID)
		if err != nil {
			return ReplyRoute{}, err
		}
		own = &channel
		if channel.Type == replyType && channel.Live() {
			return ReplyRoute{Channel: own}, nil
		}
	}
	adapter, ok := s.channels.Adapter(replyType)
	if !ok {
		return ReplyRoute{}, nil
	}
	if adapter.Definition().Singleton {
		live, found, err := s.liveChannelOfType(ctx, tx, workspaceID, replyType)
		if err != nil || found {
			return ReplyRoute{Channel: &live}, err
		}
	}
	if own != nil && own.Type == replyType {
		return ReplyRoute{Removed: own}, nil
	}
	return ReplyRoute{}, nil
}

// otherRepliesCarrier is the live channel that carries replies of conversations that came through no
// channel (one a team member started); nil when the workspace has none.
func (s *Store) otherRepliesCarrier(ctx context.Context, tx pgx.Tx, workspaceID string) (*channels.Channel, error) {
	if s.channels == nil {
		return nil, channels.ErrNoRegistry
	}
	kind, ok := s.channels.ReplyType("")
	if !ok {
		return nil, nil
	}
	channel, found, err := s.liveChannelOfType(ctx, tx, workspaceID, kind)
	if err != nil || !found {
		return nil, err
	}
	return &channel, nil
}

// GetReplyRoute is replyRoute in its own transaction.
func (s *Store) GetReplyRoute(ctx context.Context, workspaceID, conversationID string) (ReplyRoute, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (ReplyRoute, error) {
		return s.replyRoute(ctx, tx, workspaceID, conversationID)
	})
}

// replyRecipient loads the conversation's customer with their identities on a channel.
func replyRecipient(ctx context.Context, tx pgx.Tx, workspaceID, conversationID, channelID string) (channels.Recipient, error) {
	var recipient channels.Recipient
	if err := tx.QueryRow(ctx, `SELECT ct.id::text,ct.name,COALESCE(ct.email,'') FROM conversations c
		JOIN contacts ct ON ct.workspace_id=c.workspace_id AND ct.id=c.contact_id WHERE c.workspace_id=$1 AND c.id=$2`, workspaceID, conversationID).
		Scan(&recipient.ContactID, &recipient.Name, &recipient.Email); err != nil {
		return recipient, fmt.Errorf("load recipient: %w", err)
	}
	rows, err := tx.Query(ctx, `SELECT kind,value FROM contact_identities WHERE workspace_id=$1 AND contact_id=$2 AND channel_id=$3 ORDER BY created_at,id`,
		workspaceID, recipient.ContactID, channelID)
	if err != nil {
		return recipient, fmt.Errorf("load recipient identities: %w", err)
	}
	recipient.Identities, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (channels.Identity, error) {
		var identity channels.Identity
		err := row.Scan(&identity.Kind, &identity.Value)
		return identity, err
	})
	return recipient, err
}

// reachable reports whether the recipient has an address of the kind replies need.
func reachable(recipient channels.Recipient, kind string) bool {
	if kind == "" {
		return true
	}
	if kind == channels.IdentityEmail {
		return recipient.Email != ""
	}
	for _, identity := range recipient.Identities {
		if identity.Kind == kind {
			return true
		}
	}
	return false
}

// enqueueReply queues a reply on the channel that carries the conversation's replies, in the
// transaction that stores it. With no channel that can carry it nothing is queued, as before channels
// for a workspace without email. A channel that is off, removed, or cannot reach the customer queues
// a failed delivery that says why; a person retries it once that is fixed.
func (s *Store) enqueueReply(ctx context.Context, tx pgx.Tx, workspaceID, conversationID, messageID string, cc []string) error {
	route, err := s.replyRoute(ctx, tx, workspaceID, conversationID)
	if err != nil {
		return err
	}
	failed := func(channel channels.Channel, code, reason string) error {
		_, err := tx.Exec(ctx, `INSERT INTO channel_outbox(workspace_id,message_id,status,last_error,last_error_code,created_at,recipients,transport,channel_type,channel_id)
			VALUES($1,$2,'failed',$3,$4,$5,$6,NULL,$7,$8)`, workspaceID, messageID, reason, code, s.now(), 1+len(cc), string(channel.Type), channel.ID)
		if err != nil {
			return fmt.Errorf("queue reply: %w", err)
		}
		return nil
	}
	if route.Channel == nil {
		if route.Removed != nil {
			return failed(*route.Removed, DeliveryCodeChannelRemoved, route.Removed.Name+" was removed, so this reply cannot be sent.")
		}
		return nil
	}
	channel := *route.Channel
	adapter, _ := s.channels.Adapter(channel.Type)
	definition := adapter.Definition()
	recipient, err := replyRecipient(ctx, tx, workspaceID, conversationID, channel.ID)
	if err != nil {
		return err
	}
	if !reachable(recipient, definition.ReplyTo) {
		return failed(channel, DeliveryCodeNoRecipient, "This customer has no address on "+channel.Name+", so the reply cannot be sent there.")
	}
	if !channel.Enabled {
		return failed(channel, DeliveryCodeChannelOff, channel.Name+" is off: this reply is saved but not sent until the channel is on.")
	}
	prepared, err := adapter.Outbound().Prepare(ctx, tx, channels.PrepareRequest{WorkspaceID: workspaceID, Channel: channel,
		ConversationID: conversationID, MessageID: messageID, CC: cc, Recipient: recipient, Host: s})
	if err != nil {
		return err
	}
	if prepared.ExternalID != "" {
		if _, err := tx.Exec(ctx, `UPDATE messages SET external_id=$3,channel_id=$4 WHERE workspace_id=$1 AND id=$2`,
			workspaceID, messageID, string(channel.Type)+":"+prepared.ExternalID, channel.ID); err != nil {
			return fmt.Errorf("assign reply id: %w", err)
		}
	} else if _, err := tx.Exec(ctx, `UPDATE messages SET channel_id=$3 WHERE workspace_id=$1 AND id=$2`, workspaceID, messageID, channel.ID); err != nil {
		return fmt.Errorf("assign reply channel: %w", err)
	}
	status := prepared.Status
	if status == "" {
		status = channels.DeliveryQueued
	}
	recipients := prepared.Recipients
	if recipients < 1 {
		recipients = 1 + len(cc)
	}
	var meta []byte
	if len(prepared.Meta) > 0 {
		meta = prepared.Meta
	}
	if _, err := tx.Exec(ctx, `INSERT INTO channel_outbox(workspace_id,message_id,status,last_error,last_error_code,created_at,recipients,transport,channel_type,channel_id,meta)
		VALUES($1,$2,$3,NULLIF($4,''),NULLIF($5,''),$6,$7,NULLIF($8,''),$9,$10,$11)`,
		workspaceID, messageID, status, prepared.Reason, prepared.Code, s.now(), recipients, prepared.Transport, string(channel.Type), channel.ID, meta); err != nil {
		return fmt.Errorf("queue reply: %w", err)
	}
	return nil
}

// RetryDelivery requeues a failed delivery of a reply in this workspace and conversation, if its
// channel can send now.
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
	route, err := s.replyRoute(ctx, tx, workspaceID, conversationID)
	if err != nil {
		return domain.Message{}, err
	}
	if route.Channel == nil {
		if route.Removed != nil {
			return domain.Message{}, fmt.Errorf("%w: %s was removed.", ErrOutboundNotReady, route.Removed.Name)
		}
		var kind string
		if err := tx.QueryRow(ctx, `SELECT channel_type FROM channel_outbox WHERE workspace_id=$1 AND message_id=$2`, workspaceID, messageID).Scan(&kind); err != nil {
			return domain.Message{}, fmt.Errorf("load delivery: %w", err)
		}
		name := kind
		if adapter, ok := s.channels.Adapter(channels.Type(kind)); ok {
			name = adapter.Definition().Name
		}
		return domain.Message{}, fmt.Errorf("%w: The %s channel is not connected.", ErrOutboundNotReady, strings.ToLower(name))
	}
	channel := *route.Channel
	if !channel.Enabled {
		return domain.Message{}, fmt.Errorf("%w: %s is off. Turn it on, then retry.", ErrOutboundNotReady, channel.Name)
	}
	adapter, _ := s.channels.Adapter(channel.Type)
	recipient, err := replyRecipient(ctx, tx, workspaceID, conversationID, channel.ID)
	if err != nil {
		return domain.Message{}, err
	}
	if !reachable(recipient, adapter.Definition().ReplyTo) {
		return domain.Message{}, fmt.Errorf("%w: This customer has no address on %s.", ErrOutboundNotReady, channel.Name)
	}
	prepared, err := adapter.Outbound().Prepare(ctx, tx, channels.PrepareRequest{WorkspaceID: workspaceID, Channel: channel,
		ConversationID: conversationID, MessageID: messageID, CC: message.CC, Recipient: recipient, Retry: true, Host: s})
	if err != nil {
		return domain.Message{}, err
	}
	if prepared.Status == channels.DeliveryFailed {
		return domain.Message{}, fmt.Errorf("%w: %s", ErrOutboundNotReady, prepared.Reason)
	}
	if prepared.ExternalID != "" {
		if _, err := tx.Exec(ctx, `UPDATE messages SET external_id=$3 WHERE workspace_id=$1 AND id=$2 AND external_id IS NULL`,
			workspaceID, messageID, string(channel.Type)+":"+prepared.ExternalID); err != nil {
			return domain.Message{}, fmt.Errorf("assign reply id: %w", err)
		}
	}
	now := s.now()
	recipients := prepared.Recipients
	if recipients < 1 {
		recipients = 1 + len(message.CC)
	}
	// The resend reaches everyone again, so it counts from now. While the earlier send of the row is
	// still in the cap window its people stay counted with it; the total then lasts a full window from
	// the resend, which errs on counting too much, never too little.
	var meta []byte
	if len(prepared.Meta) > 0 {
		meta = prepared.Meta
	}
	result, err := tx.Exec(ctx, `UPDATE channel_outbox SET status='queued',attempts=0,next_attempt_at=now(),locked_until=NULL,
		claim_token=NULL,last_error=NULL,last_error_code=NULL,updated_at=now(),
		recipients=CASE WHEN sent_at IS NOT NULL AND created_at > $4 THEN recipients + $5 ELSE $5 END, created_at=$3,
		transport=COALESCE(NULLIF($6,''),transport),channel_type=$7,channel_id=$8,meta=COALESCE($9,meta)
		WHERE workspace_id=$1 AND message_id=$2 AND status='failed'`,
		workspaceID, messageID, now, now.Add(-domain.TrialReplyWindow), recipients, prepared.Transport, string(channel.Type), channel.ID, meta)
	if err != nil {
		return domain.Message{}, fmt.Errorf("retry delivery: %w", err)
	}
	if result.RowsAffected() != 1 {
		return domain.Message{}, ErrDeliveryNotFailed
	}
	return getMessage(ctx, tx, workspaceID, messageID)
}

// OutboxClaim is one leased outbox row of any channel type.
type OutboxClaim struct {
	OutboxID, WorkspaceID, MessageID, ClaimToken string
	Attempts                                     int
	Type                                         channels.Type
	ChannelID                                    string
}

// ClaimDueOutbox leases due rows of every channel type, counts the attempt and stamps a claim token
// that every finish must present. A lease that expires without a result makes the row due again:
// delivery is at least once, deduplicated by the id Prepare fixed.
func (s *Store) ClaimDueOutbox(ctx context.Context, now time.Time, limit int, lease time.Duration, skip ...channels.Type) ([]OutboxClaim, error) {
	skipped := make([]string, len(skip))
	for i, kind := range skip {
		skipped[i] = string(kind)
	}
	var claimed []OutboxClaim
	err := s.db.Privileged(ctx, database.PrivilegeOutboxClaim, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `WITH due AS (
				SELECT id FROM channel_outbox WHERE status='queued' AND next_attempt_at<=$1 AND (locked_until IS NULL OR locked_until<=$1)
					AND NOT (channel_type=ANY($4))
				ORDER BY next_attempt_at LIMIT $2 FOR UPDATE SKIP LOCKED)
			UPDATE channel_outbox o SET locked_until=$3,attempts=o.attempts+1,claim_token=gen_random_uuid(),updated_at=$1 FROM due WHERE o.id=due.id
			RETURNING o.id::text,o.workspace_id::text,o.message_id::text,o.claim_token::text,o.attempts,o.channel_type,COALESCE(o.channel_id::text,'')`,
			now, limit, now.Add(lease), skipped)
		if err != nil {
			return fmt.Errorf("claim deliveries: %w", err)
		}
		claimed, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (OutboxClaim, error) {
			var item OutboxClaim
			var kind string
			err := row.Scan(&item.OutboxID, &item.WorkspaceID, &item.MessageID, &item.ClaimToken, &item.Attempts, &kind, &item.ChannelID)
			item.Type = channels.Type(kind)
			return item, err
		})
		if err != nil {
			return fmt.Errorf("scan claimed deliveries: %w", err)
		}
		return nil
	})
	return claimed, err
}

// LoadOutbound reads what a sender needs of a claimed reply.
func (s *Store) LoadOutbound(ctx context.Context, claim OutboxClaim) (channels.Outbound, error) {
	return scoped(ctx, s, claim.WorkspaceID, func(tx pgx.Tx) (channels.Outbound, error) {
		out := channels.Outbound{DeliveryID: claim.OutboxID, WorkspaceID: claim.WorkspaceID, Attempt: claim.Attempts}
		if claim.ChannelID == "" {
			// History of a channel removed before channels existed: the type's live channel, if any.
			live, found, err := s.liveChannelOfType(ctx, tx, claim.WorkspaceID, claim.Type)
			if err != nil {
				return out, err
			}
			if !found {
				live = channels.Channel{WorkspaceID: claim.WorkspaceID, Type: claim.Type, Name: string(claim.Type), Enabled: true, DeletedAt: &out.Message.CreatedAt}
			}
			claim.ChannelID = live.ID
			out.Channel = live
		}
		channel := out.Channel
		if channel.ID == "" && claim.ChannelID != "" {
			var err error
			if channel, err = s.channelByID(ctx, tx, claim.WorkspaceID, claim.ChannelID); err != nil {
				return out, err
			}
		}
		if adapter, ok := s.channels.Adapter(channel.Type); !channel.Live() && ok && adapter.Definition().Singleton {
			// Disconnected and connected again: the reply leaves through the type's live channel.
			if live, found, err := s.liveChannelOfType(ctx, tx, claim.WorkspaceID, channel.Type); err != nil {
				return out, err
			} else if found {
				if _, err := tx.Exec(ctx, `UPDATE channel_outbox SET channel_id=$3 WHERE workspace_id=$1 AND id=$2`, claim.WorkspaceID, claim.OutboxID, live.ID); err != nil {
					return out, fmt.Errorf("move delivery to the live channel: %w", err)
				}
				channel = live
			}
		}
		out.Channel = channel
		var meta []byte
		var cc []string
		var origin string
		err := tx.QueryRow(ctx, `SELECT m.id::text,m.body,m.cc,m.created_at,c.id::text,c.subject,c.channel,o.meta
			FROM channel_outbox o JOIN messages m ON m.workspace_id=o.workspace_id AND m.id=o.message_id
			JOIN conversations c ON c.workspace_id=m.workspace_id AND c.id=m.conversation_id
			WHERE o.workspace_id=$1 AND o.id=$2`, claim.WorkspaceID, claim.OutboxID).
			Scan(&out.Message.ID, &out.Message.Body, &cc, &out.Message.CreatedAt, &out.Conversation.ID, &out.Conversation.Subject, &origin, &meta)
		if err != nil {
			return out, fmt.Errorf("load delivery %s: %w", claim.OutboxID, err)
		}
		out.Message.CC, out.Conversation.Origin, out.Prepared = cc, channels.Type(origin), meta
		if out.Recipient, err = replyRecipient(ctx, tx, claim.WorkspaceID, out.Conversation.ID, channel.ID); err != nil {
			return out, err
		}
		prefix := string(channel.Type) + ":"
		rows, err := tx.Query(ctx, `SELECT substr(external_id,$5),kind FROM messages WHERE workspace_id=$1 AND conversation_id=$2 AND id<>$3 AND created_at<=$4
			AND starts_with(external_id,$6) ORDER BY created_at,id`, claim.WorkspaceID, out.Conversation.ID, out.Message.ID, out.Message.CreatedAt, len(prefix)+1, prefix)
		if err != nil {
			return out, fmt.Errorf("load thread references: %w", err)
		}
		for rows.Next() {
			var id, kind string
			if err := rows.Scan(&id, &kind); err != nil {
				rows.Close()
				return out, fmt.Errorf("scan thread reference: %w", err)
			}
			out.References = append(out.References, id)
			if kind == "incoming" {
				out.LastIncoming = id
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return out, err
		}
		if err := tx.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM messages WHERE workspace_id=$1 AND conversation_id=$2 AND kind='incoming')`,
			claim.WorkspaceID, out.Conversation.ID).Scan(&out.Conversation.Opening); err != nil {
			return out, fmt.Errorf("check thread start: %w", err)
		}
		fileRows, err := tx.Query(ctx, `SELECT filename,content_type,storage_key,sha256,size FROM attachments
			WHERE workspace_id=$1 AND message_id=$2 ORDER BY created_at,id`, claim.WorkspaceID, out.Message.ID)
		if err != nil {
			return out, fmt.Errorf("load reply files: %w", err)
		}
		out.Message.Files, err = pgx.CollectRows(fileRows, func(row pgx.CollectableRow) (channels.OutFile, error) {
			var file channels.OutFile
			err := row.Scan(&file.Name, &file.ContentType, &file.StorageKey, &file.SHA256, &file.Size)
			return file, err
		})
		return out, err
	})
}

// FinishOutbox applies a send's result while this worker still holds the claim, so a worker whose
// lease expired cannot overwrite the result of the worker that reclaimed the row. next is when a
// retryable failure is due again.
func (s *Store) FinishOutbox(ctx context.Context, claim OutboxClaim, result channels.SendResult, now, next time.Time) error {
	var set string
	var args []any
	switch {
	case result.Outcome == channels.OutcomeSent:
		set, args = `status='sent',sent_at=$4,last_error=NULLIF($5,''),last_error_code=NULL,external_id=NULLIF($6,'')`, []any{now, result.Note, result.ExternalID}
	case result.Outcome == channels.OutcomeDeferred:
		set, args = `next_attempt_at=$4,last_error=$5,last_error_code=NULLIF($6,''),attempts=GREATEST(attempts-1,0)`, []any{result.RetryAt, result.Reason, result.Code}
	case result.Retryable:
		set, args = `next_attempt_at=$4,last_error=$5,last_error_code=NULLIF($6,'')`, []any{next, result.Reason, result.Code}
	default:
		set, args = `status='failed',last_error=$4,last_error_code=NULLIF($5,'')`, []any{result.Reason, result.Code}
	}
	var meta []byte
	if len(result.Meta) > 0 {
		meta = result.Meta
	}
	args = append(args, meta)
	set += fmt.Sprintf(",meta=COALESCE($%d,meta)", len(args)+3)
	updated, err := scoped(ctx, s, claim.WorkspaceID, func(tx pgx.Tx) (int64, error) {
		tag, err := tx.Exec(ctx, `UPDATE channel_outbox SET `+set+fmt.Sprintf(",held_by_pause=%t", result.HeldByPause)+`,locked_until=NULL,claim_token=NULL,updated_at=now()
			WHERE workspace_id=$1 AND id=$2 AND claim_token=$3::uuid AND status='queued'`,
			append([]any{claim.WorkspaceID, claim.OutboxID, claim.ClaimToken}, args...)...)
		if err != nil || tag.RowsAffected() != 1 {
			return tag.RowsAffected(), err
		}
		ids := result.ExternalIDs
		if result.ExternalID != "" {
			ids = append(ids, result.ExternalID)
		}
		if len(ids) > 0 && claim.ChannelID != "" {
			if _, err := tx.Exec(ctx, `INSERT INTO channel_sent_ids(workspace_id,channel_id,external_id,message_id)
				SELECT $1,$2,id,$3 FROM unnest($4::text[]) id ON CONFLICT DO NOTHING`, claim.WorkspaceID, claim.ChannelID, claim.MessageID, ids); err != nil {
				return 0, fmt.Errorf("record sent ids: %w", err)
			}
		}
		return 1, nil
	})
	if err != nil {
		return fmt.Errorf("update delivery %s: %w", claim.OutboxID, err)
	}
	if updated != 1 {
		return fmt.Errorf("update delivery %s: %w", claim.OutboxID, ErrLeaseLost)
	}
	return nil
}

// CountChannelSentSince counts the replies a channel type sent for the workspace since a time.
func (s *Store) CountChannelSentSince(ctx context.Context, workspaceID string, kind channels.Type, since time.Time) (int, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (int, error) {
		var count int
		err := tx.QueryRow(ctx, `SELECT count(*) FROM channel_outbox WHERE workspace_id=$1 AND channel_type=$2 AND status='sent' AND sent_at>$3`,
			workspaceID, string(kind), since).Scan(&count)
		return count, err
	})
}
