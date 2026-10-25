package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/jackc/pgx/v5"
)

// Delivery codes the frame gives.
const (
	DeliveryCodeChannelOff     = "channel_off"
	DeliveryCodeChannelRemoved = "channel_removed"
	DeliveryCodeNoRecipient    = "no_recipient"
	// DeliveryCodeNotConnected: no channel of the type that carries the reply is connected yet.
	DeliveryCodeNotConnected = "channel_not_connected"
	DeliveryCodeGaveUp       = "gave_up"
)

// ReplyRoute is the channel a reply on a conversation leaves through.
type ReplyRoute struct {
	// Channel is nil when no channel can carry it; Removed then names the conversation's own channel
	// that was removed, if that is why.
	Channel *channels.Channel
	Removed *channels.Channel
	// InsteadOf is the removed channel the replies would leave from, when Channel (the type's default)
	// stands in for it.
	InsteadOf *channels.Channel
	// Pin is set when the conversation's replies are not pinned yet and should be to Channel: its own
	// channel cannot send (a web form, an import, a conversation an agent started).
	Pin bool
	// Type is the type that carries the replies, also when no channel of it is connected.
	Type channels.Type
}

// replyChoice is what decides where a conversation's replies leave: where it came from, its own
// channel and the one its replies are pinned to (nil when none), and the source mailboxes of an import.
type replyChoice struct {
	origin    channels.Type
	own, pin  *channels.Channel
	mailboxes []string
}

// replyRoute finds the channel that carries replies of a conversation: see routeReply.
func (s *Store) replyRoute(ctx context.Context, tx pgx.Tx, workspaceID, conversationID string) (ReplyRoute, error) {
	if s.channels == nil {
		return ReplyRoute{}, channels.ErrNoRegistry
	}
	var origin string
	var channelID, pinID *string
	var mailboxes []string
	if err := tx.QueryRow(ctx, `SELECT channel,channel_id::text,reply_channel_id::text,
			COALESCE(ARRAY(SELECT jsonb_array_elements_text(CASE WHEN jsonb_typeof(origin->'mailboxes')='array' THEN origin->'mailboxes' END)),'{}')
		FROM conversations WHERE workspace_id=$1 AND id=$2`, workspaceID, conversationID).
		Scan(&origin, &channelID, &pinID, &mailboxes); err != nil {
		return ReplyRoute{}, fmt.Errorf("load conversation channel: %w", err)
	}
	choice := replyChoice{origin: channels.Type(origin), mailboxes: mailboxes}
	for _, item := range []struct {
		id     *string
		target **channels.Channel
	}{{channelID, &choice.own}, {pinID, &choice.pin}} {
		if item.id == nil {
			continue
		}
		channel, err := s.channelByID(ctx, tx, workspaceID, *item.id)
		if err != nil {
			return ReplyRoute{}, err
		}
		*item.target = &channel
	}
	return routeReply(s.channels, choice, channelFinder{
		defaultOf: func(kind channels.Type) (*channels.Channel, error) {
			live, found, err := s.defaultChannelOfType(ctx, tx, workspaceID, kind)
			if err != nil || !found {
				return nil, err
			}
			return &live, nil
		},
		mailbox: func(addresses []string) (*channels.Channel, error) {
			return s.channelOfMailbox(ctx, tx, workspaceID, addresses)
		},
	})
}

// ReplyRoute is replyRoute in its own transaction.
func (s *Store) ReplyRoute(ctx context.Context, workspaceID, conversationID string) (ReplyRoute, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (ReplyRoute, error) {
		return s.replyRoute(ctx, tx, workspaceID, conversationID)
	})
}

// channelFinder finds channels for routeReply: a type's default, and the email channel of a source
// mailbox (nil when it finds none).
type channelFinder struct {
	defaultOf func(channels.Type) (*channels.Channel, error)
	mailbox   func([]string) (*channels.Channel, error)
}

// routeReply is the one rule of where a reply leaves.
//  1. The conversation's own channel when its type sends the replies: while it lives, on or off; once
//     removed, a type whose customers are the same on every channel (SharedIdentity) answers through
//     its default channel, another type cannot.
//  2. Else (a web form, an import, a conversation an agent started) the channel its replies are pinned
//     to: while it lives, on or off; once removed, the type's default stands in, without pinning again.
//  3. Not pinned yet: an import's source mailbox when the workspace has it, else the type's default;
//     the first reply pins it.
func routeReply(registry *channels.Registry, choice replyChoice, find channelFinder) (ReplyRoute, error) {
	replyType, ok := registry.ReplyType(choice.origin)
	if !ok {
		return ReplyRoute{}, nil
	}
	adapter, ok := registry.Adapter(replyType)
	if !ok {
		return ReplyRoute{}, nil
	}
	own := choice.own
	if own != nil && own.Type == replyType {
		if own.Live() {
			return ReplyRoute{Channel: own, Type: replyType}, nil
		}
		if adapter.Definition().SharedIdentity {
			if channel, err := find.defaultOf(replyType); err != nil || channel != nil {
				return ReplyRoute{Channel: channel, InsteadOf: own, Type: replyType}, err
			}
		}
		return ReplyRoute{Removed: own, Type: replyType}, nil
	}
	if pin := choice.pin; pin != nil && pin.Type == replyType {
		if pin.Live() {
			return ReplyRoute{Channel: pin, Type: replyType}, nil
		}
		channel, err := find.defaultOf(replyType)
		if err != nil || channel == nil {
			return ReplyRoute{Type: replyType}, err
		}
		return ReplyRoute{Channel: channel, InsteadOf: pin, Type: replyType}, nil
	}
	if len(choice.mailboxes) > 0 && find.mailbox != nil {
		if channel, err := find.mailbox(choice.mailboxes); err != nil || channel != nil {
			return ReplyRoute{Channel: channel, Pin: true, Type: replyType}, err
		}
	}
	channel, err := find.defaultOf(replyType)
	if err != nil || channel == nil {
		return ReplyRoute{Type: replyType}, err
	}
	return ReplyRoute{Channel: channel, Pin: true, Type: replyType}, nil
}

// pinReply pins the conversation's replies to the channel route chose, once.
func pinReply(ctx context.Context, tx pgx.Tx, workspaceID, conversationID string, route ReplyRoute) error {
	if !route.Pin || route.Channel == nil {
		return nil
	}
	if _, err := tx.Exec(ctx, `UPDATE conversations SET reply_channel_id=$3 WHERE workspace_id=$1 AND id=$2 AND reply_channel_id IS NULL`,
		workspaceID, conversationID, route.Channel.ID); err != nil {
		return fmt.Errorf("pin the reply channel: %w", err)
	}
	return nil
}

// attachChannelRefs gives each conversation its channel and the one its replies leave through, from
// one read of the workspace's channels.
func (s *Store) attachChannelRefs(ctx context.Context, tx pgx.Tx, workspaceID string, items []domain.Conversation) error {
	if s.channels == nil || len(items) == 0 {
		return nil
	}
	rows, err := tx.Query(ctx, `SELECT `+channelRowColumns+` FROM channels WHERE workspace_id=$1`, workspaceID)
	if err != nil {
		return fmt.Errorf("load channels: %w", err)
	}
	byID := map[string]*channels.Channel{}
	defaults := map[channels.Type]*channels.Channel{}
	for rows.Next() {
		channel, err := s.scanChannelRow(rows)
		if err != nil {
			rows.Close()
			return err
		}
		byID[channel.ID] = &channel
		if channel.IsDefault {
			defaults[channel.Type] = &channel
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	ref := func(channel *channels.Channel) *domain.ChannelRef {
		return &domain.ChannelRef{ID: channel.ID, Type: string(channel.Type), Name: channel.Name, Enabled: channel.Enabled, Deleted: !channel.Live()}
	}
	// The mailbox of an import is read only for an unpinned import: the workspace's mailboxes are read
	// once, at the first one, and every address is looked up in them.
	var mailboxes map[string]*channels.Channel
	for i := range items {
		choice := replyChoice{origin: channels.Type(items[i].Channel)}
		if items[i].ChannelID != nil {
			choice.own = byID[*items[i].ChannelID]
		}
		if items[i].ReplyChannelID != nil {
			choice.pin = byID[*items[i].ReplyChannelID]
		}
		if items[i].Origin != nil {
			choice.mailboxes = items[i].Origin.Mailboxes
		}
		route, err := routeReply(s.channels, choice, channelFinder{
			defaultOf: func(kind channels.Type) (*channels.Channel, error) { return defaults[kind], nil },
			mailbox: func(addresses []string) (*channels.Channel, error) {
				if mailboxes == nil {
					keys, err := mailboxKeys(ctx, tx, workspaceID)
					if err != nil {
						return nil, err
					}
					mailboxes = map[string]*channels.Channel{}
					for key, id := range keys {
						if channel := byID[id]; channel != nil && channel.Live() {
							mailboxes[key] = channel
						}
					}
				}
				for _, address := range addresses {
					if channel := mailboxes[email.MailboxKey(address)]; channel != nil {
						return channel, nil
					}
				}
				return nil, nil
			},
		})
		if err != nil {
			return err
		}
		switch {
		case choice.own != nil:
			items[i].ChannelRef = ref(choice.own)
		case route.Channel != nil:
			items[i].ChannelRef = ref(route.Channel)
		default:
			continue
		}
		if route.Channel != nil {
			via := &domain.ChannelRepliesVia{ID: route.Channel.ID, Name: route.Channel.Name, Enabled: route.Channel.Enabled}
			if route.InsteadOf != nil {
				via.InsteadOf = &domain.ChannelName{ID: route.InsteadOf.ID, Name: route.InsteadOf.Name}
			}
			items[i].ChannelRef.RepliesVia = via
		}
	}
	return nil
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
	channel, found, err := s.defaultChannelOfType(ctx, tx, workspaceID, kind)
	if err != nil || !found {
		return nil, err
	}
	return &channel, nil
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
// transaction that stores it. A channel that is off, removed, not connected yet (a web form without
// email), or cannot reach the customer queues a failed delivery that says why; a person retries it once
// that is fixed. Only a conversation whose replies no type carries queues nothing.
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
		adapter, ok := s.channels.Adapter(route.Type)
		if !ok {
			return nil
		}
		name := adapter.Definition().Name
		if _, err := tx.Exec(ctx, `INSERT INTO channel_outbox(workspace_id,message_id,status,last_error,last_error_code,created_at,recipients,transport,channel_type)
			VALUES($1,$2,'failed',$3,$4,$5,$6,NULL,$7)`, workspaceID, messageID, name+" is not connected: this reply is saved but not sent. Connect it, then retry.",
			DeliveryCodeNotConnected, s.now(), 1+len(cc), string(route.Type)); err != nil {
			return fmt.Errorf("queue reply: %w", err)
		}
		return nil
	}
	channel := *route.Channel
	if err := pinReply(ctx, tx, workspaceID, conversationID, route); err != nil {
		return err
	}
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
	if err := pinReply(ctx, tx, workspaceID, conversationID, route); err != nil {
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
	// The reply is now one this channel carries, as a reply queued on it at once is (enqueueReply): it may
	// have been saved while the channel was off, or before any channel was connected.
	externalID := ""
	if prepared.ExternalID != "" {
		externalID = string(channel.Type) + ":" + prepared.ExternalID
	}
	if _, err := tx.Exec(ctx, `UPDATE messages SET channel_id=$4,external_id=COALESCE(external_id,NULLIF($3,'')) WHERE workspace_id=$1 AND id=$2`,
		workspaceID, messageID, externalID, channel.ID); err != nil {
		return domain.Message{}, fmt.Errorf("assign reply channel: %w", err)
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
			live, found, err := s.defaultChannelOfType(ctx, tx, claim.WorkspaceID, claim.Type)
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
		if adapter, ok := s.channels.Adapter(channel.Type); !channel.Live() && ok && adapter.Definition().SharedIdentity {
			// Its channel was removed: a type whose customers are the same on every channel sends through its default one.
			if live, found, err := s.defaultChannelOfType(ctx, tx, claim.WorkspaceID, channel.Type); err != nil {
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

// mailboxKeys maps the mailbox of each email channel of the workspace to the channel.
func mailboxKeys(ctx context.Context, tx pgx.Tx, workspaceID string) (map[string]string, error) {
	rows, err := tx.Query(ctx, `SELECT mailbox_key,channel_id::text FROM email_channels WHERE workspace_id=$1`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("read the mailboxes: %w", err)
	}
	keys, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) ([2]string, error) {
		var pair [2]string
		return pair, row.Scan(&pair[0], &pair[1])
	})
	if err != nil {
		return nil, fmt.Errorf("read the mailboxes: %w", err)
	}
	byKey := make(map[string]string, len(keys))
	for _, pair := range keys {
		byKey[pair[0]] = pair[1]
	}
	return byKey, nil
}
