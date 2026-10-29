package database

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
)

const handoffLinksMigration = "20261029165910-handoff-links.up.sql"

// Rolling the hand-off kind back deletes only hand-offs (a minute's worth of sign-ins in flight); the
// pairing links and invites of every workspace stay, and the upgrade applies again.
func TestHandoffLinksDownKeepsPairingLinksAndInvites(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("up: %v", err)
	}
	err := New(pool).Privileged(ctx, PrivilegeMigrate, func(tx pgx.Tx) error {
		var workspace string
		if err := tx.QueryRow(ctx, `INSERT INTO workspaces(slug,name) VALUES('handoff-down','Handoff Down') RETURNING id::text`).Scan(&workspace); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO pairing_links(workspace_id,email,kind,role_ceiling,token_hash,expires_at) VALUES
			($1::uuid,'a@example.com','pairing','admin','\x01',now()+interval '1 day'),
			($1::uuid,'b@example.com','invite','agent','\x02',now()+interval '1 day'),
			($1::uuid,'a@example.com','handoff','admin','\x03',now()+interval '1 minute')`, workspace)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := rollbackThrough(ctx, pool, handoffLinksMigration); err != nil {
		t.Fatalf("down: %v", err)
	}
	var kinds string
	if err := pool.QueryRow(ctx, `SELECT string_agg(kind,',' ORDER BY kind) FROM pairing_links`).Scan(&kinds); err != nil || kinds != "invite,pairing" {
		t.Fatalf("links after the down migration: %q err=%v, want invite,pairing", kinds, err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO pairing_links(workspace_id,email,kind,role_ceiling,token_hash,expires_at)
		SELECT id,'c@example.com','handoff','admin','\x04',now() FROM workspaces WHERE slug='handoff-down'`); err == nil {
		t.Fatal("a hand-off was accepted after the down migration")
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("up again: %v", err)
	}
}
