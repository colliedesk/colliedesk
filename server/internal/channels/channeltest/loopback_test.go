package channeltest_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/channels/channeltest"
	"github.com/colliedesk/colliedesk/server/internal/channels/outbox"
	"github.com/colliedesk/colliedesk/server/internal/channels/pipeline"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

type loopDesk struct {
	pool                 *pgxpool.Pool
	store                *store.Store
	pipeline             *pipeline.Pipeline
	clock                *time.Time
	workspaceID, agentID string
	channel              channels.Channel
}

func newLoopDesk(t *testing.T, loop *channeltest.Loopback) loopDesk {
	t.Helper()
	pool, st := loopbackStore(t, loop)
	ctx := context.Background()
	workspace, _, err := auth.New(pool, "https://desk.example.com").CreateWorkspace(ctx, "Loop Desk", "admin@loop.example.com")
	if err != nil {
		t.Fatal(err)
	}
	var agentID string
	if err := pool.QueryRow(ctx, `SELECT id::text FROM agents WHERE workspace_id=$1`, workspace.ID).Scan(&agentID); err != nil {
		t.Fatal(err)
	}
	channel, err := st.CreateChannel(ctx, workspace.ID, loop.Type, store.ChannelInput{Config: json.RawMessage(`{"account":"oa-1"}`)}, store.ChannelActor{AgentID: agentID})
	if err != nil {
		t.Fatal(err)
	}
	clock := time.Now().UTC()
	return loopDesk{pool: pool, store: st, pipeline: pipeline.New(st, func() time.Time { return clock }), clock: &clock,
		workspaceID: workspace.ID, agentID: agentID, channel: channel}
}

func (d loopDesk) send(t *testing.T, events ...channeltest.LoopbackEvent) {
	t.Helper()
	raws := make([]channels.Raw, len(events))
	for i, event := range events {
		encoded, _ := json.Marshal(event)
		raws[i] = channels.Raw{ExternalEventID: event.ID, JSON: encoded}
	}
	if _, err := d.pipeline.Receive(context.Background(), d.channel, raws); err != nil {
		t.Fatal(err)
	}
	if _, err := d.pipeline.ProcessDue(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func (d loopDesk) one(t *testing.T, query string, args ...any) string {
	t.Helper()
	var value string
	if err := d.pool.QueryRow(context.Background(), query, append([]any{d.workspaceID}, args...)...).Scan(&value); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return value
}

// Messages of one sender are collected until the sender is quiet, then open one conversation; a
// confirmation sends them at once and a cancel throws them away.
func TestCollectedMessagesOpenOneConversation(t *testing.T) {
	d := newLoopDesk(t, channeltest.NewLoopback("loopback", channels.Async))
	d.send(t, channeltest.LoopbackEvent{ID: "c1", From: "u1", Text: "my invoice is wrong", Collect: true, Intent: "command:bug"},
		channeltest.LoopbackEvent{ID: "c2", From: "u1", Text: "Hi", Collect: true})
	if n := d.one(t, `SELECT count(*)::text FROM conversations WHERE workspace_id=$1`); n != "0" {
		t.Fatalf("conversations=%s while collecting, want 0", n)
	}
	*d.clock = d.clock.Add(11 * time.Second)
	if _, err := d.pipeline.ProcessDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	if body := d.one(t, `SELECT string_agg(body,'|') FROM messages WHERE workspace_id=$1`); body != "my invoice is wrong\nHi" {
		t.Fatalf("collected body=%q", body)
	}
	if got := d.one(t, `SELECT priority||':'||(SELECT string_agg(t.name,',') FROM conversation_tags ct JOIN tags t ON t.id=ct.tag_id WHERE ct.conversation_id=c.id)
		FROM conversations c WHERE workspace_id=$1`); got != "high:bug" {
		t.Fatalf("command applied %q, want high priority and the bug tag", got)
	}
	if n := d.one(t, `SELECT count(*)::text FROM channel_inbound_events WHERE workspace_id=$1 AND state='processed'`); n != "2" {
		t.Fatalf("processed events=%s, want both", n)
	}

	d.send(t, channeltest.LoopbackEvent{ID: "c3", From: "u2", Text: "first", Collect: true},
		channeltest.LoopbackEvent{ID: "c4", From: "u2", Text: "ok send", Collect: true, Intent: channels.IntentConfirm})
	if _, err := d.pipeline.ProcessDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := d.one(t, `SELECT count(*)::text FROM conversations WHERE workspace_id=$1`); n != "2" {
		t.Fatalf("conversations=%s after a confirmation, want 2", n)
	}

	d.send(t, channeltest.LoopbackEvent{ID: "c5", From: "u3", Text: "draft", Collect: true},
		channeltest.LoopbackEvent{ID: "c6", From: "u3", Text: "never mind", Collect: true, Intent: channels.IntentCancel})
	*d.clock = d.clock.Add(time.Minute)
	if _, err := d.pipeline.ProcessDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := d.one(t, `SELECT count(*)::text FROM channel_inbound_events WHERE workspace_id=$1 AND drop_reason='cancelled'`); n != "2" {
		t.Fatalf("cancelled events=%s, want both", n)
	}
	if n := d.one(t, `SELECT count(*)::text FROM conversations WHERE workspace_id=$1`); n != "2" {
		t.Fatalf("conversations=%s after a cancel, want still 2", n)
	}
}

// A customer known only on the channel becomes a contact without email, found again by identity;
// a sender with an email address joins the contact that has it.
func TestChannelIdentityMakesAContactWithoutEmail(t *testing.T) {
	d := newLoopDesk(t, channeltest.NewLoopback("loopback", channels.Async))
	d.send(t, channeltest.LoopbackEvent{ID: "i1", From: "zalo-1", Name: "Minh", Text: "Xin chào"},
		channeltest.LoopbackEvent{ID: "i2", From: "zalo-1", Name: "Minh Tran", Text: "again", Group: "g1"})
	if got := d.one(t, `SELECT count(*)::text||':'||COALESCE(max(email),'none') FROM contacts WHERE workspace_id=$1`); got != "1:none" {
		t.Fatalf("contacts=%s, want one without email", got)
	}
	if got := d.one(t, `SELECT display_name FROM contact_identities WHERE workspace_id=$1 AND value='zalo-1'`); got != "Minh Tran" {
		t.Fatalf("identity name=%q, want the latest", got)
	}
	if _, err := d.pool.Exec(context.Background(), `INSERT INTO contacts(workspace_id,name,email) VALUES($1,'Lan','lan@example.com')`, d.workspaceID); err != nil {
		t.Fatal(err)
	}
	d.send(t, channeltest.LoopbackEvent{ID: "i3", From: "zalo-2", Email: "Lan@example.com", Text: "hello"})
	if got := d.one(t, `SELECT c.email FROM contact_identities i JOIN contacts c ON c.id=i.contact_id WHERE i.workspace_id=$1 AND i.value='zalo-2'`); got != "lan@example.com" {
		t.Fatalf("identity joined %q, want the contact with that email", got)
	}
}

// Echoes are dropped, archived chatter is kept without a conversation, and a report that a reply
// did not arrive fails its delivery.
func TestLoopbackKindsOtherThanMessages(t *testing.T) {
	loop := channeltest.NewLoopback("loopback", channels.Async)
	d := newLoopDesk(t, loop)
	d.send(t, channeltest.LoopbackEvent{ID: "k1", From: "u1", Text: "real"})
	conversationID := d.one(t, `SELECT id::text FROM conversations WHERE workspace_id=$1`)
	reply, err := d.store.CreateMessage(context.Background(), d.workspaceID, conversationID, store.Actor{AgentID: d.agentID}, "outgoing", "answer")
	if err != nil {
		t.Fatal(err)
	}
	d.send(t, channeltest.LoopbackEvent{ID: "k2", From: "oa", Text: "answer", Echo: true},
		channeltest.LoopbackEvent{ID: "k3", From: "u9", Text: "group chatter", Archive: true},
		channeltest.LoopbackEvent{ID: "k4", From: "u1", Failed: "out-" + reply.ID})
	if got := d.one(t, `SELECT string_agg(state||':'||COALESCE(drop_reason,''),',' ORDER BY external_event_id) FROM channel_inbound_events
		WHERE workspace_id=$1 AND external_event_id IN ('k2','k3','k4')`); got != "dropped:echo,processed:,dropped:delivery_update" {
		t.Fatalf("journal=%s", got)
	}
	if got := d.one(t, `SELECT status||':'||last_error_code FROM channel_outbox WHERE workspace_id=$1 AND message_id=$2`, reply.ID); got != "failed:bounced" {
		t.Fatalf("delivery after the report=%s", got)
	}
}

// A secret refreshed by two senders at once is replaced by one of them only.
func TestChannelCredentialIsReplacedByCompareAndSwap(t *testing.T) {
	d := newLoopDesk(t, channeltest.NewLoopback("loopback", channels.Async))
	ctx := context.Background()
	hint, err := d.store.SetChannelCredential(ctx, d.workspaceID, d.channel.ID, "token", "refresh-0001", store.ChannelActor{AgentID: d.agentID})
	if err != nil || hint != "0001" {
		t.Fatalf("hint=%q (%v)", hint, err)
	}
	_, version, _, err := d.store.OpenChannelCredential(ctx, d.workspaceID, d.channel.ID, "token")
	if err != nil {
		t.Fatal(err)
	}
	won, err := d.store.ReplaceChannelCredential(ctx, d.workspaceID, d.channel.ID, "token", "refresh-0002", version, nil)
	if err != nil || !won {
		t.Fatalf("first refresh won=%v (%v)", won, err)
	}
	lost, err := d.store.ReplaceChannelCredential(ctx, d.workspaceID, d.channel.ID, "token", "refresh-0003", version, nil)
	if err != nil || lost {
		t.Fatalf("second refresh with the old version won=%v (%v), want refused", lost, err)
	}
	value, _, _, err := d.store.OpenChannelCredential(ctx, d.workspaceID, d.channel.ID, "token")
	if err != nil || value != "refresh-0002" {
		t.Fatalf("token=%q (%v)", value, err)
	}
	if _, err := d.store.SetChannelCredential(ctx, d.workspaceID, d.channel.ID, "password", "x", store.ChannelActor{}); err == nil {
		t.Fatal("a secret the type does not declare was stored")
	}
}

// One outside account belongs to one channel on the whole server; a second is refused, and the
// account routes to its workspace.
func TestExternalAccountIsUniqueAcrossWorkspaces(t *testing.T) {
	loop := channeltest.NewLoopback("loopback", channels.Async)
	d := newLoopDesk(t, loop)
	ctx := context.Background()
	other, _, err := auth.New(d.pool, "https://desk.example.com").CreateWorkspace(ctx, "Other Desk", "admin@other.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.store.CreateChannel(ctx, other.ID, loop.Type, store.ChannelInput{Config: json.RawMessage(`{"account":"oa-1"}`)}, store.ChannelActor{}); !errors.Is(err, channels.ErrExternalAccount) {
		t.Fatalf("second channel of the same account: %v, want ErrExternalAccount", err)
	}
	found, ok, err := d.store.ChannelForExternalAccount(ctx, loop.Type, "oa-1")
	if err != nil || !ok || found.ID != d.channel.ID || found.WorkspaceID != d.workspaceID {
		t.Fatalf("route oa-1: %+v %v %v", found, ok, err)
	}
	if err := d.store.RemoveChannel(ctx, d.workspaceID, d.channel.ID, store.ChannelActor{AgentID: d.agentID}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.store.CreateChannel(ctx, other.ID, loop.Type, store.ChannelInput{Config: json.RawMessage(`{"account":"oa-1"}`)}, store.ChannelActor{}); err != nil {
		t.Fatalf("the account after its channel was removed: %v", err)
	}
}

// A channel can greet the customer: its received notice is a reply that goes out through the outbox.
func TestReceivedNoticeIsSentThroughTheOutbox(t *testing.T) {
	loop := channeltest.NewLoopback("loopback", channels.Async)
	loop.Defaults.Notices.Received = "Thanks, we got your message."
	d := newLoopDesk(t, loop)
	d.send(t, channeltest.LoopbackEvent{ID: "n1", From: "u1", Text: "help"})
	if got := d.one(t, `SELECT m.body||':'||o.status FROM messages m JOIN channel_outbox o ON o.message_id=m.id WHERE m.workspace_id=$1 AND m.kind='outgoing'`); got != "Thanks, we got your message.:queued" {
		t.Fatalf("notice=%q", got)
	}
}

// A reply sent in parts that fails halfway keeps what went: the retry sends only the rest, and every
// part's id is known, so the copies the channel sends back are dropped as our own.
func TestReplySentInPartsResumesAndKnowsItsEchoes(t *testing.T) {
	loop := channeltest.NewLoopback("loopback", channels.Async)
	loop.PartSize = 5
	d := newLoopDesk(t, loop)
	ctx := context.Background()
	d.send(t, channeltest.LoopbackEvent{ID: "p0", From: "u1", Text: "long question"})
	conversationID := d.one(t, `SELECT id::text FROM conversations WHERE workspace_id=$1`)
	reply, err := d.store.CreateMessage(ctx, d.workspaceID, conversationID, store.Actor{AgentID: d.agentID}, "outgoing", "abcdefghijklmno")
	if err != nil {
		t.Fatal(err)
	}
	clock := time.Now().UTC().Add(time.Minute)
	worker := outbox.New(d.store, outbox.Options{Now: func() time.Time { return clock }})
	loop.Endpoint.FailPartOnce(2)
	if _, err := worker.ProcessDue(ctx); err != nil {
		t.Fatal(err)
	}
	if got := d.one(t, `SELECT status||':'||(meta->>'sent') FROM channel_outbox WHERE workspace_id=$1 AND message_id=$2`, reply.ID); got != "queued:1" {
		t.Fatalf("after part 2 failed: %s, want queued with one part sent", got)
	}
	clock = clock.Add(outbox.Backoff(1) + time.Second)
	if _, err := worker.ProcessDue(ctx); err != nil {
		t.Fatal(err)
	}
	var texts []string
	for _, sent := range loop.Endpoint.Sent() {
		texts = append(texts, sent.Text)
	}
	if strings.Join(texts, "|") != "abcde|fghij|klmno" {
		t.Fatalf("parts sent=%v, want each once", texts)
	}
	if got := d.one(t, `SELECT count(*)::text FROM channel_sent_ids WHERE workspace_id=$1 AND message_id=$2`, reply.ID); got != "3" {
		t.Fatalf("sent ids=%s, want 3", got)
	}
	d.send(t, channeltest.LoopbackEvent{ID: "part-2-" + reply.ID, From: "oa", Text: "fghij"})
	if got := d.one(t, `SELECT state||':'||drop_reason FROM channel_inbound_events WHERE workspace_id=$1 AND external_event_id=$2`, "part-2-"+reply.ID); got != "dropped:echo" {
		t.Fatalf("the echo of a part: %s", got)
	}
}

// Two senders that find a token expired at once refresh it once; the second gets the new token.
func TestOneTimeTokenIsRefreshedOnce(t *testing.T) {
	d := newLoopDesk(t, channeltest.NewLoopback("loopback", channels.Async))
	ctx := context.Background()
	if _, err := d.store.SetChannelCredential(ctx, d.workspaceID, d.channel.ID, "token", "expired", store.ChannelActor{AgentID: d.agentID}); err != nil {
		t.Fatal(err)
	}
	_, version, _, err := d.store.OpenChannelCredential(ctx, d.workspaceID, d.channel.ID, "token")
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	refresh := func(ctx context.Context, current string) (string, *time.Time, error) {
		calls.Add(1)
		time.Sleep(50 * time.Millisecond)
		return "fresh", nil, nil
	}
	var wg sync.WaitGroup
	values := make([]string, 2)
	errs := make([]error, 2)
	for i := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			values[i], _, errs[i] = d.store.RefreshChannelCredential(ctx, d.workspaceID, d.channel.ID, "token", version, refresh)
		}()
	}
	wg.Wait()
	if calls.Load() != 1 || values[0] != "fresh" || values[1] != "fresh" || errs[0] != nil || errs[1] != nil {
		t.Fatalf("refresh calls=%d values=%v errs=%v, want one refresh both see", calls.Load(), values, errs)
	}
}

// A command cuts what is being collected: the messages before it go on their own (here to review,
// since they named no command) and the command starts a new request. Reopen adds a message to the
// customer's conversation it names.
func TestCommandCutsCollectedMessagesAndReopens(t *testing.T) {
	loop := channeltest.NewLoopback("loopback", channels.Async)
	loop.Defaults.Review = true
	d := newLoopDesk(t, loop)
	ctx := context.Background()
	d.send(t, channeltest.LoopbackEvent{ID: "r1", From: "u1", Text: "chào shop", Collect: true},
		channeltest.LoopbackEvent{ID: "r2", From: "u1", Text: "bao loi: không đăng nhập được", Collect: true})
	if _, err := d.pipeline.ProcessDue(ctx); err != nil { // the next run sends what the command closed
		t.Fatal(err)
	}
	if got := d.one(t, `SELECT string_agg(external_event_id||':'||state||':'||COALESCE(hold_reason,''),',' ORDER BY external_event_id) FROM channel_inbound_events WHERE workspace_id=$1`); got != "r1:held:review,r2:pending:" {
		t.Fatalf("after the command: %s, want the earlier message kept for review and the command collecting", got)
	}
	*d.clock = d.clock.Add(11 * time.Second)
	if _, err := d.pipeline.ProcessDue(ctx); err != nil {
		t.Fatal(err)
	}
	conversationID := d.one(t, `SELECT id::text FROM conversations WHERE workspace_id=$1 AND priority='high'`)
	if _, err := d.pool.Exec(ctx, `UPDATE conversations SET status='resolved',updated_at=now()-interval '30 days' WHERE id=$1`, conversationID); err != nil {
		t.Fatal(err)
	}
	d.send(t, channeltest.LoopbackEvent{ID: "r3", From: "u1", Text: "reopen " + conversationID})
	if got := d.one(t, `SELECT c.id::text||':'||c.status FROM messages m JOIN conversations c ON c.id=m.conversation_id WHERE m.workspace_id=$1 AND m.external_id='loopback:r3'`); got != conversationID+":open" {
		t.Fatalf("reopen landed in %s, want %s open again", got, conversationID)
	}
	d.send(t, channeltest.LoopbackEvent{ID: "r4", From: "u2", Text: "hủy"})
	if got := d.one(t, `SELECT state||':'||drop_reason FROM channel_inbound_events WHERE workspace_id=$1 AND external_event_id='r4'`); got != "dropped:cancelled" {
		t.Fatalf("cancel command: %s", got)
	}
}

// Groups and one-to-one messages follow their scope's rules: in a group only a command opens a
// conversation, chatter is kept without one, and the parser reads the group's organization from the
// type's snapshot. A scope the type does not declare is refused.
func TestScopesGiveGroupsTheirOwnRules(t *testing.T) {
	d := newLoopDesk(t, channeltest.NewLoopback("loopback", channels.Async))
	ctx := context.Background()
	settings := json.RawMessage(`{"scopes":{"group":{"mode":"commands"}},"loopback_groups":{"g1":"Acme Hospital"}}`)
	if _, err := d.store.UpdateChannel(ctx, d.workspaceID, d.channel.ID, store.ChannelInput{Settings: settings}, store.ChannelActor{AgentID: d.agentID}); err != nil {
		t.Fatal(err)
	}
	d.send(t, channeltest.LoopbackEvent{ID: "g-1", From: "m1", Group: "g1", Text: "good morning everyone"},
		channeltest.LoopbackEvent{ID: "g-2", From: "m1", Group: "g1", Text: "report bug: the printer is down"},
		channeltest.LoopbackEvent{ID: "g-3", From: "m2", Group: "g1", Text: "same here"},
		channeltest.LoopbackEvent{ID: "u-1", From: "m3", Text: "hello"})
	if got := d.one(t, `SELECT string_agg(external_event_id||':'||state||':'||(message_id IS NOT NULL),',' ORDER BY external_event_id) FROM channel_inbound_events WHERE workspace_id=$1`); got != "g-1:processed:false,g-2:processed:true,g-3:processed:true,u-1:processed:true" {
		t.Fatalf("journal=%s, want chatter archived, the command and its follow-up stored, the direct message stored", got)
	}
	if got := d.one(t, `SELECT count(*)::text FROM conversations WHERE workspace_id=$1`); got != "2" {
		t.Fatalf("conversations=%s, want the group's request and the direct message", got)
	}
	if got := d.one(t, `SELECT organization FROM contacts WHERE workspace_id=$1 AND name='m1'`); got != "Acme Hospital" {
		t.Fatalf("group member's organization=%q, want it from the snapshot", got)
	}
	bad := json.RawMessage(`{"scopes":{"page_comments":{"mode":"commands"}}}`)
	var fieldErr *channels.FieldError
	if _, err := d.store.UpdateChannel(ctx, d.workspaceID, d.channel.ID, store.ChannelInput{Settings: bad}, store.ChannelActor{}); !errors.As(err, &fieldErr) {
		t.Fatalf("undeclared scope: %v, want a field error", err)
	}
}

// A conversation keeps the scope its first message came in, which the channel's AI levels are read
// by; a type without scopes leaves it empty.
func TestAConversationKeepsTheScopeItStartedIn(t *testing.T) {
	d := newLoopDesk(t, channeltest.NewLoopback("loopback", channels.Async))
	d.send(t, channeltest.LoopbackEvent{ID: "s1", From: "u1", Text: "one to one"},
		channeltest.LoopbackEvent{ID: "s2", From: "u2", Group: "g1", Text: "in the group"})
	if got := d.one(t, `SELECT string_agg(m.body||'='||COALESCE(c.channel_scope,'-'),',' ORDER BY m.body) FROM conversations c
		JOIN messages m ON m.workspace_id=c.workspace_id AND m.conversation_id=c.id WHERE c.workspace_id=$1 AND m.kind='incoming'`); got != "in the group=group,one to one=user" {
		t.Fatalf("scopes = %q", got)
	}
}

// decisions lists each journal row as id:basis:rule ('-' for none), in id order.
func (d loopDesk) decisions(t *testing.T, ids ...string) string {
	t.Helper()
	return d.one(t, `SELECT string_agg(external_event_id||':'||COALESCE(decision_basis,'-')||':'||COALESCE(decision_rule,'-'),',' ORDER BY external_event_id)
		FROM channel_inbound_events WHERE workspace_id=$1 AND external_event_id=ANY($2)`, ids)
}

// The journal says what the channel's rules decided about every message they decided on, and on what
// basis; what the frame handled before them (a duplicate, a channel off) and what is still being
// collected carry no decision.
func TestTheJournalSaysWhatTheRulesDecided(t *testing.T) {
	d := newLoopDesk(t, channeltest.NewLoopback("loopback", channels.Async))
	ctx := context.Background()
	d.send(t, channeltest.LoopbackEvent{ID: "j-1", From: "u1", Text: "hello"},
		channeltest.LoopbackEvent{ID: "j-2", From: "u1", Text: "again"},
		channeltest.LoopbackEvent{ID: "j-3", From: "u2", Reference: "j-1", Text: "about that"},
		channeltest.LoopbackEvent{ID: "j-4", From: "u3", Text: "report bug: the printer is down"},
		channeltest.LoopbackEvent{ID: "j-5", From: "u4", Text: "never mind", Intent: channels.IntentCancel})
	if got := d.decisions(t, "j-1", "j-2", "j-3", "j-4", "j-5"); got != "j-1:default:new_thread,j-2:match:thread_key,j-3:match:thread_reference,j-4:match:command_open,j-5:match:command_cancel" {
		t.Fatalf("decisions = %s", got)
	}
	// A message whose join policy keeps it out of its old conversation opens another, by default.
	if _, err := d.pool.Exec(ctx, `UPDATE conversations SET status='resolved',updated_at=now()-interval '30 days' WHERE workspace_id=$1`, d.workspaceID); err != nil {
		t.Fatal(err)
	}
	d.send(t, channeltest.LoopbackEvent{ID: "j-6", From: "u1", Text: "back again"})
	if got := d.decisions(t, "j-6"); got != "j-6:default:join_policy" {
		t.Fatalf("decisions = %s", got)
	}
	settings := json.RawMessage(`{"scopes":{"group":{"mode":"commands"}}}`)
	if _, err := d.store.UpdateChannel(ctx, d.workspaceID, d.channel.ID, store.ChannelInput{Settings: settings}, store.ChannelActor{AgentID: d.agentID}); err != nil {
		t.Fatal(err)
	}
	d.send(t, channeltest.LoopbackEvent{ID: "g-1", From: "m1", Group: "g1", Text: "good morning"},
		channeltest.LoopbackEvent{ID: "g-2", From: "m1", Group: "g1", Text: "report bug: the printer"},
		channeltest.LoopbackEvent{ID: "g-3", From: "m2", Group: "g1", Text: "same here"},
		channeltest.LoopbackEvent{ID: "j-1", From: "u1", Text: "hello"})
	if got := d.decisions(t, "g-1", "g-2", "g-3"); got != "g-1:default:scope_archive,g-2:match:command_open,g-3:match:thread_key" {
		t.Fatalf("decisions = %s", got)
	}
	if got := d.one(t, `SELECT count(*)::text FROM channel_inbound_events WHERE workspace_id=$1 AND external_event_id='j-1'`); got != "1" {
		t.Fatalf("the duplicate wrote a row: %s", got)
	}
	// Collected messages without a command, held for review, and what arrives while the channel is off.
	review := json.RawMessage(`{"review":true}`)
	if _, err := d.store.UpdateChannel(ctx, d.workspaceID, d.channel.ID, store.ChannelInput{Settings: review}, store.ChannelActor{AgentID: d.agentID}); err != nil {
		t.Fatal(err)
	}
	d.send(t, channeltest.LoopbackEvent{ID: "r-1", From: "u9", Text: "is anyone there", Collect: true})
	if got := d.decisions(t, "r-1"); got != "r-1:-:-" {
		t.Fatalf("a message being collected = %s", got)
	}
	*d.clock = d.clock.Add(time.Minute)
	if _, err := d.pipeline.ProcessDue(ctx); err != nil {
		t.Fatal(err)
	}
	if got := d.decisions(t, "r-1"); got != "r-1:default:review_hold" {
		t.Fatalf("decisions = %s", got)
	}
	off := false
	if _, err := d.store.UpdateChannel(ctx, d.workspaceID, d.channel.ID, store.ChannelInput{Enabled: &off}, store.ChannelActor{AgentID: d.agentID}); err != nil {
		t.Fatal(err)
	}
	d.send(t, channeltest.LoopbackEvent{ID: "o-1", From: "u1", Text: "while off"})
	if got := d.decisions(t, "o-1"); got != "o-1:-:-" {
		t.Fatalf("held while off = %s", got)
	}
}

// Where several customers write in one place, a reply to another customer's message opens a request
// of its own by default (AI may decide it later); a reply to one's own message still continues it.
func TestAReplyToAnotherCustomerInASharedScopeIsNoMatch(t *testing.T) {
	loop := channeltest.NewLoopback("loopback", channels.Async)
	loop.SharedScopes = []string{"group"}
	d := newLoopDesk(t, loop)
	d.send(t, channeltest.LoopbackEvent{ID: "s-1", From: "m1", Group: "g1", Text: "the printer is down"},
		channeltest.LoopbackEvent{ID: "s-2", From: "m2", Group: "g1", Reference: "s-1", Text: "and mine is jammed"},
		channeltest.LoopbackEvent{ID: "s-3", From: "m1", Group: "g1", Reference: "s-1", Text: "still down"})
	if got := d.decisions(t, "s-1", "s-2", "s-3"); got != "s-1:default:new_thread,s-2:default:reference_other_customer,s-3:match:thread_reference" {
		t.Fatalf("decisions = %s", got)
	}
	if got := d.one(t, `SELECT count(*)::text FROM conversations WHERE workspace_id=$1`); got != "2" {
		t.Fatalf("conversations = %s", got)
	}
}
