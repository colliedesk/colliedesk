package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

type checkoutRefusalView struct {
	Plan   string `json:"plan"`
	Reason string `json:"reason"`
}

type planFitView struct {
	CheckoutRefusals   []checkoutRefusalView `json:"checkout_refusals"`
	StorageUpgradePlan *string               `json:"storage_upgrade_plan"`
}

func planFit(t *testing.T, f limitsFixture, token string) planFitView {
	t.Helper()
	response := request(t, f.api, http.MethodGet, "/api/v1/billing", nil, token, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/billing status=%d body=%s", response.Code, response.Body.String())
	}
	var view planFitView
	decode(t, response, &view)
	return view
}

func refusalFor(view planFitView, plan string) string {
	for _, refusal := range view.CheckoutRefusals {
		if refusal.Plan == plan {
			return refusal.Reason
		}
	}
	return ""
}

func TestCheckoutRefusesAPlanWithFewerSeatsThanTheWorkspaceUses(t *testing.T) {
	f := newLimitsFixture(t, true)
	// The admin and two pending invites hold three seats.
	for number := 1; number <= 2; number++ {
		if got := invite(t, f, f.adminA, number); got.Code != http.StatusCreated {
			t.Fatalf("invite %d status=%d", number, got.Code)
		}
	}
	refused := request(t, f.api, http.MethodPost, "/api/v1/billing/checkout", map[string]any{"plan": "solo_monthly"}, f.adminA, nil)
	want := "The Solo plan allows 1 agent and this workspace uses 3 seats (agents and pending invites). Deactivate agents or revoke invites to free 2 seats, then choose Solo."
	if refused.Code != http.StatusConflict || !strings.Contains(refused.Body.String(), "plan_does_not_fit") || !strings.Contains(refused.Body.String(), want) {
		t.Fatalf("solo checkout with 3 seats: status=%d body=%s, want 409 plan_does_not_fit with %q", refused.Code, refused.Body.String(), want)
	}
	if calls := f.billing.fake.callsTo("/v1/checkouts/"); len(calls) != 0 {
		t.Fatalf("a refused checkout reached Polar: %v", calls)
	}
	if got := request(t, f.api, http.MethodPost, "/api/v1/billing/checkout", map[string]any{"plan": "small_monthly"}, f.adminA, nil).Code; got != http.StatusOK {
		t.Fatalf("small checkout with 3 seats: status=%d, want 200 (3 seats fit)", got)
	}
	view := planFit(t, f, f.adminA)
	for _, plan := range []string{"solo_monthly", "solo_yearly"} {
		if refusalFor(view, plan) != want {
			t.Errorf("checkout_refusals for %s=%q, want %q", plan, refusalFor(view, plan), want)
		}
	}
	for _, plan := range []string{"small_monthly", "team_monthly", "team25_yearly", "founding"} {
		if reason := refusalFor(view, plan); reason != "" {
			t.Errorf("%s refused with %q, want it offered", plan, reason)
		}
	}
}

func TestCheckoutRefusesAPlanWhoseStorageCapIsBelowTheStoredBytesImportsIncluded(t *testing.T) {
	f := newLimitsFixture(t, true)
	// Imported files do not count on the trial but count on every paid plan.
	if _, err := f.pool.Exec(context.Background(), `UPDATE workspaces SET stored_storage_bytes=$2 WHERE id=$1`, f.workspaceA, int64(12)<<30); err != nil {
		t.Fatal(err)
	}
	refused := request(t, f.api, http.MethodPost, "/api/v1/billing/checkout", map[string]any{"plan": "solo_yearly"}, f.adminA, nil)
	want := "The Solo plan stores up to 10 GiB of attachments and this workspace stores 12 GiB, imported files included. Choose a plan with more storage."
	if refused.Code != http.StatusConflict || !strings.Contains(refused.Body.String(), want) {
		t.Fatalf("solo checkout with 12 GiB stored: status=%d body=%s, want 409 with %q", refused.Code, refused.Body.String(), want)
	}
	view := planFit(t, f, f.adminA)
	if refusalFor(view, "solo_monthly") != want || refusalFor(view, "small_monthly") != "" {
		t.Fatalf("checkout_refusals=%+v, want Solo refused for storage and Small offered", view.CheckoutRefusals)
	}
}

// moveTo replaces the workspace's subscription with one on the plan product buys, the way a plan
// change in the Polar portal arrives: the same subscription id with a newer modification.
func moveTo(t *testing.T, f limitsFixture, subscriptionID, product string, at time.Time) {
	t.Helper()
	sub := activeTeam(f.workspaceA, subscriptionID, at)
	sub.Product = product
	f.billing.mustDeliver(t, fmt.Sprintf("evt_%s_%d", product, at.UnixNano()), sub.event("subscription.updated"))
}

type meView struct {
	Plan struct {
		Plan       string `json:"plan"`
		AgentsOver int    `json:"agents_over"`
		SeatPaused bool   `json:"seat_paused"`
	} `json:"plan"`
}

func me(t *testing.T, f limitsFixture, token string) meView {
	t.Helper()
	response := request(t, f.api, http.MethodGet, "/api/v1/auth/me", nil, token, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("GET /auth/me status=%d body=%s", response.Code, response.Body.String())
	}
	var view meView
	decode(t, response, &view)
	return view
}

func TestADowngradeBelowTheTeamSizePausesTheNewestAgentsUntilTheAdminMakesRoom(t *testing.T) {
	f := newLimitsFixture(t, true)
	moveTo(t, f, "sub_down", productMonthly, f.clock.Now())
	// The admin (the owner) and three agents, joined oldest to newest.
	agents := make([]string, 3)
	for index := range agents {
		agents[index] = joinAsAgent(t, f.api, f.adminA, fmt.Sprintf("agent%d@limits-alpha.example.com", index+1))
	}
	// The workspace moves to Small (3 agents) in the portal while it has 4.
	moveTo(t, f, "sub_down", productSmallMonthly, f.clock.Now().Add(time.Minute))

	if view := me(t, f, f.adminA); view.Plan.Plan != "small" || view.Plan.AgentsOver != 1 || view.Plan.SeatPaused {
		t.Fatalf("admin /auth/me plan=%+v, want small, 1 agent over the plan, the admin not paused", view.Plan)
	}
	newest := agents[2]
	if view := me(t, f, newest); !view.Plan.SeatPaused {
		t.Fatalf("newest agent /auth/me plan=%+v, want seat_paused", view.Plan)
	}
	paused := request(t, f.api, http.MethodGet, "/api/v1/conversations", nil, newest, nil)
	if paused.Code != http.StatusPaymentRequired || !strings.Contains(paused.Body.String(), "seat_paused") ||
		!strings.Contains(paused.Body.String(), "The Small plan allows 3 agents") {
		t.Fatalf("newest agent reads the inbox: status=%d body=%s, want 402 seat_paused naming the plan", paused.Code, paused.Body.String())
	}
	if got := request(t, f.api, http.MethodPost, "/api/v1/auth/logout", nil, agents[0], nil).Code; got != http.StatusNoContent {
		t.Fatalf("an older agent signs out: status=%d", got)
	}
	if got := request(t, f.api, http.MethodGet, "/api/v1/conversations", nil, agents[1], nil).Code; got != http.StatusOK {
		t.Fatalf("an agent inside the plan reads the inbox: status=%d, want 200", got)
	}
	if got := invite(t, f, f.adminA, 9); got.Code != http.StatusPaymentRequired {
		t.Fatalf("invite while over the plan: status=%d body=%s, want 402", got.Code, got.Body)
	}

	// The admin deactivates the newest agent: the workspace fits again.
	var newestID string
	for _, member := range teamMembers(t, f, f.adminA) {
		if member.Email == "agent3@limits-alpha.example.com" {
			newestID = member.ID
		}
	}
	if got := request(t, f.api, http.MethodPatch, "/api/v1/agents/"+newestID, map[string]any{"active": false}, f.adminA, nil).Code; got != http.StatusOK {
		t.Fatalf("deactivate the newest agent: status=%d", got)
	}
	if view := me(t, f, f.adminA); view.Plan.AgentsOver != 0 {
		t.Fatalf("after deactivating: plan=%+v, want no agent over the plan", view.Plan)
	}
}

func TestTheOwnerIsNeverPausedEvenWhenAddedBeforeEveryoneElse(t *testing.T) {
	f := newLimitsFixture(t, true)
	moveTo(t, f, "sub_solo", productMonthly, f.clock.Now())
	agent := joinAsAgent(t, f.api, f.adminA, "agent1@limits-alpha.example.com")
	moveTo(t, f, "sub_solo", productSoloMonthly, f.clock.Now().Add(time.Minute))
	if view := me(t, f, f.adminA); view.Plan.SeatPaused || view.Plan.AgentsOver != 1 {
		t.Fatalf("owner on Solo with one more agent: plan=%+v, want not paused and 1 over", view.Plan)
	}
	if got := request(t, f.api, http.MethodGet, "/api/v1/conversations", nil, f.adminA, nil).Code; got != http.StatusOK {
		t.Fatalf("owner reads the inbox: status=%d, want 200", got)
	}
	if view := me(t, f, agent); !view.Plan.SeatPaused {
		t.Fatalf("the other agent on Solo: plan=%+v, want paused", view.Plan)
	}
}

type seatMemberView struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

func teamMembers(t *testing.T, f limitsFixture, token string) []seatMemberView {
	t.Helper()
	response := request(t, f.api, http.MethodGet, "/api/v1/agents", nil, token, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("GET /agents status=%d body=%s", response.Code, response.Body.String())
	}
	var page struct {
		Items []seatMemberView `json:"items"`
	}
	decode(t, response, &page)
	return page.Items
}

func TestStorageUpgradePlanIsTheNextPlanOnSaleWithMoreStorage(t *testing.T) {
	for product, want := range map[string]string{productSoloMonthly: "small", productSmallYearly: "team", productFounding: "team", productMonthly: "team25", productTeam25Yearly: ""} {
		t.Run(product, func(t *testing.T) {
			f := newLimitsFixture(t, true)
			moveTo(t, f, "sub_storage", product, f.clock.Now())
			got := planFit(t, f, f.adminA).StorageUpgradePlan
			if (want == "") != (got == nil) || (got != nil && *got != want) {
				t.Fatalf("storage_upgrade_plan=%v, want %q", got, want)
			}
		})
	}
}
