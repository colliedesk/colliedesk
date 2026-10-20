package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

func invite(t *testing.T, f limitsFixture, admin string, number int) *responseView {
	t.Helper()
	response := request(t, f.api, http.MethodPost, "/api/v1/auth/invites",
		map[string]any{"email": fmt.Sprintf("teammate%d@limits-alpha.example.com", number), "role_ceiling": "agent"}, admin, nil)
	return &responseView{Code: response.Code, Body: response.Body.String()}
}

type responseView struct {
	Code int
	Body string
}

func TestTrialCapsAgentsAtTenCountingPendingInvites(t *testing.T) {
	f := newLimitsFixture(t, true)
	// The admin is seat 1; nine pending invites fill seats 2 to 10.
	for number := 1; number <= 9; number++ {
		if got := invite(t, f, f.adminA, number); got.Code != http.StatusCreated {
			t.Fatalf("invite %d status=%d body=%s, want 201", number, got.Code, got.Body)
		}
	}
	eleventh := request(t, f.api, http.MethodPost, "/api/v1/auth/invites",
		map[string]any{"email": "teammate10@limits-alpha.example.com", "role_ceiling": "agent"}, f.adminA, nil)
	if eleventh.Code != http.StatusPaymentRequired {
		t.Fatalf("eleventh seat status=%d body=%s, want 402", eleventh.Code, eleventh.Body.String())
	}
	var failure apiError
	decode(t, eleventh, &failure)
	if failure.Error.Code != "plan_limit_reached" || !strings.Contains(failure.Error.Message, "trial") ||
		!strings.Contains(failure.Error.Message, "10 agents") {
		t.Fatalf("error=%+v, want the trial and the cap named", failure.Error)
	}
	if got := f.usage(t, f.adminA).Usage.Agents; got != 1 {
		t.Fatalf("agents=%d, want 1: a refused invite must not add anyone", got)
	}

	// The way out: revoking a pending invite frees its seat.
	var list struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	decode(t, request(t, f.api, http.MethodGet, "/api/v1/auth/invites", nil, f.adminA, nil), &list)
	if len(list.Items) != 9 {
		t.Fatalf("pending invites=%d, want 9 (the refused one is not stored)", len(list.Items))
	}
	if got := request(t, f.api, http.MethodDelete, "/api/v1/auth/invites/"+list.Items[0].ID, nil, f.adminA, nil).Code; got != http.StatusNoContent {
		t.Fatalf("revoke status=%d", got)
	}
	if got := invite(t, f, f.adminA, 10); got.Code != http.StatusCreated {
		t.Fatalf("invite after a revoke status=%d body=%s, want 201", got.Code, got.Body)
	}
}

func TestExpiredAndUsedInvitesDoNotHoldASeat(t *testing.T) {
	f := newLimitsFixture(t, true)
	for number := 1; number <= 9; number++ {
		invite(t, f, f.adminA, number)
	}
	f.clock.set(trialStart.Add(time.Hour)) // past the 15 minute life of the first nine links
	for number := 10; number <= 12; number++ {
		if got := invite(t, f, f.adminA, number); got.Code != http.StatusCreated {
			t.Fatalf("invite %d after the old ones expired: status=%d body=%s", number, got.Code, got.Body)
		}
	}
}

func TestPaidPlansAllowTenAgents(t *testing.T) {
	f := newLimitsFixture(t, true)
	f.upgrade(t, f.workspaceA, "sub_paid_agents")
	for number := 1; number <= 9; number++ {
		if got := invite(t, f, f.adminA, number); got.Code != http.StatusCreated {
			t.Fatalf("paid invite %d status=%d body=%s, want 201", number, got.Code, got.Body)
		}
	}
	eleventh := invite(t, f, f.adminA, 10)
	if eleventh.Code != http.StatusPaymentRequired || !strings.Contains(eleventh.Body, "10 agents") {
		t.Fatalf("eleventh seat status=%d body=%s, want 402 naming the 10 agent cap", eleventh.Code, eleventh.Body)
	}
}

func TestSelfHostedHasNoAgentLimitAndNoTrial(t *testing.T) {
	f := newLimitsFixture(t, false)
	for number := 1; number <= 12; number++ {
		if got := invite(t, f, f.adminA, number); got.Code != http.StatusCreated {
			t.Fatalf("self-hosted invite %d status=%d body=%s, want 201", number, got.Code, got.Body)
		}
	}
	// A trial that ended long ago means nothing without billing.
	if _, err := f.pool.Exec(context.Background(), `UPDATE workspaces SET trial_ends_at='2026-09-01T00:00:00Z'`); err != nil {
		t.Fatal(err)
	}
	result := f.receive(t, false, 1, "Years after any trial")
	if result.Status != "created" || result.ConversationID == nil {
		t.Fatalf("result=%+v", result)
	}
	note := request(t, f.api, http.MethodPost, "/api/v1/conversations/"+*result.ConversationID+"/messages",
		map[string]any{"kind": "note", "body": "still writable"}, f.adminA, nil)
	if note.Code != http.StatusCreated {
		t.Fatalf("self-hosted write status=%d body=%s, want 201", note.Code, note.Body.String())
	}
	var me struct {
		Plan *json.RawMessage `json:"plan"`
	}
	decode(t, request(t, f.api, http.MethodGet, "/api/v1/auth/me", nil, f.adminA, nil), &me)
	if me.Plan != nil && string(*me.Plan) != "null" {
		t.Fatalf("me.plan=%s, want null without billing", *me.Plan)
	}
}

func TestConcurrentInvitesNeverPassTheCap(t *testing.T) {
	f := newLimitsFixture(t, true)
	var wg sync.WaitGroup
	statuses := make(chan int, 14)
	for number := 1; number <= 14; number++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			statuses <- invite(t, f, f.adminA, number).Code
		}()
	}
	wg.Wait()
	close(statuses)
	accepted := 0
	for code := range statuses {
		if code == http.StatusCreated {
			accepted++
		}
	}
	if accepted != 9 {
		t.Fatalf("%d invites accepted on a trial workspace with 1 agent, want exactly 9", accepted)
	}
}

func TestReinvitingAnExistingAgentOrResendingAPendingInviteTakesNoSeat(t *testing.T) {
	f := newLimitsFixture(t, true)
	// Seat 2 is an agent; seats 3 to 10 are pending invites: the workspace is at the cap of 10.
	_, agentURL := createContractInvite(t, f.api, f.adminA, "agent@limits-alpha.example.com", "agent")
	if got := request(t, f.api, http.MethodPost, "/api/v1/auth/redeem", map[string]any{"token": linkToken(t, agentURL)}, "", nil).Code; got != http.StatusOK {
		t.Fatalf("redeem status=%d", got)
	}
	for number := 1; number <= 8; number++ {
		if got := invite(t, f, f.adminA, number); got.Code != http.StatusCreated {
			t.Fatalf("pending invite %d status=%d", number, got.Code)
		}
	}

	again := func(email string) int {
		return request(t, f.api, http.MethodPost, "/api/v1/auth/invites",
			map[string]any{"email": email, "role_ceiling": "admin"}, f.adminA, nil).Code
	}
	if got := again("agent@limits-alpha.example.com"); got != http.StatusCreated {
		t.Fatalf("role change for an existing agent at the cap status=%d, want 201", got)
	}
	if got := again("Teammate1@limits-alpha.example.com"); got != http.StatusCreated {
		t.Fatalf("re-sending a pending invite status=%d, want 201 (same person, same seat)", got)
	}
	if got := f.usage(t, f.adminA).Usage; got.Agents != 2 || got.PendingInvites != 8 {
		t.Fatalf("usage=%+v, want 2 agents and 8 pending invites (one person, not one per link)", got)
	}
	if got := again("someone-new@limits-alpha.example.com"); got != http.StatusPaymentRequired {
		t.Fatalf("a genuinely new person at the cap status=%d, want 402", got)
	}
}

func fillTeamSeats(t *testing.T, f limitsFixture) responseView {
	t.Helper()
	return fillSeats(t, f, productMonthly, 10)
}

// fillSeats moves workspace A to the plan product buys, fills its agents seats (the admin holds
// one) with pending invites, and returns the refusal of one more invite.
func fillSeats(t *testing.T, f limitsFixture, product string, agents int) responseView {
	t.Helper()
	sub := activeTeam(f.workspaceA, "sub_"+product, f.clock.Now())
	sub.Product = product
	f.billing.mustDeliver(t, "evt_"+product, sub.event("subscription.active"))
	for number := 1; number < agents; number++ {
		if got := invite(t, f, f.adminA, number); got.Code != http.StatusCreated {
			t.Fatalf("invite %d status=%d body=%s, want 201 under the cap of %d", number, got.Code, got.Body, agents)
		}
	}
	return *invite(t, f, f.adminA, agents)
}

func TestSoloSeatLimitPointsToSmall(t *testing.T) {
	refused := fillSeats(t, newLimitsFixture(t, true), productSoloMonthly, 1)
	want := "The Solo plan allows 1 agent, pending invites included. Revoke a pending invite to free a seat, or upgrade to Small (up to 3 agents) under Manage billing."
	if refused.Code != http.StatusPaymentRequired || !strings.Contains(refused.Body, want) {
		t.Fatalf("status=%d body=%s, want 402 with %q", refused.Code, refused.Body, want)
	}
}

func TestSmallSeatLimitPointsToTeam(t *testing.T) {
	refused := fillSeats(t, newLimitsFixture(t, true), productSmallYearly, 3)
	if refused.Code != http.StatusPaymentRequired || !strings.Contains(refused.Body, "upgrade to Team (up to 10 agents)") {
		t.Fatalf("status=%d body=%s, want a pointer to Team", refused.Code, refused.Body)
	}
}

func TestTeamSeatLimitPointsToTeam25WhenItIsOnSale(t *testing.T) {
	refused := fillTeamSeats(t, newLimitsFixture(t, true))
	if refused.Code != http.StatusPaymentRequired || !strings.Contains(refused.Body, "upgrade to Team 25") {
		t.Fatalf("status=%d body=%s, want a pointer to Team 25", refused.Code, refused.Body)
	}
}

func TestTeamSeatLimitSaysToContactUsWhenTeam25IsNotOnSale(t *testing.T) {
	f := newLimitsFixtureConfigured(t, true, nil, func(config *httpapi.Config) {
		config.Billing.Products.Team25Monthly, config.Billing.Products.Team25Yearly = "", ""
	})
	refused := fillTeamSeats(t, f)
	if refused.Code != http.StatusPaymentRequired || !strings.Contains(refused.Body, "contact us for more than 10 agents") ||
		strings.Contains(strings.ToLower(refused.Body), "upgrade") {
		t.Fatalf("status=%d body=%s, want a contact-us message without an upgrade offer", refused.Code, refused.Body)
	}
}
