package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
)

// redeemLink redeems an invite or pairing link and returns the response.
func redeemLink(t *testing.T, api http.Handler, link string) (int, string) {
	t.Helper()
	response := request(t, api, http.MethodPost, "/api/v1/auth/redeem", map[string]any{"token": linkToken(t, link)}, "", nil)
	return response.Code, response.Body.String()
}

// Someone who may invite but may not change a member's roles cannot use an invite to sign in as that
// member: not with a new invite to their address, and not with an older invite to an address whose
// person joined and was promoted since.
func TestAnInviteDoesNotSignInAsAMemberTheInviterMayNotChange(t *testing.T) {
	api, service := testAPI(t)
	boss := createSession(t, api, service, "invite-member-alpha", "boss@invite-member-alpha.example.com")
	kbCall(t, api, http.MethodPost, "/api/v1/roles", map[string]any{"name": "Recruiter", "permissions": []string{"team:invite", "team:read"}}, boss, http.StatusCreated, nil)
	invited := request(t, api, http.MethodPost, "/api/v1/auth/invites",
		map[string]any{"email": "junior@invite-member-alpha.example.com", "roles": []string{"Agent", "Recruiter"}}, boss, nil)
	var juniorInvite struct {
		URL string `json:"url"`
	}
	decode(t, invited, &juniorInvite)
	var juniorSession struct {
		Token string `json:"token"`
	}
	decode(t, request(t, api, http.MethodPost, "/api/v1/auth/redeem", map[string]any{"token": linkToken(t, juniorInvite.URL)}, "", nil), &juniorSession)
	junior := juniorSession.Token
	if junior == "" {
		t.Fatal("junior did not join")
	}

	// An invite to the Admin's own address.
	response := request(t, api, http.MethodPost, "/api/v1/auth/invites",
		map[string]any{"email": "boss@invite-member-alpha.example.com", "roles": []string{"Agent"}}, junior, nil)
	if response.Code != http.StatusForbidden || strings.Contains(response.Body.String(), "token=") {
		t.Fatalf("invite to the Admin's address by a recruiter status=%d body=%s, want 403 and no link", response.Code, response.Body.String())
	}

	// Two invites to a new address; the person joins with the first and is then made an Admin.
	_, first := createContractInvite(t, api, junior, "alice@invite-member-alpha.example.com", "agent")
	_, second := createContractInvite(t, api, junior, "alice@invite-member-alpha.example.com", "agent")
	if first == "" || second == "" {
		t.Fatal("both invites to a new address must be created")
	}
	if code, body := redeemLink(t, api, first); code != http.StatusOK {
		t.Fatalf("alice joins status=%d body=%s", code, body)
	}
	if got := setRoles(t, api, boss, memberIDByEmail(t, api, boss, "alice@invite-member-alpha.example.com"), "Agent", "Admin"); got.code != http.StatusOK {
		t.Fatalf("promote alice: %d %s", got.code, got.body)
	}
	if code, body := redeemLink(t, api, second); code != http.StatusUnauthorized || strings.Contains(body, `"token"`) {
		t.Fatalf("older invite to alice after she joined status=%d body=%s, want 401 and no session", code, body)
	}
}

// An agent token may invite new people, but it cannot turn an invite into a session of someone who is
// already a member, whether the address was a member when it invited or became one later.
func TestAnAgentTokenCannotTurnAnInviteIntoAMembersSession(t *testing.T) {
	api, service := testAPI(t)
	owner := createSession(t, api, service, "invite-member-beta", "owner@invite-member-beta.example.com")
	kbCall(t, api, http.MethodPost, "/api/v1/roles", map[string]any{"name": "Bare", "permissions": []string{}}, owner, http.StatusCreated, nil)
	token := customToken(t, api, owner, "inviter", "team:invite")

	response := request(t, api, http.MethodPost, "/api/v1/auth/invites",
		map[string]any{"email": "owner@invite-member-beta.example.com", "roles": []string{"Bare"}}, token, nil)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "already_a_member") || strings.Contains(response.Body.String(), "token=") {
		t.Fatalf("token invite to the owner's address status=%d body=%s, want 409 already_a_member and no link", response.Code, response.Body.String())
	}

	tokenInvite := request(t, api, http.MethodPost, "/api/v1/auth/invites",
		map[string]any{"email": "carol@invite-member-beta.example.com", "roles": []string{"Bare"}}, token, nil)
	var early struct {
		URL string `json:"url"`
	}
	decode(t, tokenInvite, &early)
	if tokenInvite.Code != http.StatusCreated || early.URL == "" {
		t.Fatalf("token invite to a new address status=%d body=%s, want 201", tokenInvite.Code, tokenInvite.Body.String())
	}
	_, ownInvite := createContractInvite(t, api, owner, "carol@invite-member-beta.example.com", "agent")
	if code, body := redeemLink(t, api, ownInvite); code != http.StatusOK {
		t.Fatalf("carol joins status=%d body=%s", code, body)
	}
	if code, body := redeemLink(t, api, early.URL); code != http.StatusUnauthorized || strings.Contains(body, `"token"`) {
		t.Fatalf("the token's invite after carol joined status=%d body=%s, want 401 and no session", code, body)
	}
}
