package httpapi_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

type adminTierMe struct {
	Agent struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	} `json:"agent"`
	Scope *string `json:"scope"`
}

func meOf(t *testing.T, api http.Handler, token string) adminTierMe {
	t.Helper()
	var me adminTierMe
	decode(t, request(t, api, http.MethodGet, "/api/v1/auth/me", nil, token, nil), &me)
	return me
}

// The admin tier acts with its creator's role, and only while the creator still has it.
func TestAdminTokenComesFromAnAdminAndNeverOutranksItsCreator(t *testing.T) {
	desk := newRequestDesk(t, "Admin Tier")
	api, admin := desk.api, desk.admin
	agentSession := joinAsAgent(t, api, admin, "agent@admin-tier.example.com")

	refused := request(t, api, http.MethodPost, "/api/v1/auth/tokens", map[string]any{"label": "too much", "scope": "admin"}, agentSession, nil)
	if refused.Code != http.StatusForbidden || !strings.Contains(refused.Body.String(), "admin_token_needs_admin") {
		t.Fatalf("an agent creating an admin token: status=%d body=%s, want 403 admin_token_needs_admin", refused.Code, refused.Body.String())
	}

	token := issueToken(t, api, admin, "Ops agent", "admin").Token
	if me := meOf(t, api, token); me.Agent.Role != "admin" || me.Scope == nil || *me.Scope != "admin" {
		t.Fatalf("admin token acts as %+v, want the admin role with the admin scope", me)
	}
	if response := request(t, api, http.MethodPatch, "/api/v1/workspace/timezone", map[string]any{"timezone": "Asia/Tokyo"}, token, nil); response.Code != http.StatusOK {
		t.Fatalf("admin token changes the workspace timezone: status=%d body=%s", response.Code, response.Body.String())
	}

	// A second admin demotes the creator: the token keeps working, with the agent role only.
	_ = joinAsAgent(t, api, admin, "second@admin-tier.example.com")
	secondID := memberIDByEmail(t, api, admin, "second@admin-tier.example.com")
	if response := request(t, api, http.MethodPatch, "/api/v1/agents/"+secondID, map[string]any{"role": "admin"}, admin, nil); response.Code != http.StatusOK {
		t.Fatalf("promote second: status=%d body=%s", response.Code, response.Body.String())
	}
	pairing, err := desk.service.Pair(context.Background(), "admin-tier", "second@admin-tier.example.com")
	if err != nil {
		t.Fatal(err)
	}
	second := redeemLabelled(t, api, pairing, "Second admin")
	creatorID := memberIDByEmail(t, api, admin, "admin@admin-tier.example.com")
	if response := request(t, api, http.MethodPatch, "/api/v1/agents/"+creatorID, map[string]any{"role": "agent"}, second, nil); response.Code != http.StatusOK {
		t.Fatalf("demote creator: status=%d body=%s", response.Code, response.Body.String())
	}
	if me := meOf(t, api, token); me.Agent.Role != "agent" {
		t.Fatalf("token of a demoted creator acts as %q, want agent", me.Agent.Role)
	}
	if response := request(t, api, http.MethodPatch, "/api/v1/workspace/timezone", map[string]any{"timezone": "UTC"}, token, nil); response.Code != http.StatusForbidden {
		t.Fatalf("token of a demoted creator changes the workspace timezone: status=%d body=%s, want 403", response.Code, response.Body.String())
	}

	if response := request(t, api, http.MethodPatch, "/api/v1/agents/"+creatorID, map[string]any{"active": false}, second, nil); response.Code != http.StatusOK {
		t.Fatalf("deactivate creator: status=%d body=%s", response.Code, response.Body.String())
	}
	if response := request(t, api, http.MethodGet, "/api/v1/auth/me", nil, token, nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("token of a deactivated creator: status=%d, want 401", response.Code)
	}
}

// Granting the admin role is approval-required for every tier, the admin tier included.
func TestGrantingTheAdminRoleIsARequestEvenForTheAdminTier(t *testing.T) {
	desk := newRequestDesk(t, "Admin Grant")
	api, admin := desk.api, desk.admin
	_ = joinAsAgent(t, api, admin, "agent@admin-grant.example.com")
	agentID := memberIDByEmail(t, api, admin, "agent@admin-grant.example.com")
	token := issueToken(t, api, admin, "Ops agent", "admin").Token

	invite := accepted(t, api, http.MethodPost, "/api/v1/auth/invites", map[string]any{"email": "boss@admin-grant.example.com", "role_ceiling": "admin"}, token)
	if !strings.Contains(invite.Summary, "boss@admin-grant.example.com") || !strings.Contains(invite.Summary, "admin") {
		t.Errorf("admin invite summary = %q, want the address and the role", invite.Summary)
	}
	promote := accepted(t, api, http.MethodPatch, "/api/v1/agents/"+agentID, map[string]any{"role": "admin"}, token)
	if !strings.Contains(promote.Summary, "agent@admin-grant.example.com") || !strings.Contains(promote.Summary, "agent → admin") {
		t.Errorf("promotion summary = %q, want who and agent → admin", promote.Summary)
	}
	for _, member := range []string{"agent@admin-grant.example.com"} {
		var body struct {
			Items []teamMemberView `json:"items"`
		}
		decode(t, request(t, api, http.MethodGet, "/api/v1/agents", nil, admin, nil), &body)
		for _, item := range body.Items {
			if item.Email == member && item.Role != "agent" {
				t.Fatalf("%s became %s before anyone approved", member, item.Role)
			}
		}
	}
	// A non-admin invite and a demotion run directly for the admin tier.
	created := request(t, api, http.MethodPost, "/api/v1/auth/invites", map[string]any{"email": "helper@admin-grant.example.com", "role_ceiling": "agent"}, token, nil)
	if created.Code != http.StatusCreated {
		t.Fatalf("admin token creating an agent invite: status=%d body=%s", created.Code, created.Body.String())
	}
}

// An invite link and the inbound secret are credentials: no token ever receives them, directly, in a
// list, or through an approved request.
func TestNoInviteLinkOrSecretReachesAToken(t *testing.T) {
	desk := newRequestDesk(t, "Secret Keeper")
	api, admin := desk.api, desk.admin
	secret := connectEmail(t, api, admin, "support@secret-keeper.example.com")
	_, adminLink := createContractInvite(t, api, admin, "listed@secret-keeper.example.com", "agent")
	adminToken := issueToken(t, api, admin, "Ops agent", "admin").Token
	send := issueToken(t, api, admin, "Sender", "send").Token
	read := issueToken(t, api, admin, "Reader", "read").Token

	direct := request(t, api, http.MethodPost, "/api/v1/auth/invites", map[string]any{"email": "direct@secret-keeper.example.com", "role_ceiling": "agent"}, adminToken, nil)
	if direct.Code != http.StatusCreated {
		t.Fatalf("admin token invite: status=%d body=%s", direct.Code, direct.Body.String())
	}
	asked := accepted(t, api, http.MethodPost, "/api/v1/auth/invites", map[string]any{"email": "asked@secret-keeper.example.com", "role_ceiling": "agent"}, send)
	approved := approve(t, api, admin, asked.RequestID, nil)
	if approved.Code != http.StatusOK || !strings.Contains(approved.Body.String(), "token=") {
		t.Fatalf("the approver sees the invite link once: status=%d body=%s", approved.Code, approved.Body.String())
	}
	answers := map[string]string{
		"admin token POST invites":     direct.Body.String(),
		"request as the token sees it": request(t, api, http.MethodGet, "/api/v1/agent-requests/"+asked.RequestID, nil, send, nil).Body.String(),
	}
	for _, token := range []string{read, adminToken} {
		for _, path := range []string{"/api/v1/auth/invites", "/api/v1/auth/sessions", "/api/v1/channels/email", "/api/v1/channels/web-form"} {
			response := request(t, api, http.MethodGet, path, nil, token, nil)
			if response.Code != http.StatusOK {
				t.Fatalf("GET %s with a token: status=%d body=%s", path, response.Code, response.Body.String())
			}
			answers["GET "+path] = response.Body.String()
		}
	}
	for label, body := range answers {
		for _, leak := range []string{secret, linkToken(t, adminLink), `"url"`, "cdk_agent_"} {
			if strings.Contains(body, leak) {
				t.Errorf("%s leaks %q: %s", label, leak, body)
			}
		}
	}
	stored := getRequest(t, api, admin, asked.RequestID)
	if strings.Contains(string(stored.Result), "token=") || stored.ResultNote == nil {
		t.Errorf("the stored result of an approved invite keeps the link: result=%s note=%v", stored.Result, stored.ResultNote)
	}
}

// What a person reads before approving a settings change says what changes, from what to what.
func TestSettingsRequestsSayWhatChangesFromWhatToWhat(t *testing.T) {
	desk := newRequestDesk(t, "Settings Summary")
	api, admin := desk.api, desk.admin
	send := issueToken(t, api, admin, "Sender", "send").Token
	_ = joinAsAgent(t, api, admin, "agent@settings-summary.example.com")
	agentID := memberIDByEmail(t, api, admin, "agent@settings-summary.example.com")
	inviteID, _ := createContractInvite(t, api, admin, "pending@settings-summary.example.com", "agent")
	connectEmail(t, api, admin, "old@settings-summary.example.com")

	cases := []struct {
		method, path string
		body         any
		want         []string
	}{
		{http.MethodPatch, "/api/v1/workspace/timezone", map[string]any{"timezone": "Asia/Tokyo"}, []string{"workspace timezone", "UTC → Asia/Tokyo"}},
		{http.MethodPatch, "/api/v1/agents/" + agentID, map[string]any{"active": false}, []string{"agent@settings-summary.example.com", "deactivate"}},
		{http.MethodPost, "/api/v1/auth/invites", map[string]any{"email": "new@settings-summary.example.com", "role_ceiling": "agent"}, []string{"new@settings-summary.example.com", "agent"}},
		{http.MethodDelete, "/api/v1/auth/invites/" + inviteID, nil, []string{"pending@settings-summary.example.com"}},
		{http.MethodPut, "/api/v1/channels/web-form", map[string]any{"enabled": true, "allowed_origins": []string{"https://shop.example.com"}}, []string{"off → on", "https://shop.example.com"}},
		{http.MethodPut, "/api/v1/channels/email", map[string]any{"support_address": "new@settings-summary.example.com"}, []string{"old@settings-summary.example.com → new@settings-summary.example.com"}},
	}
	for _, item := range cases {
		filed := accepted(t, api, item.method, item.path, item.body, send)
		for _, want := range item.want {
			if !strings.Contains(filed.Summary, want) {
				t.Errorf("%s %s summary = %q, want it to contain %q", item.method, item.path, filed.Summary, want)
			}
		}
	}
	// A call with a field the route does not take is refused before anything is stored.
	extra := request(t, api, http.MethodPut, "/api/v1/channels/email", map[string]any{"support_address": "x@settings-summary.example.com", "smtp_password": "hunter2"}, send, nil)
	if extra.Code != http.StatusBadRequest {
		t.Fatalf("a settings request with an unknown field: status=%d body=%s, want 400", extra.Code, extra.Body.String())
	}
	for _, item := range listRequests(t, api, admin, "") {
		if strings.Contains(string(item.Body), "hunter2") {
			t.Fatalf("a stored request keeps a field the route does not take: %s", item.Body)
		}
	}
}

// An admin token reaches only its own workspace's team, invites and sessions.
func TestAdminTokenCannotReachAnotherWorkspace(t *testing.T) {
	beta := newRequestDesk(t, "Admin Beta")
	api := beta.api
	_ = joinAsAgent(t, api, beta.admin, "agent@admin-beta.example.com")
	betaAgent := memberIDByEmail(t, api, beta.admin, "agent@admin-beta.example.com")
	betaInvite, _ := createContractInvite(t, api, beta.admin, "invitee@admin-beta.example.com", "agent")
	betaSession := issueToken(t, api, beta.admin, "Beta helper", "read")
	gamma := createSession(t, api, beta.service, "Admin Gamma", "admin@admin-gamma.example.com")
	token := issueToken(t, api, gamma, "Gamma ops", "admin").Token

	for _, probe := range []struct {
		method, path string
		body         any
	}{
		{http.MethodPatch, "/api/v1/agents/" + betaAgent, map[string]any{"active": false}},
		{http.MethodDelete, "/api/v1/auth/invites/" + betaInvite, nil},
		{http.MethodDelete, "/api/v1/auth/sessions/" + betaSession.Session.ID, nil},
	} {
		if response := request(t, api, probe.method, probe.path, probe.body, token, nil); response.Code != http.StatusNotFound {
			t.Errorf("admin token %s %s of another workspace: status=%d body=%s, want 404", probe.method, probe.path, response.Code, response.Body.String())
		}
	}
	for _, path := range []string{"/api/v1/auth/invites", "/api/v1/auth/sessions", "/api/v1/agents", "/api/v1/channels/email", "/api/v1/channels/web-form"} {
		body := request(t, api, http.MethodGet, path, nil, token, nil).Body.String()
		if strings.Contains(body, "admin-beta") {
			t.Errorf("admin token GET %s shows another workspace: %s", path, body)
		}
	}
	if response := request(t, api, http.MethodGet, "/api/v1/auth/me", nil, betaSession.Token, nil); response.Code != http.StatusOK {
		t.Fatalf("the other workspace's token was revoked: status=%d", response.Code)
	}
}

// An agent asking for an admin token waits for an admin: only an admin can create one.
func TestAnAdminTokenRequestWaitsForAnAdmin(t *testing.T) {
	desk := newRequestDesk(t, "Admin Ask")
	send := issueToken(t, desk.api, desk.admin, "Sender", "send").Token
	asked := accepted(t, desk.api, http.MethodPost, "/api/v1/auth/tokens", map[string]any{"label": "Ops", "scope": "admin"}, send)
	plain := accepted(t, desk.api, http.MethodPost, "/api/v1/auth/tokens", map[string]any{"label": "Helper", "scope": "read"}, send)
	if got := getRequest(t, desk.api, desk.admin, asked.RequestID).ApprovableBy; got != "admin" {
		t.Errorf("an admin token request is approvable by %q, want admin", got)
	}
	if got := getRequest(t, desk.api, desk.admin, plain.RequestID).ApprovableBy; got != "agent" {
		t.Errorf("a read token request is approvable by %q, want agent", got)
	}
}
