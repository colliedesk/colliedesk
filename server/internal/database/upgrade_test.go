package database

import (
	"context"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/migrations"
	"github.com/jackc/pgx/v5/pgxpool"
)

// importMigration is the only migration not on main yet. Main may already run
// migrations sorted after it (the email channel), so upgrades apply it last.
const importMigration = "20261014101230-attachments-time-entries-and-import-ledger.up.sql"

func upMigrations(t *testing.T) []string {
	t.Helper()
	entries, err := fs.ReadDir(migrations.Files, ".")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".up.sql") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names
}

func TestEveryMigrationAppliesOnFreshDatabaseAndRollsBackInReverse(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("up on fresh database: %v", err)
	}
	var applied int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied != len(upMigrations(t)) {
		t.Fatalf("applied=%d, migrations=%d", applied, len(upMigrations(t)))
	}
	for range applied {
		if err := rollbackLatest(ctx, pool); err != nil {
			t.Fatalf("down: %v", err)
		}
	}
	var tables []string
	rows, err := pool.Query(ctx, `SELECT table_name FROM information_schema.tables WHERE table_schema=current_schema() AND table_name<>'schema_migrations' ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, name)
	}
	rows.Close()
	if len(tables) != 0 {
		t.Fatalf("tables left after rolling back every migration: %v", tables)
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("up again: %v", err)
	}
}

func TestImportMigrationUpgradesMainSchemaWithBusinessData(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	applyMainSchema(t, pool)
	ids := seedBusinessData(t, pool)
	before := rowCounts(t, pool)

	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	if after := rowCounts(t, pool); after != before {
		t.Fatalf("row counts changed by the upgrade: %s -> %s", before, after)
	}
	var body, created, tags string
	var author, organization, html, origin *string
	err := pool.QueryRow(ctx, `SELECT m.body,to_char(m.created_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS'),m.author_name,m.body_html,m.origin::text,c.organization,
		(SELECT string_agg(t.name,',' ORDER BY t.name) FROM conversation_tags ct JOIN tags t ON t.id=ct.tag_id WHERE ct.conversation_id=v.id)
		FROM messages m JOIN conversations v ON v.id=m.conversation_id JOIN contacts c ON c.id=v.contact_id
		WHERE m.workspace_id=$1 AND m.id=$2`, ids.workspace, ids.message).Scan(&body, &created, &author, &html, &origin, &organization, &tags)
	if err != nil {
		t.Fatal(err)
	}
	if body != "Existing note" || created != "2026-01-02T03:04:05" || author != nil || html != nil || origin != nil || organization != nil || tags != "billing,vip" {
		t.Fatalf("upgraded row = %q %q %v %v %v %v %q", body, created, author, html, origin, organization, tags)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO attachments(workspace_id,message_id,filename,content_type,size,sha256,storage_key)
		VALUES($1,$2,'old.txt','text/plain',1,repeat('a',64),'k')`, ids.workspace, ids.message); err != nil {
		t.Fatalf("attachment on a pre-upgrade message: %v", err)
	}
	if err := rollbackThrough(ctx, pool, importMigration); err != nil {
		t.Fatalf("down to main schema: %v", err)
	}
	if after := rowCounts(t, pool); after != before {
		t.Fatalf("rolling back the import migration changed main data: %s -> %s", before, after)
	}
	var keyKept bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_constraint WHERE conname='messages_workspace_id_id_key')`).Scan(&keyKept); err != nil || !keyKept {
		t.Fatalf("rollback dropped the key the email outbox depends on: %v", err)
	}
}

func applyMainSchema(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `CREATE TABLE schema_migrations (name text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		t.Fatal(err)
	}
	for _, name := range upMigrations(t) {
		if name == importMigration || name >= rlsMigration {
			continue
		}
		contents, err := migrations.Files.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, string(contents)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO schema_migrations(name) VALUES($1)`, name); err != nil {
			t.Fatal(err)
		}
	}
}

type seededIDs struct{ workspace, message string }

func seedBusinessData(t *testing.T, pool *pgxpool.Pool) seededIDs {
	t.Helper()
	var ids seededIDs
	err := pool.QueryRow(context.Background(), `WITH w AS (INSERT INTO workspaces(slug,name,timezone) VALUES('legacy','Legacy','America/New_York') RETURNING id),
		a AS (INSERT INTO agents(workspace_id,email,display_name,role) SELECT id,'admin@legacy.example','Admin','admin' FROM w RETURNING id,workspace_id),
		a2 AS (INSERT INTO agents(workspace_id,email,role) SELECT id,'agent@legacy.example','agent' FROM w RETURNING id),
		s AS (INSERT INTO sessions(workspace_id,agent_id,token_hash,role_ceiling,label,expires_at) SELECT workspace_id,id,'\x01','admin','Laptop',now()+interval '1 day' FROM a RETURNING id),
		c AS (INSERT INTO contacts(workspace_id,name,email) SELECT id,'Ada','ada@example.com' FROM w RETURNING id,workspace_id),
		v AS (INSERT INTO conversations(workspace_id,contact_id,subject,status,priority,assignee_id)
			SELECT c.workspace_id,c.id,'Printer jam','pending','high',a.id FROM c,a RETURNING id,workspace_id),
		v2 AS (INSERT INTO conversations(workspace_id,contact_id,subject) SELECT workspace_id,id,'Second' FROM c RETURNING id),
		t1 AS (INSERT INTO tags(workspace_id,name) SELECT id,'billing' FROM w RETURNING id,workspace_id),
		t2 AS (INSERT INTO tags(workspace_id,name) SELECT id,'vip' FROM w RETURNING id,workspace_id),
		ct AS (INSERT INTO conversation_tags(workspace_id,conversation_id,tag_id) SELECT v.workspace_id,v.id,t.id FROM v,(SELECT id FROM t1 UNION ALL SELECT id FROM t2) t RETURNING 1),
		m2 AS (INSERT INTO messages(workspace_id,conversation_id,author_agent_id,kind,body) SELECT v.workspace_id,v.id,a.id,'outgoing','Reply' FROM v,a RETURNING id)
		INSERT INTO messages(workspace_id,conversation_id,kind,body,created_at) SELECT workspace_id,id,'note','Existing note','2026-01-02T03:04:05Z' FROM v
		RETURNING workspace_id::text,id::text`).Scan(&ids.workspace, &ids.message)
	if err != nil {
		t.Fatalf("seed main-schema data: %v", err)
	}
	return ids
}

func rowCounts(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	var parts []string
	for _, table := range []string{"workspaces", "agents", "sessions", "contacts", "conversations", "messages", "tags", "conversation_tags"} {
		var n int
		if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM `+table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		parts = append(parts, fmt.Sprintf("%s=%d", table, n))
	}
	return strings.Join(parts, " ")
}

func TestAttachmentCannotPointAtAnotherWorkspacesMessage(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var workspaceA, messageB string
	if err := pool.QueryRow(ctx, `INSERT INTO workspaces(slug,name,trial_ends_at) VALUES('alpha','Alpha',now()) RETURNING id::text`).Scan(&workspaceA); err != nil {
		t.Fatal(err)
	}
	err := pool.QueryRow(ctx, `WITH w AS (INSERT INTO workspaces(slug,name,trial_ends_at) VALUES('beta','Beta',now()) RETURNING id),
		c AS (INSERT INTO contacts(workspace_id,name,email) SELECT id,'B','b@example.com' FROM w RETURNING id,workspace_id),
		v AS (INSERT INTO conversations(workspace_id,contact_id,subject) SELECT workspace_id,id,'B' FROM c RETURNING id,workspace_id)
		INSERT INTO messages(workspace_id,conversation_id,kind,body) SELECT workspace_id,id,'note','B' FROM v RETURNING id::text`).Scan(&messageB)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO attachments(workspace_id,message_id,filename,content_type,size,sha256,storage_key)
		VALUES($1,$2,'a.txt','text/plain',1,repeat('a',64),'k')`, workspaceA, messageB)
	if err == nil {
		t.Fatal("cross-workspace attachment was accepted")
	}
}
