package httpapi_test

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/jackc/pgx/v5"
)

// queryCounter counts the statements a pool sends, and how many of them open a transaction.
type queryCounter struct {
	mu         sync.Mutex
	statements int
	begins     int
}

func (c *queryCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.statements++
	if strings.EqualFold(strings.TrimSpace(data.SQL), "begin") {
		c.begins++
	}
	return ctx
}

func (*queryCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// measure runs one request and returns the statements and transactions it cost.
func (c *queryCounter) measure(t *testing.T, f limitsFixture, path, token string) (statements, begins int) {
	t.Helper()
	// A session used in the last minute skips its last-seen write. Make the write due, so that every
	// measurement takes the same path whatever clock the fixture runs on.
	if _, err := f.pool.Exec(context.Background(), `UPDATE sessions SET last_seen_at=timestamptz '2000-01-01'`); err != nil {
		t.Fatal(err)
	}
	c.mu.Lock()
	c.statements, c.begins = 0, 0
	c.mu.Unlock()
	if got := request(t, f.api, http.MethodGet, path, nil, token, nil).Code; got != http.StatusOK {
		t.Fatalf("GET %s status=%d, want 200", path, got)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.statements, c.begins
}

// The lock check must ride on the transaction that authenticates the session: a paying customer
// pays no extra transaction per request, and /auth/me reads the state without a query of its own.
func TestPlanAccessAddsNoTransactionToARequest(t *testing.T) {
	onCounter, offCounter := &queryCounter{}, &queryCounter{}
	on := newLimitsFixtureTraced(t, true, onCounter)
	off := newLimitsFixtureTraced(t, false, offCounter)
	for _, path := range []string{"/api/v1/auth/me", "/api/v1/conversations"} {
		baseStatements, baseBegins := offCounter.measure(t, off, path, off.adminA)

		statements, begins := onCounter.measure(t, on, path, on.adminA)
		if begins != baseBegins || statements > baseStatements+1 {
			t.Errorf("%s in the trial: %d statements, %d transactions; billing off: %d, %d (allowed: one extra statement, no extra transaction)",
				path, statements, begins, baseStatements, baseBegins)
		}

		on.upgrade(t, on.workspaceA, "sub_cost_"+strings.ReplaceAll(path, "/", "_"))
		on.clock.set(trialEnd.Add(24 * time.Hour))
		statements, begins = onCounter.measure(t, on, path, on.adminA)
		if begins != baseBegins || statements > baseStatements+1 {
			t.Errorf("%s after the trial with a paid plan: %d statements, %d transactions; billing off: %d, %d",
				path, statements, begins, baseStatements, baseBegins)
		}
		on.clock.set(trialStart)
	}
}

func TestLockedAllowlistOnlyNamesRealRoutes(t *testing.T) {
	routes := map[string]bool{}
	for _, route := range httpapi.Routes() {
		routes[route.Method+" "+route.Pattern] = true
	}
	for _, key := range httpapi.LockedAllowlist() {
		if !routes[key] {
			t.Errorf("allowlist entry %q matches no route in Routes(); a rename would silently lock it", key)
		}
	}
}

func TestLockedAdminCanStillReadTheEmailChannelItMayDisconnect(t *testing.T) {
	f := newLimitsFixture(t, true)
	f.clock.set(trialEnd)
	if got := request(t, f.api, http.MethodGet, "/api/v1/channels/email", nil, f.adminA, nil).Code; got != http.StatusOK {
		t.Fatalf("GET channel while locked: status=%d, want 200", got)
	}
}

// An invite made while the workspace was open must not change anyone's role once it is locked,
// even for a person who already holds a seat.
func TestInviteCannotChangeARoleWhileLocked(t *testing.T) {
	f := newLimitsFixture(t, true)
	_, agentURL := createContractInvite(t, f.api, f.adminA, "member@limits-alpha.example.com", "agent")
	if got := request(t, f.api, http.MethodPost, "/api/v1/auth/redeem", map[string]any{"token": linkToken(t, agentURL)}, "", nil).Code; got != http.StatusOK {
		t.Fatalf("first redeem status=%d", got)
	}
	_, promotion := createContractInvite(t, f.api, f.adminA, "member@limits-alpha.example.com", "admin")
	if _, err := f.pool.Exec(context.Background(), `UPDATE workspaces SET trial_ends_at=$2 WHERE id=$1`, f.workspaceA, f.clock.Now()); err != nil {
		t.Fatal(err)
	}
	refused := request(t, f.api, http.MethodPost, "/api/v1/auth/redeem", map[string]any{"token": linkToken(t, promotion)}, "", nil)
	wantLocked(t, &responseView{Code: refused.Code, Body: refused.Body.String()}, "role change while locked")
	if role := queryString(t, f.pool, `SELECT role FROM agents WHERE email='member@limits-alpha.example.com'`); role != "agent" {
		t.Fatalf("role=%s, want agent: a locked workspace must not change roles", role)
	}
	if unused := queryString(t, f.pool, `SELECT count(*)::text FROM pairing_links WHERE email='member@limits-alpha.example.com' AND kind='invite' AND used_at IS NULL`); unused != "1" {
		t.Fatalf("unused invite links=%s, want 1: the link survives", unused)
	}
	f.upgrade(t, f.workspaceA, "sub_role_unlock")
	if got := request(t, f.api, http.MethodPost, "/api/v1/auth/redeem", map[string]any{"token": linkToken(t, promotion)}, "", nil).Code; got != http.StatusOK {
		t.Fatalf("redeem after paying status=%d, want 200", got)
	}
}
