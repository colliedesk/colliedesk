package database

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const channelsMigration = "20261025090000-channels.up.sql"

// migrateBefore applies every migration older than name, so a test can seed the schema an older
// release left.
func migrateBefore(t *testing.T, pool *pgxpool.Pool, name string) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `CREATE TABLE schema_migrations (name text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		t.Fatal(err)
	}
	for _, migration := range upMigrations(t) {
		if migration < name {
			if err := applyMigration(ctx, pool, migration); err != nil {
				t.Fatal(err)
			}
		}
	}
}

type channelsSeed struct {
	both, emailOnly, formOff, none, gone                   string
	emailConversation, formConversation, agentConversation string
	goneConversation                                       string
	reply                                                  string
}

// seedChannelWorkspaces writes, the way the release before channels did, a workspace with email (its
// domain verified through the provider, Gmail connected, a consent in flight) and an enabled web form,
// one with email only, one with a web form that is off, one with neither (and a consent that cannot
// finish), and one that disconnected email after a customer wrote in and a reply left.
func seedChannelWorkspaces(t *testing.T, pool *pgxpool.Pool) channelsSeed {
	t.Helper()
	ctx := context.Background()
	var s channelsSeed
	for slug, target := range map[string]*string{"both": &s.both, "email-only": &s.emailOnly, "form-off": &s.formOff, "none": &s.none, "gone": &s.gone} {
		if err := pool.QueryRow(ctx, `INSERT INTO workspaces(slug,name) VALUES($1,$1) RETURNING id::text`, slug).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{s.both, s.emailOnly} {
		if _, err := pool.Exec(ctx, `INSERT INTO email_channels(workspace_id,support_address,domain_verification_token,created_at)
			SELECT id,'support@'||slug||'.example','token-'||slug,'2026-10-01 10:00:00+00' FROM workspaces WHERE id=$1`, id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE email_channels SET domain_verified_at='2026-10-02 10:00:00+00',provider_domain_id='pd-both',
		provider_domain_records='[{"record":"DKIM","type":"TXT","name":"x","value":"y","status":"verified"}]',provider_domain_created_at='2026-10-01 11:00:00+00',
		provider_domain_first_verified_at='2026-10-02 10:00:00+00' WHERE workspace_id=$1`, s.both); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO web_form_channels(workspace_id,enabled,allowed_origins,updated_at) VALUES
		($1,true,'{https://both.example}','2026-10-02 10:00:00+00'),($2,false,'{}','2026-10-03 10:00:00+00')`, s.both, s.formOff); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `WITH a AS (INSERT INTO agents(workspace_id,email,display_name,role) VALUES($1,'admin@both.example','Admin','admin') RETURNING id),
		m AS (INSERT INTO mailbox_connections(workspace_id,provider,address,scopes,token_key_id,refresh_token_sealed) VALUES($1,'google','support@both.example','s','k1','\x01') RETURNING 1)
		INSERT INTO mailbox_oauth_states(state_hash,workspace_id,agent_id,provider,code_verifier,expires_at)
		SELECT sha256('both'::bytea),$1,id,'google','v',now()+interval '5 minutes' FROM a`, s.both); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `WITH a AS (INSERT INTO agents(workspace_id,email,display_name,role) VALUES($1,'admin@none.example','Admin','admin') RETURNING id)
		INSERT INTO mailbox_oauth_states(state_hash,workspace_id,agent_id,provider,code_verifier,expires_at)
		SELECT sha256('none'::bytea),$1,id,'google','v',now()+interval '5 minutes' FROM a`, s.none); err != nil {
		t.Fatal(err)
	}
	err := pool.QueryRow(ctx, `WITH c AS (INSERT INTO contacts(workspace_id,name,email) VALUES($1,'Ann','ann@example.com') RETURNING id),
		e AS (INSERT INTO conversations(workspace_id,contact_id,subject,channel,email_reply_token) SELECT $1,id,'Mail','email','tok-1' FROM c RETURNING id),
		f AS (INSERT INTO conversations(workspace_id,contact_id,subject,channel) SELECT $1,id,'Form','web_form' FROM c RETURNING id),
		a AS (INSERT INTO conversations(workspace_id,contact_id,subject,channel) SELECT $1,id,'Started','agent' FROM c RETURNING id),
		m AS (INSERT INTO messages(workspace_id,conversation_id,kind,body,email_message_id) SELECT $1,id,'incoming','hi','<in-1@example.com>' FROM e RETURNING 1),
		r AS (INSERT INTO messages(workspace_id,conversation_id,kind,body,email_message_id) SELECT $1,id,'outgoing','hello','<out-1@both.example>' FROM e RETURNING id)
		SELECT (SELECT id::text FROM e),(SELECT id::text FROM f),(SELECT id::text FROM a),(SELECT id::text FROM r)`, s.both).
		Scan(&s.emailConversation, &s.formConversation, &s.agentConversation, &s.reply)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO email_outbox(workspace_id,message_id,status,sent_at) VALUES($1,$2,'sent',now())`, s.both, s.reply); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `WITH c AS (INSERT INTO contacts(workspace_id,name,email) VALUES($1,'Bo','bo@example.com') RETURNING id),
		e AS (INSERT INTO conversations(workspace_id,contact_id,subject,channel,created_at) SELECT $1,id,'Old mail','email','2026-09-01 08:00:00+00' FROM c RETURNING id),
		r AS (INSERT INTO messages(workspace_id,conversation_id,kind,body,email_message_id) SELECT $1,id,'outgoing','bye','<out-1@gone.example>' FROM e RETURNING id),
		o AS (INSERT INTO email_outbox(workspace_id,message_id,status,sent_at) SELECT $1,id,'sent',now() FROM r RETURNING 1)
		SELECT id::text FROM e`, s.gone).Scan(&s.goneConversation); err != nil {
		t.Fatal(err)
	}
	return s
}

func channelOf(t *testing.T, pool *pgxpool.Pool, workspaceID, kind string) (id string, enabled bool, disabledAt *time.Time, deleted bool) {
	t.Helper()
	err := pool.QueryRow(context.Background(), `SELECT id::text,enabled,disabled_at,deleted_at IS NOT NULL FROM channels
		WHERE workspace_id=$1 AND type=$2 ORDER BY deleted_at IS NULL DESC,created_at DESC LIMIT 1`, workspaceID, kind).Scan(&id, &enabled, &disabledAt, &deleted)
	if err == pgx.ErrNoRows {
		return "", false, nil, false
	}
	if err != nil {
		t.Fatal(err)
	}
	return id, enabled, disabledAt, deleted
}

// upgradedChannels migrates a seeded schema to channels.
func upgradedChannels(t *testing.T) (*pgxpool.Pool, channelsSeed) {
	t.Helper()
	pool := testPool(t)
	migrateBefore(t, pool, channelsMigration)
	s := seedChannelWorkspaces(t, pool)
	if err := Migrate(context.Background(), pool); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	return pool, s
}

// The upgrade makes every email and web form setup a channel, the default of its type, and moves what
// they held to the channel: settings, domain state, Gmail, consents, Message-IDs and reply tokens. A
// channel disconnected before it still names the conversations and deliveries it carried.
func TestChannelsMigrationBackfillsAChannelForEveryEmailAndWebFormSetup(t *testing.T) {
	pool, s := upgradedChannels(t)
	ctx := context.Background()

	var live, setups, defaults int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM channels WHERE deleted_at IS NULL),(SELECT count(*) FROM email_channels)+(SELECT count(*) FROM web_form_channels),
		(SELECT count(*) FROM channels WHERE is_default)`).Scan(&live, &setups, &defaults); err != nil {
		t.Fatal(err)
	}
	if live != setups || live != 4 || defaults != 4 {
		t.Fatalf("live channels=%d, email and web form setups=%d, defaults=%d, want 4 each", live, setups, defaults)
	}
	emailID, on, _, _ := channelOf(t, pool, s.both, "email")
	formID, formOn, _, _ := channelOf(t, pool, s.both, "web_form")
	if emailID == "" || !on || formID == "" || !formOn {
		t.Fatalf("both: email %q on=%v, form %q on=%v", emailID, on, formID, formOn)
	}
	if id, _, _, _ := channelOf(t, pool, s.emailOnly, "web_form"); id != "" {
		t.Fatalf("email-only got a web form channel %s", id)
	}
	if id, _, _, _ := channelOf(t, pool, s.none, "email"); id != "" {
		t.Fatalf("a workspace without email got an email channel %s", id)
	}
	offID, offOn, offSince, _ := channelOf(t, pool, s.formOff, "web_form")
	if offID == "" || offOn || offSince == nil || !offSince.Equal(time.Date(2026, 10, 3, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("form-off: channel %q on=%v disabled_at=%v, want off since its last change", offID, offOn, offSince)
	}
	var linked, local string
	var legacy bool
	if err := pool.QueryRow(ctx, `SELECT e.channel_id::text,e.hosted_local,f.legacy_slug FROM email_channels e JOIN web_form_channels f USING (workspace_id) WHERE workspace_id=$1`, s.both).
		Scan(&linked, &local, &legacy); err != nil || linked != emailID || local != "support" || !legacy {
		t.Fatalf("email_channels.channel_id=%q hosted_local=%q, form legacy_slug=%v (%v), want %s, support, true", linked, local, legacy, err, emailID)
	}
	var domainName, token, providerID string
	var verified *time.Time
	if err := pool.QueryRow(ctx, `SELECT domain,verification_token,verified_at,provider_domain_id FROM email_domains WHERE workspace_id=$1`, s.both).
		Scan(&domainName, &token, &verified, &providerID); err != nil || domainName != "both.example" || token != "token-both" || verified == nil || providerID != "pd-both" {
		t.Fatalf("email_domains: %q %q %v %q (%v)", domainName, token, verified, providerID, err)
	}
	var mailboxChannel, consentChannel string
	if err := pool.QueryRow(ctx, `SELECT (SELECT channel_id::text FROM mailbox_connections WHERE workspace_id=$1),(SELECT channel_id::text FROM mailbox_oauth_states WHERE workspace_id=$1)`, s.both).
		Scan(&mailboxChannel, &consentChannel); err != nil || mailboxChannel != emailID || consentChannel != emailID {
		t.Fatalf("gmail channel=%q consent channel=%q (%v), want %s", mailboxChannel, consentChannel, err, emailID)
	}
	var orphanChannel *string
	if err := pool.QueryRow(ctx, `SELECT channel_id::text FROM mailbox_oauth_states WHERE workspace_id=$1`, s.none).Scan(&orphanChannel); err != nil || orphanChannel != nil {
		t.Fatalf("a consent of a workspace without email: channel %v (%v), want it kept with none", orphanChannel, err)
	}

	// gone disconnected email before the upgrade: a removed channel carries its history.
	goneID, _, _, goneDeleted := channelOf(t, pool, s.gone, "email")
	var goneDefault bool
	var goneCreated time.Time
	if err := pool.QueryRow(ctx, `SELECT is_default,created_at FROM channels WHERE id=$1`, goneID).Scan(&goneDefault, &goneCreated); err != nil {
		t.Fatal(err)
	}
	if goneID == "" || !goneDeleted || goneDefault || !goneCreated.Equal(time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)) {
		t.Fatalf("gone: channel %q removed=%v default=%v created=%v", goneID, goneDeleted, goneDefault, goneCreated)
	}
	var goneConversation, goneOutbox string
	if err := pool.QueryRow(ctx, `SELECT (SELECT channel_id::text FROM conversations WHERE id=$2),(SELECT channel_id::text FROM channel_outbox WHERE workspace_id=$1)`,
		s.gone, s.goneConversation).Scan(&goneConversation, &goneOutbox); err != nil || goneConversation != goneID || goneOutbox != goneID {
		t.Fatalf("gone: conversation channel=%q delivery channel=%q (%v), want %s", goneConversation, goneOutbox, err, goneID)
	}
	var history []string
	rows, err := pool.Query(ctx, `SELECT kind FROM channel_events WHERE channel_id=$1 ORDER BY created_at,seq`, goneID)
	if err != nil {
		t.Fatal(err)
	}
	if history, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil || fmt.Sprint(history) != "[added removed]" {
		t.Fatalf("gone history = %v (%v)", history, err)
	}

	conversationChannel := func(id string) *string {
		var channel *string
		if err := pool.QueryRow(ctx, `SELECT channel_id::text FROM conversations WHERE workspace_id=$1 AND id=$2`, s.both, id).Scan(&channel); err != nil {
			t.Fatal(err)
		}
		return channel
	}
	if got := conversationChannel(s.emailConversation); got == nil || *got != emailID {
		t.Errorf("email conversation channel_id=%v, want %s", got, emailID)
	}
	if got := conversationChannel(s.formConversation); got == nil || *got != formID {
		t.Errorf("web form conversation channel_id=%v, want %s", got, formID)
	}
	if got := conversationChannel(s.agentConversation); got != nil {
		t.Errorf("a conversation an agent started got channel %s", *got)
	}
	var external []string
	rows, err = pool.Query(ctx, `SELECT external_id FROM messages WHERE workspace_id=$1 AND external_id IS NOT NULL ORDER BY 1`, s.both)
	if err != nil {
		t.Fatal(err)
	}
	if external, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(external) != "[email:<in-1@example.com> email:<out-1@both.example>]" {
		t.Errorf("external ids = %v", external)
	}
	var key string
	if err := pool.QueryRow(ctx, `SELECT key FROM conversation_thread_keys WHERE workspace_id=$1 AND conversation_id=$2 AND channel_type='email'`,
		s.both, s.emailConversation).Scan(&key); err != nil || key != "reply-token:tok-1" {
		t.Errorf("thread key=%q (%v)", key, err)
	}
	var outboxChannel, outboxType, transport string
	if err := pool.QueryRow(ctx, `SELECT channel_id::text,channel_type,transport FROM channel_outbox WHERE workspace_id=$1`, s.both).
		Scan(&outboxChannel, &outboxType, &transport); err != nil || outboxChannel != emailID || outboxType != "email" || transport != "smtp" {
		t.Errorf("outbox row channel=%q type=%q transport=%q (%v)", outboxChannel, outboxType, transport, err)
	}
	// What only an older image read is gone, not kept twice.
	for _, old := range []struct{ table, column string }{{"messages", "email_message_id"}, {"conversations", "email_reply_token"},
		{"web_form_channels", "enabled"}, {"email_channels", "domain_verified_at"}} {
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema=current_schema() AND table_name=$1 AND column_name=$2)`,
			old.table, old.column).Scan(&exists); err != nil || exists {
			t.Errorf("%s.%s is still there (%v)", old.table, old.column, err)
		}
	}
	if tableExists(t, pool, "email_outbox") {
		t.Error("email_outbox is still there")
	}
}

// The schema keeps a workspace's channels apart: one default per type, names, mailboxes, forwarding
// addresses and Gmail accounts of live channels never repeat, and a removed channel blocks nothing.
func TestChannelsSchemaRefusesDuplicates(t *testing.T) {
	pool, s := upgradedChannels(t)
	ctx := context.Background()
	refused := func(constraint, sql string, args ...any) {
		t.Helper()
		_, err := pool.Exec(ctx, sql, args...)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || (pgErr.Code != "23505" && pgErr.Code != "23514") || pgErr.ConstraintName != constraint {
			t.Fatalf("%s: %v, want a violation of %s", sql, err, constraint)
		}
	}
	addEmail := `WITH c AS (INSERT INTO channels(workspace_id,type,name) VALUES($1,'email',$2) RETURNING id,workspace_id)
		INSERT INTO email_channels(workspace_id,channel_id,support_address,sender_name,hosted_local) SELECT workspace_id,id,$3,'Desk',$4 FROM c`
	refused("channels_default_idx", `INSERT INTO channels(workspace_id,type,name,is_default) VALUES($1,'email','Second',true)`, s.both)
	refused("channels_name_idx", `INSERT INTO channels(workspace_id,type,name) VALUES($1,'email','EMAIL')`, s.both)
	refused("email_channels_mailbox_idx", addEmail, s.both, "Plus", "support+vip@both.example", "plus")
	refused("email_channels_hosted_local_idx", addEmail, s.both, "Local", "sales@both.example", "support")
	if _, err := pool.Exec(ctx, addEmail, s.both, "Billing", "billing@both.example", "billing"); err != nil {
		t.Fatalf("a second mailbox on the domain: %v", err)
	}
	refused("mailbox_connections_workspace_address_idx", `INSERT INTO mailbox_connections(workspace_id,channel_id,provider,address,scopes,token_key_id,refresh_token_sealed)
		SELECT workspace_id,channel_id,'google','support@both.example','s','k','\x01' FROM email_channels WHERE support_address='billing@both.example'`)
	if _, err := pool.Exec(ctx, addEmail, s.both, "Other", "x@other.example", "x"); err == nil {
		t.Fatal("an email channel on a domain without its email_domains row was stored")
	}
	refused("email_channels_support_address_shape", `UPDATE email_channels SET support_address='+tag@both.example' WHERE support_address='billing@both.example'`)

	// A removed channel's name and its type's default are free again.
	if _, err := pool.Exec(ctx, `UPDATE channels SET name='Old',enabled=false,disabled_at=now(),deleted_at=now(),is_default=false WHERE workspace_id=$1 AND type='web_form'`, s.both); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO channels(workspace_id,type,name,is_default) VALUES($1,'web_form','Old',true)`, s.both); err != nil {
		t.Fatalf("a removed channel still blocks its name or the default: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE channels SET is_default=true WHERE deleted_at IS NOT NULL`); err == nil {
		t.Fatal("a removed channel became a default")
	}
}

// Rolling back works for a workspace that fits the old schema and refuses, naming it, for one that
// does not: the default channel's settings, domain, Gmail, Message-IDs and reply tokens come back.
func TestChannelsMigrationRollsBackOnlyWhatItCan(t *testing.T) {
	pool, s := upgradedChannels(t)
	ctx := context.Background()
	refused := func(setup, undo, want string) {
		t.Helper()
		if _, err := pool.Exec(ctx, setup); err != nil {
			t.Fatal(err)
		}
		err := rollbackThrough(ctx, pool, channelsMigration)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("rollback with %q: %v, want refusal %q", setup, err, want)
		}
		if _, err := pool.Exec(ctx, undo); err != nil {
			t.Fatal(err)
		}
	}
	refused(`INSERT INTO contacts(workspace_id,name) SELECT id,'Chat only' FROM workspaces WHERE slug='none'`,
		`DELETE FROM contacts WHERE email IS NULL`, "contacts known only on a channel")
	refused(`UPDATE channels SET enabled=false,disabled_at=now() WHERE type='email' AND workspace_id=(SELECT id FROM workspaces WHERE slug='both')`,
		`UPDATE channels SET enabled=true,disabled_at=NULL WHERE type='email' AND deleted_at IS NULL`, "email channel is off")
	refused(`INSERT INTO channel_inbound_events(workspace_id,channel_id,state,raw,hold_reason) SELECT workspace_id,id,'held','{}','channel_off' FROM channels WHERE type='web_form' LIMIT 1`,
		`DELETE FROM channel_inbound_events`, "not conversations yet")
	refused(`WITH c AS (INSERT INTO channels(workspace_id,type,name) SELECT id,'email','Billing' FROM workspaces WHERE slug='both' RETURNING id,workspace_id)
		INSERT INTO email_channels(workspace_id,channel_id,support_address,sender_name,hosted_local) SELECT workspace_id,id,'billing@both.example','Both','billing' FROM c`,
		`DELETE FROM channels WHERE name='Billing'`, "both: Email <support@both.example>, Billing <billing@both.example>")
	refused(`WITH c AS (INSERT INTO channels(workspace_id,type,name) SELECT id,'web_form','Careers form' FROM workspaces WHERE slug='both' RETURNING id,workspace_id)
		INSERT INTO web_form_channels(workspace_id,channel_id) SELECT workspace_id,id FROM c`,
		`DELETE FROM channels WHERE name='Careers form'`, "more than one web form (both: Web form, Careers form)")
	refused(`INSERT INTO channel_types VALUES('loopback',true); INSERT INTO channels(workspace_id,type,name) SELECT id,'loopback','Loop' FROM workspaces WHERE slug='none'`,
		`DELETE FROM channels WHERE type='loopback'; DELETE FROM channel_types WHERE type='loopback'`, "other than email and web form")

	// The form channel was turned off in the new schema only; rolling back keeps it off.
	if _, err := pool.Exec(ctx, `UPDATE channels SET enabled=false,disabled_at=now() WHERE type='web_form' AND workspace_id=$1`, s.both); err != nil {
		t.Fatal(err)
	}
	if err := rollbackThrough(ctx, pool, channelsMigration); err != nil {
		t.Fatalf("roll back: %v", err)
	}
	var setups, sent, gmail int
	var formOn bool
	var verified *time.Time
	var providerID, messageID, token string
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM email_channels)+(SELECT count(*) FROM web_form_channels),
		(SELECT count(*) FROM email_outbox WHERE status='sent'),(SELECT enabled FROM web_form_channels WHERE workspace_id=$1),
		(SELECT domain_verified_at FROM email_channels WHERE workspace_id=$1),(SELECT provider_domain_id FROM email_channels WHERE workspace_id=$1),
		(SELECT count(*) FROM mailbox_connections WHERE workspace_id=$1),
		(SELECT email_message_id FROM messages WHERE id=$2),(SELECT email_reply_token FROM conversations WHERE id=$3)`, s.both, s.reply, s.emailConversation).
		Scan(&setups, &sent, &formOn, &verified, &providerID, &gmail, &messageID, &token); err != nil {
		t.Fatal(err)
	}
	if setups != 4 || sent != 2 || formOn || verified == nil || providerID != "pd-both" || gmail != 1 || messageID != "<out-1@both.example>" || token != "tok-1" {
		t.Fatalf("after rollback: setups=%d sent=%d form on=%v verified=%v provider=%q gmail=%d Message-ID=%q token=%q",
			setups, sent, formOn, verified, providerID, gmail, messageID, token)
	}
	if tableExists(t, pool, "channels") || tableExists(t, pool, "email_domains") {
		t.Fatal("channels or email_domains survived the rollback")
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("up again: %v", err)
	}
	var channels int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM channels WHERE deleted_at IS NULL`).Scan(&channels); err != nil || channels != 4 {
		t.Fatalf("live channels after up again=%d (%v), want 4", channels, err)
	}
}

// A held event's raw bytes count toward the workspace's storage while it is held, and stop counting
// once it becomes a message or is discarded, so a channel left off cannot be filled without limit.
func TestHeldInboundEventsCountTowardStorage(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var workspaceID, channelID string
	if err := pool.QueryRow(ctx, `WITH w AS (INSERT INTO workspaces(slug,name) VALUES('held','Held') RETURNING id),
		c AS (INSERT INTO channels(workspace_id,type,name) SELECT id,'web_form','Form' FROM w RETURNING id,workspace_id)
		SELECT workspace_id::text,id::text FROM c`).Scan(&workspaceID, &channelID); err != nil {
		t.Fatal(err)
	}
	counted := func() (int64, int64) {
		t.Helper()
		var c, s int64
		if err := pool.QueryRow(ctx, `SELECT counted_storage_bytes,stored_storage_bytes FROM workspaces WHERE id=$1`, workspaceID).Scan(&c, &s); err != nil {
			t.Fatal(err)
		}
		return c, s
	}
	var held, other string
	if err := pool.QueryRow(ctx, `INSERT INTO channel_inbound_events(workspace_id,channel_id,state,raw_object_key,raw_bytes,hold_reason)
		VALUES($1,$2,'held','k/1',1000,'channel_off') RETURNING id::text`, workspaceID, channelID).Scan(&held); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO channel_inbound_events(workspace_id,channel_id,state,raw_object_key,raw_bytes,drop_reason)
		VALUES($1,$2,'dropped','k/2',500,'own_address') RETURNING id::text`, workspaceID, channelID).Scan(&other); err != nil {
		t.Fatal(err)
	}
	if c, s := counted(); c != 1000 || s != 1000 {
		t.Fatalf("held: counted=%d stored=%d, want 1000 each (the dropped event counts nothing)", c, s)
	}
	if _, err := pool.Exec(ctx, `UPDATE channel_inbound_events SET state='processed',hold_reason=NULL WHERE id=$1`, held); err != nil {
		t.Fatal(err)
	}
	if c, s := counted(); c != 0 || s != 0 {
		t.Fatalf("replayed: counted=%d stored=%d, want 0 (its attachment counts it now)", c, s)
	}
	if _, err := pool.Exec(ctx, `UPDATE channel_inbound_events SET state='held',hold_reason='channel_off' WHERE id=$1`, held); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM channel_inbound_events WHERE id=$1`, held); err != nil {
		t.Fatal(err)
	}
	if c, s := counted(); c != 0 || s != 0 {
		t.Fatalf("deleted: counted=%d stored=%d, want 0", c, s)
	}
}

const microsoftMigration = "20261025120000-microsoft-connection.up.sql"

// Microsoft 365 joins Gmail as a mailbox provider: a connection, a consent, its replies' transport and its
// mailbox_unavailable reason are accepted. Rolling back refuses while any of them exists, naming the
// workspace, and drops nothing; without them it restores the Gmail-only checks.
func TestMicrosoftConnectionMigrationAcceptsMicrosoftAndRollsBackOnlyWithoutIt(t *testing.T) {
	pool, s := upgradedChannels(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `UPDATE mailbox_connections SET provider='microsoft',account_id='00000000-0000-0000-0000-00000000abcd',
		status='failed',error_code='mailbox_unavailable',failed_at=now() WHERE workspace_id=$1`, s.both); err != nil {
		t.Fatalf("microsoft connection refused: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE mailbox_oauth_states SET provider='microsoft' WHERE workspace_id=$1`, s.both); err != nil {
		t.Fatalf("microsoft consent refused: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE channel_outbox SET transport='microsoft' WHERE message_id=$1`, s.reply); err != nil {
		t.Fatalf("microsoft transport refused: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE channel_outbox SET transport='outlook' WHERE message_id=$1`, s.reply); err == nil {
		t.Fatal("an unknown transport was accepted")
	}
	err := rollbackThrough(ctx, pool, microsoftMigration)
	if err == nil || !strings.Contains(err.Error(), "workspaces both") {
		t.Fatalf("rollback with a Microsoft connection: %v, want a refusal naming the workspace", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE mailbox_connections SET provider='google',account_id=NULL,status='connected',error_code=NULL,failed_at=NULL WHERE workspace_id=$1`, s.both); err != nil {
		t.Fatal(err)
	}
	if err := rollbackThrough(ctx, pool, microsoftMigration); err == nil || !strings.Contains(err.Error(), "replies went through it") {
		t.Fatalf("rollback with a reply sent through Microsoft: %v, want a refusal", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE channel_outbox SET transport='smtp' WHERE message_id=$1`, s.reply); err != nil {
		t.Fatal(err)
	}
	if err := rollbackThrough(ctx, pool, microsoftMigration); err != nil {
		t.Fatalf("roll back: %v", err)
	}
	var connections, consents int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM mailbox_connections),(SELECT count(*) FROM mailbox_oauth_states WHERE workspace_id=$1)`, s.both).
		Scan(&connections, &consents); err != nil {
		t.Fatal(err)
	}
	if connections != 1 || consents != 0 {
		t.Fatalf("after rollback: connections=%d, Microsoft consents left=%d; want the Gmail connection kept and the consent gone", connections, consents)
	}
	if _, err := pool.Exec(ctx, `UPDATE mailbox_connections SET provider='microsoft' WHERE workspace_id=$1`, s.both); err == nil {
		t.Fatal("the rolled back schema accepted a Microsoft connection")
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("up again: %v", err)
	}
}
