package httpapi_test

import (
	"net/http"
	"slices"
	"testing"
	"time"
)

// The agent step reads connected once the workspace has a live agent token, and none again once it is revoked.
func TestSetupAgentStepFollowsAnAgentToken(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "Agent Step Desk", "admin@agent-step.example.com")
	if got := readSetup(t, api, admin).state(t, "agent"); got != "none" {
		t.Fatalf("fresh workspace agent = %q, want none", got)
	}
	issued := issueToken(t, api, admin, "Claude", "read")
	if got := readSetup(t, api, admin).state(t, "agent"); got != "connected" {
		t.Fatalf("with a live token agent = %q, want connected", got)
	}
	// A token past its expiry is not a connection.
	if _, err := pool.Exec(t.Context(), `UPDATE sessions SET expires_at='2000-01-01T00:00:00Z' WHERE id=$1`, issued.Session.ID); err != nil {
		t.Fatal(err)
	}
	if got := readSetup(t, api, admin).state(t, "agent"); got != "none" {
		t.Fatalf("with an expired token agent = %q, want none", got)
	}
	second := issueToken(t, api, admin, "Codex", "draft")
	if got := readSetup(t, api, admin).state(t, "agent"); got != "connected" {
		t.Fatalf("with a second live token agent = %q, want connected", got)
	}
	if response := request(t, api, http.MethodDelete, "/api/v1/auth/sessions/"+second.Session.ID, nil, admin, nil); response.Code != http.StatusNoContent && response.Code != http.StatusOK {
		t.Fatalf("revoke status=%d body=%s", response.Code, response.Body.String())
	}
	if got := readSetup(t, api, admin).state(t, "agent"); got != "none" {
		t.Fatalf("with the token revoked agent = %q, want none", got)
	}
}

// An app connected with OAuth counts once it exchanged its code; a consent nobody exchanged is not a connection.
func TestSetupAgentStepFollowsAnOAuthGrant(t *testing.T) {
	f := newOAuthFixture(t)
	admin := createSession(t, f.api, f.service, "Agent OAuth Desk", "admin@agent-oauth.example.com")
	_, challenge := pkce(t)
	redirectParams(t, f.allow(t, admin, f.register(t, "Claude", oauthRedirect), oauthRedirect, challenge, nil))
	if got := readSetup(t, f.api, admin).state(t, "agent"); got != "none" {
		t.Fatalf("with a code not exchanged agent = %q, want none", got)
	}
	f.connect(t, admin)
	if got := readSetup(t, f.api, admin).state(t, "agent"); got != "connected" {
		t.Fatalf("with a connected app agent = %q, want connected", got)
	}
	var sessions struct {
		Items []struct {
			ID  string    `json:"id"`
			App *struct{} `json:"app"`
		} `json:"items"`
	}
	decode(t, request(t, f.api, http.MethodGet, "/api/v1/auth/sessions", nil, admin, nil), &sessions)
	for _, session := range sessions.Items {
		if session.App == nil {
			continue
		}
		if response := request(t, f.api, http.MethodDelete, "/api/v1/auth/sessions/"+session.ID, nil, admin, nil); response.Code != http.StatusNoContent && response.Code != http.StatusOK {
			t.Fatalf("disconnect status=%d body=%s", response.Code, response.Body.String())
		}
	}
	if got := readSetup(t, f.api, admin).state(t, "agent"); got != "none" {
		t.Fatalf("with the app disconnected agent = %q, want none", got)
	}
	// A grant that slid past its 30 idle days needs reconnecting: not a connection either.
	f.connect(t, admin)
	if got := readSetup(t, f.api, admin).state(t, "agent"); got != "connected" {
		t.Fatalf("with an app connected again agent = %q, want connected", got)
	}
	if _, err := f.pool.Exec(t.Context(), `UPDATE sessions SET expires_at=$1 WHERE scope='oauth' AND revoked_at IS NULL`, f.clock.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if got := readSetup(t, f.api, admin).state(t, "agent"); got != "none" {
		t.Fatalf("with the grant expired agent = %q, want none", got)
	}
}

// For a team that signed up to let AI agents work, connecting one is the first step and the recommended one until
// it is done; every other goal has it after reply targets and before the invites. complete never waits for it.
func TestSetupAgentStepIsFirstForTheAIAgentsGoalAndNeverHoldsSetupBack(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "Agent Goal Desk", "admin@agent-goal.example.com")
	slug := workspaceSlug(t, api, admin)
	setGoal := func(goal string) {
		t.Helper()
		if _, err := pool.Exec(t.Context(), `INSERT INTO workspace_onboarding(workspace_id,goal) SELECT id,$2 FROM workspaces WHERE slug=$1
			ON CONFLICT (workspace_id) DO UPDATE SET goal=EXCLUDED.goal`, slug, goal); err != nil {
			t.Fatal(err)
		}
	}
	usual := []string{"email", "import", "ai_drafts", "reply_targets", "agent", "invite", "first_reply"}
	if got := readSetup(t, api, admin).keys(); !slices.Equal(got, usual) {
		t.Fatalf("no goal: order = %v, want %v", got, usual)
	}
	for _, goal := range []string{"shared_inbox", "other"} {
		setGoal(goal)
		if got := readSetup(t, api, admin).keys(); !slices.Equal(got, usual) {
			t.Fatalf("goal %s: order = %v, want %v", goal, got, usual)
		}
	}
	setGoal("leave_helpdesk")
	if got, want := readSetup(t, api, admin).keys(), []string{"import", "email", "ai_drafts", "reply_targets", "agent", "invite", "first_reply"}; !slices.Equal(got, want) {
		t.Fatalf("goal leave_helpdesk: order = %v, want %v", got, want)
	}

	setGoal("ai_agents")
	view := readSetup(t, api, admin)
	if got, want := view.keys(), []string{"agent", "email", "import", "ai_drafts", "reply_targets", "invite", "first_reply"}; !slices.Equal(got, want) {
		t.Fatalf("goal ai_agents: order = %v, want %v", got, want)
	}
	if view.Recommended == nil || *view.Recommended != "agent" {
		t.Fatalf("goal ai_agents recommended = %v, want agent", view.Recommended)
	}
	issueToken(t, api, admin, "Claude", "read")
	view = readSetup(t, api, admin)
	if view.state(t, "agent") != "connected" || view.Recommended != nil {
		t.Fatalf("goal ai_agents with an agent connected: agent=%q recommended=%v, want connected and nothing", view.state(t, "agent"), view.Recommended)
	}
	if view.Complete {
		t.Fatal("a connected agent alone must not complete the setup")
	}
}

func TestSetupAgentStepNeverCountsAnotherWorkspacesToken(t *testing.T) {
	api, service, _ := testAPIWithPool(t)
	mine := createSession(t, api, service, "Mine Agent Desk", "admin@mine-agent.example.com")
	theirs := createSession(t, api, service, "Theirs Agent Desk", "admin@theirs-agent.example.com")
	issueToken(t, api, theirs, "Claude", "read")
	if got := readSetup(t, api, theirs).state(t, "agent"); got != "connected" {
		t.Fatalf("theirs agent = %q, want connected", got)
	}
	if got := readSetup(t, api, mine).state(t, "agent"); got != "none" {
		t.Fatalf("mine agent = %q, want none", got)
	}
}
