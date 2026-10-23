package mcpserver_test

import (
	"context"
	"testing"
)

// An AI agent accepts a triage suggestion with its ids alone: that is the rest of it, applied to the
// conversation like a person's change; then one part of the next by name.
func TestDraftTokenAcceptsATriageSuggestion(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("Triage Desk", "admin@triage.example.com")
	ctx := context.Background()
	var conversation, tag, suggestion string
	if err := e.pool.QueryRow(ctx, `WITH ct AS (INSERT INTO contacts(workspace_id,name,email) VALUES($1,'Lin','lin@customer.example') RETURNING id)
		INSERT INTO conversations(workspace_id,contact_id,subject) SELECT $1,id,'Printer down' FROM ct RETURNING id::text`, w.id).Scan(&conversation); err != nil {
		t.Fatal(err)
	}
	if err := e.pool.QueryRow(ctx, `INSERT INTO tags(workspace_id,name) VALUES($1,'hardware') RETURNING id::text`, w.id).Scan(&tag); err != nil {
		t.Fatal(err)
	}
	insert := func(priority string) string {
		var id string
		if err := e.pool.QueryRow(ctx, `INSERT INTO ai_triage_suggestions(workspace_id,conversation_id,status,tag_ids,priority,reason,ready_at)
			VALUES($1,$2,'ready',ARRAY[$3::uuid],$4,'The printer is down.',now()) RETURNING id::text`, w.id, conversation, tag, priority).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	suggestion = insert("urgent")
	session := e.connect(w.draft, "")
	got := e.ok(session, "colliedesk_ai_triage_get", map[string]any{"conversation_id": conversation})
	if at(t, got, "suggestion", "id") != suggestion {
		t.Fatalf("get = %v", got)
	}
	accepted := e.ok(session, "colliedesk_ai_triage_accept", map[string]any{"conversation_id": conversation, "suggestion_id": suggestion})
	if at(t, accepted, "suggestion", "outcome") != "accepted" {
		t.Fatalf("accept with ids alone = %v", accepted)
	}
	var priority string
	var tags int
	if err := e.pool.QueryRow(ctx, `SELECT priority,(SELECT count(*) FROM conversation_tags WHERE conversation_id=$1) FROM conversations WHERE id=$1`, conversation).Scan(&priority, &tags); err != nil {
		t.Fatal(err)
	}
	if priority != "urgent" || tags != 1 {
		t.Fatalf("conversation after accept = %s, %d tags", priority, tags)
	}

	next := insert("low")
	part := e.ok(session, "colliedesk_ai_triage_accept", map[string]any{"conversation_id": conversation, "suggestion_id": next, "priority": "low"})
	if at(t, part, "suggestion", "outcome") != nil || at(t, part, "suggestion", "applied", "priority") != "low" {
		t.Fatalf("accept one part = %v", part)
	}
}

// An AI agent undoes what AI applied of a suggestion by itself, and an agent of another workspace cannot.
func TestDraftTokenUndoesWhatAIApplied(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("Undo Desk", "admin@undo.example.com")
	other := e.newWorkspace("Other Desk", "admin@other-undo.example.com")
	ctx := context.Background()
	var conversation, tag, suggestion string
	if err := e.pool.QueryRow(ctx, `WITH ct AS (INSERT INTO contacts(workspace_id,name,email) VALUES($1,'Lin','lin@customer.example') RETURNING id)
		INSERT INTO conversations(workspace_id,contact_id,subject,priority) SELECT $1,id,'Printer down','urgent' FROM ct RETURNING id::text`, w.id).Scan(&conversation); err != nil {
		t.Fatal(err)
	}
	if err := e.pool.QueryRow(ctx, `INSERT INTO tags(workspace_id,name) VALUES($1,'hardware') RETURNING id::text`, w.id).Scan(&tag); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(ctx, `INSERT INTO conversation_tags(workspace_id,conversation_id,tag_id) VALUES($1,$2,$3)`, w.id, conversation, tag); err != nil {
		t.Fatal(err)
	}
	set := `{"tag_ids":["` + tag + `"],"priority":"urgent","assignee_agent_id":null}`
	if err := e.pool.QueryRow(ctx, `INSERT INTO ai_triage_suggestions(workspace_id,conversation_id,status,automatic,tag_ids,priority,base,ready_at,applied,auto_applied,auto_applied_at,outcome,decided_at)
		VALUES($1,$2,'ready',true,ARRAY[$3::uuid],'urgent','{"priority":"normal","assignee_agent_id":null}',now(),$4,$4,now(),'auto_applied',now()) RETURNING id::text`,
		w.id, conversation, tag, set).Scan(&suggestion); err != nil {
		t.Fatal(err)
	}
	if message := e.fails(e.connect(other.draft, ""), "colliedesk_ai_triage_undo", map[string]any{"conversation_id": conversation, "suggestion_id": suggestion}); message == "" {
		t.Fatal("another workspace undid it")
	}
	undone := e.ok(e.connect(w.draft, ""), "colliedesk_ai_triage_undo", map[string]any{"conversation_id": conversation, "suggestion_id": suggestion})
	if at(t, undone, "suggestion", "outcome") != "undone" {
		t.Fatalf("undo = %v", undone)
	}
	var priority string
	var tags int
	if err := e.pool.QueryRow(ctx, `SELECT priority,(SELECT count(*) FROM conversation_tags WHERE conversation_id=$1) FROM conversations WHERE id=$1`, conversation).Scan(&priority, &tags); err != nil {
		t.Fatal(err)
	}
	if priority != "normal" || tags != 0 {
		t.Fatalf("conversation after undo = %s, %d tags", priority, tags)
	}
}
