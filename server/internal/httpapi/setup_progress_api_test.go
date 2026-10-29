package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

type setupStepView struct {
	Key    string  `json:"key"`
	State  string  `json:"state"`
	Detail *string `json:"detail"`
}

type setupView struct {
	Steps       []setupStepView `json:"steps"`
	Complete    bool            `json:"complete"`
	Recommended *string         `json:"recommended"`
}

func readSetup(t *testing.T, api http.Handler, token string) setupView {
	t.Helper()
	req := newRequest(t, http.MethodGet, "/api/v1/workspace/setup", nil, token, nil)
	response := serve(api, req)
	if response.Code != http.StatusOK {
		t.Fatalf("GET setup status=%d body=%s", response.Code, response.Body.String())
	}
	validateContractResponse(t, req, response)
	var view setupView
	decode(t, response, &view)
	return view
}

func (v setupView) state(t *testing.T, key string) string {
	t.Helper()
	for _, step := range v.Steps {
		if step.Key == key {
			return step.State
		}
	}
	t.Fatalf("setup has no step %q: %+v", key, v.Steps)
	return ""
}

func (v setupView) keys() []string {
	keys := make([]string, 0, len(v.Steps))
	for _, step := range v.Steps {
		keys = append(keys, step.Key)
	}
	return keys
}

func wantSetup(t *testing.T, view setupView, email, importState, invite, firstReply string) {
	t.Helper()
	got := []string{view.state(t, "email"), view.state(t, "import"), view.state(t, "invite"), view.state(t, "first_reply")}
	want := []string{email, importState, invite, firstReply}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("setup states (email, import, invite, first_reply) = %v, want %v", got, want)
		}
	}
}

func insertImport(t *testing.T, pool *pgxpool.Pool, slug, state string) {
	t.Helper()
	if _, err := pool.Exec(t.Context(), `INSERT INTO import_jobs(workspace_id,kind,source,domain,account_source,state,phase,options,status,started_at,finished_at)
		SELECT id,'import','freshdesk','acme.freshdesk.com','freshdesk:account:1',$2,'full','{}'::jsonb,CASE WHEN $2='finished' THEN 'OK' END,now(),CASE WHEN $2='finished' THEN now() END
		FROM workspaces WHERE slug=$1`, slug, state); err != nil {
		t.Fatal(err)
	}
}

func TestSetupProgressFollowsTheWorkspaceThroughEveryStep(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "Setup Desk", "admin@setup.example.com")
	slug := workspaceSlug(t, api, admin)

	fresh := readSetup(t, api, admin)
	wantSetup(t, fresh, "none", "none", "alone", "none")
	if fresh.Complete {
		t.Fatal("a fresh workspace must not be complete")
	}
	if got := fresh.keys(); len(got) != 4 || got[0] != "email" || got[3] != "first_reply" {
		t.Fatalf("default order = %v, want email, import, invite, first_reply", got)
	}

	secret := connectEmail(t, api, admin, "support@setup.example.com")
	wantSetup(t, readSetup(t, api, admin), "waiting", "none", "alone", "none")

	thread := inboundOK(t, api, secret, mail([]string{"From: s@customer.example", "Subject: Hello", "Message-ID: <setup-1@customer.example>"}, "Hi."))
	wantSetup(t, readSetup(t, api, admin), "working", "none", "alone", "none")

	insertImport(t, pool, slug, "running")
	wantSetup(t, readSetup(t, api, admin), "working", "running", "alone", "none")
	insertImport(t, pool, slug, "finished")
	wantSetup(t, readSetup(t, api, admin), "working", "done", "alone", "none")

	_, annURL := createContractInvite(t, api, admin, "ann@setup.example.com", "agent")
	wantSetup(t, readSetup(t, api, admin), "working", "done", "invited", "none")
	redeemLabelled(t, api, annURL, "Ann laptop")
	wantSetup(t, readSetup(t, api, admin), "working", "done", "joined", "none")

	// A note is not an answer, and a queued or failed reply is not delivered.
	create(t, api, admin, "/api/v1/conversations/"+*thread.ConversationID+"/messages", map[string]any{"kind": "note", "body": "Private."})
	wantSetup(t, readSetup(t, api, admin), "working", "done", "joined", "none")
	reply := create(t, api, admin, "/api/v1/conversations/"+*thread.ConversationID+"/messages", map[string]any{"kind": "outgoing", "body": "Answer."})
	wantSetup(t, readSetup(t, api, admin), "working", "done", "joined", "none")
	if _, err := pool.Exec(t.Context(), `UPDATE channel_outbox SET status='failed',last_error='boom' WHERE message_id=$1`, reply); err != nil {
		t.Fatal(err)
	}
	wantSetup(t, readSetup(t, api, admin), "working", "done", "joined", "none")
	if _, err := pool.Exec(t.Context(), `UPDATE channel_outbox SET status='sent',sent_at=now(),attempts=1 WHERE message_id=$1`, reply); err != nil {
		t.Fatal(err)
	}
	done := readSetup(t, api, admin)
	wantSetup(t, done, "working", "done", "joined", "sent")
	if !done.Complete {
		t.Fatalf("every step is done, setup = %+v", done)
	}
}

func TestSetupProgressIsOrderedByTheSignUpGoal(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "Goal Desk", "admin@goal.example.com")
	slug := workspaceSlug(t, api, admin)
	for goal, first := range map[string]string{"leave_helpdesk": "import", "shared_inbox": "email", "lower_cost": "invite", "ai_agents": "email", "other": "email"} {
		if _, err := pool.Exec(t.Context(), `INSERT INTO workspace_onboarding(workspace_id,goal) SELECT id,$2 FROM workspaces WHERE slug=$1
			ON CONFLICT (workspace_id) DO UPDATE SET goal=EXCLUDED.goal`, slug, goal); err != nil {
			t.Fatal(err)
		}
		view := readSetup(t, api, admin)
		if got := view.keys(); got[0] != first || got[len(got)-1] != "first_reply" || len(got) != 4 {
			t.Fatalf("goal %s: order = %v, want %s first and first_reply last", goal, got, first)
		}
		if goal == "ai_agents" || goal == "other" {
			if view.Recommended != nil {
				t.Fatalf("goal %s recommends %q, want nothing", goal, *view.Recommended)
			}
		} else if view.Recommended == nil || *view.Recommended != first {
			t.Fatalf("goal %s recommended = %v, want %s", goal, view.Recommended, first)
		}
	}
}

func TestSetupProgressNeverCountsAnotherWorkspace(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	mine := createSession(t, api, service, "Mine Desk", "admin@mine.example.com")
	theirs := createSession(t, api, service, "Theirs Desk", "admin@theirs.example.com")
	secret := connectEmail(t, api, theirs, "support@theirs.example.com")
	thread := inboundOK(t, api, secret, mail([]string{"From: s@customer.example", "Subject: Theirs", "Message-ID: <theirs-1@customer.example>"}, "Hi."))
	reply := create(t, api, theirs, "/api/v1/conversations/"+*thread.ConversationID+"/messages", map[string]any{"kind": "outgoing", "body": "Answer."})
	if _, err := pool.Exec(t.Context(), `UPDATE channel_outbox SET status='sent',sent_at=now(),attempts=1 WHERE message_id=$1`, reply); err != nil {
		t.Fatal(err)
	}
	insertImport(t, pool, workspaceSlug(t, api, theirs), "finished")
	_, url := createContractInvite(t, api, theirs, "ann@theirs.example.com", "agent")
	redeemLabelled(t, api, url, "Ann laptop")
	createContractInvite(t, api, theirs, "bob@theirs.example.com", "agent")

	wantSetup(t, readSetup(t, api, theirs), "working", "done", "joined", "sent")
	wantSetup(t, readSetup(t, api, mine), "none", "none", "alone", "none")
}

func TestSetupProgressIsReadableByEveryone(t *testing.T) {
	f := newTeamFixture(t)
	wantSetup(t, readSetup(t, f.api, f.ann), "none", "none", "joined", "none")
	if response := request(t, f.api, http.MethodGet, "/api/v1/workspace/setup", nil, "", nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("signed-out status=%d, want 401", response.Code)
	}
}

func TestSetupGuideHiddenFlagRoundTripsPerAgent(t *testing.T) {
	f := newTeamFixture(t)
	hidden := func(token string) bool {
		var me struct {
			Hidden bool `json:"setup_guide_hidden"`
		}
		decode(t, request(t, f.api, http.MethodGet, "/api/v1/auth/me", nil, token, nil), &me)
		return me.Hidden
	}
	if hidden(f.admin) || hidden(f.ann) {
		t.Fatal("the guide starts shown for everyone")
	}
	put := func(token string, value bool) {
		t.Helper()
		req := newRequest(t, http.MethodPut, "/api/v1/agents/me/setup-guide", map[string]any{"hidden": value}, token, nil)
		response := serve(f.api, req)
		if response.Code != http.StatusOK {
			t.Fatalf("PUT setup-guide status=%d body=%s", response.Code, response.Body.String())
		}
		validateContractResponse(t, req, response)
	}
	put(f.ann, true)
	if !hidden(f.ann) || hidden(f.admin) {
		t.Fatalf("hiding is per agent: ann=%v admin=%v, want true,false", hidden(f.ann), hidden(f.admin))
	}
	put(f.ann, true)
	put(f.ann, false)
	if hidden(f.ann) {
		t.Fatal("showing the guide again must clear the flag")
	}
	for _, body := range []any{map[string]any{}, map[string]any{"hidden": "yes"}, map[string]any{"hidden": nil}} {
		if response := request(t, f.api, http.MethodPut, "/api/v1/agents/me/setup-guide", body, f.ann, nil); response.Code != http.StatusBadRequest {
			t.Fatalf("PUT %v status=%d, want 400", body, response.Code)
		}
	}
}
