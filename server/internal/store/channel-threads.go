package store

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/intake/rules"
	"github.com/jackc/pgx/v5"
)

// Chat threads (docs/internals/channels.md "Chat threads"): every message said in a place of a chat
// channel is an item of that place's thread, written with its journal row, whatever became of it. A
// conversation reads the items of its threads; the intake reads them for context. No public route
// reads them: a customer never sees another customer's message.

// Item dispositions: what became of an item.
const (
	ItemUnresolved    = "unresolved"
	ItemGathering     = "gathering"
	ItemConversation  = "conversation"
	ItemPendingReview = "pending_review"
	ItemNotRequest    = "not_request"
	ItemStaff         = "staff"
	ItemBot           = "bot"
	ItemArchived      = "archived"
)

// itemOutcome is what became of a message the store just journaled.
type itemOutcome struct {
	disposition, reason string
	contactID           string
	// conversationID and messageID when it is part of a conversation now; linkReason says how.
	conversationID, messageID, linkReason string
}

// linkReasonOf is how a message the rules decided on came to its conversation.
func linkReasonOf(decision rules.Decision) string {
	switch decision.Rule {
	case rules.CommandOpen:
		return "command"
	case rules.CommandReopen:
		return "reopen"
	case rules.ThreadKey, rules.ThreadReference:
		return "reply"
	case rules.IntoConversation:
		return "agent"
	}
	return "opened"
}

// recordThreadItems writes the item of each journal event of a message said in a place: a new item, or
// the one written when it was collected, now concluded. A replay or a second delivery never doubles one.
func recordThreadItems(ctx context.Context, tx pgx.Tx, journal *journalWriter, in channels.Inbound, outcome itemOutcome) error {
	if in.Place == nil {
		return nil
	}
	channel := journal.channel
	eventIDs := journal.request.EventIDs
	if len(eventIDs) == 0 && journal.eventID != "" {
		eventIDs = []string{journal.eventID}
	}
	if len(eventIDs) == 0 {
		return nil
	}
	occurred := journal.now
	if !in.SentAt.IsZero() {
		occurred = in.SentAt
	}
	threadContact := ""
	if in.Place.Kind == channels.PlaceDirect {
		threadContact = outcome.contactID
	}
	var threadID string
	if err := tx.QueryRow(ctx, `INSERT INTO channel_threads(workspace_id,channel_id,kind,external_id,name,contact_id,opens_conversations,last_item_at)
		VALUES($1,$2,$3,$4,NULLIF($5,''),NULLIF($6,'')::uuid,$7,$8)
		ON CONFLICT (workspace_id,channel_id,external_id) DO UPDATE SET name=COALESCE(EXCLUDED.name,channel_threads.name),
			contact_id=COALESCE(channel_threads.contact_id,EXCLUDED.contact_id),opens_conversations=EXCLUDED.opens_conversations,
			last_item_at=GREATEST(channel_threads.last_item_at,EXCLUDED.last_item_at),updated_at=now()
		RETURNING id::text`, channel.WorkspaceID, channel.ID, in.Place.Kind, in.Place.ExternalID, in.Place.Name, threadContact,
		in.Place.OpensConversations, occurred).Scan(&threadID); err != nil {
		return fmt.Errorf("record chat thread: %w", err)
	}
	role := in.Role
	if role == "" {
		role = channels.RoleCustomer
	}
	// Collected messages are stored as one message: each of their items, written as it came, concludes now.
	tag, err := tx.Exec(ctx, `UPDATE channel_thread_items SET disposition=$3,disposition_reason=NULLIF($4,''),
			message_id=COALESCE(NULLIF($5,'')::uuid,message_id),contact_id=COALESCE(NULLIF($6,'')::uuid,contact_id)
		WHERE workspace_id=$1 AND inbound_event_id::text=ANY($2)`, channel.WorkspaceID, eventIDs, outcome.disposition, outcome.reason,
		outcome.messageID, outcome.contactID)
	if err != nil {
		return fmt.Errorf("conclude chat items: %w", err)
	}
	if tag.RowsAffected() == 0 && len(eventIDs) == 1 {
		quote := ""
		if len(in.Thread.References) > 0 {
			quote = in.Thread.References[0]
		}
		if _, err := tx.Exec(ctx, `INSERT INTO channel_thread_items(workspace_id,channel_id,thread_id,direction,role,sender_kind,sender_value,sender_name,
				contact_id,external_id,quote_external_id,body,inbound_event_id,message_id,disposition,disposition_reason,occurred_at)
			VALUES($1,$2,$3,'in',$4,NULLIF($5,''),NULLIF($6,''),NULLIF($7,''),NULLIF($8,'')::uuid,NULLIF($9,''),NULLIF($10,''),$11,$12::uuid,
				NULLIF($13,'')::uuid,$14,NULLIF($15,''),$16)
			ON CONFLICT DO NOTHING`, channel.WorkspaceID, channel.ID, threadID, role, clip(in.Sender.Identity.Kind, 40), clip(in.Sender.Identity.Value, 200),
			clip(in.Sender.Name, 200), outcome.contactID, clip(in.ExternalID, 1000), clip(quote, 1000), clip(in.Text, 20000), eventIDs[0],
			outcome.messageID, outcome.disposition, outcome.reason, occurred); err != nil {
			return fmt.Errorf("record chat item: %w", err)
		}
	}
	if outcome.conversationID == "" {
		return nil
	}
	if _, err := tx.Exec(ctx, `INSERT INTO conversation_thread_items(workspace_id,conversation_id,item_id,reason,linked_by_agent_id)
		SELECT $1,$2,id,$4,NULLIF($5,'')::uuid FROM channel_thread_items WHERE workspace_id=$1 AND inbound_event_id::text=ANY($3)
		ON CONFLICT DO NOTHING`, channel.WorkspaceID, outcome.conversationID, eventIDs, outcome.linkReason, journal.request.DecidedBy); err != nil {
		return fmt.Errorf("link chat items: %w", err)
	}
	return nil
}

// recordSentItem writes the item of a reply or a notice a chat channel sent, in the thread its
// conversation's items are in, as the team's or the channel's own.
func recordSentItem(ctx context.Context, tx pgx.Tx, claim OutboxClaim, externalID string, now time.Time) error {
	if claim.ChannelID == "" {
		return nil
	}
	if claim.ThreadItemID != "" {
		// A notice in a chat where no conversation is: its item was written when it was queued.
		if _, err := tx.Exec(ctx, `UPDATE channel_thread_items SET external_id=COALESCE(NULLIF($3,''),external_id),occurred_at=$4
			WHERE workspace_id=$1 AND id=$2`, claim.WorkspaceID, claim.ThreadItemID, clip(externalID, 1000), now); err != nil {
			return fmt.Errorf("record sent chat notice: %w", err)
		}
		return nil
	}
	tag, err := tx.Exec(ctx, `WITH thread AS (
			SELECT i.thread_id,l.conversation_id FROM messages m
			JOIN conversation_thread_items l ON l.workspace_id=m.workspace_id AND l.conversation_id=m.conversation_id AND l.unlinked_at IS NULL
			JOIN channel_thread_items i ON i.workspace_id=l.workspace_id AND i.id=l.item_id AND i.channel_id=$3
			WHERE m.workspace_id=$1 AND m.id=$2 ORDER BY l.linked_at DESC,l.id DESC LIMIT 1),
		item AS (
			INSERT INTO channel_thread_items(workspace_id,channel_id,thread_id,direction,role,agent_id,external_id,body,message_id,disposition,occurred_at)
			SELECT $1,$3,t.thread_id,'out',CASE WHEN m.author_agent_id IS NULL THEN 'bot' ELSE 'staff' END,m.author_agent_id,NULLIF($4,''),
				left(m.body,20000),m.id,CASE WHEN m.author_agent_id IS NULL THEN 'bot' ELSE 'staff' END,$5
			FROM thread t JOIN messages m ON m.workspace_id=$1 AND m.id=$2
			ON CONFLICT DO NOTHING RETURNING id,thread_id)
		INSERT INTO conversation_thread_items(workspace_id,conversation_id,item_id,reason)
		SELECT $1,t.conversation_id,item.id,'reply' FROM item JOIN thread t ON t.thread_id=item.thread_id`,
		claim.WorkspaceID, claim.MessageID, claim.ChannelID, clip(externalID, 1000), now)
	if err != nil {
		return fmt.Errorf("record sent chat item: %w", err)
	}
	if tag.RowsAffected() > 0 {
		if _, err := tx.Exec(ctx, `UPDATE channel_threads t SET last_item_at=GREATEST(t.last_item_at,$3),updated_at=now()
			FROM channel_thread_items i WHERE i.workspace_id=$1 AND i.message_id=$2 AND t.workspace_id=i.workspace_id AND t.id=i.thread_id`,
			claim.WorkspaceID, claim.MessageID, now); err != nil {
			return fmt.Errorf("touch chat thread: %w", err)
		}
	}
	return nil
}

func clip(value string, size int) string {
	runes := []rune(value)
	if len(runes) <= size {
		return value
	}
	return string(runes[:size])
}

// ChatItem is an item of a chat thread as the API shows it.
type ChatItem struct {
	ID        string     `json:"id"`
	ThreadID  string     `json:"thread_id"`
	Direction string     `json:"direction"`
	Role      string     `json:"role"`
	Sender    ChatSender `json:"sender"`
	ContactID *string    `json:"contact_id"`
	AgentID   *string    `json:"agent_id"`
	// IsOtherCustomer: on a conversation's chat, a customer who is not the conversation's.
	IsOtherCustomer   bool                 `json:"is_other_customer"`
	Body              string               `json:"body"`
	ExternalID        *string              `json:"external_id"`
	QuoteExternalID   *string              `json:"quote_external_id"`
	MessageID         *string              `json:"message_id"`
	Disposition       string               `json:"disposition"`
	DispositionReason *string              `json:"disposition_reason"`
	OccurredAt        time.Time            `json:"occurred_at"`
	Files             []ChannelInboundFile `json:"files"`
	// Linked and LinkReason: on a conversation's chat, whether the item is part of it, and how it came.
	Linked     bool    `json:"linked"`
	LinkReason *string `json:"link_reason"`
	// DeliveryStatus is how a notice the channel sent where no conversation is went out (queued, sent,
	// failed); nil for every other item.
	DeliveryStatus *string `json:"delivery_status"`
}

// ChatSender is who wrote an item, as the channel names them.
type ChatSender struct {
	Kind  *string `json:"kind"`
	Value *string `json:"value"`
	Name  *string `json:"name"`
}

// ChannelInboundFile is a file of an item that came in, as it was fetched; its bytes reach the API
// only through the message it became.
type ChannelInboundFile struct {
	Name        string  `json:"name"`
	ContentType string  `json:"content_type"`
	Status      string  `json:"status"`
	Size        *int64  `json:"size"`
	Error       *string `json:"error"`
}

// ChatPage is a page of items, newest first, with the cursor of the next older page.
type ChatPage struct {
	Items []ChatItem `json:"items"`
	Next  *string    `json:"next_cursor"`
}

// ChatThread is a place of a chat channel.
type ChatThread struct {
	ID                 string     `json:"id"`
	ChannelID          string     `json:"channel_id"`
	Kind               string     `json:"kind"`
	ExternalID         string     `json:"external_id"`
	Name               *string    `json:"name"`
	ContactID          *string    `json:"contact_id"`
	OpensConversations bool       `json:"opens_conversations"`
	LastItemAt         *time.Time `json:"last_item_at"`
	LastItem           *ChatItem  `json:"last_item"`
}

// ChatThreadPage is a page of threads, most recently active first.
type ChatThreadPage struct {
	Items []ChatThread `json:"items"`
	Next  *string      `json:"next_cursor"`
}

// MaxChatPage is the most items or threads a page holds.
const MaxChatPage = 100

const chatItemColumns = `i.id::text,i.thread_id::text,i.direction,i.role,i.sender_kind,i.sender_value,i.sender_name,i.contact_id::text,i.agent_id::text,
	i.body,i.external_id,i.quote_external_id,i.message_id::text,i.disposition,i.disposition_reason,i.occurred_at,
	(SELECT COALESCE(json_agg(json_build_object('name',f.name,'content_type',f.content_type,'status',f.status,'size',f.size,'error',f.error) ORDER BY f.idx),'[]')
		FROM channel_inbound_files f WHERE f.workspace_id=i.workspace_id AND f.event_id=i.inbound_event_id),
	(SELECT o.status FROM channel_outbox o WHERE o.workspace_id=i.workspace_id AND o.thread_item_id=i.id)`

func scanChatItem(row pgx.Row, extra ...any) (ChatItem, error) {
	var item ChatItem
	var files []byte
	err := row.Scan(append([]any{&item.ID, &item.ThreadID, &item.Direction, &item.Role, &item.Sender.Kind, &item.Sender.Value, &item.Sender.Name,
		&item.ContactID, &item.AgentID, &item.Body, &item.ExternalID, &item.QuoteExternalID, &item.MessageID, &item.Disposition,
		&item.DispositionReason, &item.OccurredAt, &files, &item.DeliveryStatus}, extra...)...)
	if err != nil {
		return item, err
	}
	item.OccurredAt = domain.UTC(item.OccurredAt)
	item.Files = []ChannelInboundFile{}
	if err := json.Unmarshal(files, &item.Files); err != nil {
		return item, err
	}
	return item, nil
}

var itemID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func encodeItemCursor(at time.Time, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(at.UTC().Format(time.RFC3339Nano) + "|" + id))
}

func decodeItemCursor(cursor string) (time.Time, string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, "", ErrBadCursor
	}
	at, id, found := strings.Cut(string(raw), "|")
	when, err := time.Parse(time.RFC3339Nano, at)
	if !found || err != nil || !itemID.MatchString(id) {
		return time.Time{}, "", ErrBadCursor
	}
	return when, id, nil
}

// pageLimit bounds a page's size.
func pageLimit(limit int) int {
	if limit <= 0 || limit > MaxChatPage {
		return MaxChatPage
	}
	return limit
}

// ConversationChat reads the items of the threads a conversation's items are in, newest first, before
// the cursor; linkedOnly keeps those that are part of it. It is ErrNotFound for another workspace's.
func (s *Store) ConversationChat(ctx context.Context, workspaceID, conversationID, cursor string, limit int, linkedOnly bool) (ChatPage, error) {
	page := ChatPage{Items: []ChatItem{}}
	limit = pageLimit(limit)
	before, beforeID := time.Time{}, ""
	if cursor != "" {
		var err error
		if before, beforeID, err = decodeItemCursor(cursor); err != nil {
			return page, err
		}
	}
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (ChatPage, error) {
		var contactID string
		if err := tx.QueryRow(ctx, `SELECT contact_id::text FROM conversations WHERE workspace_id=$1 AND id=$2::uuid`, workspaceID, conversationID).Scan(&contactID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return page, pgx.ErrNoRows
			}
			return page, err
		}
		rows, err := tx.Query(ctx, `WITH threads AS (
				SELECT DISTINCT i.thread_id FROM conversation_thread_items l
				JOIN channel_thread_items i ON i.workspace_id=l.workspace_id AND i.id=l.item_id
				WHERE l.workspace_id=$1 AND l.conversation_id=$2::uuid AND l.unlinked_at IS NULL)
			SELECT `+chatItemColumns+`,l.reason
			FROM channel_thread_items i
			LEFT JOIN conversation_thread_items l ON l.workspace_id=i.workspace_id AND l.item_id=i.id AND l.conversation_id=$2::uuid AND l.unlinked_at IS NULL
			WHERE i.workspace_id=$1 AND i.thread_id IN (SELECT thread_id FROM threads) AND (NOT $3 OR l.id IS NOT NULL)
				AND ($4::timestamptz IS NULL OR (i.occurred_at,i.id) < ($4,NULLIF($5,'')::uuid))
			ORDER BY i.occurred_at DESC,i.id DESC LIMIT $6`, workspaceID, conversationID, linkedOnly, nullTime(before), beforeID, limit+1)
		if err != nil {
			return page, fmt.Errorf("read conversation chat: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var reason *string
			item, err := scanChatItem(rows, &reason)
			if err != nil {
				return page, fmt.Errorf("read conversation chat: %w", err)
			}
			item.Linked, item.LinkReason = reason != nil, reason
			item.IsOtherCustomer = item.Role == channels.RoleCustomer && item.ContactID != nil && *item.ContactID != contactID
			page.Items = append(page.Items, item)
		}
		if err := rows.Err(); err != nil {
			return page, err
		}
		page.Items, page.Next = cutItemPage(page.Items, limit)
		return page, nil
	})
}

func cutItemPage(items []ChatItem, limit int) ([]ChatItem, *string) {
	if len(items) <= limit {
		return items, nil
	}
	items = items[:limit]
	last := items[limit-1]
	next := encodeItemCursor(last.OccurredAt, last.ID)
	return items, &next
}

func nullTime(at time.Time) *time.Time {
	if at.IsZero() {
		return nil
	}
	return &at
}

// ChannelThreads lists a channel's threads, most recently active first, of a kind when one is named.
func (s *Store) ChannelThreads(ctx context.Context, workspaceID, channelID, kind, cursor string, limit int) (ChatThreadPage, error) {
	page := ChatThreadPage{Items: []ChatThread{}}
	limit = pageLimit(limit)
	before, beforeID := time.Time{}, ""
	if cursor != "" {
		var err error
		if before, beforeID, err = decodeItemCursor(cursor); err != nil {
			return page, err
		}
	}
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (ChatThreadPage, error) {
		if _, err := s.channelByID(ctx, tx, workspaceID, channelID); err != nil {
			return page, err
		}
		rows, err := tx.Query(ctx, `SELECT t.id::text,t.channel_id::text,t.kind,t.external_id,t.name,t.contact_id::text,t.opens_conversations,
				COALESCE(t.last_item_at,t.created_at)
			FROM channel_threads t WHERE t.workspace_id=$1 AND t.channel_id=$2::uuid AND ($3='' OR t.kind=$3)
				AND ($4::timestamptz IS NULL OR (COALESCE(t.last_item_at,t.created_at),t.id) < ($4,NULLIF($5,'')::uuid))
			ORDER BY COALESCE(t.last_item_at,t.created_at) DESC,t.id DESC LIMIT $6`, workspaceID, channelID, kind, nullTime(before), beforeID, limit+1)
		if err != nil {
			return page, fmt.Errorf("read chat threads: %w", err)
		}
		threads, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (ChatThread, error) {
			var t ChatThread
			var at time.Time
			err := row.Scan(&t.ID, &t.ChannelID, &t.Kind, &t.ExternalID, &t.Name, &t.ContactID, &t.OpensConversations, &at)
			at = domain.UTC(at)
			t.LastItemAt = &at
			return t, err
		})
		if err != nil {
			return page, fmt.Errorf("read chat threads: %w", err)
		}
		if len(threads) > limit {
			threads = threads[:limit]
			last := threads[limit-1]
			next := encodeItemCursor(*last.LastItemAt, last.ID)
			page.Next = &next
		}
		for index := range threads {
			item, err := scanChatItem(tx.QueryRow(ctx, `SELECT `+chatItemColumns+` FROM channel_thread_items i WHERE i.workspace_id=$1 AND i.thread_id=$2::uuid
				ORDER BY i.occurred_at DESC,i.id DESC LIMIT 1`, workspaceID, threads[index].ID))
			switch {
			case errors.Is(err, pgx.ErrNoRows):
			case err != nil:
				return page, fmt.Errorf("read chat threads: %w", err)
			default:
				threads[index].LastItem = &item
			}
		}
		page.Items = threads
		return page, nil
	})
}

// ThreadItems reads a thread's items, newest first, before the cursor: everything said there, what AI
// set aside included.
func (s *Store) ThreadItems(ctx context.Context, workspaceID, channelID, threadID, cursor string, limit int) (ChatPage, error) {
	page := ChatPage{Items: []ChatItem{}}
	limit = pageLimit(limit)
	before, beforeID := time.Time{}, ""
	if cursor != "" {
		var err error
		if before, beforeID, err = decodeItemCursor(cursor); err != nil {
			return page, err
		}
	}
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (ChatPage, error) {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM channel_threads WHERE workspace_id=$1 AND channel_id=$2::uuid AND id=$3::uuid)`,
			workspaceID, channelID, threadID).Scan(&exists); err != nil {
			return page, err
		}
		if !exists {
			return page, pgx.ErrNoRows
		}
		rows, err := tx.Query(ctx, `SELECT `+chatItemColumns+` FROM channel_thread_items i WHERE i.workspace_id=$1 AND i.thread_id=$2::uuid
				AND ($3::timestamptz IS NULL OR (i.occurred_at,i.id) < ($3,NULLIF($4,'')::uuid))
			ORDER BY i.occurred_at DESC,i.id DESC LIMIT $5`, workspaceID, threadID, nullTime(before), beforeID, limit+1)
		if err != nil {
			return page, fmt.Errorf("read chat items: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			item, err := scanChatItem(rows)
			if err != nil {
				return page, fmt.Errorf("read chat items: %w", err)
			}
			page.Items = append(page.Items, item)
		}
		if err := rows.Err(); err != nil {
			return page, err
		}
		page.Items, page.Next = cutItemPage(page.Items, limit)
		return page, nil
	})
}

// UnresolvedItems lists the customer items of threads that open conversations which nobody concluded
// on (arrived while the channel was off, or failed), older than olderThan, oldest first.
func (s *Store) UnresolvedItems(ctx context.Context, workspaceID string, olderThan time.Duration, cursor string, limit int, now time.Time) (ChatPage, error) {
	page := ChatPage{Items: []ChatItem{}}
	limit = pageLimit(limit)
	after, afterID := time.Time{}, ""
	if cursor != "" {
		var err error
		if after, afterID, err = decodeItemCursor(cursor); err != nil {
			return page, err
		}
	}
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (ChatPage, error) {
		rows, err := tx.Query(ctx, `SELECT `+chatItemColumns+` FROM channel_thread_items i
			JOIN channel_threads t ON t.workspace_id=i.workspace_id AND t.id=i.thread_id AND t.opens_conversations
			WHERE i.workspace_id=$1 AND i.disposition='unresolved' AND i.role='customer' AND i.occurred_at<=$2
				AND ($3::timestamptz IS NULL OR (i.occurred_at,i.id) > ($3,NULLIF($4,'')::uuid))
			ORDER BY i.occurred_at,i.id LIMIT $5`, workspaceID, now.Add(-olderThan), nullTime(after), afterID, limit+1)
		if err != nil {
			return page, fmt.Errorf("read unresolved chat items: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			item, err := scanChatItem(rows)
			if err != nil {
				return page, fmt.Errorf("read unresolved chat items: %w", err)
			}
			page.Items = append(page.Items, item)
		}
		if err := rows.Err(); err != nil {
			return page, err
		}
		page.Items, page.Next = cutItemPage(page.Items, limit)
		return page, nil
	})
}

// queueThreadNotice answers the customer's item of the journal's event, in its chat, with a notice the
// channel sends by itself: its item (the channel's, quoting the customer's) and its delivery are written
// now, in the transaction that keeps the customer's message. An item answered already is not answered
// again (a second delivery, a replay).
func queueThreadNotice(ctx context.Context, tx pgx.Tx, journal *journalWriter, kind, text string) error {
	if journal.eventID == "" || text == "" {
		return nil
	}
	channel := journal.channel
	var itemID string
	err := tx.QueryRow(ctx, `WITH customer AS (
			SELECT i.thread_id,i.external_id FROM channel_thread_items i WHERE i.workspace_id=$1 AND i.inbound_event_id=$2::uuid AND i.direction='in'
				AND NOT EXISTS (SELECT 1 FROM channel_thread_items b WHERE b.workspace_id=i.workspace_id AND b.thread_id=i.thread_id AND b.role='bot'
					AND b.disposition_reason=$5 AND b.quote_external_id=i.external_id)
			LIMIT 1)
		INSERT INTO channel_thread_items(workspace_id,channel_id,thread_id,direction,role,quote_external_id,body,disposition,disposition_reason,occurred_at)
		SELECT $1,$3,thread_id,'out','bot',external_id,left($4,20000),'bot',$5,$6 FROM customer RETURNING id::text`,
		channel.WorkspaceID, journal.eventID, channel.ID, text, kind, journal.now).Scan(&itemID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("record chat notice: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO channel_outbox(workspace_id,thread_item_id,status,created_at,recipients,channel_type,channel_id)
		VALUES($1,$2,'queued',$3,1,$4,$5)`, channel.WorkspaceID, itemID, journal.now, string(channel.Type), channel.ID); err != nil {
		return fmt.Errorf("queue chat notice: %w", err)
	}
	return nil
}

// loadThreadNotice reads what a sender needs of a notice queued in a chat where no conversation is: its
// text, the place, and the customer it answers, whose message it quotes.
func loadThreadNotice(ctx context.Context, tx pgx.Tx, claim OutboxClaim, out channels.Outbound) (channels.Outbound, error) {
	var place channels.Place
	var quote, senderKind, senderValue, senderName, contactID *string
	err := tx.QueryRow(ctx, `SELECT n.id::text,n.body,n.created_at,t.kind,t.external_id,COALESCE(t.name,''),n.quote_external_id,c.sender_kind,c.sender_value,c.sender_name,c.contact_id::text
		FROM channel_thread_items n JOIN channel_threads t ON t.workspace_id=n.workspace_id AND t.id=n.thread_id
		LEFT JOIN LATERAL (SELECT i.sender_kind,i.sender_value,i.sender_name,i.contact_id FROM channel_thread_items i
			WHERE i.workspace_id=n.workspace_id AND i.thread_id=n.thread_id AND i.direction='in' AND i.external_id=n.quote_external_id LIMIT 1) c ON true
		WHERE n.workspace_id=$1 AND n.id=$2`, claim.WorkspaceID, claim.ThreadItemID).
		Scan(&out.Message.ID, &out.Message.Body, &out.Message.CreatedAt, &place.Kind, &place.ExternalID, &place.Name, &quote, &senderKind, &senderValue, &senderName, &contactID)
	if err != nil {
		return out, fmt.Errorf("load chat notice %s: %w", claim.ThreadItemID, err)
	}
	out.Place = &place
	if quote != nil {
		out.References, out.LastIncoming = []string{*quote}, *quote
	}
	if contactID != nil {
		out.Recipient.ContactID = *contactID
	}
	if senderName != nil {
		out.Recipient.Name = *senderName
	}
	if senderKind != nil && senderValue != nil {
		out.Recipient.Identities = []channels.Identity{{Kind: *senderKind, Value: *senderValue}}
	}
	out.Message.Files = []channels.OutFile{}
	return out, nil
}
