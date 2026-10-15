package telemetry_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
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
	reporter := telemetry.New(db, config, server.Client())
	if err := reporter.SendOnce(ctx); err != nil {
		t.Fatalf("SendOnce: %v", err)
	}
	if err := reporter.SendOnce(ctx); err != nil {
		t.Fatalf("second SendOnce: %v", err)
	}
	if len(recorded.bodies) != 2 {
		t.Fatalf("requests = %d, want 2", len(recorded.bodies))
	}
	if recorded.paths[0] != "POST /i/v0/e/" {
		t.Fatalf("request = %q, want POST /i/v0/e/", recorded.paths[0])
	}

	var first, second heartbeat
	if err := json.Unmarshal(recorded.bodies[0], &first); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(recorded.bodies[1], &second); err != nil {
		t.Fatal(err)
	}
	if first.APIKey != "phc_test" || first.Event != "instance heartbeat" {
		t.Fatalf("api_key/event = %q/%q", first.APIKey, first.Event)
	}
	if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`).MatchString(first.DistinctID) {
		t.Fatalf("distinct_id = %q, want a random install id", first.DistinctID)
	}
	if second.DistinctID != first.DistinctID {
		t.Fatalf("install id changed between reports: %q then %q", first.DistinctID, second.DistinctID)
	}
	if _, err := time.Parse(time.RFC3339, first.Timestamp); err != nil {
		t.Fatalf("timestamp %q: %v", first.Timestamp, err)
	}

	// Expected values come from the fixture: two workspaces; three agents seen in the last 30 days
	// (one only 40 days ago is not counted); two replies in the last 30 days (a note and a reply
	// from 40 days ago are not counted); an email channel in one workspace, no web form, one
	// Freshdesk import.
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

	// Nothing that names a person, a customer or what they wrote leaves the instance.
	raw := string(recorded.bodies[0])
	for _, secret := range []string{"owner-a@example.com", "owner-b@example.com", "helper-a@example.com", "buyer@example.org",
		"Where is my order", "Refund please", "Internal note", "Acme Support", "Beta Help", "support@beta.example"} {
		if strings.Contains(raw, secret) {
			t.Errorf("report contains %q: %s", secret, raw)
		}
	}
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
	exec := func(sql string, args ...any) {
		t.Helper()
		err := db.Privileged(ctx, database.PrivilegeMigrate, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, sql, args...)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
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

	exec(`INSERT INTO email_channels(workspace_id,support_address,domain_verification_token) VALUES($1,'support@beta.example','token')`, b.ID)
	exec(`INSERT INTO import_items(workspace_id,source,entity,source_id,status) VALUES($1,'freshdesk:account:42','ticket','1','done')`, a.ID)
}
