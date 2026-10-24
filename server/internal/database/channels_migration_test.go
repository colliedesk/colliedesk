package database

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
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
	both, emailOnly, formOff, none                         string
	emailConversation, formConversation, agentConversation string
	reply                                                  string
}

// seedChannelWorkspaces writes, the way the release before channels did, a workspace with email and
// an enabled web form, one with email only, one with a web form that is off and one with neither.
func seedChannelWorkspaces(t *testing.T, pool *pgxpool.Pool) channelsSeed {
	t.Helper()
	ctx := context.Background()
	var s channelsSeed
	for slug, target := range map[string]*string{"both": &s.both, "email-only": &s.emailOnly, "form-off": &s.formOff, "none": &s.none} {
		if err := pool.QueryRow(ctx, `INSERT INTO workspaces(slug,name) VALUES($1,$1) RETURNING id::text`, slug).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{s.both, s.emailOnly} {
		if _, err := pool.Exec(ctx, `INSERT INTO email_channels(workspace_id,support_address,domain_verification_token,created_at)
			SELECT id,'support@'||slug||'.example','token','2026-10-01 10:00:00+00' FROM workspaces WHERE id=$1`, id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO web_form_channels(workspace_id,enabled,allowed_origins,updated_at) VALUES
		($1,true,'{https://both.example}','2026-10-02 10:00:00+00'),($2,false,'{}','2026-10-03 10:00:00+00')`, s.both, s.formOff); err != nil {
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

// The upgrade makes every email and web form setup a channel and ties what they hold to it, without
// changing a value an older release wrote.
func TestChannelsMigrationBackfillsAChannelForEveryEmailAndWebFormSetup(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	migrateBefore(t, pool, channelsMigration)
	s := seedChannelWorkspaces(t, pool)
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("upgrade: %v", err)
	}

	var channels, setups int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM channels),(SELECT count(*) FROM email_channels)+(SELECT count(*) FROM web_form_channels)`).
		Scan(&channels, &setups); err != nil {
		t.Fatal(err)
	}
	if channels != setups || channels != 4 {
		t.Fatalf("channels=%d, email and web form setups=%d, want 4 each", channels, setups)
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
	var linked string
	if err := pool.QueryRow(ctx, `SELECT channel_id::text FROM email_channels WHERE workspace_id=$1`, s.both).Scan(&linked); err != nil || linked != emailID {
		t.Fatalf("email_channels.channel_id=%q (%v), want %s", linked, err, emailID)
	}
	var events int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM channel_events WHERE kind='added' AND detail->>'source'='upgrade'`).Scan(&events); err != nil || events != 4 {
		t.Fatalf("upgrade history events=%d (%v), want 4", events, err)
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
	rows, err := pool.Query(ctx, `SELECT external_id FROM messages WHERE workspace_id=$1 AND external_id IS NOT NULL ORDER BY 1`, s.both)
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
	var viewRows int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM email_outbox WHERE workspace_id=$1 AND status='sent'`, s.both).Scan(&viewRows); err != nil || viewRows != 1 {
		t.Errorf("email_outbox view rows=%d (%v), want 1", viewRows, err)
	}
}

// After a rollback of the image that keeps the schema, an older image writes only the old columns.
// The compatibility triggers give its rows the channel they belong to, as the app role it runs as.
func TestChannelsMigrationKeepsAnOlderImageWorking(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	migrateBefore(t, pool, channelsMigration)
	s := seedChannelWorkspaces(t, pool)
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	db := New(pool)
	inWorkspace := func(workspaceID, sql string, args ...any) {
		t.Helper()
		if err := db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, sql, args...)
			return err
		}); err != nil {
			t.Fatalf("%s: %v", strings.Fields(sql)[0]+" "+strings.Fields(sql)[2], err)
		}
	}
	// The old upsert of email settings (store/email-channels.go) on a workspace that has email reuses its channel.
	connect := `INSERT INTO email_channels(workspace_id,support_address,domain_verification_token,sender_name) VALUES($1,$2,'token','Desk')
		ON CONFLICT(workspace_id) DO UPDATE SET support_address=EXCLUDED.support_address,updated_at=now()`
	before, _, _, _ := channelOf(t, pool, s.both, "email")
	inWorkspace(s.both, connect, s.both, "help@both.example")
	if after, _, _, _ := channelOf(t, pool, s.both, "email"); after != before {
		t.Fatalf("an upsert on connected email made channel %s, want %s kept", after, before)
	}
	// Connecting email in a workspace without it creates the channel.
	inWorkspace(s.none, connect, s.none, "support@none.example")
	created, on, _, _ := channelOf(t, pool, s.none, "email")
	if created == "" || !on {
		t.Fatalf("connect: channel %q on=%v", created, on)
	}
	// Disconnecting removes it, and connecting again makes a new one.
	inWorkspace(s.none, `DELETE FROM email_channels WHERE workspace_id=$1`, s.none)
	if id, on, _, deleted := channelOf(t, pool, s.none, "email"); id != created || on || !deleted {
		t.Fatalf("disconnect: channel %q on=%v deleted=%v, want %s removed", id, on, deleted, created)
	}
	inWorkspace(s.none, connect, s.none, "support@none.example")
	if again, _, _, deleted := channelOf(t, pool, s.none, "email"); again == created || deleted {
		t.Fatalf("reconnect: channel %q deleted=%v, want a new live channel", again, deleted)
	}

	// The old web form save (store/web-form.go) turns the channel on and off.
	save := `INSERT INTO web_form_channels(workspace_id,enabled,allowed_origins) VALUES($1,$2,'{}')
		ON CONFLICT(workspace_id) DO UPDATE SET enabled=EXCLUDED.enabled,allowed_origins=EXCLUDED.allowed_origins,updated_at=now()`
	formID, _, _, _ := channelOf(t, pool, s.formOff, "web_form")
	inWorkspace(s.formOff, save, s.formOff, true)
	if id, on, since, _ := channelOf(t, pool, s.formOff, "web_form"); id != formID || !on || since != nil {
		t.Fatalf("turn form on: channel %q on=%v disabled_at=%v, want %s on", id, on, since, formID)
	}
	inWorkspace(s.formOff, save, s.formOff, false)
	if _, on, since, _ := channelOf(t, pool, s.formOff, "web_form"); on || since == nil {
		t.Fatalf("turn form off: on=%v disabled_at=%v", on, since)
	}
	inWorkspace(s.emailOnly, save, s.emailOnly, false)
	if id, on, since, _ := channelOf(t, pool, s.emailOnly, "web_form"); id == "" || on || since == nil {
		t.Fatalf("first save of a form that is off: channel %q on=%v disabled_at=%v", id, on, since)
	}
	var kinds []string
	rows, err := pool.Query(ctx, `SELECT kind FROM channel_events WHERE workspace_id=$1 ORDER BY created_at,seq`, s.formOff)
	if err != nil {
		t.Fatal(err)
	}
	if kinds, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil || fmt.Sprint(kinds) != "[added enabled disabled]" {
		t.Fatalf("form-off history = %v (%v)", kinds, err)
	}

	// Conversations, messages, reply tokens and deliveries written the old way.
	emailID, _, _, _ := channelOf(t, pool, s.both, "email")
	var conversationID, messageID string
	if err := db.InWorkspace(ctx, s.both, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `WITH c AS (SELECT id FROM contacts WHERE workspace_id=$1 LIMIT 1),
			v AS (INSERT INTO conversations(workspace_id,contact_id,subject,channel) SELECT $1,id,'New mail','email' FROM c RETURNING id),
			m AS (INSERT INTO messages(workspace_id,conversation_id,kind,body,email_message_id) SELECT $1,id,'incoming','x','<in-2@example.com>' FROM v RETURNING id)
			SELECT (SELECT id::text FROM v),(SELECT id::text FROM m)`, s.both).Scan(&conversationID, &messageID)
	}); err != nil {
		t.Fatal(err)
	}
	var channel, external *string
	if err := pool.QueryRow(ctx, `SELECT c.channel_id::text,m.external_id FROM conversations c JOIN messages m ON m.conversation_id=c.id WHERE m.id=$1`, messageID).
		Scan(&channel, &external); err != nil {
		t.Fatal(err)
	}
	if channel == nil || *channel != emailID || external == nil || *external != "email:<in-2@example.com>" {
		t.Fatalf("old insert: conversation channel=%v message external_id=%v", channel, external)
	}
	var replyID string
	if err := db.InWorkspace(ctx, s.both, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `INSERT INTO messages(workspace_id,conversation_id,kind,body) VALUES($1,$2,'outgoing','re') RETURNING id::text`,
			s.both, conversationID).Scan(&replyID); err != nil {
			return err
		}
		// enqueueReply (store/email-enqueue.go) gives the reply its Message-ID and the conversation a token, then queues it.
		if _, err := tx.Exec(ctx, `UPDATE messages SET email_message_id='<out-2@both.example>' WHERE workspace_id=$1 AND id=$2`, s.both, replyID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE conversations SET email_reply_token=COALESCE(email_reply_token,'tok-2') WHERE workspace_id=$1 AND id=$2`, s.both, conversationID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO email_outbox(workspace_id,message_id,status,last_error,created_at,recipients,transport) VALUES($1,$2,'queued',NULL,now(),1,'smtp')`,
			s.both, replyID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT external_id FROM messages WHERE id=$1`, replyID).Scan(&external); err != nil || external == nil || *external != "email:<out-2@both.example>" {
		t.Fatalf("Message-ID assigned after insert: external_id=%v (%v)", external, err)
	}
	var key string
	if err := pool.QueryRow(ctx, `SELECT key FROM conversation_thread_keys WHERE conversation_id=$1`, conversationID).Scan(&key); err != nil || key != "reply-token:tok-2" {
		t.Fatalf("reply token key=%q (%v)", key, err)
	}
	var outboxChannel, outboxType string
	if err := pool.QueryRow(ctx, `SELECT channel_id::text,channel_type FROM channel_outbox WHERE message_id=$1`, replyID).Scan(&outboxChannel, &outboxType); err != nil ||
		outboxChannel != emailID || outboxType != "email" {
		t.Fatalf("old outbox insert: channel=%q type=%q (%v)", outboxChannel, outboxType, err)
	}

	// The worker's claim (store/email-outbox.go) through the view, on the privileged path, takes only email rows.
	if _, err := pool.Exec(ctx, `INSERT INTO channel_types(type,connectable,singleton) VALUES('loopback',true,false)`); err != nil {
		t.Fatal(err)
	}
	var otherMessage string
	if err := pool.QueryRow(ctx, `WITH ch AS (INSERT INTO channels(workspace_id,type,singleton,name) VALUES($1,'loopback',false,'Loop') RETURNING id),
		m AS (INSERT INTO messages(workspace_id,conversation_id,kind,body) VALUES($1,$2,'outgoing','loop') RETURNING id),
		o AS (INSERT INTO channel_outbox(workspace_id,message_id,channel_type,channel_id,transport) SELECT $1,m.id,'loopback',ch.id,NULL FROM m,ch RETURNING 1)
		SELECT id::text FROM m`, s.both, conversationID).Scan(&otherMessage); err != nil {
		t.Fatal(err)
	}
	var claimed []string
	if err := db.Privileged(ctx, PrivilegeOutboxClaim, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `WITH due AS (
				SELECT id FROM email_outbox WHERE status='queued' AND next_attempt_at<=$1 AND (locked_until IS NULL OR locked_until<=$1)
				ORDER BY next_attempt_at LIMIT $2 FOR UPDATE SKIP LOCKED)
			UPDATE email_outbox o SET locked_until=$3,attempts=o.attempts+1,claim_token=gen_random_uuid(),updated_at=$1 FROM due WHERE o.id=due.id
			RETURNING o.message_id::text`, time.Now().Add(time.Minute), 10, time.Now().Add(time.Hour))
		if err != nil {
			return err
		}
		claimed, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(claimed) != fmt.Sprint([]string{replyID}) {
		t.Fatalf("claim through the view took %v, want only the email reply %s", claimed, replyID)
	}
	// Row-level security holds through the view.
	var seen int
	if err := db.InWorkspace(ctx, s.formOff, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM email_outbox`).Scan(&seen)
	}); err != nil || seen != 0 {
		t.Fatalf("another workspace saw %d outbox rows through the view (%v)", seen, err)
	}
}

// Rolling back keeps every email and web form setup and refuses while a row holds what the old
// schema has no place for.
func TestChannelsMigrationRollsBackOnlyWhatItCan(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	migrateBefore(t, pool, channelsMigration)
	s := seedChannelWorkspaces(t, pool)
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
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
		`UPDATE channels SET enabled=true,disabled_at=NULL WHERE type='email'`, "email channel is off")
	refused(`INSERT INTO channel_inbound_events(workspace_id,channel_id,state,raw,hold_reason) SELECT workspace_id,id,'held','{}','channel_off' FROM channels WHERE type='web_form' LIMIT 1`,
		`DELETE FROM channel_inbound_events`, "not conversations yet")
	refused(`INSERT INTO channel_types VALUES('loopback',true,false); INSERT INTO channels(workspace_id,type,singleton,name) SELECT id,'loopback',false,'Loop' FROM workspaces WHERE slug='none'`,
		`DELETE FROM channels WHERE type='loopback'; DELETE FROM channel_types WHERE type='loopback'`, "other than email and web form")

	// The form channel was turned off in the new schema only; rolling back keeps it off.
	if _, err := pool.Exec(ctx, `UPDATE channels SET enabled=false,disabled_at=now() WHERE type='web_form' AND workspace_id=$1`, s.both); err != nil {
		t.Fatal(err)
	}
	if err := rollbackThrough(ctx, pool, channelsMigration); err != nil {
		t.Fatalf("roll back: %v", err)
	}
	var setups, sent int
	var formOn bool
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM email_channels)+(SELECT count(*) FROM web_form_channels),
		(SELECT count(*) FROM email_outbox WHERE status='sent'),(SELECT enabled FROM web_form_channels WHERE workspace_id=$1)`, s.both).
		Scan(&setups, &sent, &formOn); err != nil {
		t.Fatal(err)
	}
	if setups != 4 || sent != 1 || formOn {
		t.Fatalf("after rollback: setups=%d sent deliveries=%d form on=%v, want 4, 1, off", setups, sent, formOn)
	}
	if tableExists(t, pool, "channels") {
		t.Fatal("channels survived the rollback")
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("up again: %v", err)
	}
	var channels int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM channels`).Scan(&channels); err != nil || channels != 4 {
		t.Fatalf("channels after up again=%d (%v), want 4", channels, err)
	}
}
