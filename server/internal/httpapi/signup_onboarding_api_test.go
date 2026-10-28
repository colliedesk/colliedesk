package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

// signUpWith posts a signup with any body, so a test can send the onboarding answers or bad values.
func (f signupFixture) signUpWith(t *testing.T, email, peer string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	req := newRequest(t, http.MethodPost, "/api/v1/auth/cloud/signup", body, f.token(t, map[string]any{"email": email}), nil)
	req.Header.Set("Origin", testOrigin)
	req.RemoteAddr = peer
	return serve(f.api, req)
}

type meOnboarding struct {
	OnboardingGoal *string `json:"onboarding_goal"`
	Workspace      struct {
		ID string `json:"id"`
	} `json:"workspace"`
}

func (f signupFixture) meAfter(t *testing.T, created *httptest.ResponseRecorder) meOnboarding {
	t.Helper()
	response := request(t, f.api, http.MethodGet, "/api/v1/auth/me", nil, "", sessionCookie(t, created))
	if response.Code != http.StatusOK {
		t.Fatalf("me status=%d body=%s", response.Code, response.Body.String())
	}
	var me meOnboarding
	decode(t, response, &me)
	return me
}

func TestSignupStoresOnboardingAnswersAndMeReturnsTheGoal(t *testing.T) {
	f := newSignupFixture(t, nil)
	created := f.signUpWith(t, "ada@onboarding.example.com", defaultPeer, map[string]any{
		"workspace_name": "Onboarding Acme", "goal": "leave_helpdesk", "role": "support_lead", "source": "reddit",
	})
	wantResponse(t, created, http.StatusCreated, "")
	me := f.meAfter(t, created)
	if me.OnboardingGoal == nil || *me.OnboardingGoal != "leave_helpdesk" {
		t.Fatalf("onboarding_goal=%v, want leave_helpdesk", me.OnboardingGoal)
	}
	got := queryString(t, f.pool, `SELECT goal||'/'||role||'/'||source FROM workspace_onboarding WHERE workspace_id=$1`, me.Workspace.ID)
	if got != "leave_helpdesk/support_lead/reddit" {
		t.Fatalf("stored answers=%q, want leave_helpdesk/support_lead/reddit", got)
	}
}

func TestSignupWithoutOnboardingAnswersStoresNulls(t *testing.T) {
	f := newSignupFixture(t, nil)
	created := f.signUpWith(t, "old-client@onboarding.example.com", defaultPeer, map[string]any{"workspace_name": "Old Client"})
	wantResponse(t, created, http.StatusCreated, "")
	me := f.meAfter(t, created)
	if me.OnboardingGoal != nil {
		t.Fatalf("onboarding_goal=%q, want null", *me.OnboardingGoal)
	}
	got := queryString(t, f.pool, `SELECT (goal IS NULL AND role IS NULL AND source IS NULL)::text FROM workspace_onboarding WHERE workspace_id=$1`, me.Workspace.ID)
	if got != "true" {
		t.Fatalf("answers all null=%s, want true", got)
	}
}

func TestSignupRefusesUnknownOnboardingAnswersAndCreatesNothing(t *testing.T) {
	cases := map[string]map[string]any{
		"goal":           {"goal": "world_domination"},
		"role":           {"role": "ceo"},
		"source":         {"source": "tv"},
		"empty goal":     {"goal": ""},
		"goal not text":  {"goal": 3},
		"source in case": {"source": "Reddit"},
	}
	for name, answers := range cases {
		t.Run(name, func(t *testing.T) {
			f := newSignupFixture(t, nil)
			body := map[string]any{"workspace_name": "Bad Answers"}
			for key, value := range answers {
				body[key] = value
			}
			wantResponse(t, f.signUpWith(t, "bad@onboarding.example.com", defaultPeer, body), http.StatusBadRequest, "invalid_request")
			if got := f.count(t, "workspaces"); got != "0" {
				t.Fatalf("workspaces=%s after a refused signup, want 0", got)
			}
			if got := f.count(t, "cloud_signups"); got != "0" {
				t.Fatalf("cloud_signups=%s after a refused signup, want 0", got)
			}
		})
	}
}

func TestTrialStartedCarriesOnlyTheOnboardingAnswersGiven(t *testing.T) {
	funnel := &recordingFunnel{}
	f := newSignupFixture(t, func(config *httpapi.Config) { config.Funnel = funnel })
	wantResponse(t, f.signUpWith(t, "full@onboarding.example.com", defaultPeer, map[string]any{
		"workspace_name": "Funnel Full", "goal": "ai_agents", "role": "developer", "source": "github",
	}), http.StatusCreated, "")
	wantResponse(t, f.signUpWith(t, "partial@onboarding.example.com", "198.51.100.9:1", map[string]any{
		"workspace_name": "Funnel Partial", "source": "x",
	}), http.StatusCreated, "")
	events := funnel.named("trial_started")
	if len(events) != 2 {
		t.Fatalf("events=%+v, want two trial_started", events)
	}
	full, partial := events[0].Properties, events[1].Properties
	if len(full) != 3 || full["goal"] != "ai_agents" || full["role"] != "developer" || full["source"] != "github" {
		t.Fatalf("first properties=%v, want goal ai_agents, role developer, source github", full)
	}
	if len(partial) != 1 || partial["source"] != "x" {
		t.Fatalf("second properties=%v, want only source x", partial)
	}
}

func TestOnboardingAnswersStayInTheirWorkspace(t *testing.T) {
	f := newSignupFixture(t, nil)
	createdA := f.signUpWith(t, "a@onboarding-a.example.com", defaultPeer, map[string]any{"workspace_name": "Workspace A", "goal": "lower_cost", "role": "founder", "source": "email"})
	wantResponse(t, createdA, http.StatusCreated, "")
	createdB := f.signUpWith(t, "b@onboarding-b.example.com", "198.51.100.10:1", map[string]any{"workspace_name": "Workspace B"})
	wantResponse(t, createdB, http.StatusCreated, "")
	a, b := f.meAfter(t, createdA), f.meAfter(t, createdB)
	if b.OnboardingGoal != nil {
		t.Fatalf("workspace B sees onboarding_goal=%q, want its own null", *b.OnboardingGoal)
	}
	if a.OnboardingGoal == nil || *a.OnboardingGoal != "lower_cost" {
		t.Fatalf("workspace A onboarding_goal=%v, want lower_cost", a.OnboardingGoal)
	}

	// The database refuses it too: as the app role scoped to B, no row of A is visible or writable.
	ctx := context.Background()
	var role string
	if err := f.pool.QueryRow(ctx, `SELECT roles[1]::text FROM pg_policies WHERE schemaname=current_schema() AND tablename='workspace_onboarding' AND policyname='workspace_isolation'`).Scan(&role); err != nil {
		t.Fatalf("workspace_onboarding has no workspace_isolation policy: %v", err)
	}
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, `SELECT set_config('role',$1,true), set_config('app.workspace_id',$2,true)`, role, b.Workspace.ID); err != nil {
		t.Fatal(err)
	}
	var visible int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM workspace_onboarding WHERE workspace_id=$1`, a.Workspace.ID).Scan(&visible); err != nil {
		t.Fatal(err)
	}
	if visible != 0 {
		t.Fatalf("scoped to B, %d onboarding rows of A are visible, want 0", visible)
	}
	tag, err := tx.Exec(ctx, `UPDATE workspace_onboarding SET goal='other' WHERE workspace_id=$1`, a.Workspace.ID)
	if err != nil {
		t.Fatal(err)
	}
	if tag.RowsAffected() != 0 {
		t.Fatalf("scoped to B, an update changed %d rows of A, want 0", tag.RowsAffected())
	}
}

func TestSignupStoresAIAssistantSourceAndTracksTrialStarted(t *testing.T) {
	funnel := &recordingFunnel{}
	f := newSignupFixture(t, func(config *httpapi.Config) { config.Funnel = funnel })
	created := f.signUpWith(t, "claude@onboarding.example.com", defaultPeer, map[string]any{
		"workspace_name": "Assistant Signup", "source": "ai_claude",
	})
	wantResponse(t, created, http.StatusCreated, "")
	me := f.meAfter(t, created)
	if got := queryString(t, f.pool, `SELECT source FROM workspace_onboarding WHERE workspace_id=$1`, me.Workspace.ID); got != "ai_claude" {
		t.Fatalf("stored source=%q, want ai_claude", got)
	}
	events := funnel.named("trial_started")
	if len(events) != 1 || events[0].Properties["source"] != "ai_claude" {
		t.Fatalf("trial_started=%+v, want one event with source ai_claude", events)
	}
	wantResponse(t, f.signUpWith(t, "unknown@onboarding.example.com", "198.51.100.12:1", map[string]any{
		"workspace_name": "Unknown Assistant", "source": "ai_unknown",
	}), http.StatusBadRequest, "invalid_request")
	if got := f.count(t, "workspaces"); got != "1" {
		t.Fatalf("workspaces=%s, want only the accepted signup", got)
	}
}
