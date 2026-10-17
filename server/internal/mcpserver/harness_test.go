package mcpserver_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/mcpserver"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	testOrigin  = "https://desk.example.com"
	testVersion = "9.8.7-test"
)

// workspace is one signed-in workspace: the admin's own session and one token per tier.
type workspace struct {
	id    string
	admin string
	read  string
	draft string
	send  string
	// adminTier is an agent token of the admin tier; admin is the admin person's own session.
	adminTier string
}

type env struct {
	t       *testing.T
	api     http.Handler
	pool    *pgxpool.Pool
	service *auth.Service
	server  *httptest.Server
}

func newEnv(t *testing.T) *env { return newEnvWith(t, nil) }

// newEnvWith builds the API with a change to its configuration, for example a billing setup.
func newEnvWith(t *testing.T, mutate func(*httpapi.Config)) *env { return newEnvBuilt(t, mutate, nil) }

// newEnvBuilt also puts a handler in front of the REST API that the tools call (the web app and the
// tests still reach the real one), so a test can act as a second caller in the middle of a tool call.
func newEnvBuilt(t *testing.T, mutate func(*httpapi.Config), front func(real http.Handler) http.Handler) *env {
	t.Helper()
	pool := testPool(t)
	if err := database.Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	storage, err := attachments.NewLocalDisk(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	config := httpapi.Config{Origin: testOrigin, Attachments: storage}
	if mutate != nil {
		mutate(&config)
	}
	api := httpapi.New(pool, config)
	service := auth.New(pool, testOrigin)
	toolAPI := http.Handler(api)
	if front != nil {
		toolAPI = front(api)
	}
	server := httptest.NewServer(mcpserver.Mount(mcpserver.Config{API: toolAPI, Auth: service, Version: testVersion}, api))
	t.Cleanup(server.Close)
	return &env{t: t, api: api, pool: pool, service: service, server: server}
}

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is unset; skipping real PostgreSQL MCP test")
	}
	schema := "test_" + regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(strings.ToLower(t.Name()), "_") + fmt.Sprint(time.Now().UnixNano())
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
	return pool
}

func (e *env) mcpURL() string { return e.server.URL + mcpserver.Path }

// rest calls the REST API directly and returns the status and the decoded JSON object.
func (e *env) rest(method, path string, body any, token string) (int, map[string]any) {
	e.t.Helper()
	var data bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&data).Encode(body); err != nil {
			e.t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &data)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	recorder := httptest.NewRecorder()
	e.api.ServeHTTP(recorder, req)
	var decoded map[string]any
	if recorder.Body.Len() > 0 {
		if err := json.Unmarshal(recorder.Body.Bytes(), &decoded); err != nil {
			e.t.Fatalf("%s %s: decode %q: %v", method, path, recorder.Body.String(), err)
		}
	}
	return recorder.Code, decoded
}

func (e *env) created(path string, body any, token string) string {
	e.t.Helper()
	status, decoded := e.rest(http.MethodPost, path, body, token)
	if status != http.StatusCreated {
		e.t.Fatalf("POST %s status=%d body=%v", path, status, decoded)
	}
	return decoded["id"].(string)
}

func (e *env) newWorkspace(name, adminEmail string) workspace {
	e.t.Helper()
	_, pairingURL, err := e.service.CreateWorkspace(context.Background(), name, adminEmail)
	if err != nil {
		e.t.Fatal(err)
	}
	parsed, err := url.Parse(pairingURL)
	if err != nil {
		e.t.Fatal(err)
	}
	status, redeemed := e.rest(http.MethodPost, "/api/v1/auth/redeem", map[string]any{"token": parsed.Query().Get("token")}, "")
	if status != http.StatusOK {
		e.t.Fatalf("redeem status=%d body=%v", status, redeemed)
	}
	w := workspace{admin: redeemed["token"].(string)}
	w.id = redeemed["workspace"].(map[string]any)["id"].(string)
	for tier, target := range map[string]*string{"read": &w.read, "draft": &w.draft, "send": &w.send, "admin": &w.adminTier} {
		status, issued := e.rest(http.MethodPost, "/api/v1/auth/tokens", map[string]any{"label": "test " + tier, "scope": tier}, w.admin)
		if status != http.StatusCreated {
			e.t.Fatalf("create %s token status=%d body=%v", tier, status, issued)
		}
		*target = issued["token"].(string)
	}
	return w
}

// incoming stores a customer message the way the email channel does; the REST API cannot create one.
func (e *env) incoming(w workspace, conversationID, body string) {
	e.t.Helper()
	if _, err := e.pool.Exec(context.Background(),
		`INSERT INTO messages(workspace_id,conversation_id,kind,body) VALUES($1,$2,'incoming',$3)`, w.id, conversationID, body); err != nil {
		e.t.Fatal(err)
	}
}

// nameAgents gives every agent of the workspace the display name; the REST API cannot set one.
func (e *env) nameAgents(w workspace, name string) {
	e.t.Helper()
	if _, err := e.pool.Exec(context.Background(), `UPDATE agents SET display_name=$2 WHERE workspace_id=$1`, w.id, name); err != nil {
		e.t.Fatal(err)
	}
}

// imported stores a message the way the Freshdesk importer does: its Freshdesk classification in origin
// and either a matched Colliedesk agent or only the display name of its author. The REST API cannot
// create one.
func (e *env) imported(w workspace, conversationID, kind, originKind string, agentID *string, authorName, body string) {
	e.t.Helper()
	origin := fmt.Sprintf(`{"system":"freshdesk","external_id":"%s-%d","kind":"%s","from":null,"to":[],"cc":[],"bcc":[]}`, originKind, time.Now().UnixNano(), originKind)
	if _, err := e.pool.Exec(context.Background(),
		`INSERT INTO messages(workspace_id,conversation_id,author_agent_id,author_name,kind,body,origin) VALUES($1,$2,$3,$4,$5,$6,$7::jsonb)`,
		w.id, conversationID, agentID, authorName, kind, body, origin); err != nil {
		e.t.Fatal(err)
	}
}
