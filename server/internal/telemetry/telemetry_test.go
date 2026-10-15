package telemetry_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/telemetry"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestConfigFromEnv(t *testing.T) {
	cases := []struct {
		name       string
		env        map[string]string
		origin     string
		cloud      bool
		wantOff    string
		wantDomain string
		wantRealm  string
	}{
		{name: "on by default", origin: "https://help.acme.com", wantDomain: "help.acme.com", wantRealm: "self-hosted"},
		{name: "DISABLE_TELEMETRY=true", env: map[string]string{"DISABLE_TELEMETRY": "true"}, origin: "https://help.acme.com", wantOff: "DISABLE_TELEMETRY", wantDomain: "help.acme.com", wantRealm: "self-hosted"},
		{name: "DISABLE_TELEMETRY=1", env: map[string]string{"DISABLE_TELEMETRY": "1"}, origin: "https://help.acme.com", wantOff: "DISABLE_TELEMETRY", wantDomain: "help.acme.com", wantRealm: "self-hosted"},
		{name: "DISABLE_TELEMETRY=false keeps it on", env: map[string]string{"DISABLE_TELEMETRY": "false"}, origin: "https://help.acme.com", wantDomain: "help.acme.com", wantRealm: "self-hosted"},
		{name: "DO_NOT_TRACK=1", env: map[string]string{"DO_NOT_TRACK": "1"}, origin: "https://help.acme.com", wantOff: "DO_NOT_TRACK", wantDomain: "help.acme.com", wantRealm: "self-hosted"},
		{name: "cloud realm", origin: "https://app.colliedesk.com", cloud: true, wantDomain: "app.colliedesk.com", wantRealm: "cloud"},
		{name: "localhost is not sent", origin: "http://localhost:8080", wantRealm: "self-hosted"},
		{name: "private address is not sent", origin: "http://10.0.0.5:8080", wantRealm: "self-hosted"},
		{name: "public address literal is not sent", origin: "http://203.0.113.7", wantRealm: "self-hosted"},
		{name: "lan name is not sent", origin: "https://desk.lan", wantRealm: "self-hosted"},
		{name: "single-label host is not sent", origin: "http://colliedesk:8080", wantRealm: "self-hosted"},
		{name: "port and case are dropped", origin: "https://Help.Acme.com:8443", wantDomain: "help.acme.com", wantRealm: "self-hosted"},
		// Any other non-empty opt-out value means off: someone who set it wants it off.
		{name: "DISABLE_TELEMETRY=y", env: map[string]string{"DISABLE_TELEMETRY": "y"}, origin: "https://help.acme.com", wantOff: "DISABLE_TELEMETRY", wantDomain: "help.acme.com", wantRealm: "self-hosted"},
		{name: "DISABLE_TELEMETRY=disable", env: map[string]string{"DISABLE_TELEMETRY": "disable"}, origin: "https://help.acme.com", wantOff: "DISABLE_TELEMETRY", wantDomain: "help.acme.com", wantRealm: "self-hosted"},
		{name: "DO_NOT_TRACK=2", env: map[string]string{"DO_NOT_TRACK": "2"}, origin: "https://help.acme.com", wantOff: "DO_NOT_TRACK", wantDomain: "help.acme.com", wantRealm: "self-hosted"},
		{name: "DISABLE_TELEMETRY=0 keeps it on", env: map[string]string{"DISABLE_TELEMETRY": "0"}, origin: "https://help.acme.com", wantDomain: "help.acme.com", wantRealm: "self-hosted"},
		{name: "DISABLE_TELEMETRY=Off keeps it on", env: map[string]string{"DISABLE_TELEMETRY": "Off"}, origin: "https://help.acme.com", wantDomain: "help.acme.com", wantRealm: "self-hosted"},
		// Only names under an ICANN public suffix are sent.
		{name: "onion service is not sent", origin: "http://abcdefghijklmnop.onion", wantRealm: "self-hosted"},
		{name: "corp name is not sent", origin: "https://desk.acme.corp", wantRealm: "self-hosted"},
		{name: "home name is not sent", origin: "https://desk.home", wantRealm: "self-hosted"},
		{name: "test name is not sent", origin: "https://desk.test", wantRealm: "self-hosted"},
		{name: "tailnet name is not sent", origin: "https://myhost.tail1234.ts.net", wantRealm: "self-hosted"},
		{name: "address in a wildcard DNS name is not sent", origin: "http://10.0.0.5.nip.io", wantRealm: "self-hosted"},
		{name: "short IPv4 form is not sent", origin: "http://127.1:8080", wantRealm: "self-hosted"},
		{name: "hex IPv4 form is not sent", origin: "http://0x7f.0.0.1", wantRealm: "self-hosted"},
		{name: "two trailing dots do not bypass the filter", origin: "https://desk.lan..", wantRealm: "self-hosted"},
		{name: "IPv6 literal is not sent", origin: "http://[2001:db8::1]:8080", wantRealm: "self-hosted"},
		{name: "a bare public suffix is not sent", origin: "https://co.uk", wantRealm: "self-hosted"},
		{name: "credentials in the origin are never sent", origin: "https://user:secret@help.acme.com", wantDomain: "help.acme.com", wantRealm: "self-hosted"},
		{name: "international name is sent in ASCII form", origin: "https://bücher.example.de", wantDomain: "xn--bcher-kva.example.de", wantRealm: "self-hosted"},
		{name: "trailing dot is dropped", origin: "https://help.acme.com.", wantDomain: "help.acme.com", wantRealm: "self-hosted"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			getenv := func(key string) string { return tc.env[key] }
			config := telemetry.ConfigFromEnv(getenv, tc.origin, tc.cloud, "1.2.3")
			if tc.wantOff == "" && config.OffReason != "" {
				t.Fatalf("OffReason = %q, want on", config.OffReason)
			}
			if tc.wantOff != "" && !strings.Contains(config.OffReason, tc.wantOff) {
				t.Fatalf("OffReason = %q, want it to name %s", config.OffReason, tc.wantOff)
			}
			if config.Domain != tc.wantDomain {
				t.Fatalf("Domain = %q, want %q", config.Domain, tc.wantDomain)
			}
			if config.Realm != tc.wantRealm {
				t.Fatalf("Realm = %q, want %q", config.Realm, tc.wantRealm)
			}
			if config.Version != "1.2.3" {
				t.Fatalf("Version = %q, want 1.2.3", config.Version)
			}
		})
	}
}

func TestDevelopmentBuildSendsNothing(t *testing.T) {
	config := telemetry.ConfigFromEnv(func(string) string { return "" }, "https://help.acme.com", false, "dev")
	if !strings.Contains(config.OffReason, "development build") {
		t.Fatalf("OffReason = %q, want a development build to be off", config.OffReason)
	}
}

func TestEnabledNeedsAProjectKey(t *testing.T) {
	config := telemetry.Config{Endpoint: "https://example.test", Realm: "self-hosted"}
	if config.Enabled() {
		t.Fatal("a build without a project key must not send anything")
	}
	config.ProjectKey = "phc_test"
	if !config.Enabled() {
		t.Fatal("a key and no off reason should be enabled")
	}
	config.OffReason = "DISABLE_TELEMETRY is set"
	if config.Enabled() {
		t.Fatal("an off reason must win over the key")
	}
}

// capture records every request PostHog would receive.
type capture struct {
	mu     sync.Mutex
	bodies [][]byte
	paths  []string
}

func (c *capture) server(t *testing.T) *httptest.Server {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		c.mu.Lock()
		c.bodies = append(c.bodies, body)
		c.paths = append(c.paths, r.Method+" "+r.URL.Path)
		c.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	return server
}

type heartbeat struct {
	APIKey     string         `json:"api_key"`
	Event      string         `json:"event"`
	DistinctID string         `json:"distinct_id"`
	Timestamp  string         `json:"timestamp"`
	Properties map[string]any `json:"properties"`
}

func TestHeartbeatSendsInstanceCountsAndNoPersonalData(t *testing.T) {
	db := newDB(t)
	ctx := context.Background()
	fixture(t, db)

	recorded := &capture{}
	server := recorded.server(t)
	config := telemetry.Config{Endpoint: server.URL, ProjectKey: "phc_test", Version: "1.2.3", Realm: "self-hosted", Domain: "help.acme.com"}
	if err := telemetry.New(db, config, server.Client()).SendOnce(ctx); err != nil {
		t.Fatalf("SendOnce: %v", err)
	}
	if len(recorded.bodies) != 1 {
		t.Fatalf("requests = %d, want 1", len(recorded.bodies))
	}
	if recorded.paths[0] != "POST /i/v0/e/" {
		t.Fatalf("request = %q, want POST /i/v0/e/", recorded.paths[0])
	}
	first := decode(t, recorded.bodies[0])
	if first.APIKey != "phc_test" || first.Event != "instance heartbeat" {
		t.Fatalf("api_key/event = %q/%q", first.APIKey, first.Event)
	}
	if !uuidPattern.MatchString(first.DistinctID) {
		t.Fatalf("distinct_id = %q, want a random install id", first.DistinctID)
	}
	if _, err := time.Parse(time.RFC3339, first.Timestamp); err != nil {
		t.Fatalf("timestamp %q: %v", first.Timestamp, err)
	}

	// Expected values come from the fixture: two workspaces; three agents seen in the last 30 days
	// (one only 40 days ago is not counted); two replies in the last 30 days (a note, a reply from
	// 40 days ago and a reply imported from Freshdesk are not counted); an email channel in one
	// workspace; a web form row that is switched off; one Freshdesk import.
	want := map[string]any{
		"version":           "1.2.3",
		"realm":             "self-hosted",
		"domain":            "help.acme.com",
		"workspaces":        float64(2),
		"agents_active_30d": float64(3),
		"replies_30d":       float64(2),
		"email_channel":     true,
		"web_form":          false,
		"freshdesk_import":  true,
	}
	for key, value := range want {
		if first.Properties[key] != value {
			t.Errorf("properties[%q] = %#v, want %#v", key, first.Properties[key], value)
		}
	}
	for _, key := range []string{"os", "arch"} {
		if first.Properties[key] == "" || first.Properties[key] == nil {
			t.Errorf("properties[%q] missing", key)
		}
	}

	// The fields are exactly the ones docs/user/telemetry.md lists; a new one needs the docs first.
	if got, want := keys(first.Properties), "$process_person_profile,agents_active_30d,arch,domain,email_channel,freshdesk_import,os,realm,replies_30d,version,web_form,workspaces"; got != want {
		t.Errorf("property keys = %s, want %s", got, want)
	}
	// An install is not a person: the report must not create one next to the people who use the
	// website and the cloud app in the same PostHog project.
	if first.Properties["$process_person_profile"] != false {
		t.Errorf("$process_person_profile = %#v, want false", first.Properties["$process_person_profile"])
	}

	// Nothing that names a person, a customer or what they wrote leaves the instance.
	raw := string(recorded.bodies[0])
	for _, secret := range []string{"owner-a@example.com", "owner-b@example.com", "helper-a@example.com", "buyer@example.org",
		"Where is my order", "Refund please", "Internal note", "Acme Support", "Beta Help", "support@beta.example"} {
		if strings.Contains(raw, secret) {
			t.Errorf("report contains %q: %s", secret, raw)
		}
	}
}

func TestOneReportPerDayAndTheInstallIDStays(t *testing.T) {
	db := newDB(t)
	ctx := context.Background()
	fixture(t, db)
	recorded := &capture{}
	server := recorded.server(t)
	config := telemetry.Config{Endpoint: server.URL, ProjectKey: "phc_test", Version: "1.2.3", Realm: "self-hosted"}

	if err := telemetry.New(db, config, server.Client()).SendOnce(ctx); err != nil {
		t.Fatalf("SendOnce: %v", err)
	}
	// The same server again, and a second server on the same database (a restart, a crash loop or
	// another replica), stay quiet until a day has passed.
	for range 2 {
		if err := telemetry.New(db, config, server.Client()).SendOnce(ctx); err != nil {
			t.Fatalf("repeat SendOnce: %v", err)
		}
	}
	if len(recorded.bodies) != 1 {
		t.Fatalf("requests = %d, want 1 within a day", len(recorded.bodies))
	}

	execSQL(t, db, `UPDATE instance SET last_reported_at = now() - interval '24 hours'`)
	execSQL(t, db, `UPDATE web_form_channels SET enabled = true`)
	if err := telemetry.New(db, config, server.Client()).SendOnce(ctx); err != nil {
		t.Fatalf("next-day SendOnce: %v", err)
	}
	if len(recorded.bodies) != 2 {
		t.Fatalf("requests = %d, want 2 after a day", len(recorded.bodies))
	}
	first, second := decode(t, recorded.bodies[0]), decode(t, recorded.bodies[1])
	if second.DistinctID != first.DistinctID {
		t.Fatalf("install id changed between reports: %q then %q", first.DistinctID, second.DistinctID)
	}
	if second.Properties["web_form"] != true {
		t.Fatalf("web_form = %#v after switching a form on, want true", second.Properties["web_form"])
	}
}

func TestMissingInstanceRowIsRecreated(t *testing.T) {
	db := newDB(t)
	execSQL(t, db, `DELETE FROM instance`)
	recorded := &capture{}
	server := recorded.server(t)
	config := telemetry.Config{Endpoint: server.URL, ProjectKey: "phc_test", Version: "1.2.3", Realm: "self-hosted"}
	if err := telemetry.New(db, config, server.Client()).SendOnce(context.Background()); err != nil {
		t.Fatalf("SendOnce: %v", err)
	}
	if len(recorded.bodies) != 1 || !uuidPattern.MatchString(decode(t, recorded.bodies[0]).DistinctID) {
		t.Fatalf("want one report with a new install id, got %d requests", len(recorded.bodies))
	}
}

func TestRunSaysWhyTheReportIsOff(t *testing.T) {
	var logs bytes.Buffer
	done := make(chan struct{})
	go func() {
		telemetry.New(nil, telemetry.Config{OffReason: "DISABLE_TELEMETRY is set"}, nil).Run(context.Background(), slog.New(slog.NewTextHandler(&logs, nil)))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run kept running while the report is off")
	}
	if !strings.Contains(logs.String(), "usage report is off") || !strings.Contains(logs.String(), "DISABLE_TELEMETRY is set") {
		t.Fatalf("log = %q, want the off line with its reason", logs.String())
	}
}

func TestRunStopsWhenTheServerStops(t *testing.T) {
	var logs bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	config := telemetry.Config{Endpoint: "http://127.0.0.1:1", ProjectKey: "phc_test", Version: "1.2.3"}
	go func() {
		telemetry.New(nil, config, nil).Run(ctx, slog.New(slog.NewTextHandler(&logs, nil)))
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop when its context ended")
	}
	if !strings.Contains(logs.String(), "usage report is on") || !strings.Contains(logs.String(), "DISABLE_TELEMETRY=true") {
		t.Fatalf("log = %q, want the on line that says how to turn it off", logs.String())
	}
}

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func decode(t *testing.T, body []byte) heartbeat {
	t.Helper()
	var event heartbeat
	if err := json.Unmarshal(body, &event); err != nil {
		t.Fatal(err)
	}
	return event
}

func keys(m map[string]any) string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	slices.Sort(names)
	return strings.Join(names, ",")
}

func TestDisabledReporterSendsNothing(t *testing.T) {
	db := newDB(t)
	recorded := &capture{}
	server := recorded.server(t)
	config := telemetry.Config{Endpoint: server.URL, ProjectKey: "phc_test", OffReason: "DISABLE_TELEMETRY is set"}
	if err := telemetry.New(db, config, server.Client()).SendOnce(context.Background()); err != nil {
		t.Fatalf("SendOnce: %v", err)
	}
	if len(recorded.bodies) != 0 {
		t.Fatalf("requests = %d, want none", len(recorded.bodies))
	}
}

func TestRejectedReportIsAnError(t *testing.T) {
	db := newDB(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(server.Close)
	config := telemetry.Config{Endpoint: server.URL, ProjectKey: "phc_test"}
	err := telemetry.New(db, config, server.Client()).SendOnce(context.Background())
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("SendOnce error = %v, want one naming status 401", err)
	}
}

func newDB(t *testing.T) *database.DB {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is unset; skipping real PostgreSQL telemetry test")
	}
	schema := "test_telemetry_" + regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(strings.ToLower(t.Name()), "_") + fmt.Sprint(time.Now().UnixNano())
	admin, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(context.Background(), "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		admin.Close()
	})
	if err := database.Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	return database.New(pool)
}

// fixture builds two workspaces with agents, sessions, replies, a note, an email channel and an
// import ledger row, with some activity older than 30 days.
func fixture(t *testing.T, db *database.DB) {
	t.Helper()
	ctx := context.Background()
	service := auth.NewWithDB(db, "https://help.acme.com")
	a, _, err := service.CreateWorkspace(ctx, "Acme Support", "owner-a@example.com")
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := service.CreateWorkspace(ctx, "Beta Help", "owner-b@example.com")
	if err != nil {
		t.Fatal(err)
	}
	exec := func(sql string, args ...any) { t.Helper(); execSQL(t, db, sql, args...) }
	exec(`INSERT INTO agents(workspace_id,email,role) VALUES($1,'helper-a@example.com','agent')`, a.ID)
	exec(`INSERT INTO agents(workspace_id,email,role) VALUES($1,'idle-b@example.com','agent')`, b.ID)
	session := func(workspaceID, email, seen string) {
		exec(`INSERT INTO sessions(workspace_id,agent_id,token_hash,role_ceiling,label,last_seen_at,expires_at)
			SELECT workspace_id,id,convert_to(gen_random_uuid()::text,'UTF8'),role,'test',now()-$3::interval,now()+interval '30 days'
			FROM agents WHERE workspace_id=$1 AND email=$2`, workspaceID, email, seen)
	}
	session(a.ID, "owner-a@example.com", "1 day")
	session(a.ID, "owner-a@example.com", "2 hours") // a second session of the same agent counts once
	session(a.ID, "helper-a@example.com", "3 days")
	session(b.ID, "owner-b@example.com", "29 days")
	session(b.ID, "idle-b@example.com", "40 days")

	exec(`INSERT INTO contacts(workspace_id,name,email) VALUES($1,'Buyer','buyer@example.org')`, a.ID)
	exec(`INSERT INTO conversations(workspace_id,contact_id,subject)
		SELECT workspace_id,id,'Where is my order' FROM contacts WHERE workspace_id=$1`, a.ID)
	message := func(kind, body, age string) {
		exec(`INSERT INTO messages(workspace_id,conversation_id,author_agent_id,kind,body,created_at)
			SELECT c.workspace_id,c.id,a.id,$2,$3,now()-$4::interval
			FROM conversations c JOIN agents a ON a.workspace_id=c.workspace_id AND a.email='owner-a@example.com'
			WHERE c.workspace_id=$1`, a.ID, kind, body, age)
	}
	message("outgoing", "Refund please", "1 hour")
	message("outgoing", "Refund please", "10 days")
	message("note", "Internal note", "1 hour")
	message("outgoing", "Refund please", "40 days")
	message("incoming", "Refund please", "1 hour")
	// A reply imported from Freshdesk keeps its original recent timestamp; it was not sent here.
	exec(`UPDATE messages SET origin = '{"system":"freshdesk"}'::jsonb WHERE id = (
		SELECT id FROM messages WHERE kind = 'outgoing' AND created_at >= now() - interval '2 hours')`)
	message("outgoing", "Refund please", "30 minutes")

	exec(`INSERT INTO email_channels(workspace_id,support_address,domain_verification_token) VALUES($1,'support@beta.example','token')`, b.ID)
	exec(`INSERT INTO web_form_channels(workspace_id,enabled) VALUES($1,false)`, a.ID)
	exec(`INSERT INTO import_items(workspace_id,source,entity,source_id,status) VALUES($1,'freshdesk:account:42','ticket','1','done')`, a.ID)
}

func execSQL(t *testing.T, db *database.DB, sql string, args ...any) {
	t.Helper()
	ctx := context.Background()
	err := db.Privileged(ctx, database.PrivilegeMigrate, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, sql, args...)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}
