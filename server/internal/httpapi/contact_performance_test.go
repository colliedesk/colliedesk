package httpapi_test

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
)

// sqlRecorder keeps the text of every statement a pool runs.
type sqlRecorder struct {
	mu   sync.Mutex
	sqls []string
}

func (r *sqlRecorder) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	r.mu.Lock()
	r.sqls = append(r.sqls, data.SQL)
	r.mu.Unlock()
	return ctx
}

func (r *sqlRecorder) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func (r *sqlRecorder) take() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	taken := r.sqls
	r.sqls = nil
	return taken
}

// A page of a contact's history, or of the inbox without a search, must not read messages: joining them
// made the cost grow with every message of the workspace. A search reads them only through EXISTS.
func TestConversationListReadsMessagesOnlyToSearchThem(t *testing.T) {
	recorder := &sqlRecorder{}
	api, service, _, _ := testAPIOnPool(t, testPoolTraced(t, recorder), nil)
	token := createSession(t, api, service, "Plan Desk", "admin@plan.example.com")
	contact := create(t, api, token, "/api/v1/contacts", map[string]any{"name": "Ada", "email": "ada@plan.example.com"})
	create(t, api, token, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Hello"})
	listing := func(query string) string {
		t.Helper()
		recorder.take()
		if response := request(t, api, http.MethodGet, "/api/v1/conversations?"+query, nil, token, nil); response.Code != http.StatusOK {
			t.Fatalf("GET %s status=%d", query, response.Code)
		}
		for _, sql := range recorder.take() {
			if strings.Contains(sql, "LIMIT") && strings.Contains(sql, "FROM conversations c") {
				return sql
			}
		}
		t.Fatalf("no list query for %s", query)
		return ""
	}
	for _, query := range []string{"contact=" + contact, "status=open", ""} {
		if sql := listing(query); strings.Contains(sql, "messages") || strings.Contains(sql, "GROUP BY") {
			t.Errorf("the list for %q reads messages: %s", query, sql)
		}
	}
	if sql := listing("search=hello"); !strings.Contains(sql, "EXISTS") || strings.Contains(sql, "JOIN messages") {
		t.Errorf("a search must read messages through EXISTS, not a join: %s", sql)
	}
}

// TestContactPageLatencyBudget holds the contact page to the inbox budget: the contact (with its count
// and merges), the first page of its history, and a contact search. The contact has 5,000 conversations
// with messages and tags, beside 20,000 other contacts, and a second workspace of the same size.
func TestContactPageLatencyBudget(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	tokenA := createSession(t, api, service, "Performance Workspace A", "admin-a@performance.example.com")
	_ = createSession(t, api, service, "Performance Workspace B", "admin-b@performance.example.com")
	seedInboxBudget(t, pool, "Performance Workspace A", "budget-needle")
	seedInboxBudget(t, pool, "Performance Workspace B", "other-workspace")
	if _, err := pool.Exec(context.Background(), `INSERT INTO contacts(workspace_id,name,email)
		SELECT w.id,'Filler '||n,'filler-'||n||'-'||w.id||'@example.com' FROM workspaces w CROSS JOIN generate_series(1,10000) n`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `ANALYZE contacts; ANALYZE conversations; ANALYZE messages`); err != nil {
		t.Fatal(err)
	}
	contact := queryString(t, pool, `SELECT c.id::text FROM contacts c JOIN workspaces w ON w.id=c.workspace_id WHERE w.name='Performance Workspace A' AND c.name='Load Contact'`)

	detailP95 := inboxP95(t, api, tokenA, "/api/v1/contacts/"+contact)
	historyP95 := inboxP95(t, api, tokenA, "/api/v1/conversations?contact="+contact+"&page_size=50")
	searchP95 := inboxP95(t, api, tokenA, "/api/v1/contacts?search=filler-77&limit=8")
	t.Logf("contact page p95: detail=%s history=%s search=%s", detailP95, historyP95, searchP95)
	if detailP95 > inboxLatencyBudget || historyP95 > inboxLatencyBudget || searchP95 > inboxLatencyBudget {
		t.Fatalf("contact page p95 exceeds %s: detail=%s history=%s search=%s", inboxLatencyBudget, detailP95, historyP95, searchP95)
	}
}
