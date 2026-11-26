package zalo_test

import (
	"context"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/store"
)

// addStaff puts more Zalo users on the channel's team list, as an admin does on its Team tab.
func (a at) addStaff(t *testing.T, config string) {
	t.Helper()
	if _, err := a.store.UpdateChannel(context.Background(), a.workspaceID, a.channel.ID,
		store.ChannelInput{Config: []byte(config)}, store.ChannelActor{AgentID: a.agentID}); err != nil {
		t.Fatal(err)
	}
}

// listenToGroups makes every message in a group open or join a conversation, as a team supporting
// its customers in their groups sets it.
func (a *at) listenToGroups(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	if _, err := a.store.UpdateChannel(ctx, a.workspaceID, a.channel.ID, store.ChannelInput{Settings: []byte(`{"scopes":{"group":{"mode":"all"}},"notices":{"status_closed":"Closed {ref}."}}`)},
		store.ChannelActor{AgentID: a.agentID}); err != nil {
		t.Fatal(err)
	}
	channel, err := a.store.GetChannel(ctx, a.workspaceID, a.channel.ID)
	if err != nil {
		t.Fatal(err)
	}
	a.channel = channel
}

// A team member who wrote in a group before they were on the team list opened a conversation that is
// nobody's request. Adding them closes it without a word to the group, and leaves the customers' own open.
func TestAddingATeamMemberClosesTheConversationsOnlyTheyWroteQuietly(t *testing.T) {
	a := newAt(t)
	a.listenToGroups(t)
	oa := a.channel.ExternalAccount
	a.receive(t, event("user_send_group_text", oa, "1110005", "g-1", "m-team", "let me look"),
		event("user_send_text", oa, "1110002", oa, "m-customer", "Hello, the invoice is wrong"))
	team := a.one(t, `SELECT conversation_id::text FROM messages WHERE workspace_id=$1 AND external_id='zalo:m-team'`)
	customer := a.one(t, `SELECT conversation_id::text FROM messages WHERE workspace_id=$1 AND external_id='zalo:m-customer'`)
	a.deliver(t)
	sent := len(a.zalo.sends())

	a.addStaff(t, `{"staff_user_ids":["5550001","1110005"]}`)
	a.deliver(t)

	if got := a.one(t, `SELECT status FROM conversations WHERE workspace_id=$1 AND id=$2`, team); got != "closed" {
		t.Fatalf("team member's conversation = %s, want closed", got)
	}
	if got := a.one(t, `SELECT status FROM conversations WHERE workspace_id=$1 AND id=$2`, customer); got != "open" {
		t.Fatalf("customer's conversation = %s, want open", got)
	}
	if got := len(a.zalo.sends()); got != sent {
		t.Fatalf("sends = %d after adding the team member, want %d: the group must not hear of it", got, sent)
	}
	if got := a.one(t, `SELECT (detail->>'closed_conversations') FROM channel_events WHERE workspace_id=$1 AND kind='config_changed' ORDER BY seq DESC LIMIT 1`); got != "1" {
		t.Fatalf("history says %s conversations closed, want 1", got)
	}
	if got := a.one(t, `SELECT quiet::text||','||COALESCE(actor_agent_id::text,'') FROM conversation_status_changes
		WHERE workspace_id=$1 AND conversation_id=$2 AND to_status='closed'`, team); got != "true,"+a.agentID {
		t.Fatalf("status history = %s, want a quiet close by the admin who added the team member", got)
	}
}

// A conversation a customer wrote in too is a request: adding the team member leaves it open.
func TestAddingATeamMemberLeavesAConversationACustomerWroteInOpen(t *testing.T) {
	a := newAt(t)
	a.listenToGroups(t)
	oa := a.channel.ExternalAccount
	a.receive(t, event("user_send_group_text", oa, "1110005", "g-1", "m-team", "let me look"),
		event("user_send_group_text", oa, "1110001", "g-1", "m-customer", "the scale prints nothing"))
	team := a.one(t, `SELECT conversation_id::text FROM messages WHERE workspace_id=$1 AND external_id='zalo:m-team'`)
	from := a.one(t, `SELECT conversation_id::text FROM messages WHERE workspace_id=$1 AND external_id='zalo:m-customer'`)
	message := a.one(t, `SELECT id::text FROM messages WHERE workspace_id=$1 AND external_id='zalo:m-customer'`)
	// A person put the customer's message with the team member's, as the inbox's Move does.
	if _, err := a.store.MoveMessages(context.Background(), a.workspaceID, from, store.MoveInput{MessageIDs: []string{message},
		To: store.MoveTarget{ConversationID: team}, CloseSource: true}, store.Actor{AgentID: a.agentID}, time.Now()); err != nil {
		t.Fatal(err)
	}

	a.addStaff(t, `{"staff_user_ids":["5550001","1110005"]}`)

	if got := a.one(t, `SELECT status FROM conversations WHERE workspace_id=$1 AND id=$2`, team); got != "open" {
		t.Fatalf("conversation = %s, want open: a customer wrote in it", got)
	}
}

// status is a conversation's status, by the external id of one of its messages.
func (a at) status(t *testing.T, messageExternalID string) string {
	t.Helper()
	return a.one(t, `SELECT c.status FROM conversations c JOIN messages m ON m.workspace_id=c.workspace_id AND m.conversation_id=c.id
		WHERE c.workspace_id=$1 AND m.external_id=$2`, "zalo:"+messageExternalID)
}

// Adding someone by mistake must not close their own requests: what they wrote to the OA alone, and
// a group conversation the team already took on (assigned or answered), stay open.
func TestAddingAPersonLeavesTheirOneToOneChatsAndConversationsTheTeamTookOnOpen(t *testing.T) {
	a := newAt(t)
	a.listenToGroups(t)
	oa := a.channel.ExternalAccount
	a.receive(t, event("user_send_text", oa, "1110007", oa, "m-direct", "my order is late"))
	a.receive(t, event("user_send_group_text", oa, "1110007", "g-1", "m-assigned", "the invoice is wrong"))
	assigned := a.one(t, `SELECT conversation_id::text FROM messages WHERE workspace_id=$1 AND external_id='zalo:m-assigned'`)
	me := a.agentID
	if _, err := a.store.UpdateConversation(context.Background(), a.workspaceID, assigned, store.ConversationUpdate{AssigneeID: func() **string { p := &me; return &p }(), ActorID: a.agentID}); err != nil {
		t.Fatal(err)
	}

	a.addStaff(t, `{"staff_user_ids":["5550001","1110007"]}`)

	for _, id := range []string{"m-direct", "m-assigned"} {
		if got := a.status(t, id); got != "open" {
			t.Fatalf("%s conversation = %s, want open", id, got)
		}
	}
}

// Two team members who wrote in one conversation: adding the second closes it, as both are on the list.
func TestAddingTheSecondTeamMemberClosesTheConversationBothWroteIn(t *testing.T) {
	a := newAt(t)
	a.listenToGroups(t)
	oa := a.channel.ExternalAccount
	a.receive(t, event("user_send_group_text", oa, "1110005", "g-1", "m-one", "let me look"),
		event("user_send_group_text", oa, "1110006", "g-1", "m-two", "I'll check too"))
	one := a.one(t, `SELECT conversation_id::text FROM messages WHERE workspace_id=$1 AND external_id='zalo:m-one'`)
	from := a.one(t, `SELECT conversation_id::text FROM messages WHERE workspace_id=$1 AND external_id='zalo:m-two'`)
	if from != one {
		message := a.one(t, `SELECT id::text FROM messages WHERE workspace_id=$1 AND external_id='zalo:m-two'`)
		if _, err := a.store.MoveMessages(context.Background(), a.workspaceID, from, store.MoveInput{MessageIDs: []string{message},
			To: store.MoveTarget{ConversationID: one}, CloseSource: true}, store.Actor{AgentID: a.agentID}, time.Now()); err != nil {
			t.Fatal(err)
		}
	}

	a.addStaff(t, `{"staff_user_ids":["5550001","1110005"]}`)
	if got := a.status(t, "m-one"); got != "open" {
		t.Fatalf("after the first = %s, want open: the second is not on the list yet", got)
	}
	a.addStaff(t, `{"staff_user_ids":["5550001","1110005","1110006"]}`)
	if got := a.status(t, "m-one"); got != "closed" {
		t.Fatalf("after the second = %s, want closed", got)
	}
}

// A customer message whose sender is unknown (an imported one) is still a customer's: the conversation stays open.
func TestAConversationWithAnUnknownSendersMessageStaysOpen(t *testing.T) {
	a := newAt(t)
	a.listenToGroups(t)
	oa := a.channel.ExternalAccount
	a.receive(t, event("user_send_group_text", oa, "1110005", "g-1", "m-team", "let me look"),
		event("user_send_group_text", oa, "1110001", "g-1", "m-customer", "the scale prints nothing"))
	team := a.one(t, `SELECT conversation_id::text FROM messages WHERE workspace_id=$1 AND external_id='zalo:m-team'`)
	from := a.one(t, `SELECT conversation_id::text FROM messages WHERE workspace_id=$1 AND external_id='zalo:m-customer'`)
	message := a.one(t, `SELECT id::text FROM messages WHERE workspace_id=$1 AND external_id='zalo:m-customer'`)
	if _, err := a.store.MoveMessages(context.Background(), a.workspaceID, from, store.MoveInput{MessageIDs: []string{message},
		To: store.MoveTarget{ConversationID: team}, CloseSource: true}, store.Actor{AgentID: a.agentID}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := a.pool.Exec(context.Background(), `UPDATE channel_thread_items SET sender_kind=NULL,sender_value=NULL WHERE workspace_id=$1 AND message_id=$2`, a.workspaceID, message); err != nil {
		t.Fatal(err)
	}

	a.addStaff(t, `{"staff_user_ids":["5550001","1110005"]}`)

	if got := a.status(t, "m-team"); got != "open" {
		t.Fatalf("conversation = %s, want open: an unknown customer wrote in it", got)
	}
}
