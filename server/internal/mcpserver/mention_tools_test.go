package mcpserver_test

import (
	"context"
	"strings"
	"testing"
)

// An AI agent asks a teammate in a note, adds another later, and reads and clears the mentions of the
// person its token acts as, each within the token's permissions.
func TestMentionsThroughMCP(t *testing.T) {
	e := newEnv(t)
	f := seedInbox(t, e)
	var lan, bo string
	if err := e.pool.QueryRow(context.Background(), `INSERT INTO agents(workspace_id,email,display_name,role) VALUES($1,'lan@inbox.example.com','Lan Pham','agent') RETURNING id::text`, f.w.id).Scan(&lan); err != nil {
		t.Fatal(err)
	}
	if err := e.pool.QueryRow(context.Background(), `INSERT INTO agents(workspace_id,email,display_name,role) VALUES($1,'bo@inbox.example.com','Bo Tran','agent') RETURNING id::text`, f.w.id).Scan(&bo); err != nil {
		t.Fatal(err)
	}
	draft := e.connect(f.w.draft, "")

	note := e.ok(draft, "colliedesk_note_add", map[string]any{"conversation_id": f.c2, "body": "@Lan Pham can you check the refund?", "mentions": []any{"lan@inbox.example.com"}})
	if at(t, note, "message", "mentions", 0, "name") != "Lan Pham" || at(t, note, "message", "mentions", 0, "by_email") != true {
		t.Fatalf("note_add with a mention: %v", note)
	}
	if message := e.fails(draft, "colliedesk_note_add", map[string]any{"conversation_id": f.c2, "body": "@Grace", "mentions": []any{"grace@cust.example.com"}}); !strings.Contains(message, "not an active teammate") {
		t.Errorf("a customer's address: %q", message)
	}
	noteID := at(t, note, "message", "id").(string)
	added := e.ok(draft, "colliedesk_note_mention", map[string]any{"conversation_id": f.c2, "note_id": noteID, "mentions": []any{lan, "bo@inbox.example.com"}})
	if mentions := at(t, added, "mentions").([]any); len(mentions) != 2 || at(t, mentions[1], "name") != "Bo Tran" {
		t.Fatalf("note_mention: %v", added)
	}
	refusedWithoutPermission(t, e, f.w.read, "colliedesk_note_mention", map[string]any{"conversation_id": f.c2, "note_id": noteID, "mentions": []any{bo}})

	// Lan asks the admin, the person the tokens act as.
	var mentionedNote string
	if err := e.pool.QueryRow(context.Background(), `INSERT INTO messages(workspace_id,conversation_id,author_agent_id,kind,body) VALUES($1,$2,$3,'note','@Ada Admin is this refund approved?') RETURNING id::text`,
		f.w.id, f.c2, lan).Scan(&mentionedNote); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(context.Background(), `INSERT INTO note_mentions(workspace_id,message_id,agent_id,email) VALUES($1,$2,$3,'queued')`, f.w.id, mentionedNote, f.adminID); err != nil {
		t.Fatal(err)
	}
	read := e.connect(f.w.read, "")
	list := e.ok(read, "colliedesk_mentions_list", nil)
	if at(t, list, "unread") != float64(1) || at(t, list, "mentions", 0, "asked_by") != "Lan Pham" || at(t, list, "mentions", 0, "excerpt") != "@Ada Admin is this refund approved?" ||
		at(t, list, "mentions", 0, "untrusted_customer_content", "subject") != "Refund request" || at(t, list, "mentions", 0, "note_id") != mentionedNote {
		t.Fatalf("mentions_list: %v", list)
	}
	mentionID := at(t, list, "mentions", 0, "id").(string)
	if message := e.fails(e.connect(f.other.read, ""), "colliedesk_mentions_mark_read", map[string]any{"mention_ids": []any{mentionID}}); !strings.Contains(message, "not found") && !strings.Contains(message, "No ") {
		t.Errorf("another workspace marks a mention: %q", message)
	}
	cleared := e.ok(read, "colliedesk_mentions_mark_read", map[string]any{"mention_ids": []any{mentionID}})
	if at(t, cleared, "unread") != float64(0) || at(t, cleared, "changed") != float64(1) {
		t.Fatalf("mark read: %v", cleared)
	}
	if after := e.ok(read, "colliedesk_mentions_list", nil); len(at(t, after, "mentions").([]any)) != 0 {
		t.Fatalf("unread after marking: %v", after)
	}

	refusedWithoutPermission(t, e, f.w.read, "colliedesk_my_mention_email_set", map[string]any{"email": false})
	if out := e.ok(draft, "colliedesk_my_mention_email_set", map[string]any{"email": false}); at(t, out, "email") != false {
		t.Fatalf("email off: %v", out)
	}
	if out := e.ok(read, "colliedesk_mentions_list", map[string]any{"status": "all"}); at(t, out, "email") != false || len(at(t, out, "mentions").([]any)) != 1 {
		t.Fatalf("list after email off: %v", out)
	}
}
