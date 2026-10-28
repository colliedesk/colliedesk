package database

import (
	"context"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/email"
)

// email_channels.mailbox_key is email.MailboxKey: the database and the server agree on which support
// addresses name one mailbox, and the addresses the server could read otherwise are refused.
func TestTheMailboxKeyOfTheDatabaseIsTheServers(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var workspaceID string
	if err := pool.QueryRow(ctx, `WITH w AS (INSERT INTO workspaces(slug,name) VALUES('keys','Keys') RETURNING id),
		d AS (INSERT INTO email_domains(workspace_id,domain,verification_token) SELECT id,unnest(ARRAY['acme.com','sub.acme.com','y']),'t' FROM w RETURNING 1)
		SELECT id::text FROM w`).Scan(&workspaceID); err != nil {
		t.Fatal(err)
	}
	add := func(n int, address string) (string, error) {
		var key string
		err := pool.QueryRow(ctx, `WITH c AS (INSERT INTO channels(workspace_id,type,name) VALUES($1,'email',$2) RETURNING id)
			INSERT INTO email_channels(workspace_id,channel_id,support_address,sender_name,hosted_local) SELECT $1,id,$3,'Keys',$2 FROM c RETURNING mailbox_key`,
			workspaceID, "k"+string(rune('a'+n)), address).Scan(&key)
		return key, err
	}
	for n, address := range []string{"support@acme.com", "billing+vip@acme.com", "a.b+c+d@sub.acme.com", "x@y"} {
		key, err := add(n, address)
		if err != nil {
			t.Fatalf("%s: %v", address, err)
		}
		if want := email.MailboxKey(address); key != want {
			t.Errorf("%s: mailbox_key=%q, email.MailboxKey=%q", address, key, want)
		}
	}
	for n, address := range []string{"+tag@acme.com", `"a@b"@acme.com`} {
		if _, err := add(10+n, address); err == nil {
			t.Errorf("%s was stored; email.MailboxKey reads it as %q", address, email.MailboxKey(address))
		}
	}
	if email.MailboxKey("+tag@acme.com") != "" || !email.SameMailbox("Support+VIP@Acme.com", "support@acme.com") {
		t.Error("MailboxKey or SameMailbox changed meaning")
	}
}
