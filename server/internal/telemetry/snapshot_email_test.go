package telemetry_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/channels/channelemail"
	"github.com/colliedesk/colliedesk/server/internal/channels/registry"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/colliedesk/colliedesk/server/internal/telemetry"
)

// hostedMailDomain is where the test cloud gives each new workspace its support address.
const hostedMailDomain = "mail.colliedesk.test"

// channelStore is the store a Colliedesk Cloud server runs: email arrives through the platform's
// forwarding, replies leave by SMTP or a connected Google mailbox.
func channelStore(db *database.DB) *store.Store {
	return store.NewWithDB(db).WithChannels(registry.New(registry.Config{Email: channelemail.Config{
		HostedMailDomain: hostedMailDomain, PlatformForwarding: true, Cloud: true,
		Outbound: store.OutboundPolicy{Enabled: true, GmailEnabled: true, Worker: true, RequireVerifiedDomain: true, FallbackDomain: hostedMailDomain},
	}}))
}

// cloudReporter is the reporter of a Colliedesk Cloud server that sends to server.
func cloudReporter(db *database.DB, server *httptest.Server) *telemetry.Reporter {
	return telemetry.New(db, cloudConfig(server.URL), server.Client()).WithStore(channelStore(db))
}

// A snapshot says whether email really reaches the workspace, the same way its setup guide does, and
// how many customer messages came in: on the cloud every workspace has an email channel from sign-up,
// so email_channel alone cannot tell a team that receives mail from one that never did.
func TestSnapshotSaysWhetherEmailReachesTheWorkspace(t *testing.T) {
	db := newDB(t)
	ctx := context.Background()
	service := auth.NewWithDB(db, "https://app.colliedesk.test").WithSignupKey("0123456789abcdef0123456789abcdef")
	signUp := func(name, email, ip string) string {
		t.Helper()
		workspace, err := service.SignUpCloud(ctx, auth.SignUp{Email: email, Name: name, ClientIP: ip, HostedMailDomain: hostedMailDomain})
		if err != nil {
			t.Fatalf("sign up %s: %v", name, err)
		}
		return workspace.ID
	}
	quiet := signUp("Quiet Desk", "owner@quiet.example", "198.51.100.1")
	busy := signUp("Busy Desk", "owner@busy.example", "198.51.100.2")
	mailboxDesk := signUp("Mailbox Desk", "owner@mailbox-desk.example", "198.51.100.3")
	paused := signUp("Paused Desk", "owner@paused.example", "198.51.100.4")
	// A self-made workspace without email still hears from customers through another channel.
	noEmail, _, err := service.CreateWorkspace(ctx, "Form Desk", "owner@form.example")
	if err != nil {
		t.Fatal(err)
	}
	exec := func(sql string, args ...any) { t.Helper(); execSQL(t, db, sql, args...) }

	// incoming adds a customer message to a conversation of the workspace, on its email channel when
	// it has one, created age ago.
	incoming := func(workspaceID, age, origin string) {
		t.Helper()
		exec(`WITH k AS (INSERT INTO contacts(workspace_id,name,email) VALUES($1,'Customer','customer-'||gen_random_uuid()||'@example.org') RETURNING id),
			v AS (INSERT INTO conversations(workspace_id,contact_id,channel_id,subject)
				SELECT $1,k.id,(SELECT id FROM channels WHERE workspace_id=$1 AND type='email'),'Where is my order' FROM k RETURNING id)
			INSERT INTO messages(workspace_id,conversation_id,kind,body,created_at,origin)
			SELECT $1,v.id,'incoming','Refund please',now()-$2::interval,NULLIF($3,'')::jsonb FROM v`, workspaceID, age, origin)
	}
	// Busy received one message today; one from last week (8 days) and one imported today do not
	// count as this week's, and the imported one is not mail that reached Colliedesk.
	incoming(busy, "2 hours", "")
	incoming(busy, "8 days", "")
	incoming(busy, "1 hour", `{"system":"freshdesk"}`)
	incoming(noEmail.ID, "3 hours", "")
	// Mailbox Desk connected a Google mailbox to its channel; Paused stopped sending, and its mailbox broke.
	mailbox := func(workspaceID, status string) {
		t.Helper()
		exec(`INSERT INTO mailbox_connections(workspace_id,channel_id,provider,address,scopes,token_key_id,refresh_token_sealed,status,error_code,failed_at)
			SELECT workspace_id,channel_id,'google','owner@mailbox.example','s','k','\x01',$2,
				CASE WHEN $2='failed' THEN 'revoked' END,CASE WHEN $2='failed' THEN now() END
			FROM email_channels WHERE workspace_id=$1`, workspaceID, status)
	}
	mailbox(mailboxDesk, "connected")
	mailbox(paused, "failed")
	exec(`UPDATE workspaces SET email_paused_at = now(), email_pause_reason = 'complaints' WHERE id=$1`, paused)

	recorded := &capture{}
	server := recorded.server(t)
	if err := cloudReporter(db, server).SendOnce(ctx); err != nil {
		t.Fatalf("SendOnce: %v", err)
	}
	var sent batch
	if err := json.Unmarshal(recorded.bodyAt(t, "POST /batch/"), &sent); err != nil {
		t.Fatal(err)
	}
	byWorkspace := map[string]map[string]any{}
	for _, event := range sent.Batch {
		byWorkspace[event.DistinctID] = event.Properties
	}

	// Worked out by hand from the fixture above.
	want := map[string]map[string]any{
		quiet:       {"email_channel": true, "email_state": "waiting", "inbound_messages_7d": float64(0), "mailbox_connected": false},
		busy:        {"email_channel": true, "email_state": "working", "inbound_messages_7d": float64(1), "mailbox_connected": false},
		mailboxDesk: {"email_channel": true, "email_state": "waiting", "inbound_messages_7d": float64(0), "mailbox_connected": true},
		paused:      {"email_channel": true, "email_state": "needs_attention", "inbound_messages_7d": float64(0), "mailbox_connected": false},
		noEmail.ID:  {"email_channel": false, "email_state": "none", "inbound_messages_7d": float64(1), "mailbox_connected": false},
	}
	setup := channelStore(db)
	for id, fields := range want {
		properties, ok := byWorkspace[id]
		if !ok {
			t.Fatalf("no snapshot for workspace %s", id)
		}
		for key, value := range fields {
			if got, present := properties[key]; !present || got != value {
				t.Errorf("workspace %s (%v) properties[%q] = %#v (present %v), want %#v", id, properties["workspace_name"], key, got, present, value)
			}
		}
		// The setup guide shows each workspace the same email step.
		progress, err := setup.SetupProgress(ctx, id, time.Now(), store.SetupOptions{})
		if err != nil {
			t.Fatal(err)
		}
		for _, step := range progress.Steps {
			if step.Key == store.SetupEmail && step.State != properties["email_state"] {
				t.Errorf("workspace %v: setup guide email step = %q, snapshot email_state = %#v", properties["workspace_name"], step.State, properties["email_state"])
			}
		}
	}

	// The new fields carry states and counts only: no address and nothing a customer wrote.
	raw := string(recorded.bodyAt(t, "POST /batch/"))
	for _, secret := range []string{"@" + hostedMailDomain, "owner@mailbox.example", "@example.org", "Where is my order", "Refund please"} {
		if strings.Contains(raw, secret) {
			t.Errorf("snapshot contains %q: %s", secret, raw)
		}
	}
}

// A cloud server wired without its store cannot read the email state; it says so and sends nothing,
// rather than a snapshot whose email_state is missing or wrong. The day is not taken: the next check
// tries again.
func TestCloudReportWithoutTheStoreSaysSoAndSendsNothing(t *testing.T) {
	db := newDB(t)
	fixture(t, db)
	recorded := &capture{}
	server := recorded.server(t)
	err := telemetry.New(db, cloudConfig(server.URL), server.Client()).SendOnce(context.Background())
	if err == nil || !strings.Contains(err.Error(), "WithStore") {
		t.Fatalf("SendOnce error = %v, want one naming Reporter.WithStore", err)
	}
	if len(recorded.paths) != 0 {
		t.Fatalf("requests = %v, want none", recorded.paths)
	}
	if err := cloudReporter(db, server).SendOnce(context.Background()); err != nil {
		t.Fatalf("SendOnce with the store: %v", err)
	}
	if len(recorded.paths) != 2 {
		t.Fatalf("requests = %v, want the heartbeat and the snapshots once the store is there", recorded.paths)
	}
}
