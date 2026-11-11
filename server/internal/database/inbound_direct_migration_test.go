package database

import (
	"context"
	"testing"
)

const inboundDirectMigration = "20261110112755-inbound-mail-outside-the-support-mailbox.up.sql"

// Journal rows written before the email envelope said "direct" are upgraded: a past test message and a
// past Gmail forwarding confirmation did not pass the support mailbox, a customer's forwarded mail did.
// Rolling back and upgrading again works.
func TestUpgradeMarksPastTestMessagesAndForwardingConfirmationsAsDirect(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err := rollbackThrough(ctx, pool, inboundDirectMigration); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `SELECT set_config('app.privileged','on',false)`); err != nil {
		t.Fatal(err)
	}
	var workspace, channel string
	if err := pool.QueryRow(ctx, `INSERT INTO workspaces(slug,name) VALUES('direct-upgrade','Direct Upgrade') RETURNING id::text`).Scan(&workspace); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO agents(workspace_id,email,role) VALUES($1,'ada@direct-upgrade.example','admin')`, workspace); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO email_domains(workspace_id,domain,verification_token) VALUES($1,'direct-upgrade.example','token')`, workspace); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `WITH c AS (INSERT INTO channels(workspace_id,type,name) VALUES($1,'email','Email') RETURNING id,workspace_id)
		INSERT INTO email_channels(workspace_id,channel_id,support_address,sender_name,hosted_local)
		SELECT workspace_id,id,'support@direct-upgrade.example','Desk','support' FROM c RETURNING channel_id::text`, workspace).Scan(&channel); err != nil {
		t.Fatal(err)
	}
	// row files one processed inbound email with its journal envelope, as the email channel did.
	row := func(contact, subject, messageID, body, from string) string {
		t.Helper()
		var event string
		if err := pool.QueryRow(ctx, `WITH k AS (INSERT INTO contacts(workspace_id,name,email) VALUES($1,$2,$2) RETURNING id),
			v AS (INSERT INTO conversations(workspace_id,contact_id,subject,channel_id) SELECT $1,id,$3,$6 FROM k RETURNING id),
			m AS (INSERT INTO messages(workspace_id,conversation_id,kind,body,external_id,channel_id) SELECT $1,id,'incoming',$5,'email:'||$4,$6 FROM v RETURNING id,conversation_id)
			INSERT INTO channel_inbound_events(workspace_id,channel_id,state,raw,conversation_id,message_id,processed_at)
			SELECT $1,$6,'processed',jsonb_build_object('message_id',$4::text,'from',$7::text,'platform',true),conversation_id,id,now() FROM m RETURNING id::text`,
			workspace, contact, subject, messageID, body, channel, from).Scan(&event); err != nil {
			t.Fatal(err)
		}
		return event
	}
	testMessage := row("ada@direct-upgrade.example", "Test message from Colliedesk", "cd.0123456789abcdef0123456789abcdef@direct-upgrade.example",
		"This is a test message you sent yourself from Colliedesk.\r\n\r\nIt arrived the way a customer's email does.", "ada@direct-upgrade.example")
	confirmation := row("forwarding-noreply@google.com", "(#563947195) Gmail Forwarding Confirmation - Receive Mail from support@direct-upgrade.example",
		"confirmation-1@google.com", "Confirmation code: 563947195", "forwarding-noreply@google.com")
	forwarded := row("olive@customer.example", "Broken export", "export-1@customer.example", "Export fails.", "olive@customer.example")
	// A customer's own mail with the test message's subject is still customer mail.
	lookalike := row("pat@customer.example", "Test message from Colliedesk", "lookalike-1@customer.example",
		"This is a test message you sent yourself from Colliedesk.", "pat@customer.example")

	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	direct := func(event string) string {
		t.Helper()
		var value string
		if err := pool.QueryRow(ctx, `SELECT COALESCE(raw->>'direct','') FROM channel_inbound_events WHERE workspace_id=$1 AND id=$2`, workspace, event).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	for name, item := range map[string]struct{ event, want string }{
		"test message":            {testMessage, "true"},
		"forwarding confirmation": {confirmation, "true"},
		"forwarded customer mail": {forwarded, ""},
		"customer lookalike":      {lookalike, ""},
	} {
		if got := direct(item.event); got != item.want {
			t.Errorf("%s: direct=%q, want %q", name, got, item.want)
		}
	}
	if err := rollbackThrough(ctx, pool, inboundDirectMigration); err != nil {
		t.Fatalf("roll back: %v", err)
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("upgrade again after the rollback: %v", err)
	}
	if got := direct(testMessage); got != "true" {
		t.Errorf("test message after rollback and upgrade: direct=%q, want true", got)
	}
}
