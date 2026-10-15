package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/jackc/pgx/v5/pgxpool"
)

// testClock is the clock the server under test reads; tests move it instead of sleeping.
type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) set(moment time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = moment
}

// midMonth is a hand-picked instant in the middle of a UTC month.
var midMonth = time.Date(2026, time.September, 15, 12, 0, 0, 0, time.UTC)

// limitsFixture is two cloud workspaces (billing enabled) or two self-hosted ones, each with the
// email channel connected, on a server whose clock the test controls.
type limitsFixture struct {
	api        http.Handler
	pool       *pgxpool.Pool
	clock      *testClock
	adminA     string
	adminB     string
	workspaceA string
	workspaceB string
	secretA    string
	secretB    string
	billing    billingFixture
}

func newLimitsFixture(t *testing.T, billingEnabled bool) limitsFixture {
	t.Helper()
	clock := &testClock{now: midMonth}
	fake := newFakePolarAPI(t)
	api, service, pool, _ := testAPIWithConfig(t, func(config *httpapi.Config) {
		config.Now = clock.Now
		if billingEnabled {
			config.Billing = fake.config()
		}
	})
	adminA := createSession(t, api, service, "Limits Alpha", "admin@limits-alpha.example.com")
	adminB := createSession(t, api, service, "Limits Bravo", "admin@limits-bravo.example.com")
	f := limitsFixture{api: api, pool: pool, clock: clock, adminA: adminA, adminB: adminB,
		workspaceA: workspaceID(t, api, adminA), workspaceB: workspaceID(t, api, adminB)}
	f.secretA = connectEmail(t, api, adminA, "support@limits-alpha.example.com")
	f.secretB = connectEmail(t, api, adminB, "support@limits-bravo.example.com")
	f.billing = billingFixture{api: api, service: service, pool: pool, fake: fake, adminA: adminA, adminB: adminB,
		workspaceA: f.workspaceA, workspaceB: f.workspaceB}
	return f
}

// receive delivers one new-thread customer email to workspace A (or B) and returns the result.
func (f limitsFixture) receive(t *testing.T, workspaceB bool, number int, subject string) inboundResult {
	t.Helper()
	secret, support := f.secretA, "support@limits-alpha.example.com"
	if workspaceB {
		secret, support = f.secretB, "support@limits-bravo.example.com"
	}
	return inboundOK(t, f.api, secret, mail([]string{
		fmt.Sprintf("From: customer%d@customer.example", number), "To: " + support,
		"Subject: " + subject, fmt.Sprintf("Message-ID: <limits-%d-%t@customer.example>", number, workspaceB),
	}, "Please help with "+subject+"."))
}

// seedConversations inserts n conversations created at moment straight into the database, standing
// in for earlier traffic. imported marks them with the origin an importer writes.
func (f limitsFixture) seedConversations(t *testing.T, workspace string, n int, moment time.Time, imported bool) {
	t.Helper()
	ctx := context.Background()
	var contactID string
	if err := f.pool.QueryRow(ctx, `INSERT INTO contacts(workspace_id,name,email) VALUES($1,'Seed','seed-'||gen_random_uuid()||'@customer.example') RETURNING id::text`, workspace).Scan(&contactID); err != nil {
		t.Fatal(err)
	}
	origin := any(nil)
	if imported {
		origin = `{"system":"freshdesk","external_id":"1","type":null,"source":"email","due_at":null,"first_response_due_at":null,"cc":[],"forwarded_to":[]}`
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO conversations(workspace_id,contact_id,subject,created_at,updated_at,origin)
		SELECT $1,$2,'Seed '||n,$3,$3,$4::jsonb FROM generate_series(1,$5) AS n`, workspace, contactID, moment, origin, n); err != nil {
		t.Fatal(err)
	}
}

func (f limitsFixture) upgrade(t *testing.T, workspace, subscriptionID string) {
	t.Helper()
	f.billing.mustDeliver(t, "evt_"+subscriptionID, activeTeam(workspace, subscriptionID, f.clock.Now()).event("subscription.active"))
}

func (f limitsFixture) inboxSubjects(t *testing.T, token, query string) []string {
	t.Helper()
	response := request(t, f.api, http.MethodGet, "/api/v1/conversations?page_size=100"+query, nil, token, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", response.Code, response.Body.String())
	}
	var page struct {
		Items []struct {
			Subject string `json:"subject"`
		} `json:"items"`
	}
	decode(t, response, &page)
	subjects := make([]string, 0, len(page.Items))
	for _, item := range page.Items {
		subjects = append(subjects, item.Subject)
	}
	return subjects
}

// listedHeldCount is the held count any agent sees on the inbox list response.
func (f limitsFixture) listedHeldCount(t *testing.T, token string) int {
	t.Helper()
	var page struct {
		HeldCount *int `json:"held_count"`
	}
	decode(t, request(t, f.api, http.MethodGet, "/api/v1/conversations", nil, token, nil), &page)
	if page.HeldCount == nil {
		t.Fatal("conversation list has no held_count")
	}
	return *page.HeldCount
}

func (f limitsFixture) usage(t *testing.T, token string) planUsageView {
	t.Helper()
	response := request(t, f.api, http.MethodGet, "/api/v1/billing", nil, token, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("billing status=%d body=%s", response.Code, response.Body.String())
	}
	var view planUsageView
	decode(t, response, &view)
	return view
}

type planUsageView struct {
	Plan  string `json:"plan"`
	Usage struct {
		Agents                 int `json:"agents"`
		ConversationsThisMonth int `json:"conversations_this_month"`
		HeldConversations      int `json:"held_conversations"`
	} `json:"usage"`
}

type apiError struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}
