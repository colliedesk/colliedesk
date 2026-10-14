package database

import (
	"context"
	"testing"

	"github.com/colliedesk/colliedesk/server/migrations"
)

const coreMigration = "20261014055312-core-helpdesk.up.sql"

func TestAttachmentMigrationPreservesExistingConversationData(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	core, err := migrations.Files.ReadFile(coreMigration)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(core)); err != nil {
		t.Fatalf("apply core schema: %v", err)
	}
	if _, err := pool.Exec(ctx, `CREATE TABLE schema_migrations (name text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now());
		INSERT INTO schema_migrations(name) VALUES('`+coreMigration+`')`); err != nil {
		t.Fatal(err)
	}
	var workspaceID, messageID string
	err = pool.QueryRow(ctx, `WITH w AS (INSERT INTO workspaces(slug,name) VALUES('legacy','Legacy') RETURNING id),
		c AS (INSERT INTO contacts(workspace_id,name,email) SELECT id,'Ada','ada@example.com' FROM w RETURNING id,workspace_id),
		v AS (INSERT INTO conversations(workspace_id,contact_id,subject) SELECT workspace_id,id,'Printer jam' FROM c RETURNING id,workspace_id)
		INSERT INTO messages(workspace_id,conversation_id,kind,body,created_at) SELECT workspace_id,id,'note','Existing note','2026-01-02T03:04:05Z' FROM v
		RETURNING workspace_id::text,id::text`).Scan(&workspaceID, &messageID)
	if err != nil {
		t.Fatalf("seed v0.1 data: %v", err)
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	var body, created string
	var author, organization *string
	err = pool.QueryRow(ctx, `SELECT m.body,to_char(m.created_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS'),m.author_name,c.organization
		FROM messages m JOIN conversations v ON v.id=m.conversation_id JOIN contacts c ON c.id=v.contact_id
		WHERE m.workspace_id=$1 AND m.id=$2`, workspaceID, messageID).Scan(&body, &created, &author, &organization)
	if err != nil {
		t.Fatal(err)
	}
	if body != "Existing note" || created != "2026-01-02T03:04:05" || author != nil || organization != nil {
		t.Fatalf("upgraded row = %q %q %v %v", body, created, author, organization)
	}
}

func TestAttachmentCannotPointAtAnotherWorkspacesMessage(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var workspaceA, messageB string
	if err := pool.QueryRow(ctx, `INSERT INTO workspaces(slug,name) VALUES('alpha','Alpha') RETURNING id::text`).Scan(&workspaceA); err != nil {
		t.Fatal(err)
	}
	err := pool.QueryRow(ctx, `WITH w AS (INSERT INTO workspaces(slug,name) VALUES('beta','Beta') RETURNING id),
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
