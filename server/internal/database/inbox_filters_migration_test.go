package database

import (
	"context"
	"strings"
	"testing"
	"time"
)

const inboxFiltersMigration = "20261019120000-inbox-filters.up.sql"

// The channel of an existing conversation is read from what it already holds, the newest public
// message from its timeline, and the triggers keep that one right for every later write.
func TestInboxFiltersMigrationBackfillsChannelAndWaitingStateWithoutLosingRows(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `CREATE TABLE schema_migrations (name text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		t.Fatal(err)
	}
	for _, name := range upMigrations(t) {
		if name < inboxFiltersMigration {
			if err := applyMigration(ctx, pool, name); err != nil {
				t.Fatal(err)
			}
		}
	}
	var workspace, contact string
	if err := pool.QueryRow(ctx, `WITH w AS (INSERT INTO workspaces(slug,name) VALUES('filters-a','A') RETURNING id),
		c AS (INSERT INTO contacts(workspace_id,name,email) SELECT id,'Ada','ada@example.com' FROM w RETURNING id,workspace_id)
		SELECT workspace_id::text,id::text FROM c`).Scan(&workspace, &contact); err != nil {
		t.Fatal(err)
	}
	conversation := func(subject, origin string) string {
		t.Helper()
		var id string
		if err := pool.QueryRow(ctx, `INSERT INTO conversations(workspace_id,contact_id,subject,origin) VALUES($1,$2,$3,NULLIF($4,'')::jsonb) RETURNING id::text`,
			workspace, contact, subject, origin).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	message := func(conversationID, kind, meta, emailID string, minute int) {
		t.Helper()
		// The Message-ID is email_message_id before the channels migration, external_id after it.
		column, prefix := "email_message_id", ""
		var moved bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema=current_schema()
			AND table_name='messages' AND column_name='external_id')`).Scan(&moved); err != nil {
			t.Fatal(err)
		}
		if moved {
			column, prefix = "external_id", "email:"
		}
		if _, err := pool.Exec(ctx, `INSERT INTO messages(workspace_id,conversation_id,kind,body,email_meta,`+column+`,created_at)
			VALUES($1,$2,$3,'text',NULLIF($4,'')::jsonb,NULLIF($7||$5,$7),'2026-10-01T08:00:00Z'::timestamptz+make_interval(mins=>$6))`,
			workspace, conversationID, kind, meta, emailID, minute, prefix); err != nil {
			t.Fatal(err)
		}
	}
	imported := conversation("Imported", `{"system":"freshdesk","external_id":"7"}`)
	message(imported, "incoming", `{"from":"ada@example.com","authentication":"pass","via":"workspace"}`, "<a@x>", 1)
	email := conversation("Mail", "")
	message(email, "incoming", `{"from":"ada@example.com","authentication":"pass","via":"workspace"}`, "<b@x>", 1)
	message(email, "outgoing", "", "", 2)
	message(email, "note", "", "", 3)
	oldEmail := conversation("Old mail", "")
	message(oldEmail, "incoming", `{"from":"ada@example.com","authentication":"pass"}`, "<c@x>", 1)
	webForm := conversation("Web form: pricing", "")
	message(webForm, "incoming", `{"from":"ada@example.com","authentication":"none","via":"web_form"}`, "", 1)
	oldWebForm := conversation("Web form", "")
	message(oldWebForm, "incoming", "", "", 1)
	report := conversation("Problem", "")
	message(report, "incoming", `{"from":"ada@example.com","authentication":"pass","via":"problem_report"}`, "", 1)
	started := conversation("Team started", "")
	message(started, "outgoing", "", "", 1)
	message(started, "incoming", `{"from":"ada@example.com","authentication":"pass","via":"workspace"}`, "<d@x>", 5)
	empty := conversation("No messages", "")
	notes := conversation("Only notes", "")
	message(notes, "note", "", "", 1)

	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	want := map[string][3]string{
		imported:   {"imported", "incoming", "2026-10-01T08:01"},
		email:      {"email", "outgoing", "2026-10-01T08:02"},
		oldEmail:   {"email", "incoming", "2026-10-01T08:01"},
		webForm:    {"web_form", "incoming", "2026-10-01T08:01"},
		oldWebForm: {"web_form", "incoming", "2026-10-01T08:01"},
		report:     {"problem_report", "incoming", "2026-10-01T08:01"},
		started:    {"agent", "incoming", "2026-10-01T08:05"},
		empty:      {"agent", "", ""},
		notes:      {"agent", "", ""},
	}
	check := func(stage string) {
		t.Helper()
		for id, expected := range want {
			var channel, kind, at string
			if err := pool.QueryRow(ctx, `SELECT channel,COALESCE(last_public_kind,''),COALESCE(to_char(last_public_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI'),'')
				FROM conversations WHERE id=$1`, id).Scan(&channel, &kind, &at); err != nil {
				t.Fatalf("%s: %v", stage, err)
			}
			if [3]string{channel, kind, at} != expected {
				t.Fatalf("%s: conversation %s = %v, want %v", stage, id, [3]string{channel, kind, at}, expected)
			}
		}
	}
	check("backfill")

	// A conversation's updated_at is the team's activity; the waiting state must never move it.
	var updatedBefore, updatedAfter string
	if err := pool.QueryRow(ctx, `SELECT updated_at::text FROM conversations WHERE id=$1`, email).Scan(&updatedBefore); err != nil {
		t.Fatal(err)
	}
	// An import writes an older message after a newer one: the newest still wins.
	message(email, "incoming", `{"from":"ada@example.com","authentication":"pass","via":"workspace"}`, "<e@x>", 0)
	message(email, "incoming", `{"from":"ada@example.com","authentication":"pass","via":"workspace"}`, "<f@x>", 10)
	message(email, "note", "", "", 20)
	want[email] = [3]string{"email", "incoming", "2026-10-01T08:10"}
	message(empty, "outgoing", "", "", 7)
	want[empty] = [3]string{"agent", "outgoing", "2026-10-01T08:07"}
	check("inserted")
	if err := pool.QueryRow(ctx, `SELECT updated_at::text FROM conversations WHERE id=$1`, email).Scan(&updatedAfter); err != nil || updatedAfter != updatedBefore {
		t.Fatalf("updated_at moved from %s to %s (%v)", updatedBefore, updatedAfter, err)
	}

	if _, err := pool.Exec(ctx, `DELETE FROM messages WHERE workspace_id=$1 AND conversation_id=$2 AND external_id='email:<f@x>'`, workspace, email); err != nil {
		t.Fatal(err)
	}
	want[email] = [3]string{"email", "outgoing", "2026-10-01T08:02"}
	if _, err := pool.Exec(ctx, `UPDATE messages SET created_at='2026-10-01T09:00:00Z' WHERE workspace_id=$1 AND conversation_id=$2 AND kind='outgoing'`, workspace, started); err != nil {
		t.Fatal(err)
	}
	want[started] = [3]string{"agent", "outgoing", "2026-10-01T09:00"}
	if _, err := pool.Exec(ctx, `DELETE FROM messages WHERE workspace_id=$1 AND conversation_id=$2`, workspace, empty); err != nil {
		t.Fatal(err)
	}
	want[empty] = [3]string{"agent", "", ""}
	check("deleted and moved")

	// Two writers at once: B stores the customer's newer mail and holds its transaction; A, which began
	// first, then stores an older reply. A must wait for B and count B's mail, never overwrite it.
	txA, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer txA.Rollback(ctx) //nolint:errcheck
	if _, err := txA.Exec(ctx, `SELECT 1`); err != nil {
		t.Fatal(err)
	}
	txB, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer txB.Rollback(ctx) //nolint:errcheck
	if _, err := txB.Exec(ctx, `INSERT INTO messages(workspace_id,conversation_id,kind,body,created_at) VALUES($1,$2,'incoming','newer mail','2026-10-02T09:00:00Z')`, workspace, notes); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := txA.Exec(ctx, `INSERT INTO messages(workspace_id,conversation_id,kind,body,created_at) VALUES($1,$2,'outgoing','older reply','2026-10-02T08:00:00Z')`, workspace, notes)
		done <- err
	}()
	time.Sleep(200 * time.Millisecond) // A's insert reaches the conversation row B holds
	if err := txB.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := txA.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	want[notes] = [3]string{"agent", "incoming", "2026-10-02T09:00"}
	check("concurrent writers")

	// Deleting a conversation removes its messages through the cascade; the trigger must not stop it.
	if _, err := pool.Exec(ctx, `DELETE FROM conversations WHERE id=$1`, imported); err != nil {
		t.Fatalf("delete a conversation with messages: %v", err)
	}
	delete(want, imported)
	check("conversation deleted")

	// Rolling back drops only derived columns; a saved view stops it, nothing is dropped silently.
	if _, err := pool.Exec(ctx, `INSERT INTO saved_views(workspace_id,scope,name,filters) VALUES($1,'shared','Urgent','{"priority":["urgent"]}')`, workspace); err != nil {
		t.Fatal(err)
	}
	if err := rollbackThrough(ctx, pool, inboxFiltersMigration); err == nil || !strings.Contains(err.Error(), "saved views exist") {
		t.Fatalf("down with a saved view: err=%v, want a refusal", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM saved_views`); err != nil {
		t.Fatal(err)
	}
	if err := rollbackThrough(ctx, pool, inboxFiltersMigration); err != nil {
		t.Fatalf("down: %v", err)
	}
	var conversations, messages int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM conversations),(SELECT count(*) FROM messages)`).Scan(&conversations, &messages); err != nil || conversations != 8 || messages != 14 {
		t.Fatalf("after rollback: %d conversations, %d messages (%v)", conversations, messages, err)
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("upgrade again: %v", err)
	}
	// The test moved the team's first reply after the customer's mail, so it now reads as a mail.
	want[started] = [3]string{"email", "outgoing", "2026-10-01T09:00"}
	check("upgraded again")
}
