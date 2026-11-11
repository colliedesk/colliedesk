package mcpserver_test

import (
	"context"
	"testing"
)

// An AI agent reading a conversation tells the channel's own messages (a received or status notice) from
// the team's replies, as a person does on the timeline.
func TestAConversationReadByAnAgentNamesTheChannelsOwnMessages(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("Notice Desk", "admin@notice.example.com")
	ctx := context.Background()
	var conversation string
	if err := e.pool.QueryRow(ctx, `WITH ct AS (INSERT INTO contacts(workspace_id,name,email) VALUES($1,'Lin','lin@customer.example') RETURNING id)
		INSERT INTO conversations(workspace_id,contact_id,subject) SELECT $1,id,'Printer down' FROM ct RETURNING id::text`, w.id).Scan(&conversation); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(ctx, `INSERT INTO messages(workspace_id,conversation_id,kind,body,notice_kind,created_at) VALUES
		($1,$2,'outgoing','REF-1: fixed.','status_resolved',now()),($1,$2,'outgoing','On it',NULL,now()+interval '1 second')`, w.id, conversation); err != nil {
		t.Fatal(err)
	}
	got := e.ok(e.connect(w.read, ""), "colliedesk_conversation_get", map[string]any{"conversation_id": conversation})
	messages, _ := got["messages"].([]any)
	if len(messages) != 2 {
		t.Fatalf("messages = %v", got["messages"])
	}
	if first, second := messages[0].(map[string]any), messages[1].(map[string]any); first["notice_kind"] != "status_resolved" || second["notice_kind"] != nil {
		t.Fatalf("notice kinds = %v / %v", first["notice_kind"], second["notice_kind"])
	}
}
