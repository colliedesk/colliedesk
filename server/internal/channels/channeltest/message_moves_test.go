package channeltest_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/channels/channeltest"
	"github.com/colliedesk/colliedesk/server/internal/channels/outbox"
	"github.com/colliedesk/colliedesk/server/internal/intake"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// A reply sent on a chat channel names only its own customer's messages: one moved in from another
// customer, or taken back to the queue, is never what it answers.
func TestAChatReplyNeverAnswersAnotherCustomersMovedMessage(t *testing.T) {
	loop := channeltest.NewLoopback("loopback", channels.Async)
	d := newLoopDesk(t, loop)
	ctx := context.Background()
	actor := store.Actor{AgentID: d.agentID}
	d.send(t, channeltest.LoopbackEvent{ID: "u1-1", From: "u1", Text: "the printer is down"})
	d.send(t, channeltest.LoopbackEvent{ID: "u2-1", From: "u2", Text: "mine too"})
	conversationOf := `SELECT conversation_id::text FROM messages WHERE workspace_id=$1 AND external_id LIKE '%'||$2`
	first, second := d.one(t, conversationOf, "u1-1"), d.one(t, conversationOf, "u2-1")
	moved := d.one(t, `SELECT id::text FROM messages WHERE workspace_id=$1 AND external_id LIKE '%u2-1'`)
	if _, err := d.store.MoveMessages(ctx, d.workspaceID, second, store.MoveInput{MessageIDs: []string{moved}, To: store.MoveTarget{ConversationID: first}}, actor, *d.clock); err != nil {
		t.Fatal(err)
	}
	if _, err := d.store.CreateMessage(ctx, d.workspaceID, first, actor, "outgoing", "we are on it"); err != nil {
		t.Fatal(err)
	}
	clock := time.Now().UTC().Add(time.Minute)
	if _, err := outbox.New(d.store, outbox.Options{Now: func() time.Time { return clock }}).ProcessDue(ctx); err != nil {
		t.Fatal(err)
	}
	sent := loop.Endpoint.Sent()
	if len(sent) != 1 || sent[0].To != "u1" || slices.ContainsFunc(sent[0].References, func(id string) bool { return id == "u2-1" || id == "loopback:u2-1" }) {
		t.Fatalf("sent = %+v", sent)
	}
}

// A card of messages taken back to the queue and set aside stays set aside, whatever held messages of
// the channel are thrown away, and can be put back.
func TestDiscardingHeldMessagesLeavesASetAsideCardOfTakenBackMessages(t *testing.T) {
	d := newLoopDesk(t, intakeLoop())
	ctx := context.Background()
	actor := store.Actor{AgentID: d.agentID}
	d.send(t, channeltest.LoopbackEvent{ID: "a-1", From: "a", Group: "g1", Text: "the stock export fails"})
	d.flush(t)
	message := d.one(t, `SELECT id::text FROM messages WHERE workspace_id=$1 AND kind='incoming'`)
	source := d.one(t, `SELECT conversation_id::text FROM messages WHERE workspace_id=$1 AND id=$2::uuid`, message)
	back, err := d.store.MoveMessages(ctx, d.workspaceID, source, store.MoveInput{MessageIDs: []string{message}, To: store.MoveTarget{Queue: true}}, actor, *d.clock)
	if err != nil {
		t.Fatal(err)
	}
	card := *back.Move.GatheringID
	if _, err := d.intake().Decide(ctx, d.workspaceID, card, intake.Decision{Action: intake.Ignore}, store.DecidedByAgent, store.ChannelActor{AgentID: d.agentID}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.store.DiscardHeld(ctx, d.workspaceID, d.channel.ID, nil, store.ChannelActor{AgentID: d.agentID}); err != nil {
		t.Fatal(err)
	}
	if outcome, err := d.intake().Requeue(ctx, d.workspaceID, card, store.ChannelActor{AgentID: d.agentID}); err != nil || outcome.State != store.GatheringReview {
		t.Fatalf("put back = %+v, %v", outcome, err)
	}
}

// Undoing a move to the queue puts the message back without reopening a conversation it did not open.
func TestUndoingAMoveToTheQueueKeepsTheConversationsStatus(t *testing.T) {
	d := newLoopDesk(t, intakeLoop())
	ctx := context.Background()
	actor := store.Actor{AgentID: d.agentID}
	d.send(t, channeltest.LoopbackEvent{ID: "a-1", From: "a", Group: "g1", Text: "the stock export fails"})
	d.flush(t)
	message := d.one(t, `SELECT id::text FROM messages WHERE workspace_id=$1 AND kind='incoming'`)
	source := d.one(t, `SELECT conversation_id::text FROM messages WHERE workspace_id=$1 AND id=$2::uuid`, message)
	if _, err := d.pool.Exec(ctx, `UPDATE conversations SET status='resolved' WHERE id=$1`, source); err != nil {
		t.Fatal(err)
	}
	back, err := d.store.MoveMessages(ctx, d.workspaceID, source, store.MoveInput{MessageIDs: []string{message}, To: store.MoveTarget{Queue: true}}, actor, *d.clock)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.store.UndoMessageMove(ctx, d.workspaceID, back.Move.ID, actor, *d.clock); err != nil {
		t.Fatal(err)
	}
	if got := d.one(t, `SELECT status FROM conversations WHERE workspace_id=$1 AND id=$2::uuid`, source); got != "resolved" {
		t.Fatalf("status after undo = %s", got)
	}
}

// A card of messages taken back to the queue opened as a new request with a command gets the command's
// tags, as any card does; a command that opens no request is refused.
func TestOpeningATakenBackCardWithACommandTagsIt(t *testing.T) {
	d := newLoopDesk(t, intakeLoop())
	ctx := context.Background()
	actor := store.Actor{AgentID: d.agentID}
	d.send(t, channeltest.LoopbackEvent{ID: "a-1", From: "a", Group: "g1", Text: "the stock export fails"})
	d.flush(t)
	message := d.one(t, `SELECT id::text FROM messages WHERE workspace_id=$1 AND kind='incoming'`)
	source := d.one(t, `SELECT conversation_id::text FROM messages WHERE workspace_id=$1 AND id=$2::uuid`, message)
	back, err := d.store.MoveMessages(ctx, d.workspaceID, source, store.MoveInput{MessageIDs: []string{message}, To: store.MoveTarget{Queue: true}}, actor, *d.clock)
	if err != nil {
		t.Fatal(err)
	}
	card := *back.Move.GatheringID
	person := store.ChannelActor{AgentID: d.agentID}
	if _, err := d.intake().Decide(ctx, d.workspaceID, card, intake.Decision{Action: intake.Create, Command: "cancel"}, store.DecidedByAgent, person); err == nil {
		t.Fatal("a command that opens no request was taken")
	}
	opened, err := d.intake().Decide(ctx, d.workspaceID, card, intake.Decision{Action: intake.Create, Command: "bug"}, store.DecidedByAgent, person)
	if err != nil {
		t.Fatal(err)
	}
	if got := d.one(t, `SELECT string_agg(t.name,',')||'/'||c.priority FROM conversations c JOIN conversation_tags ct ON ct.conversation_id=c.id JOIN tags t ON t.id=ct.tag_id
		WHERE c.workspace_id=$1 AND c.id=$2::uuid GROUP BY c.priority`, opened.ConversationID); got != "bug/high" {
		t.Fatalf("tags/priority = %s", got)
	}
}

// A chat message moved to another conversation takes the thread key it recorded and its chat item: the
// customer's next message follows it, the conversations' chats show the item where it is, and undo
// gives both back.
func TestAMovedChatMessageTakesItsThreadKeyAndItem(t *testing.T) {
	d := newLoopDesk(t, channeltest.NewLoopback("loopback", channels.Async))
	ctx := context.Background()
	actor := store.Actor{AgentID: d.agentID}
	d.send(t, channeltest.LoopbackEvent{ID: "a-1", From: "u", Text: "the printer is down"})
	first := d.one(t, `SELECT id::text FROM messages WHERE workspace_id=$1 AND kind='incoming'`)
	source := d.one(t, `SELECT conversation_id::text FROM messages WHERE workspace_id=$1 AND id=$2::uuid`, first)
	moved, err := d.store.MoveMessages(ctx, d.workspaceID, source, store.MoveInput{MessageIDs: []string{first}, To: store.MoveTarget{NewSubject: "Printer"}}, actor, *d.clock)
	if err != nil {
		t.Fatal(err)
	}
	split := *moved.Move.ToConversationID
	d.send(t, channeltest.LoopbackEvent{ID: "a-2", From: "u", Text: "still down"})
	follow := `SELECT conversation_id::text FROM messages WHERE workspace_id=$1 AND body=$2`
	if got := d.one(t, follow, "still down"); got != split {
		t.Fatalf("the next message went to %s, want the split %s", got, split)
	}
	live := `SELECT COALESCE(string_agg(l.conversation_id::text,','),'') FROM conversation_thread_items l JOIN channel_thread_items i
		ON i.workspace_id=l.workspace_id AND i.id=l.item_id WHERE l.workspace_id=$1 AND i.message_id=$2::uuid AND l.unlinked_at IS NULL`
	if got := d.one(t, live, first); got != split {
		t.Fatalf("the moved item is linked to %s", got)
	}
	if _, err := d.store.UndoMessageMove(ctx, d.workspaceID, moved.Move.ID, actor, *d.clock); err != nil {
		t.Fatal(err)
	}
	if got := d.one(t, live, first); got != source {
		t.Fatalf("after undo the item is linked to %s", got)
	}
	d.send(t, channeltest.LoopbackEvent{ID: "a-3", From: "u", Text: "fixed?"})
	if got := d.one(t, follow, "fixed?"); got != source {
		t.Fatalf("after undo the next message went to %s, want %s", got, source)
	}
}

// A chat message taken back to the intake queue leaves the conversation, never deleted, as a card of
// its own (T-8): a reply quoting it no longer finds the conversation, undo puts it back while the card
// waits, and a person deciding the card moves the message where they say instead of storing it again.
func TestAChatMessageTakenBackToTheQueueIsDecidedAgain(t *testing.T) {
	d := newLoopDesk(t, intakeLoop())
	ctx := context.Background()
	actor := store.Actor{AgentID: d.agentID}
	d.send(t, channeltest.LoopbackEvent{ID: "a-1", From: "a", Group: "g1", Text: "the stock export fails"})
	d.flush(t)
	message := d.one(t, `SELECT id::text FROM messages WHERE workspace_id=$1 AND kind='incoming'`)
	source := d.one(t, `SELECT conversation_id::text FROM messages WHERE workspace_id=$1 AND id=$2::uuid`, message)
	back, err := d.store.MoveMessages(ctx, d.workspaceID, source, store.MoveInput{MessageIDs: []string{message}, To: store.MoveTarget{Queue: true}}, actor, *d.clock)
	if err != nil || back.Move.ToConversationID != nil || back.Move.GatheringID == nil {
		t.Fatalf("back to the queue = %+v, %v", back, err)
	}
	card := *back.Move.GatheringID
	if got := d.one(t, `SELECT state||'/'||review_reason||'/'||rules_rule||'/'||(requeued_by_agent_id=$3::uuid AND requeued_at IS NOT NULL)::text
		FROM intake_gatherings WHERE workspace_id=$1 AND id=$2::uuid`, card, d.agentID); got != "review/requeued/detached/true" {
		t.Fatalf("card = %s", got)
	}
	if listed, err := d.store.ListMessages(ctx, d.workspaceID, source); err != nil || len(listed) != 0 {
		t.Fatalf("timeline = %d, %v", len(listed), err)
	}
	if got := d.one(t, `SELECT (detached_at IS NOT NULL)::text||'/'||conversation_id::text FROM messages WHERE workspace_id=$1 AND id=$2::uuid`, message); got != "true/"+source {
		t.Fatalf("message = %s", got)
	}
	// The customer's next words quoting it are no reply to that conversation any more.
	d.send(t, channeltest.LoopbackEvent{ID: "a-2", From: "a", Group: "g1", Text: "any news?", Reference: "a-1"})
	d.flush(t)
	if got := d.one(t, `SELECT decision_rule FROM channel_inbound_events WHERE workspace_id=$1 AND external_event_id='a-2'`); got == "thread_reference" || got == "thread_key" {
		t.Fatalf("a quote of a taken back message still matched: %s", got)
	}
	// Undo while the card waits: back in the timeline, the card cancelled.
	if _, err := d.store.UndoMessageMove(ctx, d.workspaceID, back.Move.ID, actor, *d.clock); err != nil {
		t.Fatal(err)
	}
	if got := d.one(t, `SELECT state FROM intake_gatherings WHERE workspace_id=$1 AND id=$2::uuid`, card); got != "cancelled" {
		t.Fatalf("card after undo = %s", got)
	}
	if listed, _ := d.store.ListMessages(ctx, d.workspaceID, source); len(listed) != 1 {
		t.Fatalf("timeline after undo = %d", len(listed))
	}
	// Taken back again and decided: the message moves to the conversation the person chose, once.
	again, err := d.store.MoveMessages(ctx, d.workspaceID, source, store.MoveInput{MessageIDs: []string{message}, To: store.MoveTarget{Queue: true}}, actor, *d.clock)
	if err != nil {
		t.Fatal(err)
	}
	target := d.one(t, `SELECT conversation_id::text FROM messages WHERE workspace_id=$1 AND body='any news?'`)
	outcome, err := d.intake().Decide(ctx, d.workspaceID, *again.Move.GatheringID, intake.Decision{Action: intake.Attach, ConversationID: target}, store.DecidedByAgent,
		store.ChannelActor{AgentID: d.agentID})
	if err != nil || outcome.State != store.GatheringAttached || outcome.ConversationID != target {
		t.Fatalf("decide = %+v, %v", outcome, err)
	}
	if got := d.one(t, `SELECT count(*)::text||'/'||min(conversation_id::text)||'/'||bool_and(detached_at IS NULL)::text FROM messages WHERE workspace_id=$1 AND body='the stock export fails'`); got != "1/"+target+"/true" {
		t.Fatalf("messages after the decision = %s", got)
	}
	if _, err := d.store.UndoMessageMove(ctx, d.workspaceID, again.Move.ID, actor, *d.clock); !errors.Is(err, store.ErrMoveDecided) && !errors.Is(err, store.ErrMoveSuperseded) {
		t.Fatalf("undo a decided card's move = %v", err)
	}
	// Taken back from there and opened as a new request.
	third, err := d.store.MoveMessages(ctx, d.workspaceID, target, store.MoveInput{MessageIDs: []string{message}, To: store.MoveTarget{Queue: true}}, actor, *d.clock)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := d.intake().Decide(ctx, d.workspaceID, *third.Move.GatheringID, intake.Decision{Action: intake.Create}, store.DecidedByAgent, store.ChannelActor{AgentID: d.agentID})
	if err != nil || opened.State != store.GatheringCreated || opened.ConversationID == target || opened.ConversationID == source {
		t.Fatalf("create = %+v, %v", opened, err)
	}
	if got := d.one(t, `SELECT subject FROM conversations WHERE workspace_id=$1 AND id=$2::uuid`, opened.ConversationID); got != "the stock export fails" {
		t.Fatalf("subject = %q", got)
	}
}

// A decided card keeps showing what it decided on after its messages are taken back to the queue: who
// wrote, how many messages and the first words, in the list and in its detail.
func TestADecidedCardKeepsItsMessagesAfterTheyAreTakenBack(t *testing.T) {
	d := newLoopDesk(t, intakeLoop())
	ctx := context.Background()
	d.send(t, channeltest.LoopbackEvent{ID: "a-1", From: "a", Group: "g1", Text: "the stock export fails"})
	d.flush(t)
	card := d.gatheringOf(t, "a")
	message := d.one(t, `SELECT id::text FROM messages WHERE workspace_id=$1 AND kind='incoming'`)
	source := d.one(t, `SELECT conversation_id::text FROM messages WHERE workspace_id=$1 AND id=$2::uuid`, message)
	back, err := d.store.MoveMessages(ctx, d.workspaceID, source, store.MoveInput{MessageIDs: []string{message}, To: store.MoveTarget{Queue: true}},
		store.Actor{AgentID: d.agentID}, *d.clock)
	if err != nil {
		t.Fatal(err)
	}
	page, err := d.store.IntakeGatherings(ctx, d.workspaceID, store.IntakeListDecided, "", "", "", "", store.MaxIntakePage)
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(page.Items, func(g store.IntakeGathering) bool { return g.ID == card })
	if i < 0 {
		t.Fatalf("decided list = %+v, want card %s", page.Items, card)
	}
	if got := page.Items[i]; got.ItemCount != 1 || got.Sender.Value == nil || got.Preview != "the stock export fails" {
		t.Fatalf("decided card = count %d, sender %+v, preview %q", got.ItemCount, got.Sender, got.Preview)
	}
	if detail, err := d.store.IntakeGatheringByID(ctx, d.workspaceID, card, *d.clock); err != nil || len(detail.Items) != 1 {
		t.Fatalf("decided card detail = %d items, %v", len(detail.Items), err)
	}
	// The card of the taken back message shows it too, and only it.
	if detail, err := d.store.IntakeGatheringByID(ctx, d.workspaceID, *back.Move.GatheringID, *d.clock); err != nil || detail.ItemCount != 1 || len(detail.Items) != 1 {
		t.Fatalf("taken back card = count %d, %d items, %v", detail.ItemCount, len(detail.Items), err)
	}
}

// Moving messages AI added to a conversation records a person's decision that corrects AI's, for the
// "AI corrected" count and evaluation; moving messages a person added records none.
func TestMovingWhatAIAppliedRecordsACorrection(t *testing.T) {
	d := newLoopDesk(t, intakeLoop())
	d.intakeAt(t, "auto")
	ctx := context.Background()
	actor := store.Actor{AgentID: d.agentID}
	d.send(t, channeltest.LoopbackEvent{ID: "a-1", From: "a", Group: "g1", Text: "the stock export fails"})
	d.flush(t)
	card := d.gatheringOf(t, "a")
	opened, err := d.intake().Decide(ctx, d.workspaceID, card, intake.Decision{Action: intake.Create}, store.DecidedByAI, store.ChannelActor{})
	if err != nil {
		t.Fatal(err)
	}
	message := d.one(t, `SELECT id::text FROM messages WHERE workspace_id=$1 AND conversation_id=$2::uuid AND kind='incoming'`, opened.ConversationID)
	moved, err := d.store.MoveMessages(ctx, d.workspaceID, opened.ConversationID, store.MoveInput{MessageIDs: []string{message},
		To: store.MoveTarget{NewSubject: "Stock export"}}, actor, *d.clock)
	if err != nil || moved.Move.DecisionID == nil {
		t.Fatalf("move = %+v, %v", moved, err)
	}
	correction := `SELECT d.decider||'/'||d.action||'/'||(d.corrects_decision_id=ai.id)::text||'/'||cardinality(d.items_seen)::text FROM intake_decisions d, intake_decisions ai
		WHERE d.workspace_id=$1 AND d.id=$2::uuid AND ai.workspace_id=$1 AND ai.decider='ai'`
	if got := d.one(t, correction, *moved.Move.DecisionID); got != "agent/create/true/1" {
		t.Fatalf("correction = %s", got)
	}
	again, err := d.store.MoveMessages(ctx, d.workspaceID, *moved.Move.ToConversationID, store.MoveInput{MessageIDs: []string{message},
		To: store.MoveTarget{ConversationID: opened.ConversationID}}, actor, *d.clock)
	if err != nil || again.Move.DecisionID != nil {
		t.Fatalf("a person's own placement corrected again: %+v, %v", again, err)
	}
}

// A resolved conversation a message left is built again for AI retrieval, as is one it joins.
func TestMovingAMessageRebuildsTheRetrievalOfResolvedConversations(t *testing.T) {
	d := newLoopDesk(t, channeltest.NewLoopback("loopback", channels.Async))
	ctx := context.Background()
	d.send(t, channeltest.LoopbackEvent{ID: "a-1", From: "u", Text: "the printer is down"})
	message := d.one(t, `SELECT id::text FROM messages WHERE workspace_id=$1 AND kind='incoming'`)
	source := d.one(t, `SELECT conversation_id::text FROM messages WHERE workspace_id=$1 AND id=$2::uuid`, message)
	if _, err := d.pool.Exec(ctx, `UPDATE conversations SET status='resolved' WHERE id=$1`, source); err != nil {
		t.Fatal(err)
	}
	if _, err := d.pool.Exec(ctx, `DELETE FROM ai_retrieval_pending WHERE workspace_id=$1`, d.workspaceID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.store.MoveMessages(ctx, d.workspaceID, source, store.MoveInput{MessageIDs: []string{message}, To: store.MoveTarget{NewSubject: "Printer"}},
		store.Actor{AgentID: d.agentID}, *d.clock); err != nil {
		t.Fatal(err)
	}
	if got := d.one(t, `SELECT count(*)::text FROM ai_retrieval_pending WHERE workspace_id=$1 AND conversation_id=$2::uuid`, source); got == "0" {
		t.Fatal("the resolved source was not queued for retrieval")
	}
}

// A card of messages taken back to the queue joins only a conversation of its place: another customer's
// email thread is refused and the messages stay in the queue; their own conversation takes them back.
func TestATakenBackCardJoinsOnlyAConversationOfItsPlace(t *testing.T) {
	d := newLoopDesk(t, intakeLoop())
	ctx := context.Background()
	actor := store.Actor{AgentID: d.agentID}
	d.send(t, channeltest.LoopbackEvent{ID: "a-1", From: "a", Group: "g1", Text: "the stock export fails"})
	d.flush(t)
	message := d.one(t, `SELECT id::text FROM messages WHERE workspace_id=$1 AND kind='incoming'`)
	source := d.one(t, `SELECT conversation_id::text FROM messages WHERE workspace_id=$1 AND id=$2::uuid`, message)
	back, err := d.store.MoveMessages(ctx, d.workspaceID, source, store.MoveInput{MessageIDs: []string{message}, To: store.MoveTarget{Queue: true}}, actor, *d.clock)
	if err != nil {
		t.Fatal(err)
	}
	card := *back.Move.GatheringID
	other := d.one(t, `WITH c AS (INSERT INTO contacts(workspace_id,name,email) VALUES($1,'Other','other@example.com') RETURNING id)
		INSERT INTO conversations(workspace_id,contact_id,subject,status) SELECT $1,c.id,'Invoice','resolved' FROM c RETURNING id::text`)
	who := store.ChannelActor{AgentID: d.agentID}
	if _, err := d.intake().Decide(ctx, d.workspaceID, card, intake.Decision{Action: intake.Attach, ConversationID: other}, store.DecidedByAgent, who); !errors.Is(err, store.ErrNotACandidate) {
		t.Fatalf("join another customer's email thread: %v", err)
	}
	if got := d.one(t, `SELECT state||':'||(SELECT count(*) FROM messages WHERE workspace_id=$1 AND conversation_id='`+other+`')::text FROM intake_gatherings WHERE workspace_id=$1 AND id='`+card+`'`); got != "review:0" {
		t.Fatalf("after the refusal: %s", got)
	}
	if _, err := d.intake().Decide(ctx, d.workspaceID, card, intake.Decision{Action: intake.Attach, ConversationID: source}, store.DecidedByAgent, who); err != nil {
		t.Fatalf("join its own conversation: %v", err)
	}
}
