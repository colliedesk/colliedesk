package httpapi_test

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	emailSettingsLatencyBudget = 20 * time.Millisecond
	emailSettingsWarmup        = 10
	emailSettingsSamples       = 200
	// droppedPerChannel is a year of spam to a public support address that the hosted address drops.
	droppedPerChannel = 30_000
)

// TestEmailChannelSettingsLatencyBudget times the store call behind GET /api/v1/channels/email besides
// the channel itself: the sending pause, the finished import and whether customer mail came through each
// mailbox. The inbox reads it on every load for its line about moving the mail. The fixture is the worst
// case for the forwarding rule: the default mailbox forwarded 20,000 customer mail, two newer mailboxes never
// forwarded, and every mailbox has 30,000 dropped mail in its journal (which never counts); a second
// workspace is of the same size.
func TestEmailChannelSettingsLatencyBudget(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	adminA := createSession(t, api, service, "Settings Budget A", "admin-a@settings-budget.example.com")
	adminB := createSession(t, api, service, "Settings Budget B", "admin-b@settings-budget.example.com")
	for _, admin := range []string{adminA, adminB} {
		suffix := workspaceSlug(t, api, admin)
		connectEmail(t, api, admin, "support@"+suffix+".example.com")
		addEmailChannel(t, api, admin, "billing@"+suffix+".example.com")
		addEmailChannel(t, api, admin, "sales@"+suffix+".example.com")
		seedDroppedMail(t, pool, workspaceID(t, api, admin))
		seedForwardedMail(t, pool, workspaceID(t, api, admin))
	}
	ctx := context.Background()
	for _, table := range []string{"channel_inbound_events", "conversations", "messages"} {
		if _, err := pool.Exec(ctx, `ANALYZE `+table); err != nil {
			t.Fatal(err)
		}
	}
	data := store.New(pool)
	workspaceA := workspaceID(t, api, adminA)
	read := func() (store.EmailSettingsState, time.Duration) {
		started := time.Now()
		state, err := data.EmailSettingsState(ctx, workspaceA)
		elapsed := time.Since(started)
		if err != nil {
			t.Fatal(err)
		}
		return state, elapsed
	}
	for range emailSettingsWarmup {
		read()
	}
	durations := make([]time.Duration, emailSettingsSamples)
	var sample store.EmailSettingsState
	for index := range durations {
		sample, durations[index] = read()
	}
	if len(sample.Channels) != 3 || !sample.Channels[0].Forwarded || sample.Channels[1].Forwarded || sample.Channels[2].Forwarded {
		t.Fatalf("fixture is not realistic: %+v", sample.Channels)
	}
	sort.Slice(durations, func(left, right int) bool { return durations[left] < durations[right] })
	p95 := durations[emailSettingsSamples*95/100-1]
	t.Logf("email settings state p95=%s", p95)
	if p95 > emailSettingsLatencyBudget {
		t.Fatalf("email settings state p95 exceeds %s: %s", emailSettingsLatencyBudget, p95)
	}
}

// seedDroppedMail writes droppedPerChannel dropped mail into the journal of every email channel of the workspace.
func seedDroppedMail(t *testing.T, pool *pgxpool.Pool, workspace string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `INSERT INTO channel_inbound_events(workspace_id,channel_id,state,raw,drop_reason,received_at)
		SELECT c.workspace_id,c.id,'dropped','{"from":"bulk@spam.example"}'::jsonb,'bulk',now()-(n||' minutes')::interval
		FROM channels c, generate_series(1,$2::int) AS n WHERE c.workspace_id=$1 AND c.type='email'`, workspace, droppedPerChannel); err != nil {
		t.Fatal(err)
	}
}

// seedForwardedMail gives the workspace's default email channel 20,000 customer mail forwarded through its
// mailbox.
func seedForwardedMail(t *testing.T, pool *pgxpool.Pool, workspace string) {
	t.Helper()
	ctx := context.Background()
	var contact, channel string
	if err := pool.QueryRow(ctx, `SELECT id::text FROM channels WHERE workspace_id=$1 AND type='email' AND is_default`, workspace).Scan(&channel); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO contacts(workspace_id,name,email) VALUES($1,'Fixture Contact',$2) RETURNING id::text`,
		workspace, "forwarded-"+workspace+"@customer.example").Scan(&contact); err != nil {
		t.Fatal(err)
	}
	// A conversation per customer would cost seconds of triggers; 200 threads of 100 forwarded mail each give
	// the journal the same 20,000 entries with a message.
	if _, err := pool.Exec(ctx, `WITH c AS (INSERT INTO conversations(workspace_id,contact_id,channel_id,subject,created_at,updated_at)
			SELECT $1,$2,$3,'Forwarded '||n,now()-(n||' days')::interval,now()-(n||' days')::interval FROM generate_series(1,200) AS n RETURNING id,created_at),
		m AS (INSERT INTO messages(workspace_id,conversation_id,kind,body,created_at)
			SELECT $1,c.id,'incoming','Forwarded mail',c.created_at+(k||' minutes')::interval FROM c, generate_series(1,100) AS k RETURNING id,created_at)
		INSERT INTO channel_inbound_events(workspace_id,channel_id,state,raw,message_id,received_at,processed_at)
			SELECT $1,$3,'processed','{"from":"customer@customer.example"}'::jsonb,id,created_at,created_at FROM m`, workspace, contact, channel); err != nil {
		t.Fatal(err)
	}
}
