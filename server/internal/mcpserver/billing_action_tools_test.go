package mcpserver_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/polar"
)

// sentMail accepts every message, so a migration request can go out.
type sentMail struct{}

func (sentMail) Send(context.Context, string, []string, []byte) error { return nil }

// A token that holds billing:manage asks for a Freshdesk migration at once; one without it is refused
// and nothing is sent.
func TestBillingActionRequestsAMigrationAtOnceWithBillingManage(t *testing.T) {
	e := newEnvWith(t, func(config *httpapi.Config) {
		config.Email.MailFromDomain, config.AlertMailer = "mail.example.com", sentMail{}
		config.Billing = polar.Config{AccessToken: "token-for-test", WebhookSecret: "secret-for-test", Products: polar.Products{TeamMonthly: "p1", TeamYearly: "p2", Founding: "p3"}}
	})
	w := e.newWorkspace("Move Desk", "admin@move.example.com")
	args := map[string]any{"action": "migration_request", "freshdesk_address": "acme.freshdesk.com", "times": "Tuesday afternoon UTC"}

	refusedWithoutPermission(t, e, w.send, "colliedesk_billing_action", args)
	if status, billing := e.rest(http.MethodGet, "/api/v1/billing", nil, w.admin); status != http.StatusOK || billing["migration_requested_at"] != nil {
		t.Fatalf("billing after a refused call: %d %v, want no migration request", status, billing)
	}

	session := e.connect(w.adminTier, "")
	if message := e.fails(session, "colliedesk_billing_action", map[string]any{"action": "migration_request"}); !strings.Contains(message, "freshdesk_address") {
		t.Errorf("without an address: %q, want a message naming freshdesk_address", message)
	}
	out := e.ok(session, "colliedesk_billing_action", args)
	if next, _ := out["next"].(string); !strings.Contains(next, "Ada") && !strings.Contains(next, "admin@move.example.com") {
		t.Errorf("next = %q, want it to name the person support writes to", next)
	}
	if status, billing := e.rest(http.MethodGet, "/api/v1/billing", nil, w.admin); status != http.StatusOK || billing["migration_requested_at"] == nil {
		t.Fatalf("billing after the call: %d %v, want the migration request recorded", status, billing)
	}
}
