package httpapi_test

import (
	"net/http"
	"testing"
	"time"
)

type mePlanView struct {
	Plan *struct {
		Plan   string    `json:"plan"`
		Admins *[]string `json:"admins"`
	} `json:"plan"`
}

func mePlan(t *testing.T, f billingFixture, token string) mePlanView {
	t.Helper()
	response := request(t, f.api, http.MethodGet, "/api/v1/auth/me", nil, token, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("GET /auth/me status=%d body=%s", response.Code, response.Body.String())
	}
	var view mePlanView
	decode(t, response, &view)
	return view
}

// withAdminNames gives workspace A a second admin without a display name and renames the first.
func withAdminNames(t *testing.T, f billingFixture) {
	t.Helper()
	if got := request(t, f.api, http.MethodPatch, "/api/v1/agents/me/display-name", map[string]any{"display_name": "Zed Admin"}, f.adminA, nil).Code; got != http.StatusOK {
		t.Fatalf("set display name: %d", got)
	}
	_, url := createContractInvite(t, f.api, f.adminA, "second@billing-alpha.example.com", "admin")
	if got := request(t, f.api, http.MethodPost, "/api/v1/auth/redeem", map[string]any{"token": linkToken(t, url)}, "", nil).Code; got != http.StatusOK {
		t.Fatalf("redeem second admin: %d", got)
	}
}

func TestNonAdminOnATrialOrLockedWorkspaceSeesWhoTheAdminsAre(t *testing.T) {
	f := newBillingFixture(t)
	withAdminNames(t, f)
	view := mePlan(t, f, f.agentTokenA)
	if view.Plan == nil || view.Plan.Plan != "trial" || view.Plan.Admins == nil {
		t.Fatalf("trial non-admin: %+v, want plan.admins", view.Plan)
	}
	if got := *view.Plan.Admins; len(got) != 2 || got[0] != "Zed Admin" || got[1] != "second@billing-alpha.example.com" {
		t.Fatalf("admins=%v, want [Zed Admin second@billing-alpha.example.com] (display name, else the email, ordered)", got)
	}

	if _, err := f.pool.Exec(t.Context(), `UPDATE workspaces SET trial_ends_at=$2 WHERE id=$1`, f.workspaceA, time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	locked := mePlan(t, f, f.agentTokenA)
	if locked.Plan == nil || locked.Plan.Plan != "none" || locked.Plan.Admins == nil || len(*locked.Plan.Admins) != 2 {
		t.Fatalf("locked non-admin: %+v, want plan none with admins", locked.Plan)
	}
}

func TestAdminsAreNotListedToAdminsToPayingWorkspacesOrTokens(t *testing.T) {
	f := newBillingFixture(t)
	withAdminNames(t, f)
	if view := mePlan(t, f, f.adminA); view.Plan == nil || view.Plan.Admins != nil {
		t.Fatalf("admin viewer: %+v, want plan.admins absent", view.Plan)
	}
	token := issueToken(t, f.api, f.adminA, "reader", "read").Token
	if view := mePlan(t, f, token); view.Plan == nil || view.Plan.Admins != nil {
		t.Fatalf("agent token: %+v, want plan.admins absent", view.Plan)
	}
	f.mustDeliver(t, "msg_paid", subscription{ID: "sub_paid", Workspace: f.workspaceA, Product: productFounding, Status: "active",
		PeriodEnd: time.Now().Add(30 * 24 * time.Hour), ModifiedAt: time.Now()}.event("subscription.active"))
	if view := mePlan(t, f, f.agentTokenA); view.Plan == nil || view.Plan.Plan != "founding" || view.Plan.Admins != nil {
		t.Fatalf("paid non-admin: %+v, want founding without admins", view.Plan)
	}
}

func TestDeactivatedAdminsAndMoreThanFiveAreLeftOutOfTheAdminList(t *testing.T) {
	f := newBillingFixture(t)
	for _, email := range []string{"b@x.example", "c@x.example", "d@x.example", "e@x.example", "f@x.example", "g@x.example"} {
		if _, err := f.pool.Exec(t.Context(), `INSERT INTO agents(workspace_id,email,role,deactivated_at) VALUES($1,$2,'admin',CASE WHEN $2='b@x.example' THEN now() END)`, f.workspaceA, email); err != nil {
			t.Fatal(err)
		}
	}
	view := mePlan(t, f, f.agentTokenA)
	if view.Plan == nil || view.Plan.Admins == nil || len(*view.Plan.Admins) != 5 {
		t.Fatalf("admins=%v, want five", view.Plan)
	}
	for _, name := range *view.Plan.Admins {
		if name == "b@x.example" {
			t.Fatal("a deactivated admin is listed")
		}
	}
}

func TestSelfHostedMeHasNoPlanAndNoAdmins(t *testing.T) {
	api, service, _, _ := testAPIWithConfig(t, nil)
	admin := createSession(t, api, service, "Self Hosted", "admin@self-hosted.example.com")
	_, url := createContractInvite(t, api, admin, "agent@self-hosted.example.com", "agent")
	redeemed := request(t, api, http.MethodPost, "/api/v1/auth/redeem", map[string]any{"token": linkToken(t, url)}, "", nil)
	var session struct {
		Token string `json:"token"`
	}
	decode(t, redeemed, &session)
	response := request(t, api, http.MethodGet, "/api/v1/auth/me", nil, session.Token, nil)
	var view struct {
		Plan any `json:"plan"`
	}
	decode(t, response, &view)
	if view.Plan != nil {
		t.Fatalf("plan=%v, want null on a server without billing", view.Plan)
	}
}
