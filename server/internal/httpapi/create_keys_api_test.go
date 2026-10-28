package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// keyedCreate is one create route the web app calls: the same key sent twice must create one row.
type keyedCreate struct {
	path string
	body map[string]any
	// id reads the created row's id from the answer.
	id func(map[string]any) string
	// rows counts the rows the request made in the workspace.
	rows string
	arg  string
}

func topID(body map[string]any) string { id, _ := body["id"].(string); return id }

func nestedID(field string) func(map[string]any) string {
	return func(body map[string]any) string {
		inner, _ := body[field].(map[string]any)
		id, _ := inner["id"].(string)
		return id
	}
}

func countKeyedRows(t *testing.T, pool *pgxpool.Pool, query, workspace, arg string) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(context.Background(), query, workspace, arg).Scan(&count); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	return count
}

func keyedAnswer(t *testing.T, response *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %s: %v", response.Body.String(), err)
	}
	return body
}

func TestARepeatedKeyCreatesEachKindOfRowOnce(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	cases := map[string]keyedCreate{
		"invite": {path: "/api/v1/auth/invites", body: map[string]any{"email": "teammate@keys.example.com", "role_ceiling": "agent"}, id: nestedID("invite"),
			rows: `SELECT count(*) FROM pairing_links WHERE workspace_id=$1 AND kind='invite' AND email=$2`, arg: "teammate@keys.example.com"},
		"agent token": {path: "/api/v1/auth/tokens", body: map[string]any{"label": "Claude keys", "scope": "draft"}, id: nestedID("session"),
			rows: `SELECT count(*) FROM sessions WHERE workspace_id=$1 AND label=$2`, arg: "Claude keys"},
		"contact": {path: "/api/v1/contacts", body: map[string]any{"name": "Kim Keys", "email": "kim@customer-keys.example.com"}, id: topID,
			rows: `SELECT count(*) FROM contacts WHERE workspace_id=$1 AND email=$2`, arg: "kim@customer-keys.example.com"},
		"tag": {path: "/api/v1/tags", body: map[string]any{"name": "refund-keys"}, id: topID,
			rows: `SELECT count(*) FROM tags WHERE workspace_id=$1 AND name=$2`, arg: "refund-keys"},
		"saved view": {path: "/api/v1/saved-views", body: map[string]any{"name": "Urgent keys", "scope": "personal", "filters": map[string]any{"priority": []string{"urgent"}}}, id: topID,
			rows: `SELECT count(*) FROM saved_views WHERE workspace_id=$1 AND name=$2`, arg: "Urgent keys"},
		"collection": {path: "/api/v1/kb/collections", body: map[string]any{"name": "Guides keys"}, id: topID,
			rows: `SELECT count(*) FROM kb_collections WHERE workspace_id=$1 AND name=$2`, arg: "Guides keys"},
		"email channel": {path: "/api/v1/channels", body: map[string]any{"type": "email", "name": "Billing keys", "config": map[string]any{"support_address": "billing@keys.example.com"}}, id: topID,
			rows: `SELECT count(*) FROM channels WHERE workspace_id=$1 AND name=$2`, arg: "Billing keys"},
		"web form": {path: "/api/v1/channels", body: map[string]any{"type": "web_form", "name": "Form keys", "config": map[string]any{"allowed_origins": []string{}}}, id: topID,
			rows: `SELECT count(*) FROM channels WHERE workspace_id=$1 AND name=$2`, arg: "Form keys"},
	}
	for name, item := range cases {
		t.Run(name, func(t *testing.T) {
			admin := createSession(t, api, service, "Keys "+name, "admin@keys-"+strings.ReplaceAll(name, " ", "-")+".example.com")
			workspace := workspaceID(t, api, admin)
			first := postWithKey(t, api, admin, item.path, "press-1", item.body)
			if first.Code != http.StatusCreated {
				t.Fatalf("first: status=%d body=%s", first.Code, first.Body.String())
			}
			created := item.id(keyedAnswer(t, first))
			again := postWithKey(t, api, admin, item.path, "press-1", item.body)
			if again.Code != http.StatusOK || item.id(keyedAnswer(t, again)) != created || created == "" {
				t.Fatalf("repeat: status=%d body=%s, want 200 with %q", again.Code, again.Body.String(), created)
			}
			if got := countKeyedRows(t, pool, item.rows, workspace, item.arg); got != 1 {
				t.Fatalf("%d rows after the same key twice, want 1", got)
			}
			other := map[string]any{}
			for field, value := range item.body {
				other[field] = value
			}
			for field, value := range map[string]string{"name": "Other input", "label": "Other input", "email": "other@keys.example.com"} {
				if _, ok := other[field]; ok {
					other[field] = value
				}
			}
			if reused := postWithKey(t, api, admin, item.path, "press-1", other); reused.Code != http.StatusUnprocessableEntity {
				t.Fatalf("key with other input: status=%d body=%s, want 422", reused.Code, reused.Body.String())
			}
		})
	}
}

// A native double click sends two requests at once with the key of one press: one invite is pending, and
// the link is never kept to answer the repeat (an invite link is stored only as a hash).
func TestInvitesSentAtOnceWithOneKeyMakeOneInviteAndKeepNoLink(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "Invite Keys", "admin@invite-keys.example.com")
	body := map[string]any{"email": "double@invite-keys.example.com", "role_ceiling": "agent"}
	responses := threeAtOnce(t, func() *httptest.ResponseRecorder {
		return postWithKey(t, api, admin, "/api/v1/auth/invites", "double-click", body)
	})
	links := 0
	for _, response := range responses {
		if url, _ := keyedAnswer(t, response)["url"].(string); url != "" {
			links++
		}
	}
	if links != 1 {
		t.Fatalf("%d answers carry the one-time link, want only the first", links)
	}
	var kept int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM request_idempotency_keys WHERE response::text LIKE '%/invite/%' OR response::text LIKE '%#%'`).Scan(&kept); err != nil {
		t.Fatal(err)
	}
	if kept != 0 {
		t.Fatalf("%d remembered answers hold a one-time link", kept)
	}
	if got := countKeyedRows(t, pool, `SELECT count(*) FROM pairing_links WHERE workspace_id=$1 AND kind='invite' AND email=$2`, workspaceID(t, api, admin), "double@invite-keys.example.com"); got != 1 {
		t.Fatalf("%d invites, want 1", got)
	}
}

func TestARepeatedKeyStartsOneConversation(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "Conversation Keys", "admin@conversation-keys.example.com")
	contact := create(t, api, admin, "/api/v1/contacts", map[string]any{"name": "Lee", "email": "lee@customer-conv-keys.example.com"})
	body := map[string]any{"contact_id": contact, "subject": "Keyed subject"}
	first := postWithKey(t, api, admin, "/api/v1/conversations", "start-1", body)
	again := postWithKey(t, api, admin, "/api/v1/conversations", "start-1", body)
	if first.Code != http.StatusCreated || again.Code != http.StatusOK || topID(keyedAnswer(t, first)) != topID(keyedAnswer(t, again)) {
		t.Fatalf("first=%d %s again=%d %s", first.Code, first.Body.String(), again.Code, again.Body.String())
	}
	if got := countKeyedRows(t, pool, `SELECT count(*) FROM conversations WHERE workspace_id=$1 AND subject=$2`, workspaceID(t, api, admin), "Keyed subject"); got != 1 {
		t.Fatalf("%d conversations, want 1", got)
	}
}

func TestARepeatedKeyAddsOneAIProvider(t *testing.T) {
	desk := newAIDesk(t, nil)
	body := map[string]any{"kind": "anthropic", "label": "Keyed key", "api_key": "sk-keyed-0000", "models": []string{"claude-haiku-5-5"}}
	first := postWithKey(t, desk.api, desk.adminA, "/api/v1/ai/providers", "add-1", body)
	again := postWithKey(t, desk.api, desk.adminA, "/api/v1/ai/providers", "add-1", body)
	if first.Code != http.StatusCreated || again.Code != http.StatusOK || topID(keyedAnswer(t, first)) != topID(keyedAnswer(t, again)) {
		t.Fatalf("first=%d %s again=%d %s", first.Code, first.Body.String(), again.Code, again.Body.String())
	}
	if got := countKeyedRows(t, desk.pool, `SELECT count(*) FROM ai_providers WHERE workspace_id=$1 AND label=$2`, desk.workspaceA, "Keyed key"); got != 1 {
		t.Fatalf("%d providers, want 1", got)
	}
}

func TestARepeatedKeySendsOneProblemReport(t *testing.T) {
	f := newReportFixture(t, nil)
	first := postWithKey(t, f.api, f.customer, reportPath, "report-1", goodReport())
	again := postWithKey(t, f.api, f.customer, reportPath, "report-1", goodReport())
	if first.Code != http.StatusCreated || again.Code != http.StatusOK || keyedAnswer(t, first)["reference"] != keyedAnswer(t, again)["reference"] {
		t.Fatalf("first=%d %s again=%d %s", first.Code, first.Body.String(), again.Code, again.Body.String())
	}
	reports := request(t, f.api, http.MethodGet, "/api/v1/conversations", nil, f.support, nil)
	if items, _ := keyedAnswer(t, reports)["items"].([]any); len(items) != 1 {
		t.Fatalf("the support desk has %d reports, want 1: %s", len(items), reports.Body.String())
	}
}
