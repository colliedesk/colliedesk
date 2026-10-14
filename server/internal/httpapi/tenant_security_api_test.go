package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
)

func TestAgentCannotRevokeAnotherAgentsSession(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Session Scope", "admin@scope.example.com")
	_, firstURL := createContractInvite(t, api, admin, "first@scope.example.com", "agent")
	_, secondURL := createContractInvite(t, api, admin, "second@scope.example.com", "agent")
	first := redeemLabelled(t, api, firstURL, "First agent")
	second := redeemLabelled(t, api, secondURL, "Second agent")
	secondID := findSessionID(t, api, admin, "Second agent")

	denied := request(t, api, http.MethodDelete, "/api/v1/auth/sessions/"+secondID, nil, first, nil)
	if denied.Code != http.StatusNotFound {
		t.Fatalf("foreign session revoke status=%d, want 404", denied.Code)
	}
	if me := request(t, api, http.MethodGet, "/api/v1/auth/me", nil, second, nil); me.Code != http.StatusOK {
		t.Fatalf("other agent session was revoked: status=%d", me.Code)
	}
}

func TestForeignReferencesAndFilteredSearchStayWorkspaceScoped(t *testing.T) {
	api, service := testAPI(t)
	tokenA := createSession(t, api, service, "Tenant Alpha", "admin@tenant-alpha.example.com")
	tokenB := createSession(t, api, service, "Tenant Beta", "admin@tenant-beta.example.com")
	contactA := create(t, api, tokenA, "/api/v1/contacts", map[string]any{"name": "Alpha", "email": "alpha@tenant.example.com"})
	contactB := create(t, api, tokenB, "/api/v1/contacts", map[string]any{"name": "Beta", "email": "beta@tenant.example.com"})
	conversationA := create(t, api, tokenA, "/api/v1/conversations", map[string]any{"contact_id": contactA, "subject": "Alpha subject"})
	conversationB := create(t, api, tokenB, "/api/v1/conversations", map[string]any{"contact_id": contactB, "subject": "Beta subject"})
	tagB := create(t, api, tokenB, "/api/v1/tags", map[string]any{"name": "Beta only"})
	_ = request(t, api, http.MethodPatch, "/api/v1/conversations/"+conversationB, map[string]any{"tag_ids": []string{tagB}}, tokenB, nil)
	_ = create(t, api, tokenB, "/api/v1/conversations/"+conversationB+"/messages", map[string]any{"kind": "note", "body": "other-tenant-secret"})

	foreignContact := request(t, api, http.MethodPost, "/api/v1/conversations", map[string]any{"contact_id": contactB, "subject": "Rejected"}, tokenA, nil)
	if foreignContact.Code != http.StatusNotFound {
		t.Fatalf("foreign contact status=%d, want 404", foreignContact.Code)
	}
	var beta struct {
		Agent struct {
			ID string `json:"id"`
		} `json:"agent"`
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/auth/me", nil, tokenB, nil), &beta)
	foreignAssignee := request(t, api, http.MethodPatch, "/api/v1/conversations/"+conversationA, map[string]any{"assignee_id": beta.Agent.ID}, tokenA, nil)
	if foreignAssignee.Code != http.StatusBadRequest {
		t.Fatalf("foreign assignee status=%d, want 400", foreignAssignee.Code)
	}
	for _, path := range []string{
		"/api/v1/conversations?tag=" + tagB,
		"/api/v1/conversations?search=other-tenant-secret",
	} {
		response := request(t, api, http.MethodGet, path, nil, tokenA, nil)
		if response.Code != http.StatusOK || strings.Contains(response.Body.String(), conversationB) || !strings.Contains(response.Body.String(), `"items":[]`) {
			t.Fatalf("workspace leak for %s: status=%d body=%s", path, response.Code, response.Body.String())
		}
	}
}

func TestForeignWorkspaceSessionAndInviteCannotBeRevokedOrDiscovered(t *testing.T) {
	api, service := testAPI(t)
	tokenA := createSession(t, api, service, "Access Alpha", "admin@access-alpha.example.com")
	tokenB := createSession(t, api, service, "Access Beta", "admin@access-beta.example.com")

	pairURL, err := service.Pair(t.Context(), "access-beta", "admin@access-beta.example.com")
	if err != nil {
		t.Fatal(err)
	}
	foreignSessionToken := redeemLabelled(t, api, pairURL, "Foreign workspace session")
	foreignSessionID := findSessionID(t, api, tokenB, "Foreign workspace session")
	response := request(t, api, http.MethodDelete, "/api/v1/auth/sessions/"+foreignSessionID, nil, tokenA, nil)
	if response.Code != http.StatusNotFound {
		t.Fatalf("foreign session revoke status=%d body=%s, want 404", response.Code, response.Body.String())
	}
	if response := request(t, api, http.MethodGet, "/api/v1/auth/me", nil, foreignSessionToken, nil); response.Code != http.StatusOK {
		t.Fatalf("foreign session changed after rejected revoke: status=%d", response.Code)
	}
	if sessions := request(t, api, http.MethodGet, "/api/v1/auth/sessions", nil, tokenA, nil); strings.Contains(sessions.Body.String(), foreignSessionID) {
		t.Fatalf("foreign session leaked in list: %s", sessions.Body.String())
	}

	foreignInviteID, _ := createContractInvite(t, api, tokenB, "invite@access-beta.example.com", "agent")
	response = request(t, api, http.MethodDelete, "/api/v1/auth/invites/"+foreignInviteID, nil, tokenA, nil)
	if response.Code != http.StatusNotFound {
		t.Fatalf("foreign invite revoke status=%d body=%s, want 404", response.Code, response.Body.String())
	}
	if invites := request(t, api, http.MethodGet, "/api/v1/auth/invites", nil, tokenA, nil); strings.Contains(invites.Body.String(), foreignInviteID) {
		t.Fatalf("foreign invite leaked in list: %s", invites.Body.String())
	}
	invites := request(t, api, http.MethodGet, "/api/v1/auth/invites", nil, tokenB, nil)
	var list struct {
		Items []struct {
			ID        string  `json:"id"`
			RevokedAt *string `json:"revoked_at"`
		} `json:"items"`
	}
	decode(t, invites, &list)
	for _, invite := range list.Items {
		if invite.ID == foreignInviteID && invite.RevokedAt == nil {
			return
		}
	}
	t.Fatalf("foreign invite changed after rejected revoke: %s", invites.Body.String())
}

func redeemLabelled(t *testing.T, api http.Handler, rawURL, label string) string {
	t.Helper()
	response := request(t, api, http.MethodPost, "/api/v1/auth/redeem", map[string]any{"token": linkToken(t, rawURL), "label": label}, "", nil)
	var session struct {
		Token string `json:"token"`
	}
	decode(t, response, &session)
	return session.Token
}
