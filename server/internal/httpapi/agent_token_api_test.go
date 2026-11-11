package httpapi_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
)

type issuedToken struct {
	Token   string `json:"token"`
	Session struct {
		ID         string `json:"id"`
		AgentEmail string `json:"agent_email"`
		Label      string `json:"label"`
		Token      *struct {
			Kind        string   `json:"kind"`
			Permissions []string `json:"permissions"`
		} `json:"token"`
	} `json:"session"`
}

// issueToken makes an agent token of the person signed in with sessionToken. tier is a shorthand the
// tests kept from the tiers: read, draft and send give the old tier's list of permissions (a fixed
// token), admin follows the Admin role.
func issueToken(t *testing.T, api http.Handler, sessionToken, label, tier string) issuedToken {
	t.Helper()
	body := map[string]any{"label": label}
	if tier == "admin" {
		body["role_id"] = roleID(t, api, sessionToken, "Admin")
	} else {
		body["permissions"] = tierPermissions(t, tier)
	}
	return issueTokenWith(t, api, sessionToken, body)
}

// issueTokenWith makes a token from a body of POST /api/v1/auth/tokens.
func issueTokenWith(t *testing.T, api http.Handler, sessionToken string, body map[string]any) issuedToken {
	t.Helper()
	response := request(t, api, http.MethodPost, "/api/v1/auth/tokens", body, sessionToken, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create token %v status=%d body=%s", body, response.Code, response.Body.String())
	}
	var issued issuedToken
	decode(t, response, &issued)
	return issued
}

// customToken makes a token holding exactly permissions.
func customToken(t *testing.T, api http.Handler, sessionToken, label string, permissions ...string) string {
	t.Helper()
	return issueTokenWith(t, api, sessionToken, map[string]any{"label": label, "permissions": permissions}).Token
}

// tierPermissions is what an old tier held (frozen before roles), without the agent request permissions.
func tierPermissions(t *testing.T, tier string) []string {
	t.Helper()
	data, err := os.ReadFile("testdata/legacy-token-access.json")
	if err != nil {
		t.Fatal(err)
	}
	var frozen struct {
		Presets map[string][]string `json:"presets"`
	}
	if err := json.Unmarshal(data, &frozen); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, name := range frozen.Presets[tier] {
		if !strings.HasPrefix(name, "agent_requests:") {
			names = append(names, name)
		}
	}
	if names == nil {
		t.Fatalf("no tier %q", tier)
	}
	return names
}

// roleID finds a role of the caller's workspace by name.
func roleID(t *testing.T, api http.Handler, token, name string) string {
	t.Helper()
	response := request(t, api, http.MethodGet, "/api/v1/roles", nil, token, nil)
	var roles struct {
		Items []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"items"`
	}
	decode(t, response, &roles)
	for _, role := range roles.Items {
		if role.Name == name {
			return role.ID
		}
	}
	t.Fatalf("no role %q: status=%d body=%s", name, response.Code, response.Body.String())
	return ""
}

// permissionMissing reports whether response is the 403 permission_missing refusal and returns what it
// says is missing and from whom.
func permissionMissing(t *testing.T, response *httptest.ResponseRecorder) (bool, []string, string) {
	t.Helper()
	if response.Code != http.StatusForbidden {
		return false, nil, ""
	}
	var body struct {
		Error struct {
			Code    string   `json:"code"`
			Missing []string `json:"missing_permissions"`
			From    string   `json:"missing_from"`
		} `json:"error"`
	}
	decode(t, response, &body)
	return body.Error.Code == "permission_missing", body.Error.Missing, body.Error.From
}

func TestAgentTokenIsShownOnceListedWithScopeAndRevocable(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "Token Desk", "admin@token.example.com")

	issued := issueToken(t, api, admin, "Claude Code", "draft")
	if issued.Token == "" || issued.Session.Label != "Claude Code" || issued.Session.AgentEmail != "admin@token.example.com" ||
		issued.Session.Token == nil || issued.Session.Token.Kind != "custom" || len(issued.Session.Token.Permissions) != len(tierPermissions(t, "draft")) {
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
			ID    string `json:"id"`
			Label string `json:"label"`
			Token *struct {
				Kind string `json:"kind"`
			} `json:"token"`
			LastSeenAt string  `json:"last_seen_at"`
			RevokedAt  *string `json:"revoked_at"`
		} `json:"items"`
	}
	decode(t, listed, &list)
	scopes := map[string]*string{}
	for _, item := range list.Items {
		scopes[item.Label] = nil
		if item.Token != nil {
			scopes[item.Label] = &item.Token.Kind
		}
		if item.ID == issued.Session.ID && (item.Label != "Claude Code" || item.LastSeenAt == "" || item.RevokedAt != nil) {
			t.Fatalf("listed token = %+v", item)
		}
	}
	if scope := scopes["Claude Code"]; scope == nil || *scope != "custom" {
		t.Fatalf("token kind in list = %v, want custom", scope)
	}
	if scope, ok := scopes["Browser session"]; !ok || scope != nil {
		t.Fatalf("a person's session must list token null: present=%v token=%v", ok, scope)
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
		"empty label":       {"label": "", "permissions": []string{"tags:read"}},
		"blank label":       {"label": "   ", "permissions": []string{"tags:read"}},
		"label too long":    {"label": strings.Repeat("a", 81), "permissions": []string{"tags:read"}},
		"no role or list":   {"label": "Agent"},
		"unknown scope":     {"label": "Agent", "scope": "owner"},
		"owner is no scope": {"label": "Agent", "scope": "owner"},
		"unknown field":     {"label": "Agent", "permissions": []string{"tags:read"}, "role": "admin"},
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
	if !strings.Contains(scope.Body.String(), "role_id") || !strings.Contains(scope.Body.String(), "permissions") {
		t.Fatalf("a scope's refusal does not say what to send instead: %s", scope.Body.String())
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
		} `json:"agent"`
		Permissions []string `json:"permissions"`
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/auth/me", nil, adminToken.Token, nil), &me)
	if slices.Contains(me.Permissions, "kb_media:purge") || !slices.Contains(me.Permissions, "messages:send") || me.Agent.Email != "admin@roles.example.com" {
		t.Fatalf("a send token of an admin may do %v as %s, want what it was given", me.Permissions, me.Agent.Email)
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
