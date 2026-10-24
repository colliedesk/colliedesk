package channeltest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/channels/outbox"
	"github.com/colliedesk/colliedesk/server/internal/channels/pipeline"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Spec describes a customer message for a harness to build as its type's raw event.
type Spec struct {
	// ID is unique per message; From names the customer (an address or a user id derived from it).
	ID, From, Text string
	// Answers is the Spec.ID of an earlier message this one answers, for types that thread by reference.
	Answers string
	// File adds one attachment named file.txt with this content.
	File string
}

// Harness is what Run needs of a type.
type Harness struct {
	Type channels.Type
	// Pool is the owner connection the contract checks rows with (tests only; it sees every workspace).
	Pool  Admin
	Store *store.Store
	// Workspace creates a workspace with its first admin and returns both ids.
	Workspace func(t *testing.T, name, adminEmail string) (workspaceID, agentID string)
	// Connect adds a connected channel of the type to the workspace; n tells apart two channels of a test.
	Connect func(t *testing.T, workspaceID string, n int) channels.Channel
	// Raw builds a customer message as the type receives it.
	Raw func(t *testing.T, channel channels.Channel, spec Spec) channels.Raw
	// Receive hands raw events to the type the way its route does; nil uses the pipeline.
	Receive func(t *testing.T, channel channels.Channel, raws ...channels.Raw) ([]channels.IngestResult, error)
	// Sender replaces the registry's sender in the outbox worker (a type that talks to a server the
	// test runs); nil keeps it.
	Sender channels.OutboundPart
	// Secret is a value the type keeps sealed, to check it never shows; empty skips the check.
	Secret string
}

// Run runs the contract against the harness: what every channel type must do the same way.
// Cases a type cannot run (it sends nothing, it refuses mail while off) are skipped by what its
// definition declares, never by hand.
func Run(t *testing.T, h Harness) {
	adapter, ok := h.Store.Channels().Adapter(h.Type)
	if !ok {
		t.Fatalf("the store's registry has no %s type", h.Type)
	}
	definition := adapter.Definition()
	c := &contract{h: h, definition: definition, pipeline: pipeline.New(h.Store, nil), async: adapter.Inbound().Mode() == channels.Async}

	t.Run("a new message opens one conversation with its contact and files", c.newMessage)
	t.Run("the same message twice is stored once", c.duplicate)
	t.Run("an answer joins its conversation and reopens it", c.answer)
	t.Run("what arrives while the channel is off follows its type's rule", c.off)
	t.Run("a channel's messages never reach another workspace", c.crossWorkspace)
	if c.async {
		t.Run("an event whose processing failed is processed again, once", c.failedAgain)
	}
	if adapter.Outbound() != nil {
		t.Run("a reply is queued on the channel and sent, failed or retried as the sender says", c.send)
		t.Run("a reply while the channel is off is kept and sent after a retry once it is on", c.sendWhileOff)
	} else {
		t.Run("a reply leaves through the type it names", c.repliesVia)
	}
	t.Run("the configuration shown never holds a secret", c.config)
}

type contract struct {
	h          Harness
	definition channels.Definition
	pipeline   *pipeline.Pipeline
	async      bool
	mu         sync.Mutex
	n          int
}

type desk struct {
	workspaceID, agentID string
	channel              channels.Channel
}

func (c *contract) desk(t *testing.T) desk {
	t.Helper()
	c.mu.Lock()
	c.n++
	n := c.n
	c.mu.Unlock()
	workspaceID, agentID := c.h.Workspace(t, fmt.Sprintf("%s contract %d", c.h.Type, n), fmt.Sprintf("admin%d@contract.example.com", n))
	return desk{workspaceID: workspaceID, agentID: agentID, channel: c.h.Connect(t, workspaceID, n)}
}

func (c *contract) receive(t *testing.T, channel channels.Channel, raws ...channels.Raw) []channels.IngestResult {
	t.Helper()
	results, err := c.tryReceive(t, channel, raws...)
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	return results
}

func (c *contract) tryReceive(t *testing.T, channel channels.Channel, raws ...channels.Raw) ([]channels.IngestResult, error) {
	t.Helper()
	var results []channels.IngestResult
	var err error
	if c.h.Receive != nil {
		results, err = c.h.Receive(t, channel, raws...)
	} else {
		results, err = c.pipeline.Receive(context.Background(), channel, raws)
	}
	if err != nil || !c.async {
		return results, err
	}
	if _, err := c.pipeline.ProcessDue(context.Background()); err != nil {
		return results, err
	}
	// The journal now holds the outcome; report it as a Sync type would.
	for i, result := range results {
		results[i] = c.outcome(t, channel, result.EventID)
	}
	return results, nil
}

func (c *contract) outcome(t *testing.T, channel channels.Channel, eventID string) channels.IngestResult {
	t.Helper()
	var state string
	var reason, conversationID, messageID *string
	if err := c.h.Pool.QueryRow(context.Background(), `SELECT state,COALESCE(drop_reason,hold_reason),conversation_id::text,message_id::text
		FROM channel_inbound_events WHERE workspace_id=$1 AND id=$2`, channel.WorkspaceID, eventID).Scan(&state, &reason, &conversationID, &messageID); err != nil {
		t.Fatal(err)
	}
	status := state
	switch state {
	case "processed":
		status = channels.ResultCreated
	case "held":
		status = channels.ResultHeld
	}
	return channels.IngestResult{Status: status, Reason: reason, ConversationID: conversationID, MessageID: messageID, EventID: eventID}
}

func (c *contract) count(t *testing.T, workspaceID, table, where string, args ...any) int {
	t.Helper()
	var n int
	query := `SELECT count(*) FROM ` + table + ` WHERE workspace_id=$1`
	if where != "" {
		query += ` AND ` + where
	}
	if err := c.h.Pool.QueryRow(context.Background(), query, append([]any{workspaceID}, args...)...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (c *contract) newMessage(t *testing.T) {
	d := c.desk(t)
	results := c.receive(t, d.channel, c.h.Raw(t, d.channel, Spec{ID: "first", From: "ann", Text: "My order is late.", File: "receipt"}))
	if len(results) != 1 || results[0].ConversationID == nil || results[0].MessageID == nil {
		t.Fatalf("results=%+v, want one stored message", results)
	}
	conversationID := *results[0].ConversationID
	var channelType, channelID string
	if err := c.h.Pool.QueryRow(context.Background(), `SELECT channel,channel_id::text FROM conversations WHERE workspace_id=$1 AND id=$2`,
		d.workspaceID, conversationID).Scan(&channelType, &channelID); err != nil {
		t.Fatal(err)
	}
	if channelType != string(c.h.Type) || channelID != d.channel.ID {
		t.Fatalf("conversation channel=%s channel_id=%s, want %s %s", channelType, channelID, c.h.Type, d.channel.ID)
	}
	if n := c.count(t, d.workspaceID, "conversations", ""); n != 1 {
		t.Fatalf("conversations=%d, want 1", n)
	}
	if n := c.count(t, d.workspaceID, "contacts", ""); n != 1 {
		t.Fatalf("contacts=%d, want 1", n)
	}
	if n := c.count(t, d.workspaceID, "messages", "kind='incoming' AND channel_id=$2 AND external_id IS NOT NULL", d.channel.ID); n != 1 {
		t.Fatalf("incoming messages with their channel and id=%d, want 1", n)
	}
	if n := c.count(t, d.workspaceID, "attachments", "filename='file.txt'"); n != 1 {
		t.Fatalf("file.txt attachments=%d, want 1", n)
	}
	if n := c.count(t, d.workspaceID, "channel_inbound_events", "state='processed' AND message_id=$2", *results[0].MessageID); n != 1 {
		t.Fatalf("processed journal rows=%d, want 1", n)
	}
}

func (c *contract) duplicate(t *testing.T) {
	d := c.desk(t)
	raw := c.h.Raw(t, d.channel, Spec{ID: "twice", From: "bob", Text: "Hello?"})
	c.receive(t, d.channel, raw)
	again := c.receive(t, d.channel, raw)
	if len(again) != 1 || (again[0].Status != channels.ResultDuplicate && again[0].Status != "processed" && again[0].Status != channels.ResultCreated) {
		t.Fatalf("second delivery=%+v, want a duplicate", again)
	}
	if n := c.count(t, d.workspaceID, "messages", "kind='incoming'"); n != 1 {
		t.Fatalf("incoming messages=%d after the same message twice, want 1", n)
	}
}

func (c *contract) answer(t *testing.T) {
	if c.definition.Defaults.JoinPolicy.Kind == channels.JoinNever {
		t.Skip("the type opens a conversation for every message (join policy never)")
	}
	d := c.desk(t)
	first := c.receive(t, d.channel, c.h.Raw(t, d.channel, Spec{ID: "q1", From: "cy", Text: "Question."}))
	conversationID := *first[0].ConversationID
	if _, err := c.h.Pool.Exec(context.Background(), `UPDATE conversations SET status='resolved' WHERE workspace_id=$1 AND id=$2`, d.workspaceID, conversationID); err != nil {
		t.Fatal(err)
	}
	next := c.receive(t, d.channel, c.h.Raw(t, d.channel, Spec{ID: "q2", From: "cy", Text: "One more thing.", Answers: "q1"}))
	if next[0].ConversationID == nil || *next[0].ConversationID != conversationID {
		t.Fatalf("answer landed in %v, want %s", next[0].ConversationID, conversationID)
	}
	var status string
	if err := c.h.Pool.QueryRow(context.Background(), `SELECT status FROM conversations WHERE id=$1`, conversationID).Scan(&status); err != nil || status != "open" {
		t.Fatalf("status=%q (%v), want the answered conversation open again", status, err)
	}
}

func (c *contract) setEnabled(t *testing.T, d desk, on bool) {
	t.Helper()
	if _, err := c.h.Store.UpdateChannel(context.Background(), d.workspaceID, d.channel.ID, store.ChannelInput{Enabled: &on},
		store.ChannelActor{AgentID: d.agentID}); err != nil {
		t.Fatal(err)
	}
	d.channel.Enabled = on
}

func (c *contract) off(t *testing.T) {
	d := c.desk(t)
	c.setEnabled(t, d, false)
	d.channel.Enabled = false
	ctx := context.Background()
	if c.definition.WhenOff == channels.OffRefuse {
		if _, err := c.tryReceive(t, d.channel, c.h.Raw(t, d.channel, Spec{ID: "off-1", From: "dee", Text: "Anyone?"})); !errors.Is(err, channels.ErrChannelOff) {
			t.Fatalf("receive while off: %v, want ErrChannelOff", err)
		}
		if n := c.count(t, d.workspaceID, "channel_inbound_events", ""); n != 0 {
			t.Fatalf("journal rows=%d, a refused message keeps nothing", n)
		}
		return
	}
	held := c.receive(t, d.channel, c.h.Raw(t, d.channel, Spec{ID: "off-1", From: "dee", Text: "Anyone?", File: "held file"}),
		c.h.Raw(t, d.channel, Spec{ID: "off-2", From: "eve", Text: "Spam."}))
	for _, result := range held {
		if result.Status != channels.ResultHeld {
			t.Fatalf("received while off: %+v, want held", held)
		}
	}
	if n := c.count(t, d.workspaceID, "conversations", ""); n != 0 {
		t.Fatalf("conversations=%d while off, want none", n)
	}
	replayed, err := c.pipeline.ReplayHeld(ctx, d.workspaceID, d.channel.ID, []string{held[0].EventID}, "", store.ChannelActor{AgentID: d.agentID})
	if err != nil || len(replayed) != 1 || replayed[0].ConversationID == nil {
		t.Fatalf("replay: %+v %v", replayed, err)
	}
	if n := c.count(t, d.workspaceID, "attachments", "filename='file.txt'"); n != 1 {
		t.Fatalf("replayed files=%d, want 1", n)
	}
	discarded, err := c.h.Store.DiscardHeld(ctx, d.workspaceID, d.channel.ID, nil, store.ChannelActor{AgentID: d.agentID})
	if err != nil || discarded != 1 {
		t.Fatalf("discard: %d %v, want the one left", discarded, err)
	}
	if n := c.count(t, d.workspaceID, "conversations", ""); n != 1 {
		t.Fatalf("conversations=%d, want only the replayed one", n)
	}
	if n := c.count(t, d.workspaceID, "channel_events", "kind IN ('held_replayed','held_discarded') AND actor_agent_id=$2", d.agentID); n != 2 {
		t.Fatalf("history of replay and discard=%d, want 2", n)
	}
	var counted int64
	if err := c.h.Pool.QueryRow(ctx, `SELECT COALESCE(sum(raw_bytes),0) FROM channel_inbound_events WHERE workspace_id=$1 AND state='held'`, d.workspaceID).Scan(&counted); err != nil || counted != 0 {
		t.Fatalf("held bytes left=%d (%v), want 0", counted, err)
	}
}

func (c *contract) crossWorkspace(t *testing.T) {
	a, b := c.desk(t), c.desk(t)
	c.receive(t, a.channel, c.h.Raw(t, a.channel, Spec{ID: "mine", From: "fay", Text: "For A only."}))
	if n := c.count(t, b.workspaceID, "messages", ""); n != 0 {
		t.Fatalf("workspace B has %d messages after a message to A", n)
	}
	if _, err := c.h.Store.GetChannel(context.Background(), b.workspaceID, a.channel.ID); !errors.Is(err, channels.ErrNotFound) {
		t.Fatalf("A's channel read from B: %v, want not found", err)
	}
	if _, err := c.h.Store.UpdateChannel(context.Background(), b.workspaceID, a.channel.ID, store.ChannelInput{}, store.ChannelActor{AgentID: b.agentID}); !errors.Is(err, channels.ErrNotFound) {
		t.Fatalf("A's channel changed from B: %v, want not found", err)
	}
	if _, err := c.pipeline.ReplayHeld(context.Background(), b.workspaceID, a.channel.ID, nil, "", store.ChannelActor{AgentID: b.agentID}); !errors.Is(err, channels.ErrNotFound) {
		t.Fatalf("A's held messages replayed from B: %v, want not found", err)
	}
	// The same raw event aimed at B's channel stays in B.
	c.receive(t, b.channel, c.h.Raw(t, b.channel, Spec{ID: "mine", From: "fay", Text: "For B."}))
	if n := c.count(t, a.workspaceID, "messages", ""); n != 1 {
		t.Fatalf("workspace A has %d messages, want its own 1", n)
	}
}

func (c *contract) failedAgain(t *testing.T) {
	d := c.desk(t)
	ctx := context.Background()
	results, err := c.pipeline.Receive(ctx, d.channel, []channels.Raw{c.h.Raw(t, d.channel, Spec{ID: "flaky", From: "gus", Text: "Retry me."})})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.h.Store.MarkInboundFailed(ctx, d.workspaceID, []string{results[0].EventID}, "injected failure"); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := c.pipeline.ProcessDue(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if n := c.count(t, d.workspaceID, "messages", "kind='incoming'"); n != 1 {
		t.Fatalf("messages=%d after a failure and two runs, want 1", n)
	}
	if got := c.outcome(t, d.channel, results[0].EventID); got.MessageID == nil {
		t.Fatalf("event after retry: %+v, want processed", got)
	}
}

// scripted wraps a sender with results a test chooses; with none left it sends for real.
type scripted struct {
	real channels.OutboundPart
	mu   sync.Mutex
	next []channels.SendResult
}

func (s *scripted) Prepare(ctx context.Context, tx pgxTx, request channels.PrepareRequest) (channels.Prepared, error) {
	return s.real.Prepare(ctx, tx, request)
}

func (s *scripted) Send(ctx context.Context, out channels.Outbound, kit channels.SendKit) channels.SendResult {
	s.mu.Lock()
	if len(s.next) > 0 {
		result := s.next[0]
		s.next = s.next[1:]
		s.mu.Unlock()
		return result
	}
	s.mu.Unlock()
	return s.real.Send(ctx, out, kit)
}

func (c *contract) worker(t *testing.T, clock *time.Time) (*outbox.Worker, *scripted) {
	t.Helper()
	adapter, _ := c.h.Store.Channels().Adapter(c.h.Type)
	real := c.h.Sender
	if real == nil {
		real = adapter.Outbound()
	}
	sender := &scripted{real: real}
	worker := outbox.New(c.h.Store, outbox.Options{Now: func() time.Time { return *clock },
		Senders: map[channels.Type]channels.OutboundPart{c.h.Type: sender}})
	return worker, sender
}

func (c *contract) delivery(t *testing.T, workspaceID, messageID string) (status string, lastError, code, external *string) {
	t.Helper()
	if err := c.h.Pool.QueryRow(context.Background(), `SELECT status,last_error,last_error_code,external_id FROM channel_outbox WHERE workspace_id=$1 AND message_id=$2`,
		workspaceID, messageID).Scan(&status, &lastError, &code, &external); err != nil {
		t.Fatalf("delivery of %s: %v", messageID, err)
	}
	return status, lastError, code, external
}

func (c *contract) reply(t *testing.T, d desk, conversationID, body string) string {
	t.Helper()
	message, err := c.h.Store.CreateMessage(context.Background(), d.workspaceID, conversationID, store.Actor{AgentID: d.agentID}, "outgoing", body)
	if err != nil {
		t.Fatalf("reply: %v", err)
	}
	return message.ID
}

func (c *contract) send(t *testing.T) {
	d := c.desk(t)
	ctx := context.Background()
	first := c.receive(t, d.channel, c.h.Raw(t, d.channel, Spec{ID: "need-help", From: "hal", Text: "Help."}))
	conversationID := *first[0].ConversationID
	clock := time.Now().UTC().Add(time.Minute) // ahead of the database clock that stamps new rows due
	worker, sender := c.worker(t, &clock)

	sent := c.reply(t, d, conversationID, "On it.")
	var channelID string
	if err := c.h.Pool.QueryRow(ctx, `SELECT channel_id::text FROM channel_outbox WHERE workspace_id=$1 AND message_id=$2`, d.workspaceID, sent).Scan(&channelID); err != nil ||
		channelID != d.channel.ID {
		t.Fatalf("queued on channel %q (%v), want %s", channelID, err, d.channel.ID)
	}
	if _, err := worker.ProcessDue(ctx); err != nil {
		t.Fatal(err)
	}
	if status, lastError, _, _ := c.delivery(t, d.workspaceID, sent); status != channels.DeliverySent {
		t.Fatalf("delivery=%s (%v), want sent", status, deref(lastError))
	}

	retried := c.reply(t, d, conversationID, "Second try.")
	sender.next = []channels.SendResult{channels.Retry("busy")}
	if _, err := worker.ProcessDue(ctx); err != nil {
		t.Fatal(err)
	}
	if status, lastError, _, _ := c.delivery(t, d.workspaceID, retried); status != channels.DeliveryQueued || deref(lastError) != "busy" {
		t.Fatalf("after a retryable failure: %s %q, want queued with the reason", status, deref(lastError))
	}
	clock = clock.Add(outbox.Backoff(1) + time.Second)
	if _, err := worker.ProcessDue(ctx); err != nil {
		t.Fatal(err)
	}
	if status, _, _, _ := c.delivery(t, d.workspaceID, retried); status != channels.DeliverySent {
		t.Fatalf("after the backoff: %s, want sent", status)
	}

	failed := c.reply(t, d, conversationID, "Third.")
	sender.next = []channels.SendResult{{Outcome: channels.OutcomeFailed, Code: "refused", Reason: "The channel refused it."}}
	if _, err := worker.ProcessDue(ctx); err != nil {
		t.Fatal(err)
	}
	if status, lastError, code, _ := c.delivery(t, d.workspaceID, failed); status != channels.DeliveryFailed || deref(code) != "refused" ||
		deref(lastError) != "The channel refused it." {
		t.Fatalf("failed delivery: %s %q %q", status, deref(code), deref(lastError))
	}
	if _, err := c.h.Store.RetryDelivery(ctx, d.workspaceID, conversationID, failed); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if _, err := worker.ProcessDue(ctx); err != nil {
		t.Fatal(err)
	}
	if status, _, _, _ := c.delivery(t, d.workspaceID, failed); status != channels.DeliverySent {
		t.Fatalf("after a person's retry: %s, want sent", status)
	}

	deferred := c.reply(t, d, conversationID, "Later.")
	sender.next = []channels.SendResult{channels.Defer(clock.Add(time.Hour), "quota")}
	if _, err := worker.ProcessDue(ctx); err != nil {
		t.Fatal(err)
	}
	var attempts int
	if err := c.h.Pool.QueryRow(ctx, `SELECT attempts FROM channel_outbox WHERE message_id=$1`, deferred).Scan(&attempts); err != nil || attempts != 0 {
		t.Fatalf("deferred attempts=%d (%v), want 0: deferring spends no attempt", attempts, err)
	}
}

func (c *contract) sendWhileOff(t *testing.T) {
	d := c.desk(t)
	ctx := context.Background()
	first := c.receive(t, d.channel, c.h.Raw(t, d.channel, Spec{ID: "before-off", From: "ivy", Text: "Hi."}))
	conversationID := *first[0].ConversationID
	c.setEnabled(t, d, false)
	reply := c.reply(t, d, conversationID, "Saved while off.")
	if status, _, code, _ := c.delivery(t, d.workspaceID, reply); status != channels.DeliveryFailed || deref(code) != store.DeliveryCodeChannelOff {
		t.Fatalf("reply while off: %s %q, want failed channel_off", status, deref(code))
	}
	if _, err := c.h.Store.RetryDelivery(ctx, d.workspaceID, conversationID, reply); !errors.Is(err, store.ErrOutboundNotReady) {
		t.Fatalf("retry while off: %v, want not ready", err)
	}
	c.setEnabled(t, d, true)
	if _, err := c.h.Store.RetryDelivery(ctx, d.workspaceID, conversationID, reply); err != nil {
		t.Fatalf("retry once on: %v", err)
	}
	clock := time.Now().UTC().Add(time.Minute) // ahead of the database clock that stamps new rows due
	worker, _ := c.worker(t, &clock)
	if _, err := worker.ProcessDue(ctx); err != nil {
		t.Fatal(err)
	}
	if status, _, _, _ := c.delivery(t, d.workspaceID, reply); status != channels.DeliverySent {
		t.Fatalf("after turning on and retrying: %s, want sent", status)
	}
}

func (c *contract) repliesVia(t *testing.T) {
	d := c.desk(t)
	first := c.receive(t, d.channel, c.h.Raw(t, d.channel, Spec{ID: "via", From: "jo", Text: "Question."}))
	route, err := c.h.Store.GetReplyRoute(context.Background(), d.workspaceID, *first[0].ConversationID)
	if err != nil {
		t.Fatal(err)
	}
	if route.Channel != nil && route.Channel.Type != c.definition.RepliesVia {
		t.Fatalf("replies leave through %s, want %s", route.Channel.Type, c.definition.RepliesVia)
	}
}

func (c *contract) config(t *testing.T) {
	d := c.desk(t)
	ctx := context.Background()
	adapter, _ := c.h.Store.Channels().Adapter(c.h.Type)
	specs := adapter.Config().Credentials()
	if c.h.Secret != "" && len(specs) > 0 {
		if _, err := c.h.Store.SetChannelCredential(ctx, d.workspaceID, d.channel.ID, specs[0].Name, c.h.Secret, store.ChannelActor{AgentID: d.agentID}); err != nil {
			t.Fatalf("store secret: %v", err)
		}
		var sealed []byte
		if err := c.h.Pool.QueryRow(ctx, `SELECT sealed FROM channel_credentials WHERE channel_id=$1`, d.channel.ID).Scan(&sealed); err != nil ||
			strings.Contains(string(sealed), c.h.Secret) {
			t.Fatalf("secret stored in the clear or not at all (%v)", err)
		}
		opened, _, found, err := c.h.Store.OpenChannelCredential(ctx, d.workspaceID, d.channel.ID, specs[0].Name)
		if err != nil || !found || opened != c.h.Secret {
			t.Fatalf("opened secret=%q found=%v (%v)", opened, found, err)
		}
	}
	var described store.ChannelPublic
	if err := c.h.Store.InWorkspace(ctx, d.workspaceID, func(tx pgxTx) error {
		channel, err := c.h.Store.GetChannel(ctx, d.workspaceID, d.channel.ID)
		if err != nil {
			return err
		}
		described, err = c.h.Store.DescribeChannel(ctx, tx, channel)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(described.Config)
	if err != nil {
		t.Fatal(err)
	}
	if c.h.Secret != "" && strings.Contains(string(encoded)+described.Summary, c.h.Secret) {
		t.Fatalf("public configuration %s shows the secret", encoded)
	}
	switch described.Status.State {
	case channels.StateWorking, channels.StateNeedsAttention:
	default:
		t.Fatalf("status of a connected channel that is on: %+v", described.Status)
	}
	if err := c.h.Store.RemoveChannel(ctx, d.workspaceID, d.channel.ID, store.ChannelActor{AgentID: d.agentID}); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if n := c.count(t, d.workspaceID, "channel_credentials", ""); n != 0 {
		t.Fatalf("secrets left after removing=%d", n)
	}
	if n := c.count(t, d.workspaceID, "channel_events", "kind='removed' AND actor_agent_id=$2", d.agentID); n != 1 {
		t.Fatalf("removed events with the actor=%d, want 1", n)
	}
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

type pgxTx = pgx.Tx

// Admin is the owner connection of a test database, for assertions.
type Admin interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}
