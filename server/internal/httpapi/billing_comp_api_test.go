package httpapi_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/store"
)

func (f billingFixture) grantComp(t *testing.T, workspaceID string, until *time.Time) {
	t.Helper()
	if _, err := store.New(f.pool).GrantCompPlan(context.Background(), workspaceID, "team", "chinh", until, time.Now()); err != nil {
		t.Fatal(err)
	}
}

func TestComplimentaryWorkspaceBillingSaysSoAndWhenItEnds(t *testing.T) {
	f := newBillingFixture(t)
	forever := getBilling(t, f.api, f.adminA)
	if forever.Complimentary || forever.ComplimentaryUntil != nil {
		t.Fatalf("trial workspace reads as complimentary: %+v", forever)
	}
	end := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
	f.grantComp(t, f.workspaceA, &end)
	got := getBilling(t, f.api, f.adminA)
	if !got.Complimentary || got.Plan != "team" || got.ComplimentaryUntil == nil || !got.ComplimentaryUntil.Equal(end) {
		t.Fatalf("billing=%+v, want complimentary team until %s", got, end)
	}
	f.grantComp(t, f.workspaceA, nil)
	if got := getBilling(t, f.api, f.adminA); !got.Complimentary || got.ComplimentaryUntil != nil {
		t.Fatalf("a grant with no end: %+v, want complimentary with no end", got)
	}
	if other := getBilling(t, f.api, f.adminB); other.Complimentary {
		t.Fatalf("another workspace reads as complimentary: %+v", other)
	}
}

func TestPaidWorkspaceIsNeverComplimentary(t *testing.T) {
	f := newBillingFixture(t)
	f.mustDeliver(t, "msg_paid_plain", activeTeam(f.workspaceA, "sub_plain", time.Now()).event("subscription.active"))
	if got := getBilling(t, f.api, f.adminA); got.Complimentary {
		t.Fatalf("a Polar subscription reads as complimentary: %+v", got)
	}
}

func TestCheckoutRefusesAComplimentaryWorkspaceWithoutPointingAtThePortal(t *testing.T) {
	f := newBillingFixture(t)
	f.grantComp(t, f.workspaceA, nil)
	response := request(t, f.api, http.MethodPost, "/api/v1/billing/checkout", map[string]any{"plan": "team_yearly"}, f.adminA, nil)
	if response.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s, want 409: a paid subscription on top of a comp grant would bill for nothing", response.Code, response.Body.String())
	}
	body := strings.ToLower(response.Body.String())
	if !strings.Contains(body, "complimentary") || strings.Contains(body, "portal") {
		t.Fatalf("409 message must say the plan is complimentary and not mention the portal: %s", body)
	}
	if calls := f.fake.recorded(); len(calls) != 0 {
		t.Fatalf("calls=%v, want none", calls)
	}
}
