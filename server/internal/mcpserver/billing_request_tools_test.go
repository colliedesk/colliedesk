package mcpserver_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/polar"
)

func TestBillingRequestFilesAMigrationRequestForAPersonToApprove(t *testing.T) {
	e := newEnvWith(t, func(config *httpapi.Config) {
		config.Billing = polar.Config{AccessToken: "token-for-test", WebhookSecret: "secret-for-test", Products: polar.Products{TeamMonthly: "p1", TeamYearly: "p2", Founding: "p3"}}
	})
	w := e.newWorkspace("Move Desk", "admin@move.example.com")
	session := e.connect(w.read, "")

	if message := e.fails(session, "colliedesk_billing_request", map[string]any{"action": "migration_request"}); !strings.Contains(message, "freshdesk_address") {
		t.Errorf("without an address: %q, want a message naming freshdesk_address", message)
	}
	out := e.ok(session, "colliedesk_billing_request", map[string]any{"action": "migration_request", "freshdesk_address": "acme.freshdesk.com", "times": "Tuesday afternoon UTC"})
	id, _ := at(t, out, "agent_request", "id").(string)
	if id == "" {
		t.Fatalf("migration_request: %v, want an agent request", out)
	}
	got := e.ok(session, "colliedesk_request_get", map[string]any{"request_id": id})
	if at(t, got, "request", "call") != "POST /api/v1/billing/migration-request" || at(t, got, "request", "status") != "pending" {
		t.Fatalf("filed %v, want a pending POST /api/v1/billing/migration-request", got)
	}
	if status, billing := e.rest(http.MethodGet, "/api/v1/billing", nil, w.admin); status != http.StatusOK || billing["migration_requested_at"] != nil {
		t.Fatalf("billing after filing: %d %v, want no migration request before approval", status, billing)
	}
}
