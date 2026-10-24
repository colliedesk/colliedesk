package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// IngestRequest is one normalized message of a channel for the pipeline to store.
type IngestRequest struct {
	WorkspaceID, ChannelID string
	Raw                    channels.Raw
	Inbound                channels.Inbound
	// EventIDs are the journal rows the message came from when they were written before processing
	// (an Async type, a held or collected event); empty for a Sync type, whose row is written now.
	EventIDs []string
	Now      time.Time
	// Replay opens a held event even though its channel is still off.
	Replay bool
	// IntoConversation adds the message to this conversation of the workspace instead of threading it.
	IntoConversation string
	// DecidedBy is the agent who replayed a held event.
	DecidedBy string
	// Flushing stores collected events as one message now (their Aggregate is not applied again).
	Flushing bool
}

// IngestInbound is the one writer of conversations, contacts and messages from a channel: duplicate,
// dropped, held, collected, threaded or new, in one workspace transaction with its journal row.
func (s *Store) IngestInbound(ctx context.Context, request IngestRequest) (channels.IngestResult, error) {
	result, err := s.ingestInbound(ctx, request)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		// A concurrent delivery of the same message won the race; report it as the duplicate it is.
		return s.ingestInbound(ctx, request)
	}
	return result, err
}

// ResultSkipped means another worker holds the events; nothing was done.
const ResultSkipped = "skipped"

// ErrChannelHasNoAdapter means a channel's type is not one this server runs.
var ErrChannelHasNoAdapter = errors.New("this server does not run the channel's type")

func (s *Store) ingestInbound(ctx context.Context, request IngestRequest) (channels.IngestResult, error) {
	var result channels.IngestResult
	in := request.Inbound
	now := request.Now
	tx, err := s.db.BeginWorkspace(ctx, request.WorkspaceID)
	if err != nil {
		return result, fmt.Errorf("begin inbound message: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	workspaceID := request.WorkspaceID
	channel, err := s.channelByID(ctx, tx, workspaceID, request.ChannelID)
	if err != nil {
		return result, err
	}
	adapter, ok := s.channels.Adapter(channel.Type)
	if !ok {
		return result, ErrChannelHasNoAdapter
	}
	definition := adapter.Definition()
	if len(request.EventIDs) > 0 {
		// Another worker may hold these events; leave them to it.
		allowed := []string{"received", "failed", "pending"}
		if request.Replay {
			allowed = []string{"held"}
		}
		var locked int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM (SELECT id FROM channel_inbound_events WHERE workspace_id=$1 AND channel_id=$2
			AND id::text=ANY($3) AND state=ANY($4) FOR UPDATE SKIP LOCKED) e`, workspaceID, channel.ID, request.EventIDs, allowed).Scan(&locked); err != nil {
			return result, fmt.Errorf("lock channel events: %w", err)
		}
		if locked != len(request.EventIDs) {
			result.Status = ResultSkipped
			return result, nil
		}
	}
	journal := journalWriter{tx: tx, channel: channel, request: request, now: now}
	finish := func(r channels.IngestResult) (channels.IngestResult, error) {
		if err := tx.Commit(ctx); err != nil {
			return r, fmt.Errorf("commit inbound message: %w", err)
		}
		r.EventID = journal.eventID
		return r, nil
	}
	drop := func(reason string) (channels.IngestResult, error) {
		if err := journal.write(ctx, "dropped", journalDetail{dropReason: reason}); err != nil {
			return result, err
		}
		result.Status, result.Reason = channels.ResultDropped, &reason
		return finish(result)
	}
	if !channel.Live() {
		return drop(channels.DropChannelRemoved)
	}
	off := !channel.Enabled && !request.Replay
	if off && definition.WhenOff == channels.OffRefuse {
		return result, channels.ErrChannelOff
	}
	if s.limits {
		access, err := workspaceAccess(ctx, tx, workspaceID, now)
		if err != nil {
			return result, err
		}
		if access.Locked {
			return result, domain.ErrMailboxClosed
		}
	}
	senderKey := in.Sender.Identity.Key()
	// Serialize one sender's messages on the channel so the rate count and duplicate check see each other.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "channel-inbound:"+channel.ID+":"+senderKey); err != nil {
		return result, fmt.Errorf("lock inbound sender: %w", err)
	}
	limits := channel.Settings.RateLimits
	if limits.NewConversations > 0 && limits.Scope == channels.ScopeChannel {
		// And every new conversation of the channel, so its ceiling count sees the others.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "channel-new:"+channel.ID); err != nil {
			return result, fmt.Errorf("lock channel: %w", err)
		}
	}
	externalID := ""
	if in.ExternalID != "" {
		externalID = string(channel.Type) + ":" + in.ExternalID
		var existingID, existingKind, existingConversation string
		err := tx.QueryRow(ctx, `SELECT id::text,kind,conversation_id::text FROM messages WHERE workspace_id=$1 AND external_id=$2`,
			workspaceID, externalID).Scan(&existingID, &existingKind, &existingConversation)
		switch {
		case err == nil && existingKind == "incoming":
			if len(request.EventIDs) > 0 {
				if err := journal.write(ctx, "processed", journalDetail{conversationID: existingConversation, messageID: existingID}); err != nil {
					return result, err
				}
			}
			result.Status, result.MessageID, result.ConversationID = channels.ResultDuplicate, &existingID, &existingConversation
			return finish(result)
		case err == nil:
			return drop(channels.DropOwnMessage)
		case !errors.Is(err, pgx.ErrNoRows):
			return result, fmt.Errorf("check inbound duplicate: %w", err)
		}
	}
	if in.ExternalID != "" {
		// The copy a channel sends back of what we sent is our own.
		var ours bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM channel_sent_ids WHERE workspace_id=$1 AND channel_id=$2 AND external_id=$3)`,
			workspaceID, channel.ID, in.ExternalID).Scan(&ours); err != nil {
			return result, fmt.Errorf("check sent ids: %w", err)
		}
		if ours {
			return drop(channels.DropEcho)
		}
	}
	if in.Kind == channels.KindDeliveryUpdate && in.Delivery != nil {
		if err := applyDeliveryUpdate(ctx, tx, channel, *in.Delivery); err != nil {
			return result, err
		}
	}
	switch {
	case in.DropReason != "":
		return drop(in.DropReason)
	case in.Kind == channels.KindDeliveryUpdate:
		return drop("delivery_update")
	case in.Kind == channels.KindEcho:
		return drop(channels.DropEcho)
	case in.Kind == channels.KindIgnore:
		return drop("ignored")
	case in.Kind == channels.KindArchive:
		if err := journal.write(ctx, "processed", journalDetail{senderKey: senderKey}); err != nil {
			return result, err
		}
		result.Status = channels.ResultArchived
		return finish(result)
	}
	command, commanded := channel.Settings.Commands[in.Intent.Command]
	commanded = commanded && in.Intent.Kind == channels.IntentCommand
	if commanded && command.Action == channels.ActionCancel {
		in.Intent.Kind = channels.IntentCancel
	}
	if in.Intent.Kind == channels.IntentCancel {
		if in.Aggregate != nil {
			// Cancelling throws away what was being collected with it.
			if _, err := tx.Exec(ctx, `UPDATE channel_inbound_events SET state='dropped',drop_reason=$4,flush_at=NULL,processed_at=$5
				WHERE workspace_id=$1 AND channel_id=$2 AND state='pending' AND aggregate_key=$3`,
				workspaceID, channel.ID, in.Aggregate.Key, channels.DropCancelled, now); err != nil {
				return result, fmt.Errorf("cancel collected messages: %w", err)
			}
		}
		return drop(channels.DropCancelled)
	}
	if limits.PerSender > 0 {
		var recent int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM channel_inbound_events WHERE workspace_id=$1 AND channel_id=$2 AND sender_key=$3
			AND received_at>$4 AND (state='held' OR (state='processed' AND message_id IS NOT NULL)) AND NOT (id::text=ANY($5))`,
			workspaceID, channel.ID, senderKey, now.Add(-limits.Window()), request.EventIDs).Scan(&recent); err != nil {
			return result, fmt.Errorf("check inbound rate: %w", err)
		}
		if recent >= limits.PerSender {
			return result, ErrRateLimited
		}
	}

	if off {
		return s.holdInbound(ctx, tx, &journal, in, channels.HoldChannelOff, finish)
	}
	bound := in.StorageBound
	if bound == 0 {
		for _, file := range in.Files {
			bound += int64(len(file.Data))
		}
	}
	if err := s.CheckInboundStorage(ctx, tx, workspaceID, bound, now); err != nil {
		return result, err
	}
	if in.Aggregate != nil && len(request.EventIDs) > 0 && !request.Replay && !request.Flushing {
		if commanded && command.Action != channels.ActionReopen {
			// A command starts a new request: what was collected before it is sent on its own (or kept
			// for review), and collecting starts again with this message.
			if _, err := tx.Exec(ctx, `UPDATE channel_inbound_events SET aggregate_key=left($3||'#'||$5,500),flush_at=$4
				WHERE workspace_id=$1 AND channel_id=$2 AND state='pending' AND aggregate_key=$3`,
				workspaceID, channel.ID, in.Aggregate.Key, now, request.EventIDs[0]); err != nil {
				return result, fmt.Errorf("close collected messages: %w", err)
			}
		}
		// Due when the sender has been quiet for Quiet, or Max after the first message, or now when it confirms.
		var first *time.Time
		if err := tx.QueryRow(ctx, `SELECT min(received_at) FROM channel_inbound_events WHERE workspace_id=$1 AND channel_id=$2
			AND state='pending' AND aggregate_key=$3`, workspaceID, channel.ID, in.Aggregate.Key).Scan(&first); err != nil {
			return result, fmt.Errorf("load collected messages: %w", err)
		}
		flush := now.Add(in.Aggregate.Quiet)
		if first != nil && in.Aggregate.Max > 0 && first.Add(in.Aggregate.Max).Before(flush) {
			flush = first.Add(in.Aggregate.Max)
		}
		if in.Intent.Kind == channels.IntentConfirm {
			flush = now
		}
		if _, err := tx.Exec(ctx, `UPDATE channel_inbound_events SET flush_at=$4 WHERE workspace_id=$1 AND channel_id=$2 AND state='pending' AND aggregate_key=$3`,
			workspaceID, channel.ID, in.Aggregate.Key, flush); err != nil {
			return result, fmt.Errorf("collect message: %w", err)
		}
		if err := journal.write(ctx, "pending", journalDetail{senderKey: senderKey, aggregateKey: in.Aggregate.Key, flushAt: &flush}); err != nil {
			return result, err
		}
		result.Status = channels.ResultPending
		return finish(result)
	}

	if request.Flushing && channel.Settings.Review && !commanded {
		// Collected messages that named no command wait for a person to decide what they are.
		return s.holdInbound(ctx, tx, &journal, in, channels.HoldReview, finish)
	}
	contactID, err := s.contactForIdentity(ctx, tx, channel, in.Sender)
	if err != nil {
		return result, err
	}
	conversationID := request.IntoConversation
	if conversationID == "" && commanded && command.Action == channels.ActionReopen && in.Intent.Ref != "" {
		var found string
		err := tx.QueryRow(ctx, `SELECT id::text FROM conversations WHERE workspace_id=$1 AND id::text=$2 AND contact_id=$3`,
			workspaceID, in.Intent.Ref, contactID).Scan(&found)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return result, fmt.Errorf("find conversation to reopen: %w", err)
		}
		conversationID = found
	}
	if conversationID != "" {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM conversations WHERE workspace_id=$1 AND id=$2::uuid)`, workspaceID, conversationID).Scan(&exists); err != nil {
			return result, fmt.Errorf("find conversation: %w", err)
		}
		if !exists {
			return result, pgx.ErrNoRows
		}
	} else {
		if conversationID, err = findChannelThread(ctx, tx, channel, in.Thread, now); err != nil {
			return result, err
		}
	}
	var files []EmailFile
	if files, err = s.putInboundFiles(ctx, workspaceID, in.Files); err != nil {
		return result, err
	}
	result.Status = channels.ResultThreaded
	if conversationID == "" {
		if limits.NewConversations > 0 {
			var created int
			query := `SELECT count(*) FROM conversations WHERE workspace_id=$1 AND created_at>$2`
			args := []any{workspaceID, now.Add(-limits.Window())}
			if limits.Scope == channels.ScopeChannel {
				query += ` AND channel_id=$3`
				args = append(args, channel.ID)
			}
			if err := tx.QueryRow(ctx, query, args...).Scan(&created); err != nil {
				return result, fmt.Errorf("check new conversation ceiling: %w", err)
			}
			if created >= limits.NewConversations {
				return result, ErrRateLimited
			}
		}
		result.Status = channels.ResultCreated
		if conversationID, err = s.createChannelConversation(ctx, tx, channel, contactID, in, now); err != nil {
			return result, err
		}
	} else {
		if err := setStatusActor(ctx, tx, ""); err != nil {
			return result, err
		}
		if _, err := tx.Exec(ctx, `UPDATE conversations SET status=CASE WHEN status IN ('resolved','closed') THEN 'open' ELSE status END,
			updated_at=now() WHERE workspace_id=$1 AND id=$2`, workspaceID, conversationID); err != nil {
			return result, fmt.Errorf("reopen conversation: %w", err)
		}
	}
	for _, key := range in.Thread.Record {
		if _, err := tx.Exec(ctx, `INSERT INTO conversation_thread_keys(workspace_id,conversation_id,channel_type,key) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`,
			workspaceID, conversationID, string(channel.Type), key); err != nil {
			return result, fmt.Errorf("record thread key: %w", err)
		}
	}
	messageID, err := insertChannelMessage(ctx, tx, channel, conversationID, externalID, in)
	if err != nil {
		return result, err
	}
	original, err := linkChannelFiles(ctx, tx, s.objects, workspaceID, messageID, files, in.EmailMeta != nil)
	if err != nil {
		return result, err
	}
	if err := journal.write(ctx, "processed", journalDetail{senderKey: senderKey, conversationID: conversationID, messageID: messageID,
		objectKey: original}); err != nil {
		return result, err
	}
	if result.Status == channels.ResultCreated && channel.Settings.Notices.Received != "" {
		if _, err := s.createMessage(ctx, tx, workspaceID, conversationID, Actor{}, "outgoing", channel.Settings.Notices.Received, nil, nil); err != nil {
			return result, fmt.Errorf("send received notice: %w", err)
		}
	}
	result.ConversationID, result.MessageID = &conversationID, &messageID
	return finish(result)
}

// holdInbound keeps a message that arrived while its channel was off: its original is stored and
// counts toward storage while it waits for a person to open or discard it.
func (s *Store) holdInbound(ctx context.Context, tx pgx.Tx, journal *journalWriter, in channels.Inbound, reason string,
	finish func(channels.IngestResult) (channels.IngestResult, error)) (channels.IngestResult, error) {
	var result channels.IngestResult
	detail := journalDetail{senderKey: in.Sender.Identity.Key(), holdReason: reason}
	for _, file := range in.Files {
		if !file.Original {
			continue
		}
		size := int64(len(file.Data))
		if err := s.CheckInboundStorage(ctx, tx, journal.channel.WorkspaceID, size, journal.now); err != nil {
			return result, err
		}
		stored, err := s.putInboundFiles(ctx, journal.channel.WorkspaceID, []channels.InboundFile{file})
		if err != nil {
			return result, err
		}
		if err := VerifyObjects(ctx, tx, s.objects, stored[0].Object.Key); err != nil {
			return result, err
		}
		detail.objectKey, detail.objectBytes = stored[0].Object.Key, stored[0].Object.Size
	}
	if err := journal.write(ctx, "held", detail); err != nil {
		return result, err
	}
	result.Status, result.Reason = channels.ResultHeld, &reason
	return finish(result)
}

// putInboundFiles writes the files of a message that will be stored. Objects are content-addressed
// and may be shared, so a later failure never deletes them; the sender's retry writes the same keys.
func (s *Store) putInboundFiles(ctx context.Context, workspaceID string, files []channels.InboundFile) ([]EmailFile, error) {
	stored := make([]EmailFile, 0, len(files))
	for _, file := range files {
		var object attachments.Object
		switch {
		case file.Object != nil:
			object = *file.Object
		case s.objects == nil:
			return nil, errors.New("attachment storage is not configured; inbound files cannot be kept")
		default:
			var err error
			if object, err = s.objects.Put(ctx, workspaceID, bytes.NewReader(file.Data), int64(len(file.Data))); err != nil {
				return nil, fmt.Errorf("store inbound files: %w", err)
			}
		}
		stored = append(stored, EmailFile{Filename: file.Name, ContentType: file.ContentType, Original: file.Original, Inline: file.Inline, Object: object})
	}
	return stored, nil
}

// linkChannelFiles records stored files as attachments of the message and returns the key of the
// original; an email envelope points at it as original_attachment_id.
func linkChannelFiles(ctx context.Context, tx pgx.Tx, storage attachments.Storage, workspaceID, messageID string, files []EmailFile, envelope bool) (string, error) {
	original := ""
	linked := make([]EmailFile, len(files))
	for i, file := range files {
		if file.Original {
			original = file.Object.Key
		}
		file.Original = file.Original && envelope
		linked[i] = file
	}
	return original, linkFiles(ctx, tx, storage, workspaceID, messageID, linked)
}

// contactForIdentity finds or creates the contact a sender is: by email address, or by the sender's
// identity on the channel, which a new contact without an email address gets.
func (s *Store) contactForIdentity(ctx context.Context, tx pgx.Tx, channel channels.Channel, sender channels.Sender) (string, error) {
	identity := sender.Identity
	name := sender.Name
	if identity.Kind == channels.IdentityEmail {
		if name == "" {
			name = identity.Value[:max(strings.LastIndex(identity.Value, "@"), 0)]
		}
		return contactForEmail(ctx, tx, channel.WorkspaceID, name, identity.Value)
	}
	var contactID string
	err := tx.QueryRow(ctx, `SELECT COALESCE(c.merged_into_id,c.id)::text FROM contact_identities i
		JOIN contacts c ON c.workspace_id=i.workspace_id AND c.id=i.contact_id
		WHERE i.workspace_id=$1 AND i.channel_id=$2 AND i.kind=$3 AND i.value=$4`,
		channel.WorkspaceID, channel.ID, identity.Kind, identity.Value).Scan(&contactID)
	if err == nil {
		if _, err := tx.Exec(ctx, `UPDATE contact_identities SET display_name=NULLIF($5,''),updated_at=now()
			WHERE workspace_id=$1 AND channel_id=$2 AND kind=$3 AND value=$4 AND display_name IS DISTINCT FROM NULLIF($5,'')`,
			channel.WorkspaceID, channel.ID, identity.Kind, identity.Value, sender.Name); err != nil {
			return "", fmt.Errorf("update contact identity: %w", err)
		}
		return contactID, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("find contact identity: %w", err)
	}
	if name == "" {
		name = identity.Value
	}
	if sender.Email != "" {
		if contactID, err = contactForEmail(ctx, tx, channel.WorkspaceID, name, strings.ToLower(sender.Email)); err != nil {
			return "", err
		}
	} else if err := tx.QueryRow(ctx, `INSERT INTO contacts(workspace_id,name,organization) VALUES($1,$2,NULLIF($3,'')) RETURNING id::text`,
		channel.WorkspaceID, name, sender.Organization).Scan(&contactID); err != nil {
		return "", fmt.Errorf("create contact: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO contact_identities(workspace_id,contact_id,channel_id,kind,value,display_name) VALUES($1,$2,$3,$4,$5,NULLIF($6,''))`,
		channel.WorkspaceID, contactID, channel.ID, identity.Kind, identity.Value, sender.Name); err != nil {
		return "", fmt.Errorf("record contact identity: %w", err)
	}
	return contactID, nil
}

// findChannelThread finds the conversation a message continues: an earlier message it references,
// then a thread key, as the channel's join policy allows.
func findChannelThread(ctx context.Context, tx pgx.Tx, channel channels.Channel, thread channels.Thread, now time.Time) (string, error) {
	policy := channel.Settings.JoinPolicy
	if policy.Kind == channels.JoinNever {
		return "", nil
	}
	joins := func(status string, updatedAt time.Time) bool {
		if policy.Kind != channels.JoinWithinDays || (status != "resolved" && status != "closed") {
			return true
		}
		return updatedAt.After(now.Add(-time.Duration(policy.Days) * 24 * time.Hour))
	}
	if len(thread.References) > 0 {
		candidates := make([]string, len(thread.References))
		for i, reference := range thread.References {
			candidates[i] = string(channel.Type) + ":" + reference
		}
		rows, err := tx.Query(ctx, `SELECT m.external_id,c.id::text,c.status,c.updated_at FROM messages m
			JOIN conversations c ON c.workspace_id=m.workspace_id AND c.id=m.conversation_id
			WHERE m.workspace_id=$1 AND m.external_id=ANY($2)`, channel.WorkspaceID, candidates)
		if err != nil {
			return "", fmt.Errorf("match thread references: %w", err)
		}
		type match struct {
			conversationID, status string
			updatedAt              time.Time
		}
		found := map[string]match{}
		for rows.Next() {
			var id string
			var m match
			if err := rows.Scan(&id, &m.conversationID, &m.status, &m.updatedAt); err != nil {
				rows.Close()
				return "", fmt.Errorf("scan thread reference: %w", err)
			}
			found[id] = m
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return "", fmt.Errorf("iterate thread references: %w", err)
		}
		for _, candidate := range candidates {
			if m, ok := found[candidate]; ok && joins(m.status, m.updatedAt) {
				return m.conversationID, nil
			}
		}
	}
	for _, key := range thread.Keys {
		var conversationID, status string
		var updatedAt time.Time
		err := tx.QueryRow(ctx, `SELECT c.id::text,c.status,c.updated_at FROM conversation_thread_keys k
			JOIN conversations c ON c.workspace_id=k.workspace_id AND c.id=k.conversation_id
			WHERE k.workspace_id=$1 AND k.channel_type=$2 AND k.key=$3 ORDER BY k.created_at DESC LIMIT 1`,
			channel.WorkspaceID, string(channel.Type), key).Scan(&conversationID, &status, &updatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("match thread key: %w", err)
		}
		if joins(status, updatedAt) {
			return conversationID, nil
		}
		return "", nil
	}
	return "", nil
}

// createChannelConversation opens a conversation from the channel's first message, with the
// channel's default tags, priority and assignee and those of the command it named.
func (s *Store) createChannelConversation(ctx context.Context, tx pgx.Tx, channel channels.Channel, contactID string, in channels.Inbound, now time.Time) (string, error) {
	settings := channel.Settings
	tags := append([]string{}, settings.DefaultTags...)
	priority := settings.DefaultPriority
	if in.Intent.Kind == channels.IntentCommand {
		if command, ok := settings.Commands[in.Intent.Command]; ok && command.Action != channels.ActionCancel {
			tags = append(tags, command.Tags...)
			if command.Priority != "" {
				priority = command.Priority
			}
		}
	}
	if priority == "" {
		priority = "normal"
	}
	var conversationID string
	if err := tx.QueryRow(ctx, `INSERT INTO conversations(workspace_id,contact_id,subject,created_at,updated_at,channel,channel_id,priority,assignee_id)
		VALUES($1,$2,$3,$4,$4,$5,$6,$7,(SELECT id FROM agents WHERE workspace_id=$1 AND id=NULLIF($8,'')::uuid)) RETURNING id::text`,
		channel.WorkspaceID, contactID, in.Subject, now, string(channel.Type), channel.ID, priority, settings.DefaultAssigneeID).Scan(&conversationID); err != nil {
		return "", fmt.Errorf("create conversation: %w", err)
	}
	for _, name := range tags {
		var tagID string
		if err := tx.QueryRow(ctx, `INSERT INTO tags(workspace_id,name) VALUES($1,$2)
			ON CONFLICT(workspace_id,name) DO UPDATE SET name=tags.name RETURNING id::text`, channel.WorkspaceID, name).Scan(&tagID); err != nil {
			return "", fmt.Errorf("find or create tag: %w", err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO conversation_tags(workspace_id,conversation_id,tag_id) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`,
			channel.WorkspaceID, conversationID, tagID); err != nil {
			return "", fmt.Errorf("tag conversation: %w", err)
		}
	}
	return conversationID, nil
}

func insertChannelMessage(ctx context.Context, tx pgx.Tx, channel channels.Channel, conversationID, externalID string, in channels.Inbound) (string, error) {
	var envelope []byte
	if in.EmailMeta != nil {
		var err error
		if envelope, err = json.Marshal(in.EmailMeta); err != nil {
			return "", fmt.Errorf("encode email metadata: %w", err)
		}
	}
	var meta []byte
	if len(in.Meta) > 0 {
		meta = in.Meta
	}
	var sentAt *time.Time
	if !in.SentAt.IsZero() {
		sentAt = &in.SentAt
	}
	var messageID string
	if err := tx.QueryRow(ctx, `INSERT INTO messages(workspace_id,conversation_id,kind,body,body_html,external_id,channel_id,channel_meta,email_meta,created_at)
		VALUES($1,$2,'incoming',$3,NULLIF($4,''),NULLIF($5,''),$6,$7,$8,COALESCE($9,now())) RETURNING id::text`,
		channel.WorkspaceID, conversationID, in.Text, in.HTML, externalID, channel.ID, meta, envelope, sentAt).Scan(&messageID); err != nil {
		return "", fmt.Errorf("store inbound message: %w", err)
	}
	return messageID, nil
}

// applyDeliveryUpdate fails the deliveries of our replies a channel reports as failed. When every
// failed recipient was only copied, the customer still got the reply: it stays as it is and the
// bounced copies are added to its note.
func applyDeliveryUpdate(ctx context.Context, tx pgx.Tx, channel channels.Channel, update channels.DeliveryUpdate) error {
	if len(update.References) == 0 {
		return nil
	}
	ids := make([]string, len(update.References))
	for i, reference := range update.References {
		ids[i] = string(channel.Type) + ":" + reference
	}
	recipients := update.Recipients
	if recipients == nil {
		recipients = []string{}
	}
	const onlyCopies = `cardinality($3::text[]) > 0 AND (SELECT bool_and(r = ANY(COALESCE(m.cc,'{}'))) FROM unnest($3::text[]) r)`
	if _, err := tx.Exec(ctx, `UPDATE channel_outbox o SET last_error=concat_ws('; ',o.last_error,'Bounced from copied address: '||array_to_string($3::text[],', ')),
			updated_at=now()
		FROM messages m WHERE o.workspace_id=$1 AND m.workspace_id=o.workspace_id AND m.id=o.message_id
		AND m.kind='outgoing' AND m.external_id=ANY($2) AND o.status<>'failed' AND `+onlyCopies, channel.WorkspaceID, ids, recipients); err != nil {
		return fmt.Errorf("record copy bounce: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE channel_outbox o SET status='failed',last_error=$4,last_error_code='bounced',locked_until=NULL,claim_token=NULL,updated_at=now()
		FROM messages m WHERE o.workspace_id=$1 AND m.workspace_id=o.workspace_id AND m.id=o.message_id
		AND m.kind='outgoing' AND m.external_id=ANY($2) AND NOT (`+onlyCopies+`)`, channel.WorkspaceID, ids, recipients, update.Reason); err != nil {
		return fmt.Errorf("record bounce: %w", err)
	}
	return nil
}

// journalDetail is what a journal row records of an outcome.
type journalDetail struct {
	senderKey, holdReason, dropReason, aggregateKey string
	conversationID, messageID, objectKey            string
	objectBytes                                     int64
	flushAt                                         *time.Time
}

// journalWriter writes the outcome of a message to the journal: a new row for a Sync type, the rows
// it came from otherwise.
type journalWriter struct {
	tx      pgx.Tx
	channel channels.Channel
	request IngestRequest
	now     time.Time
	eventID string
}

func (j *journalWriter) write(ctx context.Context, state string, d journalDetail) error {
	var objectBytes *int64
	if d.objectBytes > 0 {
		objectBytes = &d.objectBytes
	}
	var processedAt *time.Time
	if state == "processed" || state == "dropped" {
		processedAt = &j.now
	}
	if len(j.request.EventIDs) > 0 {
		_, err := j.tx.Exec(ctx, `UPDATE channel_inbound_events SET state=$3,hold_reason=NULLIF($4,''),drop_reason=NULLIF($5,''),
			aggregate_key=COALESCE(NULLIF($6,''),aggregate_key),flush_at=$7,sender_key=COALESCE(NULLIF($8,''),sender_key),
			conversation_id=NULLIF($9,'')::uuid,message_id=NULLIF($10,'')::uuid,processed_at=$11,error=NULL,
			raw_object_key=COALESCE(raw_object_key,NULLIF($12,'')),raw_bytes=COALESCE(raw_bytes,$13),
			decided_by_agent_id=COALESCE(NULLIF($14,'')::uuid,decided_by_agent_id),attempts=attempts+1
			WHERE workspace_id=$1 AND id::text=ANY($2)`,
			j.channel.WorkspaceID, j.request.EventIDs, state, d.holdReason, d.dropReason, d.aggregateKey, d.flushAt, d.senderKey,
			d.conversationID, d.messageID, processedAt, d.objectKey, objectBytes, j.request.DecidedBy)
		if err != nil {
			return fmt.Errorf("update channel journal: %w", err)
		}
		j.eventID = j.request.EventIDs[0]
		return nil
	}
	raw := j.request.Raw.JSON
	if len(j.request.Inbound.Summary) > 0 {
		raw = j.request.Inbound.Summary
	}
	if raw == nil && d.objectKey == "" {
		raw = json.RawMessage(`{}`)
	}
	err := j.tx.QueryRow(ctx, `INSERT INTO channel_inbound_events(workspace_id,channel_id,external_event_id,state,raw,raw_object_key,raw_bytes,sender_key,
			hold_reason,drop_reason,aggregate_key,flush_at,conversation_id,message_id,attempts,received_at,processed_at)
		VALUES($1,$2,NULLIF($3,''),$4,$5,NULLIF($6,''),$7,NULLIF($8,''),NULLIF($9,''),NULLIF($10,''),NULLIF($11,''),$12,NULLIF($13,'')::uuid,NULLIF($14,'')::uuid,1,$15,$16)
		RETURNING id::text`,
		j.channel.WorkspaceID, j.channel.ID, j.request.Raw.ExternalEventID, state, raw, d.objectKey, objectBytes, d.senderKey,
		d.holdReason, d.dropReason, d.aggregateKey, d.flushAt, d.conversationID, d.messageID, j.now, processedAt).Scan(&j.eventID)
	if err != nil {
		return fmt.Errorf("write channel journal: %w", err)
	}
	return nil
}
