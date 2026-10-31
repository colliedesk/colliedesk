package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/intake/rules"
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
	// Intake applies a decision of a gathering: EventIDs are all its held events, replayed as one.
	Intake *IntakeApply
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
	// Shared with every other message of the channel, exclusive of a removal: a channel is never removed
	// between the moment a message is held on it and the moment it is counted (removeChannel).
	if _, err := tx.Exec(ctx, `SELECT 1 FROM channels WHERE workspace_id=$1 AND id=$2::uuid FOR SHARE`, workspaceID, request.ChannelID); err != nil {
		return result, fmt.Errorf("lock channel: %w", err)
	}
	channel, err := s.channelByID(ctx, tx, workspaceID, request.ChannelID)
	if err != nil {
		return result, err
	}
	adapter, ok := s.channels.Adapter(channel.Type)
	if !ok {
		return result, ErrChannelHasNoAdapter
	}
	definition := adapter.Definition()
	// The message follows its scope's rules; everything below reads them from the channel.
	var scope channels.Scope
	channel.Settings, scope = channel.Settings.ForScope(in.Scope)
	if scope.Gather != nil {
		quiet, most := time.Duration(scope.Gather.QuietSeconds)*time.Second, time.Duration(scope.Gather.MaxSeconds)*time.Second
		switch {
		case in.Aggregate != nil:
			in.Aggregate.Quiet, in.Aggregate.Max = quiet, most
		case len(in.Thread.Keys) > 0:
			in.Aggregate = &channels.Aggregate{Key: in.Thread.Keys[0], Quiet: quiet, Max: most}
		}
	}
	// Where several customers write, each one's messages are collected apart: one customer's batch is
	// never another's.
	shared := slices.Contains(definition.SharedScopes, in.Scope)
	if shared && in.Aggregate != nil {
		in.Aggregate.Key = clip(in.Aggregate.Key+"|"+in.Sender.Identity.Key(), 500)
	}
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
		// A batch listed by another worker as well may have been asked about since: it waits for its time.
		// One that took a message since it was listed (a paused channel resumed) is left to the next round,
		// which lists it whole.
		if request.Flushing {
			var due bool
			if err := tx.QueryRow(ctx, `SELECT COALESCE(bool_and(e.state='pending') AND max(e.flush_at)<=$3,false)
				AND NOT EXISTS(SELECT 1 FROM channel_inbound_events o WHERE o.workspace_id=$1 AND o.channel_id=$4 AND o.state='pending'
					AND o.aggregate_key=(SELECT aggregate_key FROM channel_inbound_events WHERE workspace_id=$1 AND id::text=$5) AND NOT (o.id::text=ANY($2)))
				FROM channel_inbound_events e WHERE e.workspace_id=$1 AND e.id::text=ANY($2)`, workspaceID, request.EventIDs, now, channel.ID, request.EventIDs[0]).Scan(&due); err != nil {
				return result, fmt.Errorf("check collected messages are due: %w", err)
			}
			if !due {
				result.Status = ResultSkipped
				return result, nil
			}
		}
	}
	// A decision of a gathering applies only while it is undecided and holds exactly these events.
	var pending pendingGathering
	if request.Intake != nil {
		if pending, err = lockGathering(ctx, tx, request.WorkspaceID, request.Intake.GatheringID, request.EventIDs); err != nil {
			return result, err
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
	senderKey := in.SenderKey()
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
		// Kept without a conversation: a team member on the channel's own app, or chatter.
		kept := itemOutcome{disposition: ItemArchived, reason: "chat"}
		if in.Role == channels.RoleStaff {
			kept = itemOutcome{disposition: ItemStaff}
		}
		if err := recordThreadItems(ctx, tx, &journal, in, kept); err != nil {
			return result, err
		}
		result.Status = channels.ResultArchived
		return finish(result)
	}
	// A batch's command is the one its messages were collected with (the journal's), not whichever of its
	// messages last looked like one: an OK written before the question is part of the request.
	if request.Flushing && len(request.EventIDs) > 0 && in.Intent.Kind == channels.IntentCommand {
		var named *string
		if err := tx.QueryRow(ctx, `SELECT command FROM channel_inbound_events WHERE workspace_id=$1 AND id::text=ANY($2) AND command IS NOT NULL
			ORDER BY received_at,id LIMIT 1`, workspaceID, request.EventIDs).Scan(&named); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return result, fmt.Errorf("read the command of collected messages: %w", err)
		}
		switch {
		case named == nil:
			in.Intent = channels.Intent{}
		case *named != in.Intent.Command:
			in.Intent = channels.Intent{Kind: channels.IntentCommand, Command: *named}
		}
	}
	command, commanded := channel.Settings.Commands[in.Intent.Command]
	commanded = commanded && in.Intent.Kind == channels.IntentCommand
	// A team member writing in a chat place on the channel's own app is context of the place: kept, never
	// a batch of a customer's, unless they name a command that opens a request.
	if in.Role == channels.RoleStaff && !(commanded && (command.Action == "" || command.Action == channels.ActionOpen)) {
		if err := journal.write(ctx, "processed", journalDetail{senderKey: senderKey}); err != nil {
			return result, err
		}
		if err := recordThreadItems(ctx, tx, &journal, in, itemOutcome{disposition: ItemStaff}); err != nil {
			return result, err
		}
		result.Status = channels.ResultArchived
		return finish(result)
	}
	// The message's own time: a journal processed late (a paused channel resumed) keeps the clocks of when
	// its messages came.
	received := now
	if in.Aggregate != nil && len(request.EventIDs) > 0 && !request.Flushing {
		if err := tx.QueryRow(ctx, `SELECT received_at FROM channel_inbound_events WHERE workspace_id=$1 AND id::text=$2`,
			workspaceID, request.EventIDs[0]).Scan(&received); err != nil {
			return result, fmt.Errorf("read when the message came: %w", err)
		}
		received = received.UTC()
	}
	// A request left waiting for its description past ExpireSeconds is thrown away by its sender's next
	// message, which is told so unless it starts a new request.
	if in.Aggregate != nil && len(request.EventIDs) > 0 && !request.Flushing && !request.Replay && scope.Gather != nil && scope.Gather.ExpireSeconds > 0 {
		var expired bool
		if err := tx.QueryRow(ctx, `WITH stale AS (
				SELECT id FROM channel_inbound_events WHERE workspace_id=$1 AND channel_id=$2 AND state='pending' AND aggregate_key=$3
					AND NOT (id::text=ANY($4))
					AND EXISTS(SELECT 1 FROM channel_inbound_events d WHERE d.workspace_id=$1 AND d.channel_id=$2 AND d.state='pending' AND d.aggregate_key=$3 AND d.description_asked_at IS NOT NULL)
					AND (SELECT max(received_at) FROM channel_inbound_events m WHERE m.workspace_id=$1 AND m.channel_id=$2 AND m.state='pending' AND m.aggregate_key=$3
						AND NOT (m.id::text=ANY($4))) < $5),
			expired AS (UPDATE channel_inbound_events e SET state='dropped',drop_reason=$6,flush_at=NULL,processed_at=$7 FROM stale WHERE e.workspace_id=$1 AND e.id=stale.id RETURNING e.id),
			items AS (UPDATE channel_thread_items SET disposition='not_request',disposition_reason=$6 WHERE workspace_id=$1 AND inbound_event_id IN (SELECT id FROM expired) RETURNING 1)
			SELECT EXISTS(SELECT 1 FROM expired)`,
			workspaceID, channel.ID, in.Aggregate.Key, request.EventIDs, received.Add(-time.Duration(scope.Gather.ExpireSeconds)*time.Second), channels.DropExpired, now).Scan(&expired); err != nil {
			return result, fmt.Errorf("expire a request: %w", err)
		}
		if expired && !(commanded && (command.Action == "" || command.Action == channels.ActionOpen)) {
			if err := journal.write(ctx, "dropped", journalDetail{dropReason: channels.DropExpired}); err != nil {
				return result, err
			}
			if err := recordThreadItems(ctx, tx, &journal, in, itemOutcome{disposition: ItemNotRequest, reason: channels.DropExpired}); err != nil {
				return result, err
			}
			if err := queueThreadNotice(ctx, tx, &journal, journal.eventID, channels.NoticeExpired, channels.RenderNotice(channel.Settings.Notices.Expired, nil), 0); err != nil {
				return result, err
			}
			reason := channels.DropExpired
			result.Status, result.Reason = channels.ResultDropped, &reason
			return finish(result)
		}
	}
	if commanded && command.Action == channels.ActionConfirm {
		// Where a request being written is asked about, a confirm sends it now; the confirm itself is no part
		// of it. Before the question, or with nothing being written, it is an ordinary message.
		var sent bool
		asks := scope.Gather != nil && scope.Gather.ConfirmSeconds > 0 && channel.Settings.Notices.AskConfirm != ""
		if in.Aggregate != nil {
			tag, err := tx.Exec(ctx, `UPDATE channel_inbound_events SET flush_at=$5 WHERE workspace_id=$1 AND channel_id=$2 AND state='pending'
				AND aggregate_key=$3 AND NOT (id::text=ANY(COALESCE($4::text[],'{}')))
				AND EXISTS(SELECT 1 FROM channel_inbound_events c WHERE c.workspace_id=$1 AND c.channel_id=$2 AND c.state='pending' AND c.aggregate_key=$3 AND c.command IS NOT NULL)
				AND (NOT $6 OR EXISTS(SELECT 1 FROM channel_inbound_events q WHERE q.workspace_id=$1 AND q.channel_id=$2 AND q.state='pending' AND q.aggregate_key=$3 AND q.confirm_asked_at IS NOT NULL))`,
				workspaceID, channel.ID, in.Aggregate.Key, request.EventIDs, now, asks)
			if err != nil {
				return result, fmt.Errorf("send a confirmed request: %w", err)
			}
			sent = tag.RowsAffected() > 0
		}
		if sent {
			if err := journal.write(ctx, "dropped", journalDetail{dropReason: channels.DropConfirmed}); err != nil {
				return result, err
			}
			if err := recordThreadItems(ctx, tx, &journal, in, itemOutcome{disposition: ItemNotRequest, reason: channels.DropConfirmed}); err != nil {
				return result, err
			}
			reason := channels.DropConfirmed
			result.Status, result.Reason = channels.ResultDropped, &reason
			return finish(result)
		}
		in.Intent, commanded, command = channels.Intent{}, false, channels.Command{}
	}
	if commanded && command.Action == channels.ActionCancel {
		// Where messages are collected, a cancel command cancels only what its sender is collecting with a
		// command in it; without one it is an ordinary message. Where they are not, it is dropped.
		batch := true
		if in.Aggregate != nil {
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM channel_inbound_events WHERE workspace_id=$1 AND channel_id=$2 AND state='pending'
				AND aggregate_key=$3 AND command IS NOT NULL AND NOT (id::text=ANY(COALESCE($4::text[],'{}'))))`,
				workspaceID, channel.ID, in.Aggregate.Key, request.EventIDs).Scan(&batch); err != nil {
				return result, fmt.Errorf("find a commanded batch: %w", err)
			}
		}
		if batch {
			in.Intent.Kind = channels.IntentCancel
		} else {
			in.Intent, commanded, command = channels.Intent{}, false, channels.Command{}
		}
	}
	if in.Intent.Kind == channels.IntentCancel {
		decision := rules.Decide(rules.Facts{Cancel: true})
		if in.Aggregate != nil {
			// Cancelling throws away what was being collected with it.
			if _, err := tx.Exec(ctx, `WITH cancelled AS (
					UPDATE channel_inbound_events SET state='dropped',drop_reason=$4,flush_at=NULL,processed_at=$5
					WHERE workspace_id=$1 AND channel_id=$2 AND state='pending' AND aggregate_key=$3 RETURNING id)
				UPDATE channel_thread_items SET disposition='not_request',disposition_reason=$4
				WHERE workspace_id=$1 AND inbound_event_id IN (SELECT id FROM cancelled)`,
				workspaceID, channel.ID, in.Aggregate.Key, channels.DropCancelled, now); err != nil {
				return result, fmt.Errorf("cancel collected messages: %w", err)
			}
		}
		if err := journal.write(ctx, "dropped", journalDetail{dropReason: channels.DropCancelled, basis: decision.Basis, rule: decision.Rule}); err != nil {
			return result, err
		}
		if err := recordThreadItems(ctx, tx, &journal, in, itemOutcome{disposition: ItemNotRequest, reason: channels.DropCancelled}); err != nil {
			return result, err
		}
		if in.Aggregate != nil && channel.Settings.Notices.Cancelled != "" {
			if err := queueThreadNotice(ctx, tx, &journal, journal.eventID, channels.NoticeCancelled, channels.RenderNotice(channel.Settings.Notices.Cancelled, nil), 0); err != nil {
				return result, err
			}
		}
		reason := channels.DropCancelled
		result.Status, result.Reason = channels.ResultDropped, &reason
		return finish(result)
	}
	if limits.PerSender > 0 {
		var recent int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM channel_inbound_events WHERE workspace_id=$1 AND channel_id=$2 AND sender_key=$3
			AND received_at>$4 AND (state='held' OR (state='processed' AND message_id IS NOT NULL)) AND NOT (id::text=ANY(COALESCE($5::text[],'{}')))`,
			workspaceID, channel.ID, senderKey, now.Add(-limits.Window()), request.EventIDs).Scan(&recent); err != nil {
			return result, fmt.Errorf("check inbound rate: %w", err)
		}
		if recent >= limits.PerSender {
			return result, ErrRateLimited
		}
	}

	if off {
		return s.holdInbound(ctx, tx, &journal, in, channels.HoldChannelOff, rules.Decision{}, finish)
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
		if commanded && command.Action != channels.ActionReopen && scope.Cuts() {
			// A command starts a new request: what was collected before it is sent on its own (or kept
			// for review), and collecting starts again with this message. What the sender said just before
			// it stays with it, unless what was collected named a command of its own.
			var keepSince *time.Time
			if scope.Gather != nil && scope.Gather.PreCommandSeconds > 0 {
				since := now.Add(-time.Duration(scope.Gather.PreCommandSeconds) * time.Second)
				keepSince = &since
			}
			if _, err := tx.Exec(ctx, `UPDATE channel_inbound_events SET aggregate_key=left($3||'#'||$5,500),flush_at=$4
				WHERE workspace_id=$1 AND channel_id=$2 AND state='pending' AND aggregate_key=$3
					AND ($6::timestamptz IS NULL OR received_at<$6 OR EXISTS(SELECT 1 FROM channel_inbound_events c WHERE c.workspace_id=$1 AND c.channel_id=$2 AND c.state='pending'
						AND c.aggregate_key=$3 AND c.command IS NOT NULL))`,
				workspaceID, channel.ID, in.Aggregate.Key, now, request.EventIDs[0], keepSince); err != nil {
				return result, fmt.Errorf("close collected messages: %w", err)
			}
		}
		// Due when the sender has been quiet for Quiet, or Max after the first message, or now when it confirms.
		var first *time.Time
		if err := tx.QueryRow(ctx, `SELECT min(received_at) FROM channel_inbound_events WHERE workspace_id=$1 AND channel_id=$2
			AND state='pending' AND aggregate_key=$3`, workspaceID, channel.ID, in.Aggregate.Key).Scan(&first); err != nil {
			return result, fmt.Errorf("load collected messages: %w", err)
		}
		flush := received.Add(in.Aggregate.Quiet)
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
		named := ""
		if commanded {
			named = in.Intent.Command
		}
		if err := journal.write(ctx, "pending", journalDetail{senderKey: senderKey, aggregateKey: in.Aggregate.Key, flushAt: &flush, command: named}); err != nil {
			return result, err
		}
		if err := recordThreadItems(ctx, tx, &journal, in, itemOutcome{disposition: ItemGathering}); err != nil {
			return result, err
		}
		result.Status = channels.ResultPending
		return finish(result)
	}

	// A request written over several messages, where the scope asks before it opens: once the customer is
	// quiet the channel asks, and the batch waits ConfirmSeconds more (a confirm, or more messages, end the
	// wait); a batch asked about already opens. More messages since the question ask it again.
	// A command alone (no words after it, no file) asks for a description instead, and waits for one.
	if request.Flushing && commanded && command.AskDescription != "" && len(in.Files) == 0 && len(request.EventIDs) > 0 {
		if _, _, rest, ok := channels.MatchCommand(in.Text, channel.Settings.Commands); ok && strings.TrimSpace(rest) == "" && !strings.Contains(strings.TrimSpace(in.Text), "\n") {
			var asked bool
			if err := tx.QueryRow(ctx, `SELECT bool_or(description_asked_at IS NOT NULL) FROM channel_inbound_events WHERE workspace_id=$1 AND id::text=ANY($2)`,
				workspaceID, request.EventIDs).Scan(&asked); err != nil {
				return result, fmt.Errorf("read the description asked of a request: %w", err)
			}
			if !asked {
				if err := queueThreadNotice(ctx, tx, &journal, request.EventIDs[len(request.EventIDs)-1], channels.NoticeAskDescription, channels.RenderNotice(command.AskDescription, nil), 0); err != nil {
					return result, err
				}
			}
			if _, err := tx.Exec(ctx, `UPDATE channel_inbound_events SET description_asked_at=COALESCE(description_asked_at,$3),flush_at='infinity' WHERE workspace_id=$1 AND id::text=ANY($2)`,
				workspaceID, request.EventIDs, now); err != nil {
				return result, fmt.Errorf("wait for a description: %w", err)
			}
			result.Status = channels.ResultPending
			return finish(result)
		}
	}
	if request.Flushing && commanded && scope.Gather != nil && scope.Gather.ConfirmSeconds > 0 && channel.Settings.Notices.AskConfirm != "" && len(request.EventIDs) > 0 {
		// asked: every message was asked about; or the customer went quiet longer than the question would
		// have waited (a journal processed late): it opens without one.
		var asked bool
		if err := tx.QueryRow(ctx, `SELECT bool_and(confirm_asked_at IS NOT NULL) OR max(received_at)<=$3 FROM channel_inbound_events WHERE workspace_id=$1 AND id::text=ANY($2)`,
			workspaceID, request.EventIDs, now.Add(-time.Duration(scope.Gather.QuietSeconds+scope.Gather.ConfirmSeconds)*time.Second)).Scan(&asked); err != nil {
			return result, fmt.Errorf("read the question of a request: %w", err)
		}
		if !asked {
			newest := request.EventIDs[len(request.EventIDs)-1]
			if err := queueThreadNotice(ctx, tx, &journal, newest, channels.NoticeAskConfirm, channels.RenderNotice(channel.Settings.Notices.AskConfirm, nil), 0); err != nil {
				return result, err
			}
			if _, err := tx.Exec(ctx, `UPDATE channel_inbound_events SET confirm_asked_at=$3,flush_at=$4 WHERE workspace_id=$1 AND id::text=ANY($2)`,
				workspaceID, request.EventIDs, now, now.Add(time.Duration(scope.Gather.ConfirmSeconds)*time.Second)); err != nil {
				return result, fmt.Errorf("wait for a confirmation: %w", err)
			}
			result.Status = channels.ResultPending
			return finish(result)
		}
	}
	// The channel's rules decide what the message is (intake/rules). Facts are read in the order the
	// rules use them, so chatter the scope keeps without a conversation never creates a contact.
	facts := rules.Facts{Commanded: commanded, CommandAction: command.Action, IntoConversation: request.IntoConversation, Replay: request.Replay,
		CommandsOnly: scope.Mode == channels.ModeCommands, Flushing: request.Flushing, Review: channel.Settings.Review,
		JoinOpen: scope.Unmatched == channels.UnmatchedJoinOpen, Notice: scope.Unmatched == channels.UnmatchedNotice}
	// A batch of a chat's intake scope: AI may decide where the rules only have a default (auto).
	level, err := intakeLevel(ctx, tx, channel, definition, in, request)
	if err != nil {
		return result, err
	}
	if facts.CommandsOnly && !commanded && !request.Replay && request.IntoConversation == "" {
		if facts.Joins, err = s.joinsWithoutCommand(ctx, tx, channel, in, request, shared, senderKey, facts.JoinOpen, now); err != nil {
			return result, err
		}
		if facts.JoinOpen && !facts.Joins {
			if facts.OpenConversation, err = onlyOpenConversation(ctx, tx, channel, in.Sender); err != nil {
				return result, err
			}
		}
	}
	// What the rules decide before the thread is read: only keeping it or holding it for a person.
	switch decision := rules.Decide(facts); decision.Action {
	case rules.Archive:
		if level == intakeAuto {
			return s.holdForIntake(ctx, tx, &journal, in, decision, finish)
		}
		// Chatter of a scope that opens conversations only on a command is kept, not opened. A message a
		// join-open scope could join nowhere stays unresolved in its chat, where it is seen and counted.
		if err := journal.write(ctx, "processed", journalDetail{senderKey: senderKey, basis: decision.Basis, rule: decision.Rule}); err != nil {
			return result, err
		}
		kept := itemOutcome{disposition: ItemArchived, reason: "chat"}
		switch decision.Rule {
		case rules.UnmatchedKept:
			kept = itemOutcome{disposition: ItemUnresolved, reason: "unmatched"}
		case rules.UnmatchedNotice:
			kept = itemOutcome{disposition: ItemArchived, reason: "unmatched"}
		}
		if err := recordThreadItems(ctx, tx, &journal, in, kept); err != nil {
			return result, err
		}
		if decision.Rule == rules.UnmatchedNotice && channel.Settings.Notices.Unmatched != "" {
			every := channels.DefaultUnmatchedEverySeconds
			if channel.Settings.Notices.UnmatchedEverySeconds != nil {
				every = *channel.Settings.Notices.UnmatchedEverySeconds
			}
			if err := queueThreadNotice(ctx, tx, &journal, journal.eventID, channels.NoticeUnmatched, channels.RenderNotice(channel.Settings.Notices.Unmatched, nil),
				time.Duration(every)*time.Second); err != nil {
				return result, err
			}
		}
		if level == intakeSuggest {
			if _, err := recordGathering(ctx, tx, &journal, in, gathering{state: GatheringApplied, decision: decision, decidedByRules: true}); err != nil {
				return result, err
			}
		}
		result.Status = channels.ResultArchived
		return finish(result)
	case rules.Review:
		if level == intakeAuto {
			return s.holdForIntake(ctx, tx, &journal, in, decision, finish)
		}
		// Collected messages that named no command wait for a person to decide what they are.
		if level == "" {
			return s.holdInbound(ctx, tx, &journal, in, channels.HoldReview, decision, finish)
		}
		var held channels.IngestResult
		if _, err := s.holdInbound(ctx, tx, &journal, in, channels.HoldReview, decision, func(r channels.IngestResult) (channels.IngestResult, error) {
			held = r
			return r, nil
		}); err != nil {
			return held, err
		}
		id, err := recordGathering(ctx, tx, &journal, in, gathering{state: GatheringReview, reviewReason: ReviewNoCommand, decision: decision})
		if err != nil {
			return held, err
		}
		// A person decides; at suggest AI says what it would.
		if level == intakeSuggest {
			if err := enqueueIntake(ctx, tx, workspaceID, id, now); err != nil {
				return held, err
			}
		}
		return finish(held)
	}
	// At auto, what the rules decide only by default waits for intake, and nothing is created for it
	// yet: the contact is found once the message is stored. A reopen command names the sender's own
	// conversation, which needs the contact first; it is a match, never held.
	reopening := request.IntoConversation == "" && commanded && command.Action == channels.ActionReopen && in.Intent.Ref != ""
	var contactID string
	if level != intakeAuto || reopening {
		if contactID, err = s.contactForIdentity(ctx, tx, channel, in.Sender); err != nil {
			return result, err
		}
	}
	if request.IntoConversation != "" {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM conversations WHERE workspace_id=$1 AND id=$2::uuid)`, workspaceID, request.IntoConversation).Scan(&exists); err != nil {
			return result, fmt.Errorf("find conversation: %w", err)
		}
		if !exists {
			return result, pgx.ErrNoRows
		}
		// A card joins only a conversation it offers, so its reply never goes to someone else.
		if request.Intake != nil && request.Intake.GatheringID != "" {
			may, err := gatheringMayJoin(ctx, tx, workspaceID, request.Intake.GatheringID, request.IntoConversation)
			if err != nil {
				return result, err
			}
			if !may {
				return result, ErrNotACandidate
			}
		}
	} else {
		if commanded && command.Action == channels.ActionReopen && in.Intent.Ref != "" {
			err := tx.QueryRow(ctx, `SELECT id::text FROM conversations WHERE workspace_id=$1 AND id::text=$2 AND contact_id=$3`,
				workspaceID, in.Intent.Ref, contactID).Scan(&facts.RefConversation)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return result, fmt.Errorf("find conversation to reopen: %w", err)
			}
		}
		// A gathering decided to be a new request opens one, whatever conversation its thread found since.
		if facts.RefConversation == "" && (request.Intake == nil || !request.Intake.ForceNew) {
			if facts.Thread, err = findChannelThread(ctx, tx, channel, in.Thread, shared, senderKey, now); err != nil {
				return result, err
			}
		}
	}
	decision := rules.Decide(facts)
	if request.Intake != nil {
		decision = request.Intake.basisOf(decision, pending)
	}
	if level == intakeAuto && decision.Basis == rules.Default {
		return s.holdForIntake(ctx, tx, &journal, in, decision, finish)
	}
	if contactID == "" {
		if contactID, err = s.contactForIdentity(ctx, tx, channel, in.Sender); err != nil {
			return result, err
		}
	}
	conversationID := decision.ConversationID
	// What intake said the batch adds to a conversation it joins; the rules never say.
	replyKind := ""
	if request.Intake != nil && conversationID != "" {
		replyKind = request.Intake.ReplyKind
	}
	var files []EmailFile
	if files, err = s.putInboundFiles(ctx, workspaceID, in.Files); err != nil {
		return result, err
	}
	result.Status = channels.ResultThreaded
	if conversationID == "" {
		// The ceiling guards against a flood of new mail; opening held mail is an admin's decision
		// about mail that already arrived, and holding it counted against nothing.
		if limits.NewConversations > 0 && !request.Replay {
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
		// An acknowledgement keeps a resolved conversation resolved, and only then is the message marked as
		// one: joined to an open conversation it kept nothing, and a closed one opens so it is seen.
		var status string
		if err := tx.QueryRow(ctx, `SELECT status FROM conversations WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, workspaceID, conversationID).Scan(&status); err != nil {
			return result, fmt.Errorf("read conversation status: %w", err)
		}
		if replyKind == ReplyAcknowledgement && status != "resolved" {
			replyKind = ReplySubstantive
		}
		if _, err := tx.Exec(ctx, `UPDATE conversations SET status=CASE WHEN status IN ('resolved','closed') AND NOT $3 THEN 'open' ELSE status END,
			updated_at=now() WHERE workspace_id=$1 AND id=$2`, workspaceID, conversationID, replyKind == ReplyAcknowledgement); err != nil {
			return result, fmt.Errorf("reopen conversation: %w", err)
		}
	}
	messageID, err := insertChannelMessage(ctx, tx, channel, conversationID, externalID, replyKind, in)
	if err != nil {
		return result, err
	}
	// A key names the message that recorded it, so moving the message moves the key.
	for _, key := range in.Thread.Record {
		if _, err := tx.Exec(ctx, `INSERT INTO conversation_thread_keys(workspace_id,conversation_id,channel_type,key,message_id) VALUES($1,$2,$3,$4,$5)
			ON CONFLICT DO NOTHING`, workspaceID, conversationID, string(channel.Type), key, messageID); err != nil {
			return result, fmt.Errorf("record thread key: %w", err)
		}
	}
	original, err := linkChannelFiles(ctx, tx, s.objects, workspaceID, messageID, files, in.EmailMeta != nil)
	if err != nil {
		return result, err
	}
	if len(request.EventIDs) > 0 && len(files) > 0 {
		// The fetched files are attachments now, which count them.
		keys := make([]string, len(files))
		for i, file := range files {
			keys[i] = file.Object.Key
		}
		if _, err := tx.Exec(ctx, `UPDATE channel_inbound_files SET attached=true,updated_at=now() WHERE workspace_id=$1 AND event_id::text=ANY($2)
			AND status='stored' AND storage_key=ANY($3)`, workspaceID, request.EventIDs, keys); err != nil {
			return result, fmt.Errorf("mark fetched files: %w", err)
		}
	}
	if err := journal.write(ctx, "processed", journalDetail{senderKey: senderKey, conversationID: conversationID, messageID: messageID,
		objectKey: original, basis: decision.Basis, rule: decision.Rule}); err != nil {
		return result, err
	}
	link := linkReasonOf(decision)
	if request.Intake != nil && request.Intake.Decider != DecidedByRules {
		link = request.Intake.Decider
	}
	if err := recordThreadItems(ctx, tx, &journal, in, itemOutcome{disposition: ItemConversation, contactID: contactID, conversationID: conversationID,
		messageID: messageID, linkReason: link}); err != nil {
		return result, err
	}
	if request.Intake != nil {
		state := GatheringAttached
		if result.Status == channels.ResultCreated {
			state = GatheringCreated
		}
		if err := concludeGathering(ctx, tx, workspaceID, *request.Intake, pending, state, conversationID, contactID, now); err != nil {
			return result, err
		}
	}
	if level != "" {
		decided := gathering{state: GatheringAttached, decision: decision, conversationID: conversationID, contactID: contactID, decidedByRules: true}
		switch {
		case level == intakeSuggest && decision.Basis == rules.Default:
			// The rules applied; AI only suggests what it would have done.
			decided.state = GatheringApplied
		case result.Status == channels.ResultCreated:
			decided.state = GatheringCreated
		}
		id, err := recordGathering(ctx, tx, &journal, in, decided)
		if err != nil {
			return result, err
		}
		if decided.state == GatheringApplied {
			if err := enqueueIntake(ctx, tx, workspaceID, id, now); err != nil {
				return result, err
			}
		}
	}
	if result.Status == channels.ResultCreated && channel.Settings.Notices.Received != "" {
		if _, err := s.createMessage(ctx, tx, workspaceID, conversationID, Actor{NoticeKind: channels.NoticeReceived}, "outgoing", channel.Settings.Notices.Received, nil, nil); err != nil {
			return result, fmt.Errorf("send received notice: %w", err)
		}
	}
	result.ConversationID, result.MessageID = &conversationID, &messageID
	return finish(result)
}

// holdInbound keeps a message that arrived while its channel was off, or that the rules hold for a
// person (decision): its original is stored and counts toward storage while it waits for a person to
// open or discard it.
func (s *Store) holdInbound(ctx context.Context, tx pgx.Tx, journal *journalWriter, in channels.Inbound, reason string, decision rules.Decision,
	finish func(channels.IngestResult) (channels.IngestResult, error)) (channels.IngestResult, error) {
	var result channels.IngestResult
	detail := journalDetail{senderKey: in.SenderKey(), holdReason: reason, basis: decision.Basis, rule: decision.Rule}
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
	// What waits for a person: a review of what was collected, or a channel that is off.
	waiting := itemOutcome{disposition: ItemPendingReview, reason: "no_command"}
	switch reason {
	case channels.HoldChannelOff:
		waiting = itemOutcome{disposition: ItemUnresolved, reason: reason}
	case channels.HoldIntake:
		waiting = itemOutcome{disposition: ItemGathering, reason: "deciding"}
	}
	if err := recordThreadItems(ctx, tx, journal, in, waiting); err != nil {
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

// knownContact is the contact a sender's identity on a channel already belongs to (the one it was merged
// into); pgx.ErrNoRows when the identity is new. It creates nothing.
func knownContact(ctx context.Context, tx pgx.Tx, workspaceID, channelID, kind, value string) (string, error) {
	var contactID string
	err := tx.QueryRow(ctx, `SELECT COALESCE(c.merged_into_id,c.id)::text FROM contact_identities i
		JOIN contacts c ON c.workspace_id=i.workspace_id AND c.id=i.contact_id
		WHERE i.workspace_id=$1 AND i.channel_id=$2 AND i.kind=$3 AND i.value=$4`, workspaceID, channelID, kind, value).Scan(&contactID)
	return contactID, err
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
	contactID, err := knownContact(ctx, tx, channel.WorkspaceID, channel.ID, string(identity.Kind), identity.Value)
	if err == nil {
		if _, err := tx.Exec(ctx, `UPDATE contact_identities SET display_name=NULLIF($5,''),updated_at=now()
			WHERE workspace_id=$1 AND channel_id=$2 AND kind=$3 AND value=$4 AND display_name IS DISTINCT FROM NULLIF($5,'')`,
			channel.WorkspaceID, channel.ID, identity.Kind, identity.Value, sender.Name); err != nil {
			return "", fmt.Errorf("update contact identity: %w", err)
		}
		// A contact still called by its id (made before the channel knew the name) takes the name now.
		if sender.Name != "" {
			if _, err := tx.Exec(ctx, `UPDATE contacts SET name=$3 WHERE workspace_id=$1 AND id=$2 AND name=$4`,
				channel.WorkspaceID, contactID, sender.Name, identity.Value); err != nil {
				return "", fmt.Errorf("name contact: %w", err)
			}
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
// then a thread key, as the channel's join policy allows. In a scope several customers share (shared),
// a reference to another sender's message (senderKey of its journal row) is no evidence of which
// request the message belongs to, and says so instead of matching.
func findChannelThread(ctx context.Context, tx pgx.Tx, channel channels.Channel, thread channels.Thread, shared bool, senderKey string, now time.Time) (rules.Thread, error) {
	var found rules.Thread
	policy := channel.Settings.JoinPolicy
	if policy.Kind == channels.JoinNever {
		return found, nil
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
		// Who sent a referenced message is read only where several customers share the scope.
		rows, err := tx.Query(ctx, `SELECT m.external_id,c.id::text,c.status,c.updated_at,
				m.kind='incoming' AND $3 AND COALESCE((SELECT e.sender_key FROM channel_inbound_events e
					WHERE e.workspace_id=m.workspace_id AND e.message_id=m.id LIMIT 1) <> $4,false)
			FROM messages m JOIN conversations c ON c.workspace_id=m.workspace_id AND c.id=m.conversation_id
			WHERE m.workspace_id=$1 AND m.external_id=ANY($2) AND m.detached_at IS NULL`, channel.WorkspaceID, candidates, shared, senderKey)
		if err != nil {
			return found, fmt.Errorf("match thread references: %w", err)
		}
		type match struct {
			conversationID, status string
			updatedAt              time.Time
			other                  bool
		}
		matches := map[string]match{}
		for rows.Next() {
			var id string
			var m match
			if err := rows.Scan(&id, &m.conversationID, &m.status, &m.updatedAt, &m.other); err != nil {
				rows.Close()
				return found, fmt.Errorf("scan thread reference: %w", err)
			}
			matches[id] = m
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return found, fmt.Errorf("iterate thread references: %w", err)
		}
		for _, candidate := range candidates {
			m, ok := matches[candidate]
			switch {
			case !ok:
			case m.other:
				return rules.Thread{OtherCustomer: true}, nil
			case joins(m.status, m.updatedAt):
				return rules.Thread{ConversationID: m.conversationID, ByReference: true}, nil
			default:
				found.Refused = true
			}
		}
	}
	for _, key := range thread.Keys {
		var conversationID, status string
		var updatedAt time.Time
		// Where several customers write, the place's key is everyone's: it continues only a
		// conversation this sender wrote in.
		err := tx.QueryRow(ctx, `SELECT c.id::text,c.status,c.updated_at FROM conversation_thread_keys k
			JOIN conversations c ON c.workspace_id=k.workspace_id AND c.id=k.conversation_id
			WHERE k.workspace_id=$1 AND k.channel_type=$2 AND k.key=$3 AND (NOT $4 OR EXISTS(SELECT 1 FROM messages m
				JOIN channel_inbound_events e ON e.workspace_id=m.workspace_id AND e.message_id=m.id
				WHERE m.workspace_id=c.workspace_id AND m.conversation_id=c.id AND m.kind='incoming' AND m.detached_at IS NULL AND e.sender_key=$5))
			ORDER BY k.created_at DESC LIMIT 1`,
			channel.WorkspaceID, string(channel.Type), key, shared, senderKey).Scan(&conversationID, &status, &updatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return found, fmt.Errorf("match thread key: %w", err)
		}
		if joins(status, updatedAt) {
			return rules.Thread{ConversationID: conversationID}, nil
		}
		return rules.Thread{Refused: true}, nil
	}
	return found, nil
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
	// A request is called by what follows its command ("report bug: the printer jams" is "the printer
	// jams"); a command with nothing after it keeps the channel's subject.
	if in.Intent.Kind == channels.IntentCommand {
		if name, _, rest, ok := channels.MatchCommand(in.Text, settings.Commands); ok && name == in.Intent.Command {
			line, _, _ := strings.Cut(strings.TrimSpace(rest), "\n")
			if runes := []rune(strings.TrimSpace(line)); len(runes) > 0 {
				in.Subject = string(runes[:min(len(runes), 80)])
			}
		}
	}
	// The scope its first message came in, which the AI levels of the channel are read by.
	scope := ""
	if adapter, ok := s.channels.Adapter(channel.Type); ok && slices.Contains(adapter.Definition().Scopes, in.Scope) {
		scope = in.Scope
	}
	var conversationID string
	if err := tx.QueryRow(ctx, `INSERT INTO conversations(workspace_id,contact_id,subject,created_at,updated_at,channel,channel_id,channel_scope,priority,assignee_id)
		VALUES($1,$2,$3,$4,$4,$5,$6,NULLIF($7,''),$8,(SELECT id FROM agents WHERE workspace_id=$1 AND id=NULLIF($9,'')::uuid)) RETURNING id::text`,
		channel.WorkspaceID, contactID, in.Subject, now, string(channel.Type), channel.ID, scope, priority, settings.DefaultAssigneeID).Scan(&conversationID); err != nil {
		return "", fmt.Errorf("create conversation: %w", err)
	}
	if err := tagConversation(ctx, tx, channel.WorkspaceID, conversationID, tags); err != nil {
		return "", err
	}
	return conversationID, nil
}

// tagConversation gives a conversation tags by name, creating the ones the workspace lacks.
func tagConversation(ctx context.Context, tx pgx.Tx, workspaceID, conversationID string, tags []string) error {
	for _, name := range tags {
		var tagID string
		if err := tx.QueryRow(ctx, `INSERT INTO tags(workspace_id,name) VALUES($1,$2)
			ON CONFLICT(workspace_id,name) DO UPDATE SET name=tags.name RETURNING id::text`, workspaceID, name).Scan(&tagID); err != nil {
			return fmt.Errorf("find or create tag: %w", err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO conversation_tags(workspace_id,conversation_id,tag_id) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`,
			workspaceID, conversationID, tagID); err != nil {
			return fmt.Errorf("tag conversation: %w", err)
		}
	}
	return nil
}

func insertChannelMessage(ctx context.Context, tx pgx.Tx, channel channels.Channel, conversationID, externalID, replyKind string, in channels.Inbound) (string, error) {
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
	if err := tx.QueryRow(ctx, `INSERT INTO messages(workspace_id,conversation_id,kind,body,body_html,external_id,channel_id,channel_meta,email_meta,created_at,intake_reply_kind)
		VALUES($1,$2,'incoming',$3,NULLIF($4,''),NULLIF($5,''),$6,$7,$8,COALESCE($9,now()),NULLIF($10,'')) RETURNING id::text`,
		channel.WorkspaceID, conversationID, in.Text, in.HTML, externalID, channel.ID, meta, envelope, sentAt, replyKind).Scan(&messageID); err != nil {
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
	// basis and rule are what the channel's rules decided (intake/rules); empty for what the frame
	// handled before them (a duplicate, an echo, a channel off) and for a message being collected.
	basis, rule string
	// command is the command a message being collected names, so a cancel and a later command know
	// whether the batch named one.
	command                              string
	conversationID, messageID, objectKey string
	objectBytes                          int64
	flushAt                              *time.Time
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
			decided_by_agent_id=COALESCE(NULLIF($14,'')::uuid,decided_by_agent_id),attempts=attempts+1,
			decision_basis=NULLIF($15,''),decision_rule=NULLIF($16,''),command=COALESCE(NULLIF($17,''),command)
			WHERE workspace_id=$1 AND id::text=ANY($2)`,
			j.channel.WorkspaceID, j.request.EventIDs, state, d.holdReason, d.dropReason, d.aggregateKey, d.flushAt, d.senderKey,
			d.conversationID, d.messageID, processedAt, d.objectKey, objectBytes, j.request.DecidedBy, d.basis, d.rule, d.command)
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
			hold_reason,drop_reason,aggregate_key,flush_at,conversation_id,message_id,attempts,received_at,processed_at,decision_basis,decision_rule,command)
		VALUES($1,$2,NULLIF($3,''),$4,$5,NULLIF($6,''),$7,NULLIF($8,''),NULLIF($9,''),NULLIF($10,''),NULLIF($11,''),$12,NULLIF($13,'')::uuid,NULLIF($14,'')::uuid,1,$15,$16,
			NULLIF($17,''),NULLIF($18,''),NULLIF($19,''))
		RETURNING id::text`,
		j.channel.WorkspaceID, j.channel.ID, j.request.Raw.ExternalEventID, state, raw, d.objectKey, objectBytes, d.senderKey,
		d.holdReason, d.dropReason, d.aggregateKey, d.flushAt, d.conversationID, d.messageID, j.now, processedAt, d.basis, d.rule, d.command).Scan(&j.eventID)
	if err != nil {
		return fmt.Errorf("write channel journal: %w", err)
	}
	return nil
}

// joinsWithoutCommand reports whether a message that names no command still belongs to a request:
// what is being collected for its key, or an open conversation its thread finds.
func (s *Store) joinsWithoutCommand(ctx context.Context, tx pgx.Tx, channel channels.Channel, in channels.Inbound, request IngestRequest, shared bool, senderKey string, joinOpen bool, now time.Time) (bool, error) {
	if request.Flushing {
		return false, nil
	}
	if in.Aggregate != nil {
		var collecting bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM channel_inbound_events WHERE workspace_id=$1 AND channel_id=$2 AND state='pending'
			AND aggregate_key=$3 AND NOT (id::text=ANY(COALESCE($4::text[],'{}'))))`, channel.WorkspaceID, channel.ID, in.Aggregate.Key, request.EventIDs).Scan(&collecting); err != nil {
			return false, fmt.Errorf("find collected messages: %w", err)
		}
		if collecting {
			return true, nil
		}
	}
	// A join-open scope joins by the sender's open conversations, not by the thread (onlyOpenConversation).
	if joinOpen {
		return false, nil
	}
	found, err := findChannelThread(ctx, tx, channel, in.Thread, shared, senderKey, now)
	if err != nil || found.ConversationID == "" {
		return false, err
	}
	conversationID := found.ConversationID
	var open bool
	err = tx.QueryRow(ctx, `SELECT status IN ('open','pending') FROM conversations WHERE workspace_id=$1 AND id=$2`, channel.WorkspaceID, conversationID).Scan(&open)
	return open, err
}

// onlyOpenConversation is the sender's open or pending conversation on the channel when they have exactly
// one, empty otherwise (none, several, or a sender the channel never stored).
func onlyOpenConversation(ctx context.Context, tx pgx.Tx, channel channels.Channel, sender channels.Sender) (string, error) {
	var conversationID string
	err := tx.QueryRow(ctx, `SELECT COALESCE((SELECT CASE WHEN count(*)=1 THEN min(c.id::text) END FROM conversations c
			WHERE c.workspace_id=$1 AND c.channel_id=$2 AND c.status IN ('open','pending') AND c.contact_id=(
				SELECT COALESCE(k.merged_into_id,k.id) FROM contact_identities i JOIN contacts k ON k.workspace_id=i.workspace_id AND k.id=i.contact_id
				WHERE i.workspace_id=$1 AND i.channel_id=$2 AND i.kind=$3 AND i.value=$4)),'')`,
		channel.WorkspaceID, channel.ID, sender.Identity.Kind, sender.Identity.Value).Scan(&conversationID)
	if err != nil {
		return "", fmt.Errorf("find the sender's open conversation: %w", err)
	}
	return conversationID, nil
}
