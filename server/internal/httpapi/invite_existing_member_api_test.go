package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
)

// memberRoles reads the role names of the member with email from the team list.
func memberRoles(t *testing.T, api http.Handler, token, email string) []string {
	t.Helper()
	var body struct {
		Items []struct {
			Email string `json:"email"`
			Roles []struct {
				Name string `json:"name"`
			} `json:"roles"`
		} `json:"items"`
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/agents", nil, token, nil), &body)
	for _, agent := range body.Items {
		if agent.Email == email {
			names := make([]string, len(agent.Roles))
			for index, role := range agent.Roles {
				names[index] = role.Name
			}
			return names
		}
	}
	t.Fatalf("no member %s in the team list", email)
	return nil
}

func TestInvitingSomeoneAlreadyInTheWorkspaceIsRefused(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "invite-member-alpha", "owner@invite-member-alpha.example.com")

	created := request(t, api, http.MethodPost, "/api/v1/auth/invites",
		map[string]any{"email": "new@invite-member-alpha.example.com", "roles": []string{"Agent"}}, admin, nil)
	if created.Code != http.StatusCreated {
		t.Fatalf("invite for a new address status=%d body=%s, want 201", created.Code, created.Body.String())
	}

	// The workspace owner is a member already; the address is matched in any case.
	for _, email := range []string{"owner@invite-member-alpha.example.com", "Owner@Invite-Member-Alpha.example.com"} {
		response := request(t, api, http.MethodPost, "/api/v1/auth/invites",
			map[string]any{"email": email, "roles": []string{"Agent"}}, admin, nil)
		if response.Code != http.StatusConflict {
			t.Fatalf("invite for member %s status=%d body=%s, want 409", email, response.Code, response.Body.String())
		}
		var body struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		decode(t, response, &body)
		if body.Error.Code != "already_a_member" {
			t.Fatalf("error code=%q, want already_a_member", body.Error.Code)
		}
		if !strings.Contains(body.Error.Message, "/api/v1/agents/{id}/roles") {
			t.Fatalf("error message %q does not say how to change a member's roles", body.Error.Message)
		}
		if strings.Contains(response.Body.String(), "token=") {
			t.Fatalf("a refused invite returned a link: %s", response.Body.String())
		}
	}
}

func TestAnInviteLinkDoesNotSignInSomeoneWhoIsAlreadyAMember(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "invite-member-beta", "owner@invite-member-beta.example.com")
	email := "teammate@invite-member-beta.example.com"

	// Two links for the same new address: the first makes them a member, the second was issued before that.
	_, first := createContractInvite(t, api, admin, email, "agent")
	_, second := createContractInvite(t, api, admin, email, "admin")
	if first == "" || second == "" {
		t.Fatal("both invites must be created while the address is not a member")
	}

	joined := request(t, api, http.MethodPost, "/api/v1/auth/redeem", map[string]any{"token": linkToken(t, first)}, "", nil)
	if joined.Code != http.StatusOK {
		t.Fatalf("redeem of a new member's invite status=%d body=%s, want 200", joined.Code, joined.Body.String())
	}
	var session struct {
		Token string `json:"token"`
	}
	decode(t, joined, &session)
	if session.Token == "" {
		t.Fatal("a new member's invite gave no session")
	}

	refused := request(t, api, http.MethodPost, "/api/v1/auth/redeem",
		map[string]any{"token": linkToken(t, second), "session_type": "cookie"}, "", nil)
	if refused.Code == http.StatusOK || refused.Code < 400 {
		t.Fatalf("redeem of an invite for an existing member status=%d body=%s, want a refusal", refused.Code, refused.Body.String())
	}
	if cookies := refused.Result().Cookies(); len(cookies) > 0 {
		t.Fatalf("a refused redeem set cookies: %v", cookies)
	}
	if strings.Contains(refused.Body.String(), `"token"`) {
		t.Fatalf("a refused redeem returned a session: %s", refused.Body.String())
	}
	if roles := memberRoles(t, api, admin, email); len(roles) != 1 || roles[0] != "Agent" {
		t.Fatalf("member roles after a refused invite = %v, want [Agent]", roles)
	}
}
