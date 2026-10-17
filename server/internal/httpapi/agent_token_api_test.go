package httpapi_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type issuedToken struct {
	Token   string `json:"token"`
	Session struct {
		ID         string  `json:"id"`
		AgentEmail string  `json:"agent_email"`
		Label      string  `json:"label"`
		Scope      *string `json:"scope"`
	} `json:"session"`
}

func issueToken(t *testing.T, api http.Handler, sessionToken, label, scope string) issuedToken {
	t.Helper()
	response := request(t, api, http.MethodPost, "/api/v1/auth/tokens", map[string]any{"label": label, "scope": scope}, sessionToken, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create %s token status=%d body=%s", scope, response.Code, response.Body.String())
	}
	var issued issuedToken
	decode(t, response, &issued)
	return issued
}

// scopeRefused reports whether response is the 403 scope_insufficient refusal and returns its message.
func scopeRefused(t *testing.T, response *httptest.ResponseRecorder) (bool, string) {
	t.Helper()
	if response.Code != http.StatusForbidden {
		return false, ""
	}
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	decode(t, response, &body)
	return body.Error.Code == "scope_insufficient", body.Error.Message
}

func TestAgentTokenIsShownOnceListedWithScopeAndRevocable(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "Token Desk", "admin@token.example.com")

	issued := issueToken(t, api, admin, "Claude Code", "draft")
	if issued.Token == "" || issued.Session.Label != "Claude Code" || issued.Session.AgentEmail != "admin@token.example.com" ||
		issued.Session.Scope == nil || *issued.Session.Scope != "draft" {
		t.Fatalf("unexpected token response: %+v", issued)
	}

	var stored []byte
	if err := pool.QueryRow(context.Background(), `SELECT token_hash FROM sessions WHERE id=$1`, issued.Session.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if want := sha256.Sum256([]byte("colliedesk-agent-token\x00" + issued.Token)); !bytes.Equal(stored, want[:]) {
		t.Fatalf("stored hash %x is not the domain-separated SHA-256 of the raw token", stored)
	}

	listed := request(t, api, http.MethodGet, "/api/v1/auth/sessions", nil, admin, nil)
	if strings.Contains(listed.Body.String(), issued.Token) {
		t.Fatal("session list leaked the raw token")
	}
	var list struct {
		Items []struct {
			ID         string  `json:"id"`
			Label      string  `json:"label"`
			Scope      *string `json:"scope"`
			LastSeenAt string  `json:"last_seen_at"`
			RevokedAt  *string `json:"revoked_at"`
		} `json:"items"`
	}
	decode(t, listed, &list)
	scopes := map[string]*string{}
	for _, item := range list.Items {
		scopes[item.Label] = item.Scope
		if item.ID == issued.Session.ID && (item.Label != "Claude Code" || item.LastSeenAt == "" || item.RevokedAt != nil) {
			t.Fatalf("listed token = %+v", item)
		}
	}
	if scope := scopes["Claude Code"]; scope == nil || *scope != "draft" {
		t.Fatalf("token scope in list = %v, want draft", scope)
	}
	if scope, ok := scopes["Browser session"]; !ok || scope != nil {
		t.Fatalf("a full session must list scope null: present=%v scope=%v", ok, scope)
	}

	me := request(t, api, http.MethodGet, "/api/v1/auth/me", nil, issued.Token, nil)
	if me.Code != http.StatusOK || !strings.Contains(me.Body.String(), "admin@token.example.com") {
		t.Fatalf("token did not authenticate: status=%d body=%s", me.Code, me.Body.String())
	}
	if revoked := request(t, api, http.MethodDelete, "/api/v1/auth/sessions/"+issued.Session.ID, nil, admin, nil); revoked.Code != http.StatusNoContent {
		t.Fatalf("revoke status=%d body=%s", revoked.Code, revoked.Body.String())
	}
	if after := request(t, api, http.MethodGet, "/api/v1/auth/me", nil, issued.Token, nil); after.Code != http.StatusUnauthorized {
		t.Fatalf("revoked token status=%d, want 401", after.Code)
	}
}

func TestAgentTokenRequestIsValidated(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Token Validation", "admin@validation.example.com")
	cases := map[string]map[string]any{
		"empty label":       {"label": "", "scope": "read"},
		"blank label":       {"label": "   ", "scope": "read"},
		"label too long":    {"label": strings.Repeat("a", 81), "scope": "read"},
		"missing scope":     {"label": "Agent"},
		"unknown scope":     {"label": "Agent", "scope": "owner"},
		"owner is no scope": {"label": "Agent", "scope": "owner"},
		"unknown field":     {"label": "Agent", "scope": "read", "role": "admin"},
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			response := request(t, api, http.MethodPost, "/api/v1/auth/tokens", body, admin, nil)
			if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"invalid_request"`) {
				t.Fatalf("status=%d body=%s, want 400 invalid_request", response.Code, response.Body.String())
			}
		})
	}
	scope := request(t, api, http.MethodPost, "/api/v1/auth/tokens", map[string]any{"label": "Agent", "scope": "owner"}, admin, nil)
	for _, tier := range []string{"read", "draft", "send"} {
		if !strings.Contains(scope.Body.String(), tier) {
			t.Fatalf("scope error does not list %q: %s", tier, scope.Body.String())
		}
	}
	var list struct {
		Items []struct{} `json:"items"`
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/auth/sessions", nil, admin, nil), &list)
	if len(list.Items) != 1 {
		t.Fatalf("rejected requests created sessions: %d listed, want only the sign-in", len(list.Items))
	}
}

func TestScopedTokenNeverAuthenticatesAsACookie(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Token Cookie", "admin@cookie.example.com")
	issued := issueToken(t, api, admin, "Cookie probe", "send")
	cookie := &http.Cookie{Name: "colliedesk_session", Value: issued.Token}

	if response := request(t, api, http.MethodGet, "/api/v1/auth/me", nil, "", cookie); response.Code != http.StatusUnauthorized {
		t.Fatalf("token as cookie (read) status=%d, want 401", response.Code)
	}
	if response := requestWithOrigin(t, api, http.MethodPost, "/api/v1/auth/logout", cookie, testOrigin); response.Code != http.StatusUnauthorized {
		t.Fatalf("token as cookie (write with a matching Origin) status=%d, want 401", response.Code)
	}
	if response := request(t, api, http.MethodGet, "/api/v1/auth/me", nil, issued.Token, nil); response.Code != http.StatusOK {
		t.Fatalf("the refused cookie must leave the bearer token working: status=%d", response.Code)
	}
}

func TestTokenOfAnAdminHasTheAgentRoleAndAnAgentGetsOnlyTheirOwn(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Token Roles", "admin@roles.example.com")

	adminToken := issueToken(t, api, admin, "Admin's agent", "send")
	var me struct {
		Agent struct {
			Email string `json:"email"`
			Role  string `json:"role"`
		} `json:"agent"`
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/auth/me", nil, adminToken.Token, nil), &me)
	if me.Agent.Role != "agent" || me.Agent.Email != "admin@roles.example.com" {
		t.Fatalf("token of an admin acts as %+v, want the agent role", me.Agent)
	}

	_, agentURL := createContractInvite(t, api, admin, "agent@roles.example.com", "agent")
	agentSession := redeemLabelled(t, api, agentURL, "Agent laptop")
	agentToken := issueToken(t, api, agentSession, "Agent's helper", "read")
	if agentToken.Session.AgentEmail != "agent@roles.example.com" {
		t.Fatalf("token belongs to %q, want the creator", agentToken.Session.AgentEmail)
	}

	if findSessionID(t, api, admin, "Agent's helper") != agentToken.Session.ID {
		t.Fatal("admin does not see the agent's token")
	}
	var own struct {
		Items []struct {
			Label string `json:"label"`
		} `json:"items"`
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/auth/sessions", nil, agentSession, nil), &own)
	for _, item := range own.Items {
		if item.Label == "Admin's agent" {
			t.Fatal("agent sees the admin's token")
		}
	}
	if revoked := request(t, api, http.MethodDelete, "/api/v1/auth/sessions/"+agentToken.Session.ID, nil, admin, nil); revoked.Code != http.StatusNoContent {
		t.Fatalf("admin revoke of an agent token status=%d", revoked.Code)
	}
	if after := request(t, api, http.MethodGet, "/api/v1/auth/me", nil, agentToken.Token, nil); after.Code != http.StatusUnauthorized {
		t.Fatalf("revoked agent token status=%d, want 401", after.Code)
	}
}
