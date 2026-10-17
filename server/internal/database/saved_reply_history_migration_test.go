package database

import (
	"context"
	"strings"
	"testing"
)

const savedReplyHistoryMigration = "20261017115546-saved-reply-history-and-trash.up.sql"

func TestSavedReplyHistoryMigrationGivesEveryReplyVersionOneAndRollsBackOnlyWithoutHistory(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `CREATE TABLE schema_migrations (name text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		t.Fatal(err)
	}
	for _, name := range upMigrations(t) {
		if name < savedReplyHistoryMigration {
			if err := applyMigration(ctx, pool, name); err != nil {
				t.Fatal(err)
			}
		}
	}
	var reply, imported, authored, author string
	if err := pool.QueryRow(ctx, `WITH w AS (INSERT INTO workspaces(slug,name) VALUES('history-a','A') RETURNING id),
		a AS (INSERT INTO agents(workspace_id,email,role) SELECT id,'admin@history.example','admin' FROM w RETURNING id,workspace_id),
		r AS (INSERT INTO saved_replies(workspace_id,title,body,created_by_agent_id,created_at,updated_at)
			SELECT workspace_id,'Refund','We refunded you.',id,'2026-10-01T08:00:00Z','2026-10-02T08:00:00Z' FROM a RETURNING id),
		i AS (INSERT INTO saved_replies(workspace_id,title,body) SELECT id,'Imported','From Freshdesk' FROM w RETURNING id),
		n AS (INSERT INTO saved_replies(workspace_id,title,body,created_by_agent_id,created_at,updated_at)
			SELECT workspace_id,'Authored','Hello',id,'2026-10-03T08:00:00Z','2026-10-03T08:00:00Z' FROM a RETURNING id)
		SELECT r.id::text,i.id::text,n.id::text,a.id::text FROM r,i,n,a`).Scan(&reply, &imported, &authored, &author); err != nil {
		t.Fatalf("seed on the previous schema: %v", err)
	}

	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	var title, body, kind string
	var version int
	var changedBy *string
	var createdAt string
	if err := pool.QueryRow(ctx, `SELECT version,kind,title,body,changed_by_agent_id::text,to_char(created_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI')
		FROM saved_reply_versions WHERE saved_reply_id=$1`, reply).Scan(&version, &kind, &title, &body, &changedBy, &createdAt); err != nil {
		t.Fatalf("version 1 of an existing reply: %v", err)
	}
	// Edited after it was created: who wrote the current text is unknown, so it is the baseline, not
	// a create by the author.
	if version != 1 || kind != "baseline" || title != "Refund" || body != "We refunded you." || changedBy != nil || createdAt != "2026-10-02T08:00" {
		t.Fatalf("backfilled version of an edited reply = %d %s %q %q by %v at %s", version, kind, title, body, changedBy, createdAt)
	}
	if err := pool.QueryRow(ctx, `SELECT kind,changed_by_agent_id::text FROM saved_reply_versions WHERE saved_reply_id=$1`, imported).Scan(&kind, &changedBy); err != nil || kind != "create" || changedBy != nil {
		t.Fatalf("backfilled version of a never edited reply = %s by %v (%v), want create", kind, changedBy, err)
	}
	if err := pool.QueryRow(ctx, `SELECT kind,changed_by_agent_id::text FROM saved_reply_versions WHERE saved_reply_id=$1`, authored).Scan(&kind, &changedBy); err != nil || kind != "create" || changedBy == nil || *changedBy != author {
		t.Fatalf("backfilled version of a never edited reply with an author = %s by %v (%v), want create by %s", kind, changedBy, err, author)
	}

	// A trashed title can be taken again.
	if _, err := pool.Exec(ctx, `UPDATE saved_replies SET deleted_at=now() WHERE id=$1`, reply); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO saved_replies(workspace_id,title,body) SELECT workspace_id,'refund','New' FROM saved_replies WHERE id=$1`, reply); err != nil {
		t.Fatalf("a title in the trash must be free: %v", err)
	}
	if err := rollbackThrough(ctx, pool, savedReplyHistoryMigration); err == nil || !strings.Contains(err.Error(), "in the trash") {
		t.Fatalf("down with a reply in the trash: err=%v, want a refusal", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM saved_replies WHERE title='refund'`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE saved_replies SET deleted_at=NULL WHERE id=$1`, reply); err != nil {
		t.Fatal(err)
	}
	if err := rollbackThrough(ctx, pool, savedReplyHistoryMigration); err != nil {
		t.Fatalf("down with only the backfilled versions: %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM saved_replies`).Scan(&count); err != nil || count != 3 {
		t.Fatalf("the rollback lost saved replies: %d, %v", count, err)
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("up again: %v", err)
	}
}
