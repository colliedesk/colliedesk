package mcpserver_test

import (
	"context"
	"testing"
)

// An agent with the draft tier moves a customer message into another conversation, lists the move from
// either side and undoes it; a read token's move waits for a person.
func TestAnAgentMovesAMessageAndUndoesIt(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("Move Tools Desk", "admin@move-tools.example.com")
	var from, to, message string
	if err := e.pool.QueryRow(context.Background(), `WITH p AS (INSERT INTO contacts(workspace_id,name,email) VALUES($1,'Kim','kim@customer.example') RETURNING id),
			a AS (INSERT INTO conversations(workspace_id,contact_id,subject) SELECT $1,p.id,'Printer' FROM p RETURNING id),
			b AS (INSERT INTO conversations(workspace_id,contact_id,subject) SELECT $1,p.id,'Invoice' FROM p RETURNING id),
			m AS (INSERT INTO messages(workspace_id,conversation_id,kind,body) SELECT $1,a.id,'incoming','The invoice is wrong' FROM a RETURNING id)
		SELECT a.id::text,b.id::text,m.id::text FROM a,b,m`, w.id).Scan(&from, &to, &message); err != nil {
		t.Fatal(err)
	}
	args := map[string]any{"conversation_id": from, "message_ids": []string{message}, "to": map[string]any{"conversation_id": to}}
	filed := e.ok(e.connect(w.read, ""), "colliedesk_messages_move", args)
	if _, waits := filed["agent_request"]; !waits {
		t.Fatalf("a read token moved: %v", filed)
	}
	draft := e.connect(w.draft, "")
	moved := e.ok(draft, "colliedesk_messages_move", args)
	id, _ := at(t, moved, "move", "id").(string)
	if id == "" || at(t, moved, "move", "to_conversation_id") != to {
		t.Fatalf("moved = %v", moved)
	}
	for _, side := range []string{from, to} {
		listed := e.ok(draft, "colliedesk_message_moves_list", map[string]any{"conversation_id": side})
		if items := at(t, listed, "items").([]any); len(items) != 1 {
			t.Fatalf("moves of %s = %v", side, listed)
		}
	}
	if undone := e.ok(draft, "colliedesk_message_move_undo", map[string]any{"move_id": id}); at(t, undone, "undone_at") == nil {
		t.Fatalf("undo = %v", undone)
	}
	var back string
	if err := e.pool.QueryRow(context.Background(), `SELECT conversation_id::text FROM messages WHERE id=$1`, message).Scan(&back); err != nil || back != from {
		t.Fatalf("after undo the message is in %s, %v", back, err)
	}
}
