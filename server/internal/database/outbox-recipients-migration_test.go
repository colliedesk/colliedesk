package database

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
)

const outboxRecipientsMigration = "20261016055326-outbox-recipient-count.up.sql"

// Replies queued before the count existed must keep counting every person they reach once the
// column arrives: a reply with two Cc addresses reached three people.
func TestOutboxRecipientCountUpgradeCountsQueuedCopies(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("up: %v", err)
	}
	if err := rollbackThrough(ctx, pool, outboxRecipientsMigration); err != nil {
		t.Fatalf("down: %v", err)
	}
	err := New(pool).Privileged(ctx, PrivilegeMigrate, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `WITH w AS (INSERT INTO workspaces(slug,name) VALUES('rcpt','Recipients Co') RETURNING id),
			c AS (INSERT INTO contacts(workspace_id,name,email) SELECT id,'Olive','olive@example.com' FROM w RETURNING id,workspace_id),
			v AS (INSERT INTO conversations(workspace_id,contact_id,subject) SELECT workspace_id,id,'Question' FROM c RETURNING id,workspace_id),
			copied AS (INSERT INTO messages(workspace_id,conversation_id,kind,body,cc)
				SELECT workspace_id,id,'outgoing','copied','{boss@example.com,ops@example.com}' FROM v RETURNING id,workspace_id),
			plain AS (INSERT INTO messages(workspace_id,conversation_id,kind,body) SELECT workspace_id,id,'outgoing','plain' FROM v RETURNING id,workspace_id)
			INSERT INTO email_outbox(workspace_id,message_id,status,created_at)
			SELECT workspace_id,id,'sent',now() FROM copied UNION ALL SELECT workspace_id,id,'sent',now() FROM plain`)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("up again: %v", err)
	}
	var copied, plain int
	if err := pool.QueryRow(ctx, `SELECT
			(SELECT o.recipients FROM email_outbox o JOIN messages m ON m.id=o.message_id WHERE m.body='copied'),
			(SELECT o.recipients FROM email_outbox o JOIN messages m ON m.id=o.message_id WHERE m.body='plain')`).Scan(&copied, &plain); err != nil {
		t.Fatal(err)
	}
	if copied != 3 || plain != 1 {
		t.Fatalf("recipients after upgrade: copied=%d plain=%d, want 3 and 1", copied, plain)
	}
}
