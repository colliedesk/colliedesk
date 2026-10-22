package database

import (
	"context"
	"strings"
	"testing"
)

const senderNameMigration = "20261022030507-email-channel-sender-name.up.sql"

// An upgraded database keeps the name its customers already see: each channel starts with its
// workspace's name, made safe for a mail header and cut to 100 characters.
func TestSenderNameMigrationKeepsTheWorkspaceNameCustomersAlreadySee(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `CREATE TABLE schema_migrations (name text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		t.Fatal(err)
	}
	for _, name := range upMigrations(t) {
		if name < senderNameMigration {
			if err := applyMigration(ctx, pool, name); err != nil {
				t.Fatal(err)
			}
		}
	}
	long := strings.Repeat("Ab", 60)
	channels := map[string]string{
		"plain":    "HC Labs",
		"long":     long,
		"broken":   "Acme\r\nBcc: victim@example.com",
		"controls": "\t\n",
	}
	for slug, name := range channels {
		if _, err := pool.Exec(ctx, `WITH w AS (INSERT INTO workspaces(slug,name) VALUES($1,$2) RETURNING id)
			INSERT INTO email_channels(workspace_id,support_address,domain_verification_token) SELECT id,'support@'||$1||'.example','token' FROM w`, slug, name); err != nil {
			t.Fatalf("seed %s: %v", slug, err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO workspaces(slug,name) VALUES('no-channel','No Channel')`); err != nil {
		t.Fatal(err)
	}

	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	want := map[string]string{
		"plain":    "HC Labs",
		"long":     long[:100],
		"broken":   "Acme  Bcc: victim@example.com",
		"controls": "controls",
	}
	for slug, expected := range want {
		var got, name string
		if err := pool.QueryRow(ctx, `SELECT ch.sender_name,w.name FROM email_channels ch JOIN workspaces w ON w.id=ch.workspace_id WHERE w.slug=$1`, slug).
			Scan(&got, &name); err != nil {
			t.Fatalf("read %s: %v", slug, err)
		}
		if got != expected {
			t.Errorf("%s: sender_name=%q, want %q", slug, got, expected)
		}
		if name != channels[slug] {
			t.Errorf("%s: the workspace name changed to %q", slug, name)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO email_channels(workspace_id,support_address,domain_verification_token)
		SELECT id,'support@no-channel.example','token' FROM workspaces WHERE slug='no-channel'`); err == nil {
		t.Error("a channel without a sender name was stored")
	}
	for _, bad := range []string{"", strings.Repeat("x", 101), "Line\nbreak"} {
		if _, err := pool.Exec(ctx, `UPDATE email_channels SET sender_name=$1 WHERE workspace_id=(SELECT id FROM workspaces WHERE slug='plain')`, bad); err == nil {
			t.Errorf("sender_name %q was stored", bad)
		}
	}
}
