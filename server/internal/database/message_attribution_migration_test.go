package database

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

const attributionMigration = "20261017020422-message-agent-token-attribution.up.sql"

func TestMessageAttributionMigrationKeepsMessagesAndBindsTokensToTheirWorkspace(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `CREATE TABLE schema_migrations (name text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		t.Fatal(err)
	}
	for _, name := range upMigrations(t) {
		if name < attributionMigration {
			if err := applyMigration(ctx, pool, name); err != nil {
				t.Fatal(err)
			}
		}
	}
	var message, ownToken, foreignToken string
	if err := pool.QueryRow(ctx, `WITH wa AS (INSERT INTO workspaces(slug,name) VALUES('attr-a','A') RETURNING id),
		wb AS (INSERT INTO workspaces(slug,name) VALUES('attr-b','B') RETURNING id),
		aa AS (INSERT INTO agents(workspace_id,email,role) SELECT id,'admin@a.example','admin' FROM wa RETURNING id,workspace_id),
		ab AS (INSERT INTO agents(workspace_id,email,role) SELECT id,'admin@b.example','admin' FROM wb RETURNING id,workspace_id),
		sa AS (INSERT INTO sessions(workspace_id,agent_id,token_hash,role_ceiling,scope,label,expires_at)
			SELECT workspace_id,id,'\x0a','agent','draft','Claude Code',now()+interval '1 day' FROM aa RETURNING id),
		sb AS (INSERT INTO sessions(workspace_id,agent_id,token_hash,role_ceiling,scope,label,expires_at)
			SELECT workspace_id,id,'\x0b','agent','draft','Other desk',now()+interval '1 day' FROM ab RETURNING id),
		c AS (INSERT INTO contacts(workspace_id,name,email) SELECT id,'Ada','ada@example.com' FROM wa RETURNING id,workspace_id),
		v AS (INSERT INTO conversations(workspace_id,contact_id,subject) SELECT workspace_id,id,'Printer jam' FROM c RETURNING id,workspace_id),
		m AS (INSERT INTO messages(workspace_id,conversation_id,author_agent_id,kind,body)
			SELECT v.workspace_id,v.id,aa.id,'note','Existing note' FROM v,aa RETURNING id)
		SELECT m.id::text,sa.id::text,sb.id::text FROM m,sa,sb`).Scan(&message, &ownToken, &foreignToken); err != nil {
		t.Fatalf("seed on the previous schema: %v", err)
	}

	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	var body string
	var token *string
	if err := pool.QueryRow(ctx, `SELECT body,agent_token_id::text FROM messages WHERE id=$1`, message).Scan(&body, &token); err != nil || body != "Existing note" || token != nil {
		t.Fatalf("upgrade changed the existing message: body=%q token=%v err=%v", body, token, err)
	}

	if _, err := pool.Exec(ctx, `UPDATE messages SET agent_token_id=$2 WHERE id=$1`, message, ownToken); err != nil {
		t.Fatalf("name a token of the same workspace: %v", err)
	}
	var violation *pgconn.PgError
	if _, err := pool.Exec(ctx, `UPDATE messages SET agent_token_id=$2 WHERE id=$1`, message, foreignToken); !errors.As(err, &violation) || violation.Code != "23503" {
		t.Fatalf("name a token of another workspace: err=%v, want a foreign key violation", err)
	}

	if _, err := pool.Exec(ctx, `DELETE FROM sessions WHERE id=$1`, ownToken); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT body,agent_token_id::text FROM messages WHERE id=$1`, message).Scan(&body, &token); err != nil || token != nil {
		t.Fatalf("removing the token must keep the message and clear the link: body=%q token=%v err=%v", body, token, err)
	}

	if err := rollbackThrough(ctx, pool, attributionMigration); err != nil {
		t.Fatalf("down: %v", err)
	}
	var hasColumn bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='messages' AND column_name='agent_token_id')`).Scan(&hasColumn); err != nil || hasColumn {
		t.Fatalf("agent_token_id survived the rollback (err=%v)", err)
	}
	if err := pool.QueryRow(ctx, `SELECT body FROM messages WHERE id=$1`, message).Scan(&body); err != nil || body != "Existing note" {
		t.Fatalf("the rollback lost the message: body=%q err=%v", body, err)
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("up again: %v", err)
	}
}

// Adding a key to a large table validates every row under a lock that blocks writes. A migration runs in
// one transaction, so the key is added without validation in the migration that creates the column and
// checked by the next one, which only needs a lock that lets reads and writes continue.
func TestMessageAttributionKeyIsAddedUnvalidatedAndValidatedByTheNextMigration(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `CREATE TABLE schema_migrations (name text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		t.Fatal(err)
	}
	for _, name := range upMigrations(t) {
		if name <= attributionMigration {
			if err := applyMigration(ctx, pool, name); err != nil {
				t.Fatal(err)
			}
		}
	}
	validated := func() bool {
		var ok bool
		if err := pool.QueryRow(ctx, `SELECT convalidated FROM pg_constraint WHERE conname='messages_agent_token_fkey' AND conrelid='messages'::regclass`).Scan(&ok); err != nil {
			t.Fatalf("read the key state: %v", err)
		}
		return ok
	}
	if validated() {
		t.Fatal("the migration that adds agent_token_id also validated its key: that scans messages while writes wait")
	}
	var violation *pgconn.PgError
	if _, err := pool.Exec(ctx, `WITH wa AS (INSERT INTO workspaces(slug,name) VALUES('key-a','A') RETURNING id),
		wb AS (INSERT INTO workspaces(slug,name) VALUES('key-b','B') RETURNING id),
		ab AS (INSERT INTO agents(workspace_id,email,role) SELECT id,'admin@b.example','admin' FROM wb RETURNING id,workspace_id),
		sb AS (INSERT INTO sessions(workspace_id,agent_id,token_hash,role_ceiling,scope,label,expires_at)
			SELECT workspace_id,id,'\x0b','agent','draft','Other desk',now()+interval '1 day' FROM ab RETURNING id),
		aa AS (INSERT INTO agents(workspace_id,email,role) SELECT id,'admin@a.example','admin' FROM wa RETURNING id,workspace_id),
		c AS (INSERT INTO contacts(workspace_id,name,email) SELECT id,'Ada','ada@example.com' FROM wa RETURNING id,workspace_id),
		v AS (INSERT INTO conversations(workspace_id,contact_id,subject) SELECT workspace_id,id,'Printer jam' FROM c RETURNING id,workspace_id)
		INSERT INTO messages(workspace_id,conversation_id,author_agent_id,kind,body,agent_token_id)
		SELECT v.workspace_id,v.id,aa.id,'note','Names a foreign token',sb.id FROM v,aa,sb`); !errors.As(err, &violation) || violation.Code != "23503" {
		t.Fatalf("an unvalidated key must already refuse a new message that names another workspace's token: err=%v", err)
	}

	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	if !validated() {
		t.Fatal("no later migration validated messages_agent_token_fkey")
	}
}
