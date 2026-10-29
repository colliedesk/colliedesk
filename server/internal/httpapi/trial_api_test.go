package httpapi_test

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

// receiveAndReport delivers a mail to workspace A and returns the conversation it created.
func (f limitsFixture) conversationA(t *testing.T) string {
	t.Helper()
	result := f.receive(t, false, 1, "Trial question")
	if result.ConversationID == nil {
		t.Fatalf("no conversation: %+v", result)
	}
	return *result.ConversationID
}

func (f limitsFixture) postNote(t *testing.T, token, conversation string) *responseView {
	t.Helper()
	response := request(t, f.api, http.MethodPost, "/api/v1/conversations/"+conversation+"/messages",
		map[string]any{"kind": "note", "body": "A note"}, token, nil)
	return &responseView{Code: response.Code, Body: response.Body.String()}
}

func (f limitsFixture) messageCount(t *testing.T, workspace string) string {
	t.Helper()
	return queryString(t, f.pool, `SELECT count(*)::text FROM messages WHERE workspace_id=$1`, workspace)
}

func wantLocked(t *testing.T, got *responseView, what string) {
	t.Helper()
	if got.Code != http.StatusPaymentRequired || !strings.Contains(got.Body, `"workspace_locked"`) {
		t.Fatalf("%s: status=%d body=%s, want 402 workspace_locked", what, got.Code, got.Body)
	}
}

func TestNewWorkspaceStartsAFourteenDayTrial(t *testing.T) {
	f := newLimitsFixture(t, true)
	view := f.usage(t, f.adminA)
	if view.Plan != "trial" || view.TrialEndsAt != "2026-10-15T09:00:00Z" || view.Locked || view.Limits.Agents != 10 {
		t.Fatalf("billing=%+v, want trial ending 2026-10-15T09:00:00Z, unlocked, 10 agents", view)
	}
	var me struct {
		Plan struct {
			Plan        string `json:"plan"`
			TrialEndsAt string `json:"trial_ends_at"`
			Locked      bool   `json:"locked"`
		} `json:"plan"`
	}
	decode(t, request(t, f.api, http.MethodGet, "/api/v1/auth/me", nil, f.adminA, nil), &me)
	if me.Plan.Plan != "trial" || me.Plan.TrialEndsAt != "2026-10-15T09:00:00Z" || me.Plan.Locked {
		t.Fatalf("me.plan=%+v, want the trial state", me.Plan)
	}
}

func TestWorkspaceLocksExactlyWhenTheTrialEnds(t *testing.T) {
	f := newLimitsFixture(t, true)
	conversation := f.conversationA(t)
	f.clock.set(time.Date(2026, time.October, 15, 8, 59, 59, 0, time.UTC))
	if got := f.postNote(t, f.adminA, conversation); got.Code != http.StatusCreated {
		t.Fatalf("one second before the end: status=%d body=%s, want 201", got.Code, got.Body)
	}
	before := f.messageCount(t, f.workspaceA)
	f.clock.set(trialEnd)
	wantLocked(t, f.postNote(t, f.adminA, conversation), "write at the trial end")
	if after := f.messageCount(t, f.workspaceA); after != before {
		t.Fatalf("messages %s -> %s: a refused write must change nothing", before, after)
	}
}

func TestLockedWorkspaceRefusesReadsToo(t *testing.T) {
	f := newLimitsFixture(t, true)
	conversation := f.conversationA(t)
	f.clock.set(trialEnd)
	for _, path := range []string{"/api/v1/conversations", "/api/v1/conversations/" + conversation, "/api/v1/contacts", "/api/v1/tags", "/api/v1/agents"} {
		response := request(t, f.api, http.MethodGet, path, nil, f.adminA, nil)
		wantLocked(t, &responseView{Code: response.Code, Body: response.Body.String()}, "GET "+path)
	}
}

func TestLockedAllowlistIsExactlyTheOpenRoutes(t *testing.T) {
	want := []string{
		"DELETE /api/v1/auth/sessions/{id}",
		"DELETE /api/v1/channels/email",
		"DELETE /api/v1/channels/email/gmail",
		"DELETE /api/v1/channels/email/microsoft",
		"DELETE /api/v1/channels/{id}/email/gmail",
		"DELETE /api/v1/channels/{id}/email/microsoft",
		"GET /api/v1/auth/me",
		"GET /api/v1/auth/sessions",
		"GET /api/v1/billing",
		"GET /api/v1/channels/email",
		"GET /api/v1/channels/{id}/email",
		"GET /api/v1/export",
		"POST /api/v1/auth/logout",
		"POST /api/v1/billing/checkout",
		"POST /api/v1/billing/portal",
		"POST /api/v1/channels/email/secret",
		"POST /api/v1/channels/{id}/email/secret",
		"POST /api/v1/export/link",
		"POST /api/v1/support/problem-reports",
	}
	got := httpapi.LockedAllowlist()
	sort.Strings(got)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("allowlist:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// A route added later is locked until someone puts it on the allowlist on purpose.
func TestEveryRouteOutsideTheAllowlistIsLockedAfterTheTrial(t *testing.T) {
	f := newLimitsFixture(t, true)
	f.clock.set(trialEnd)
	allowed := map[string]bool{}
	for _, entry := range httpapi.LockedAllowlist() {
		allowed[entry] = true
	}
	const someID = "00000000-0000-0000-0000-000000000001"
	gated := 0
	for _, route := range httpapi.Routes() {
		// The operator console belongs to no workspace: a workspace's lock never reaches it, and its
		// routes refuse a workspace session outright (TestOperatorRealmIsSeparate).
		if route.Permission == auth.PermissionPublic || route.Permission == auth.PermissionChannelInbound || route.Permission == auth.PermissionOperator {
			continue
		}
		key := route.Method + " " + route.Pattern
		if allowed[key] && route.Method != http.MethodGet {
			// Signing out or revoking a session would end the session this test uses; the
			// dedicated tests below exercise those routes.
			continue
		}
		path := strings.NewReplacer("{id}", someID, "{tagId}", someID, "{messageId}", someID).Replace(route.Pattern)
		response := request(t, f.api, route.Method, path, map[string]any{}, f.adminA, nil)
		locked := response.Code == http.StatusPaymentRequired && strings.Contains(response.Body.String(), `"workspace_locked"`)
		if allowed[key] && locked {
			t.Errorf("%s is on the allowlist but answered workspace_locked", key)
		}
		if !allowed[key] {
			gated++
			if !locked {
				t.Errorf("%s is not on the allowlist but answered %d %s", key, response.Code, response.Body.String())
			}
		}
	}
	if gated < 20 {
		t.Fatalf("only %d routes were checked; the enumeration is broken", gated)
	}
}

func TestLockedWorkspaceCanStillSignOutSeeBillingAndCheckout(t *testing.T) {
	f := newLimitsFixture(t, true)
	f.clock.set(trialEnd)
	if got := request(t, f.api, http.MethodGet, "/api/v1/auth/me", nil, f.adminA, nil).Code; got != http.StatusOK {
		t.Fatalf("me status=%d, want 200", got)
	}
	view := f.usage(t, f.adminA)
	if view.Plan != "none" || !view.Locked || view.TrialEndsAt != "2026-10-15T09:00:00Z" {
		t.Fatalf("billing=%+v, want plan none, locked", view)
	}
	checkout := request(t, f.api, http.MethodPost, "/api/v1/billing/checkout", map[string]any{"plan": "team_monthly"}, f.adminA, nil)
	if checkout.Code != http.StatusOK {
		t.Fatalf("checkout status=%d body=%s, want 200", checkout.Code, checkout.Body.String())
	}
	var sessions struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	decode(t, request(t, f.api, http.MethodGet, "/api/v1/auth/sessions", nil, f.adminA, nil), &sessions)
	if len(sessions.Items) != 1 {
		t.Fatalf("sessions=%d, want 1", len(sessions.Items))
	}
	if got := request(t, f.api, http.MethodPost, "/api/v1/auth/logout", nil, f.adminA, nil).Code; got != http.StatusNoContent {
		t.Fatalf("logout status=%d, want 204", got)
	}
}

func TestPayingUnlocksTheWorkspaceOnTheNextRequest(t *testing.T) {
	f := newLimitsFixture(t, true)
	conversation := f.conversationA(t)
	f.clock.set(trialEnd)
	wantLocked(t, f.postNote(t, f.adminA, conversation), "before paying")
	f.upgrade(t, f.workspaceA, "sub_unlock")
	if got := f.postNote(t, f.adminA, conversation); got.Code != http.StatusCreated {
		t.Fatalf("after paying: status=%d body=%s, want 201", got.Code, got.Body)
	}
	if view := f.usage(t, f.adminA); view.Plan != "team" || view.Locked {
		t.Fatalf("billing=%+v, want team, unlocked", view)
	}
}

func TestRedeemingAnInviteWhileLockedIsRefusedAndTheLinkSurvives(t *testing.T) {
	f := newLimitsFixture(t, true)
	_, inviteURL := createContractInvite(t, f.api, f.adminA, "late@limits-alpha.example.com", "agent")
	pairing, err := f.billing.service.Pair(context.Background(), "limits-alpha", "admin@limits-alpha.example.com")
	if err != nil {
		t.Fatal(err)
	}
	// End the trial now, inside the 15 minute life of both links.
	if _, err := f.pool.Exec(context.Background(), `UPDATE workspaces SET trial_ends_at=$2 WHERE id=$1`, f.workspaceA, f.clock.Now()); err != nil {
		t.Fatal(err)
	}
	refused := request(t, f.api, http.MethodPost, "/api/v1/auth/redeem", map[string]any{"token": linkToken(t, inviteURL)}, "", nil)
	wantLocked(t, &responseView{Code: refused.Code, Body: refused.Body.String()}, "new agent while locked")
	if unused := queryString(t, f.pool, `SELECT count(*)::text FROM pairing_links WHERE email='late@limits-alpha.example.com' AND used_at IS NULL`); unused != "1" {
		t.Fatalf("unused invite links=%s, want 1", unused)
	}
	// An existing agent can still sign in to reach the locked screen.
	if got := request(t, f.api, http.MethodPost, "/api/v1/auth/redeem", map[string]any{"token": linkToken(t, pairing)}, "", nil).Code; got != http.StatusOK {
		t.Fatalf("existing agent pairing status=%d, want 200", got)
	}
	f.upgrade(t, f.workspaceA, "sub_redeem_unlock")
	if got := request(t, f.api, http.MethodPost, "/api/v1/auth/redeem", map[string]any{"token": linkToken(t, inviteURL)}, "", nil).Code; got != http.StatusOK {
		t.Fatalf("redeem after paying status=%d, want 200", got)
	}
}

func TestLockedWorkspaceBouncesInboundMailAndOthersKeepReceiving(t *testing.T) {
	f := newLimitsFixture(t, true)
	// Workspace B pays; A does not.
	f.upgrade(t, f.workspaceB, "sub_bravo")
	f.clock.set(trialEnd)
	before := f.messageCount(t, f.workspaceA)
	response := inbound(t, f.api, f.secretA, mail([]string{
		"From: customer9@customer.example", "To: support@limits-alpha.example.com",
		"Subject: Too late", "Message-ID: <locked-1@customer.example>",
	}, "Hello?"))
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), `"mailbox_closed"`) {
		t.Fatalf("inbound to a locked workspace: status=%d body=%s, want 403 mailbox_closed", response.Code, response.Body.String())
	}
	if after := f.messageCount(t, f.workspaceA); after != before {
		t.Fatalf("messages %s -> %s: a bounced mail must store nothing", before, after)
	}
	if got := f.receive(t, true, 2, "Paid workspace still receives"); got.Status != "created" {
		t.Fatalf("workspace B inbound=%+v, want created", got)
	}
}

func TestOneWorkspaceLockingNeverAffectsAnother(t *testing.T) {
	f := newLimitsFixture(t, true)
	conversationB := f.receive(t, true, 3, "Bravo question")
	if _, err := f.pool.Exec(context.Background(), `UPDATE workspaces SET trial_ends_at=$2 WHERE id=$1`, f.workspaceB, trialEnd.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	f.clock.set(trialEnd)
	wantLocked(t, f.postNote(t, f.adminA, "00000000-0000-0000-0000-000000000001"), "workspace A")
	note := request(t, f.api, http.MethodPost, "/api/v1/conversations/"+*conversationB.ConversationID+"/messages",
		map[string]any{"kind": "note", "body": "B is fine"}, f.adminB, nil)
	if note.Code != http.StatusCreated {
		t.Fatalf("workspace B write status=%d body=%s, want 201", note.Code, note.Body.String())
	}
	if view := f.usage(t, f.adminB); view.Plan != "trial" || view.Locked {
		t.Fatalf("workspace B billing=%+v, want trial", view)
	}
}

// The banner says a trial was extended only when the server recorded an extension, whatever the
// length of the trial or the clock of the browser.
func TestMePlanSaysWhetherTheTrialWasExtended(t *testing.T) {
	f := newLimitsFixture(t, true)
	extended := func() bool {
		var me struct {
			Plan struct {
				TrialExtended *bool `json:"trial_extended"`
			} `json:"plan"`
		}
		decode(t, request(t, f.api, http.MethodGet, "/api/v1/auth/me", nil, f.adminA, nil), &me)
		if me.Plan.TrialExtended == nil {
			t.Fatal("me.plan has no trial_extended")
		}
		return *me.Plan.TrialExtended
	}
	if extended() {
		t.Fatal("a fresh trial is reported as extended")
	}
	if _, err := f.pool.Exec(context.Background(), `UPDATE workspaces SET trial_extended_at=now() WHERE id=$1`, f.workspaceA); err != nil {
		t.Fatal(err)
	}
	if !extended() {
		t.Fatal("a trial with trial_extended_at set is not reported as extended")
	}
}

// The reply cap is the server's number: the billing page shows what the API says, not a copy of it.
func TestBillingReportsTheTrialReplyCap(t *testing.T) {
	f := newLimitsFixture(t, true)
	replyCap := func() *int {
		var view struct {
			Limits struct {
				RepliesPer24h *int `json:"replies_per_24h"`
			} `json:"limits"`
		}
		decode(t, request(t, f.api, http.MethodGet, "/api/v1/billing", nil, f.adminA, nil), &view)
		return view.Limits.RepliesPer24h
	}
	if got := replyCap(); got == nil || *got != 200 {
		t.Fatalf("trial replies_per_24h=%v, want 200", got)
	}
	f.upgrade(t, f.workspaceA, "sub_cap_check")
	if got := replyCap(); got != nil {
		t.Fatalf("paid replies_per_24h=%d, want null", *got)
	}
}
