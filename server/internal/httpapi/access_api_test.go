package httpapi_test

import (
	"context"
	"github.com/colliedesk/colliedesk/server/internal/auth"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

func TestSessionEffectiveRoleRespectsLinkCeiling(t *testing.T) {
	// An invite adds its roles to what the person holds; it never takes one: an admin who redeems an Agent
	// invite stays an admin.
	t.Run("agent invite for existing admin", func(t *testing.T) {
		api, authService := testAPI(t)
		adminToken := createSession(t, api, authService, "Ceiling Desk", "admin@ceiling.example.com")
		created := request(t, api, http.MethodPost, "/api/v1/auth/invites", map[string]any{
			"email": "admin@ceiling.example.com", "roles": []string{"Agent"},
		}, adminToken, nil)
		var invite struct {
			URL string `json:"url"`
		}
		decode(t, created, &invite)
		redeemed := request(t, api, http.MethodPost, "/api/v1/auth/redeem", map[string]any{
			"token": linkToken(t, invite.URL),
		}, "", nil)
		var session struct {
			Token string `json:"token"`
		}
		decode(t, redeemed, &session)
		var me struct {
			Permissions []string `json:"permissions"`
		}
		decode(t, request(t, api, http.MethodGet, "/api/v1/auth/me", nil, session.Token, nil), &me)
		if len(me.Permissions) != len(auth.AllPermissions()) {
			t.Fatalf("an admin who redeemed an Agent invite may do %d permissions, want every one", len(me.Permissions))
		}
	})

	t.Run("admin pairing narrowed to agent", func(t *testing.T) {
		api, authService := testAPI(t)
		_, pairingURL, err := authService.CreateWorkspace(context.Background(), "Pairing Ceiling", "admin@pairing.example.com")
		if err != nil {
			t.Fatal(err)
		}
		redeemed := request(t, api, http.MethodPost, "/api/v1/auth/redeem", map[string]any{
			"token": linkToken(t, pairingURL), "requested_role": "agent",
		}, "", nil)
		assertAgentSession(t, api, redeemed)
	})
}

func assertAgentSession(t *testing.T, api http.Handler, redeemedResponse *httptest.ResponseRecorder) {
	t.Helper()
	var redeemed struct {
		Token string `json:"token"`
	}
	decode(t, redeemedResponse, &redeemed)
	var me struct {
		Permissions []string `json:"permissions"`
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/auth/me", nil, redeemed.Token, nil), &me)
	if !slices.Equal(slices.Sorted(slices.Values(me.Permissions)), slices.Sorted(slices.Values(auth.AgentRolePermissions()))) {
		t.Fatalf("the session may do %v, want exactly the Agent role's", me.Permissions)
	}
	forbidden := request(t, api, http.MethodPost, "/api/v1/auth/invites", map[string]any{
		"email": "forbidden@example.com", "roles": []string{"Agent"},
	}, redeemed.Token, nil)
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("effective agent created invite: status=%d body=%s", forbidden.Code, forbidden.Body.String())
	}
}

// Asking to sign in as admin through a link whose person will not hold the Admin role is refused, and
// the link stays usable.
func TestRedeemAskingForAdminWithoutTheAdminRoleIsRefused(t *testing.T) {
	api, authService := testAPI(t)
	adminToken := createSession(t, api, authService, "Escalate Desk", "admin@escalate.example.com")
	created := request(t, api, http.MethodPost, "/api/v1/auth/invites", map[string]any{"email": "sam@escalate.example.com"}, adminToken, nil)
	if created.Code != http.StatusCreated {
		t.Fatalf("create invite status=%d body=%s", created.Code, created.Body.String())
	}
	var invite struct {
		URL string `json:"url"`
	}
	decode(t, created, &invite)
	escalate := request(t, api, http.MethodPost, "/api/v1/auth/redeem", map[string]any{"token": linkToken(t, invite.URL), "requested_role": "admin"}, "", nil)
	if escalate.Code != http.StatusUnauthorized {
		t.Fatalf("redeem asking for admin status=%d body=%s, want 401", escalate.Code, escalate.Body.String())
	}
	joined := request(t, api, http.MethodPost, "/api/v1/auth/redeem", map[string]any{"token": linkToken(t, invite.URL)}, "", nil)
	if joined.Code != http.StatusOK {
		t.Fatalf("the refused link must stay usable: status=%d body=%s", joined.Code, joined.Body.String())
	}
}

func TestInviteRoleCeilingAndSessionRevocation(t *testing.T) {
	api, authService := testAPI(t)
	adminToken := createSession(t, api, authService, "Access Desk", "admin@access.example.com")
	created := request(t, api, http.MethodPost, "/api/v1/auth/invites", map[string]any{
		"email": "agent@access.example.com", "roles": []string{"Admin"},
	}, adminToken, nil)
	if created.Code != http.StatusCreated {
		t.Fatalf("create invite status=%d body=%s", created.Code, created.Body.String())
	}
	var invite struct {
		URL string `json:"url"`
	}
	decode(t, created, &invite)
	redeemed := request(t, api, http.MethodPost, "/api/v1/auth/redeem", map[string]any{
		"token": linkToken(t, invite.URL), "requested_role": "agent", "label": "Agent laptop",
	}, "", nil)
	if redeemed.Code != http.StatusOK {
		t.Fatalf("redeem narrowed invite status=%d body=%s", redeemed.Code, redeemed.Body.String())
	}
	var agentSession struct {
		Token string `json:"token"`
	}
	decode(t, redeemed, &agentSession)

	forbidden := request(t, api, http.MethodPost, "/api/v1/auth/invites", map[string]any{
		"email": "other@example.com", "roles": []string{"Agent"},
	}, agentSession.Token, nil)
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("agent invite status=%d, want 403", forbidden.Code)
	}

	listed := request(t, api, http.MethodGet, "/api/v1/auth/invites", nil, adminToken, nil)
	if listed.Code != http.StatusOK || strings.Contains(listed.Body.String(), `"url"`) || strings.Contains(listed.Body.String(), linkToken(t, invite.URL)) {
		t.Fatalf("invite list leaked secret or failed: status=%d body=%s", listed.Code, listed.Body.String())
	}
	sessions := request(t, api, http.MethodGet, "/api/v1/auth/sessions", nil, adminToken, nil)
	if sessions.Code != http.StatusOK || !strings.Contains(sessions.Body.String(), "Agent laptop") {
		t.Fatalf("admin sessions status=%d body=%s", sessions.Code, sessions.Body.String())
	}
	var sessionList struct {
		Items []struct {
			ID    string `json:"id"`
			Label string `json:"label"`
		} `json:"items"`
	}
	decode(t, sessions, &sessionList)
	var agentSessionID string
	for _, session := range sessionList.Items {
		if session.Label == "Agent laptop" {
			agentSessionID = session.ID
		}
	}
	revoked := request(t, api, http.MethodDelete, "/api/v1/auth/sessions/"+agentSessionID, nil, adminToken, nil)
	if revoked.Code != http.StatusNoContent {
		t.Fatalf("revoke session status=%d body=%s", revoked.Code, revoked.Body.String())
	}
	me := request(t, api, http.MethodGet, "/api/v1/auth/me", nil, agentSession.Token, nil)
	if me.Code != http.StatusUnauthorized {
		t.Fatalf("revoked bearer status=%d, want 401", me.Code)
	}
}

// The session list marks the one making the request, so the UI can say "This device" and warn
// that revoking it signs this browser out.
func TestSessionListMarksTheCallersSession(t *testing.T) {
	api, authService := testAPI(t)
	adminToken := createSession(t, api, authService, "Current Desk", "admin@current.example.com")
	created := request(t, api, http.MethodPost, "/api/v1/auth/invites", map[string]any{
		"email": "agent@current.example.com", "roles": []string{"Agent"},
	}, adminToken, nil)
	var invite struct {
		URL string `json:"url"`
	}
	decode(t, created, &invite)
	redeemed := request(t, api, http.MethodPost, "/api/v1/auth/redeem", map[string]any{
		"token": linkToken(t, invite.URL), "requested_role": "agent", "label": "Agent laptop",
	}, "", nil)
	var agentSession struct {
		Token string `json:"token"`
	}
	decode(t, redeemed, &agentSession)

	current := func(token string) map[string]bool {
		listed := request(t, api, http.MethodGet, "/api/v1/auth/sessions", nil, token, nil)
		if listed.Code != http.StatusOK {
			t.Fatalf("sessions status=%d body=%s", listed.Code, listed.Body.String())
		}
		var body struct {
			Items []struct {
				Label   string `json:"label"`
				Current *bool  `json:"current"`
			} `json:"items"`
		}
		decode(t, listed, &body)
		marks := map[string]bool{}
		for _, item := range body.Items {
			if item.Current == nil {
				t.Fatalf("session %q has no current field", item.Label)
			}
			marks[item.Label] = *item.Current
		}
		return marks
	}
	adminView := current(adminToken)
	if adminView["Agent laptop"] || len(adminView) != 2 {
		t.Fatalf("admin view marks=%v, want two sessions and the agent's not current", adminView)
	}
	trueCount := 0
	for _, mark := range adminView {
		if mark {
			trueCount++
		}
	}
	if trueCount != 1 {
		t.Fatalf("admin view marks=%v, want exactly one current session", adminView)
	}
	if agentView := current(agentSession.Token); !agentView["Agent laptop"] || len(agentView) != 1 {
		t.Fatalf("agent view marks=%v, want only its own session, marked current", agentView)
	}
}
