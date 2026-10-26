package channeltest_test

import (
	"context"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/channels/channeltest"
	"github.com/colliedesk/colliedesk/server/internal/intake"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

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
