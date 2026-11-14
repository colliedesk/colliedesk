package mcpserver_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/database/databasetest"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/mcpserver"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	testOrigin  = "https://desk.example.com"
	testVersion = "9.8.7-test"
)

// workspace is one signed-in workspace: the admin's own session and four agent tokens of that admin.
// read, draft and send hold a fixed list of permissions (what the tiers before roles gave);
// adminTier follows the Admin role. admin is the admin person's own session.
type workspace struct {
	id        string
	admin     string
	read      string
	draft     string
	send      string
	adminTier string
}

// The permission lists of the read, draft and send tokens: what the tiers read, draft and send gave
// before roles, so a test says what a narrow token may and may not do.
var (
	readPermissions = []string{"conversations:read", "messages:read", "ai_assist:read", "contacts:read", "tags:read", "saved_replies:read", "saved_views:read",
		"kb_articles:read", "kb_media:read", "help_center:read", "results:read", "team:read", "channels:read", "ai_settings:read", "autofix:read", "autofix_settings:read"}
	draftPermissions = append(slices.Clone(readPermissions), "conversations:update", "messages:note", "ai_assist:create", "tags:create",
		"saved_views:create", "saved_views:update", "saved_views:delete", "profile:update", "autofix:update")
	sendPermissions = append(slices.Clone(draftPermissions), "messages:send", "contacts:create", "contacts:update", "tags:update", "tags:delete",
		"saved_replies:create", "saved_replies:update", "saved_replies:delete", "kb_articles:create", "kb_articles:update", "kb_articles:delete",
		"kb_articles:publish", "kb_media:create", "kb_media:update", "kb_media:delete", "autofix:send")
)

// token makes an agent token of the person signed in with session: with permissions, or following the
// role named by role.
func (e *env) token(session, label, role string, permissions []string) string {
	e.t.Helper()
	body := map[string]any{"label": label}
	if role != "" {
		status, found := e.rest(http.MethodGet, "/api/v1/roles/"+url.PathEscape(role), nil, session)
		if status != http.StatusOK {
			e.t.Fatalf("GET role %s status=%d body=%v", role, status, found)
		}
		body["role_id"] = found["id"]
	} else {
		body["permissions"] = permissions
	}
	status, issued := e.rest(http.MethodPost, "/api/v1/auth/tokens", body, session)
	if status != http.StatusCreated {
		e.t.Fatalf("create token %s status=%d body=%v", label, status, issued)
	}
	return issued["token"].(string)
}

type env struct {
	t       *testing.T
	api     http.Handler
	pool    *pgxpool.Pool
	service *auth.Service
	server  *httptest.Server
	// files is the directory of the attachment storage.
	files string
}

func newEnv(t *testing.T) *env { return newEnvWith(t, nil) }

// newEnvWith builds the API with a change to its configuration, for example a billing setup.
func newEnvWith(t *testing.T, mutate func(*httpapi.Config)) *env { return newEnvBuilt(t, mutate, nil) }

// newEnvBuilt also puts a handler in front of the REST API that the tools call (the web app and the
// tests still reach the real one), so a test can act as a second caller in the middle of a tool call.
func newEnvBuilt(t *testing.T, mutate func(*httpapi.Config), front func(real http.Handler) http.Handler) *env {
	t.Helper()
	return newEnvMCP(t, mutate, front, nil)
}

// newEnvMCP also changes the MCP server's own configuration, for example its funnel.
func newEnvMCP(t *testing.T, mutate func(*httpapi.Config), front func(real http.Handler) http.Handler, mutateMCP func(*mcpserver.Config)) *env {
	t.Helper()
	pool := testPool(t)
	if err := database.Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	files := t.TempDir()
	storage, err := attachments.NewLocalDisk(files)
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
	mcpConfig := mcpserver.Config{API: toolAPI, Auth: service, Version: testVersion, Operators: api.Operators(), Origin: testOrigin}
	if mutateMCP != nil {
		mutateMCP(&mcpConfig)
	}
	server := httptest.NewServer(mcpserver.Mount(mcpConfig, api))
	t.Cleanup(server.Close)
	return &env{t: t, api: api, pool: pool, service: service, server: server, files: files}
}

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return databasetest.Pool(t)
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
	w.read = e.token(w.admin, "test read", "", readPermissions)
	w.draft = e.token(w.admin, "test draft", "", draftPermissions)
	w.send = e.token(w.admin, "test send", "", sendPermissions)
	w.adminTier = e.token(w.admin, "test admin", "Admin", nil)
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

// nameAgents gives every agent of the workspace the same display name in one statement.
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
