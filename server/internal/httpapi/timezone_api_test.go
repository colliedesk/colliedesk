package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
)

func TestWorkspaceAndAgentTimezoneSettings(t *testing.T) {
	api, authService := testAPI(t)
	adminToken := createSession(t, api, authService, "Timezone Desk", "admin@timezone.example.com")

	for _, value := range []string{"Mars/Olympus", "Local"} {
		invalidWorkspace := request(t, api, http.MethodPatch, "/api/v1/workspace/timezone", map[string]any{"timezone": value}, adminToken, nil)
		if invalidWorkspace.Code != http.StatusBadRequest {
			t.Fatalf("invalid workspace timezone %q status=%d body=%s", value, invalidWorkspace.Code, invalidWorkspace.Body.String())
		}
	}
	workspace := request(t, api, http.MethodPatch, "/api/v1/workspace/timezone", map[string]any{"timezone": "America/New_York"}, adminToken, nil)
	if workspace.Code != http.StatusOK || !strings.Contains(workspace.Body.String(), `"timezone":"America/New_York"`) {
		t.Fatalf("workspace timezone status=%d body=%s", workspace.Code, workspace.Body.String())
	}

	created := request(t, api, http.MethodPost, "/api/v1/auth/invites", map[string]any{
		"email": "agent@timezone.example.com", "role_ceiling": "agent",
	}, adminToken, nil)
	var invite struct {
		URL string `json:"url"`
	}
	decode(t, created, &invite)
	redeemed := request(t, api, http.MethodPost, "/api/v1/auth/redeem", map[string]any{"token": linkToken(t, invite.URL)}, "", nil)
	var agentSession struct {
		Token string `json:"token"`
	}
	decode(t, redeemed, &agentSession)

	forbidden := request(t, api, http.MethodPatch, "/api/v1/workspace/timezone", map[string]any{"timezone": "UTC"}, agentSession.Token, nil)
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("agent workspace timezone status=%d, want 403", forbidden.Code)
	}
	invalidAgent := request(t, api, http.MethodPatch, "/api/v1/agents/me/timezone", map[string]any{"timezone": "Invalid/Timezone"}, agentSession.Token, nil)
	if invalidAgent.Code != http.StatusBadRequest {
		t.Fatalf("invalid agent timezone status=%d body=%s", invalidAgent.Code, invalidAgent.Body.String())
	}
	missingAgent := request(t, api, http.MethodPatch, "/api/v1/agents/me/timezone", map[string]any{}, agentSession.Token, nil)
	if missingAgent.Code != http.StatusBadRequest {
		t.Fatalf("missing agent timezone status=%d body=%s", missingAgent.Code, missingAgent.Body.String())
	}
	override := request(t, api, http.MethodPatch, "/api/v1/agents/me/timezone", map[string]any{"timezone": "Europe/London"}, agentSession.Token, nil)
	if override.Code != http.StatusOK || !strings.Contains(override.Body.String(), `"timezone_override":"Europe/London"`) {
		t.Fatalf("agent override status=%d body=%s", override.Code, override.Body.String())
	}
	me := request(t, api, http.MethodGet, "/api/v1/auth/me", nil, agentSession.Token, nil)
	if !strings.Contains(me.Body.String(), `"timezone":"Europe/London"`) {
		t.Fatalf("effective override body=%s", me.Body.String())
	}
	cleared := request(t, api, http.MethodPatch, "/api/v1/agents/me/timezone", map[string]any{"timezone": nil}, agentSession.Token, nil)
	if cleared.Code != http.StatusOK || !strings.Contains(cleared.Body.String(), `"timezone_override":null`) {
		t.Fatalf("clear override status=%d body=%s", cleared.Code, cleared.Body.String())
	}
	me = request(t, api, http.MethodGet, "/api/v1/auth/me", nil, agentSession.Token, nil)
	if !strings.Contains(me.Body.String(), `"timezone":"America/New_York"`) {
		t.Fatalf("workspace fallback body=%s", me.Body.String())
	}
}
