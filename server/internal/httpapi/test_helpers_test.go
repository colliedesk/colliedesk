package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/database/databasetest"
	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	testOrigin     = "https://desk.example.com"
	testOriginHost = "desk.example.com"
)

func testAPI(t *testing.T) (http.Handler, *auth.Service) {
	t.Helper()
	api, service, _ := testAPIWithPool(t)
	return api, service
}

func testAPIWithPool(t *testing.T) (http.Handler, *auth.Service, *pgxpool.Pool) {
	t.Helper()
	api, service, pool, _ := testAPIWithConfig(t, nil)
	return api, service, pool
}

// fakeDNS stands in for the public DNS resolver, an external boundary. cnames maps a host to the end
// of its CNAME chain; lookups counts every question asked.
type fakeDNS struct {
	mu      sync.Mutex
	records map[string][]string
	cnames  map[string]string
	lookups int
}

func (d *fakeDNS) setCNAME(host, canonical string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.cnames[host] = canonical
}

func (d *fakeDNS) lookupCNAME(_ context.Context, host string) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.lookups++
	if canonical, ok := d.cnames[host]; ok {
		return canonical, nil
	}
	return "", &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
}

func (d *fakeDNS) questions() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.lookups
}

func (d *fakeDNS) set(name string, values ...string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.records[name] = values
}

func (d *fakeDNS) lookupTXT(_ context.Context, name string) ([]string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.lookups++
	if values, ok := d.records[name]; ok {
		return values, nil
	}
	return nil, &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
}

// testAPIWithConfig builds the API with an SMTP relay configured (no worker runs in these tests),
// the "any" From policy, a trusted authserv-id of mx.example.net and a fake DNS resolver.
func testAPIWithConfig(t *testing.T, mutate func(*httpapi.Config)) (http.Handler, *auth.Service, *pgxpool.Pool, *fakeDNS) {
	t.Helper()
	return testAPIOnPool(t, testPool(t), mutate)
}

// testAPIOnPool is testAPIWithConfig on a pool the test opened, for example one with a tracer.
func testAPIOnPool(t *testing.T, pool *pgxpool.Pool, mutate func(*httpapi.Config)) (http.Handler, *auth.Service, *pgxpool.Pool, *fakeDNS) {
	t.Helper()
	if err := database.Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	dns := &fakeDNS{records: map[string][]string{}, cnames: map[string]string{}}
	storage, err := attachments.NewLocalDisk(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	config := httpapi.Config{Origin: testOrigin, LookupTXT: dns.lookupTXT, LookupCNAME: dns.lookupCNAME, Attachments: storage, Email: email.Config{
		SMTP:              email.SMTPConfig{Host: "smtp.test.invalid", Port: 587, StartTLS: true},
		FromPolicy:        email.FromPolicyAny,
		TrustedAuthServID: "mx.example.net",
	}}
	if mutate != nil {
		mutate(&config)
	}
	return httpapi.New(pool, config), auth.New(pool, testOrigin), pool, dns
}

// testAPIWithStorage is testAPIWithConfig with local-disk attachment storage.
func testAPIWithStorage(t *testing.T) (http.Handler, *auth.Service, *pgxpool.Pool, attachments.Storage) {
	t.Helper()
	storage, err := attachments.NewLocalDisk(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	api, service, pool, _ := testAPIWithConfig(t, func(config *httpapi.Config) { config.Attachments = storage })
	return api, service, pool, storage
}

func seedAttachment(t *testing.T, pool *pgxpool.Pool, storage attachments.Storage, token string, api http.Handler, messageID, filename, content string) string {
	t.Helper()
	var me struct {
		Workspace struct {
			ID string `json:"id"`
		} `json:"workspace"`
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/auth/me", nil, token, nil), &me)
	object, err := storage.Put(context.Background(), me.Workspace.ID, strings.NewReader(content), int64(len(content)))
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.New(pool).CreateAttachment(context.Background(), me.Workspace.ID, store.NewAttachment{
		MessageID: messageID, Filename: filename, ContentType: "text/plain", Object: object,
	})
	if err != nil {
		t.Fatal(err)
	}
	return item.ID
}

func createSession(t *testing.T, api http.Handler, service *auth.Service, workspace, email string) string {
	t.Helper()
	_, pairingURL, err := service.CreateWorkspace(context.Background(), workspace, email)
	if err != nil {
		t.Fatal(err)
	}
	response := request(t, api, http.MethodPost, "/api/v1/auth/redeem", map[string]any{"token": linkToken(t, pairingURL)}, "", nil)
	var body struct {
		Token string `json:"token"`
	}
	decode(t, response, &body)
	return body.Token
}

func create(t *testing.T, api http.Handler, token, path string, body any) string {
	t.Helper()
	response := request(t, api, http.MethodPost, path, body, token, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("POST %s status=%d body=%s", path, response.Code, response.Body.String())
	}
	var result struct {
		ID string `json:"id"`
	}
	decode(t, response, &result)
	return result.ID
}

func request(t *testing.T, handler http.Handler, method, path string, body any, token string, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	return serve(handler, newRequest(t, method, path, body, token, cookie))
}

func newRequest(t *testing.T, method, path string, body any, token string, cookie *http.Cookie) *http.Request {
	t.Helper()
	var data bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&data).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &data)
	// The app answers only on APP_ORIGIN's host (helpcenter.Site.Wrap), as a browser reaches it.
	req.Host = testOriginHost
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	return req
}

func serve(handler http.Handler, request *http.Request) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func requestWithOrigin(t *testing.T, handler http.Handler, method, path string, cookie *http.Cookie, origin string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.AddCookie(cookie)
	req.Header.Set("Origin", origin)
	return serve(handler, req)
}

func decode(t *testing.T, response *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.Unmarshal(response.Body.Bytes(), target); err != nil {
		t.Fatalf("decode response %q: %v", response.Body.String(), err)
	}
}

func linkToken(t *testing.T, raw string) string {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return parsed.Query().Get("token")
}

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return testPoolTraced(t, nil)
}

// testPoolTraced is testPool with a query tracer on every connection; nil traces nothing.
func testPoolTraced(t *testing.T, tracer pgx.QueryTracer) *pgxpool.Pool {
	t.Helper()
	return databasetest.PoolWith(t, func(config *pgxpool.Config) { config.ConnConfig.Tracer = tracer })
}

func workspaceID(t *testing.T, api http.Handler, token string) string {
	t.Helper()
	var me struct {
		Workspace struct {
			ID string `json:"id"`
		} `json:"workspace"`
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/auth/me", nil, token, nil), &me)
	return me.Workspace.ID
}

// seedImportDetails gives a conversation and message the shape an importer
// writes, and logs one time entry, so tests cover the non-null API forms.
func seedImportDetails(t *testing.T, pool *pgxpool.Pool, workspace, conversationID, messageID string) {
	t.Helper()
	ctx := context.Background()
	statements := []struct {
		sql  string
		args []any
	}{
		{`UPDATE conversations SET origin='{"system":"freshdesk","external_id":"41","type":"Problem","source":"email","due_at":"2025-01-18T23:30:00Z","first_response_due_at":null,"cc":["cc@example.com"],"forwarded_to":[]}'
			WHERE workspace_id=$1 AND id=$2`, []any{workspace, conversationID}},
		{`UPDATE messages SET body_html='<p onclick="x()">Hi <b>there</b></p>',
			origin='{"system":"freshdesk","external_id":"7002","kind":"public_note","from":null,"to":["a@example.com"],"cc":[],"bcc":[]}'
			WHERE workspace_id=$1 AND id=$2`, []any{workspace, messageID}},
		{`INSERT INTO time_entries(workspace_id,conversation_id,agent_name,minutes,note,billable,executed_at)
			VALUES($1,$2,'Former Agent',90,'Call',true,'2025-01-16T02:00:00Z')`, []any{workspace, conversationID}},
	}
	for _, statement := range statements {
		if _, err := pool.Exec(ctx, statement.sql, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
}
