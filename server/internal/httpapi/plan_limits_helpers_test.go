package httpapi_test

import (
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

// trialStart is the instant the fixture workspaces are created; their trial ends 14 days later, at
// 2026-10-15T09:00:00Z.
var trialStart = time.Date(2026, time.October, 1, 9, 0, 0, 0, time.UTC)

// trialEnd is written out by hand, not computed from trialStart.
var trialEnd = time.Date(2026, time.October, 15, 9, 0, 0, 0, time.UTC)

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
	clock := &testClock{now: trialStart}
	fake := newFakePolarAPI(t)
	api, service, pool, _ := testAPIWithConfig(t, func(config *httpapi.Config) {
		config.Now = clock.Now
		if billingEnabled {
			config.Billing = fake.config()
		}
	})
	service = service.WithClock(clock.Now)
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
	Plan        string `json:"plan"`
	TrialEndsAt string `json:"trial_ends_at"`
	Locked      bool   `json:"locked"`
	Limits      struct {
		Agents int `json:"agents"`
	} `json:"limits"`
	Usage struct {
		Agents         int `json:"agents"`
		PendingInvites int `json:"pending_invites"`
	} `json:"usage"`
}

type apiError struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}
