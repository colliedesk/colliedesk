package httpapi_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

type adminDesk struct {
	requestDesk
	token string // an admin tier agent token of the desk's admin
}

func newAdminDesk(t *testing.T, name string) adminDesk {
	t.Helper()
	desk := newRequestDesk(t, name)
	return adminDesk{requestDesk: desk, token: issueToken(t, desk.api, desk.admin, "Ops agent", "admin").Token}
}

// promotedAdmin adds email to the team and makes them an admin, as a person would.
func promotedAdmin(t *testing.T, desk adminDesk, email string) string {
	t.Helper()
	_ = joinAsAgent(t, desk.api, desk.admin, email)
	id := memberIDByEmail(t, desk.api, desk.admin, email)
	if response := request(t, desk.api, http.MethodPatch, "/api/v1/agents/"+id, map[string]any{"role": "admin"}, desk.admin, nil); response.Code != http.StatusOK {
		t.Fatalf("promote %s: status=%d body=%s", email, response.Code, response.Body.String())
	}
	return id
}

func member(t *testing.T, api http.Handler, admin, id string) teamMemberView {
	t.Helper()
	var body struct {
		Items []teamMemberView `json:"items"`
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/agents", nil, admin, nil), &body)
	for _, item := range body.Items {
		if item.ID == id {
			return item
		}
	}
	t.Fatalf("no member %s", id)
	return teamMemberView{}
}

// Reactivating a deactivated admin hands the admin role back, so it waits for a person like a promotion.
func TestReactivatingAnAdminIsARequestEvenForTheAdminTier(t *testing.T) {
	desk := newAdminDesk(t, "Reactivate Admin")
	id := promotedAdmin(t, desk, "boss@reactivate-admin.example.com")
	if response := request(t, desk.api, http.MethodPatch, "/api/v1/agents/"+id, map[string]any{"active": false}, desk.admin, nil); response.Code != http.StatusOK {
		t.Fatalf("deactivate: status=%d body=%s", response.Code, response.Body.String())
	}
	filed := accepted(t, desk.api, http.MethodPatch, "/api/v1/agents/"+id, map[string]any{"active": true}, desk.token)
	if !strings.Contains(filed.Summary, "boss@reactivate-admin.example.com") || !strings.Contains(filed.Summary, "reactivate") {
		t.Errorf("summary = %q", filed.Summary)
	}
	if member(t, desk.api, desk.admin, id).DeactivatedAt == nil {
		t.Fatal("the admin was reactivated before anyone approved")
	}
	if response := approve(t, desk.api, desk.admin, filed.RequestID, nil); response.Code != http.StatusOK {
		t.Fatalf("approve: status=%d body=%s", response.Code, response.Body.String())
	}
	if member(t, desk.api, desk.admin, id).DeactivatedAt != nil {
		t.Fatal("approving did not reactivate the admin")
	}
}

// Demoting or deactivating an admin waits for a person; the same change to an agent runs directly.
func TestChangingAnAdminWaitsForAPersonButAnAgentChangeRuns(t *testing.T) {
	desk := newAdminDesk(t, "Change Admin")
	bossID := promotedAdmin(t, desk, "boss@change-admin.example.com")
	_ = joinAsAgent(t, desk.api, desk.admin, "agent@change-admin.example.com")
	agentID := memberIDByEmail(t, desk.api, desk.admin, "agent@change-admin.example.com")

	accepted(t, desk.api, http.MethodPatch, "/api/v1/agents/"+bossID, map[string]any{"role": "agent"}, desk.token)
	accepted(t, desk.api, http.MethodPatch, "/api/v1/agents/"+bossID, map[string]any{"active": false}, desk.token)
	if boss := member(t, desk.api, desk.admin, bossID); boss.Role != "admin" || boss.DeactivatedAt != nil {
		t.Fatalf("an admin was changed before anyone approved: %+v", boss)
	}
	if response := request(t, desk.api, http.MethodPatch, "/api/v1/agents/"+agentID, map[string]any{"active": false}, desk.token, nil); response.Code != http.StatusOK {
		t.Fatalf("admin token deactivates an agent: status=%d body=%s", response.Code, response.Body.String())
	}
	if response := request(t, desk.api, http.MethodPatch, "/api/v1/agents/"+agentID, map[string]any{"active": true}, desk.token, nil); response.Code != http.StatusOK {
		t.Fatalf("admin token reactivates an agent: status=%d body=%s", response.Code, response.Body.String())
	}
}

// Signing another person out waits for a person. The creator's own sign-ins and any agent token are
// revoked directly.
func TestRevokingAnotherPersonsSignInWaitsForAPerson(t *testing.T) {
	desk := newAdminDesk(t, "Revoke People")
	_, link := createContractInvite(t, desk.api, desk.admin, "agent@revoke-people.example.com", "agent")
	agentSession := redeemLabelled(t, desk.api, link, "Agent laptop")
	agentLaptop := findSessionID(t, desk.api, desk.admin, "Agent laptop")
	agentBot := issueToken(t, desk.api, agentSession, "Agent bot", "read")
	pairing, err := desk.service.Pair(context.Background(), "revoke-people", "admin@revoke-people.example.com")
	if err != nil {
		t.Fatal(err)
	}
	ownPhone := redeemLabelled(t, desk.api, pairing, "Admin phone")
	ownPhoneID := findSessionID(t, desk.api, desk.admin, "Admin phone")

	filed := accepted(t, desk.api, http.MethodDelete, "/api/v1/auth/sessions/"+agentLaptop, nil, desk.token)
	if !strings.Contains(filed.Summary, "agent@revoke-people.example.com") {
		t.Errorf("summary for an admin token = %q, want the owner", filed.Summary)
	}
	if response := request(t, desk.api, http.MethodGet, "/api/v1/auth/me", nil, agentSession, nil); response.Code != http.StatusOK {
		t.Fatalf("another person's sign-in was revoked before approval: %d", response.Code)
	}
	if response := request(t, desk.api, http.MethodDelete, "/api/v1/auth/sessions/"+agentBot.Session.ID, nil, desk.token, nil); response.Code != http.StatusNoContent {
		t.Fatalf("admin token revokes an agent token: status=%d body=%s", response.Code, response.Body.String())
	}
	if response := request(t, desk.api, http.MethodDelete, "/api/v1/auth/sessions/"+ownPhoneID, nil, desk.token, nil); response.Code != http.StatusNoContent {
		t.Fatalf("admin token revokes its creator's own sign-in: status=%d body=%s", response.Code, response.Body.String())
	}
	if response := request(t, desk.api, http.MethodGet, "/api/v1/auth/me", nil, ownPhone, nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("the creator's revoked sign-in: status=%d, want 401", response.Code)
	}

	// A token whose role cannot list another person's sessions learns nothing about them from its summary.
	send := issueToken(t, desk.api, desk.admin, "Sender", "send").Token
	hidden := accepted(t, desk.api, http.MethodDelete, "/api/v1/auth/sessions/"+agentLaptop, nil, send)
	if strings.Contains(hidden.Summary, "agent@revoke-people.example.com") || strings.Contains(hidden.Summary, "Agent laptop") {
		t.Errorf("summary for a send token reveals another person's session: %q", hidden.Summary)
	}
}

// A request on a /me route acts on the person who owns the token, so only that person approves it.
func TestMyTimezoneRequestIsApprovedOnlyByTheTokenOwner(t *testing.T) {
	desk := newAdminDesk(t, "Own Timezone")
	agentSession := joinAsAgent(t, desk.api, desk.admin, "agent@own-timezone.example.com")
	read := issueToken(t, desk.api, agentSession, "Agent reader", "read").Token
	filed := accepted(t, desk.api, http.MethodPatch, "/api/v1/agents/me/timezone", map[string]any{"timezone": "Asia/Tokyo"}, read)
	if got := getRequest(t, desk.api, desk.admin, filed.RequestID).ApprovableBy; got != "owner" {
		t.Errorf("approvable_by = %q, want owner", got)
	}
	if response := approve(t, desk.api, desk.admin, filed.RequestID, nil); response.Code != http.StatusForbidden {
		t.Fatalf("another person approves: status=%d body=%s, want 403", response.Code, response.Body.String())
	}
	if got := getRequest(t, desk.api, desk.admin, filed.RequestID).Status; got != "pending" {
		t.Fatalf("after a refused approval the request is %s, want pending", got)
	}
	if response := approve(t, desk.api, agentSession, filed.RequestID, nil); response.Code != http.StatusOK {
		t.Fatalf("the owner approves: status=%d body=%s", response.Code, response.Body.String())
	}
	var owner, admin struct {
		Timezone string `json:"timezone"`
	}
	decode(t, request(t, desk.api, http.MethodGet, "/api/v1/auth/me", nil, agentSession, nil), &owner)
	decode(t, request(t, desk.api, http.MethodGet, "/api/v1/auth/me", nil, desk.admin, nil), &admin)
	if owner.Timezone != "Asia/Tokyo" || admin.Timezone != "UTC" {
		t.Fatalf("timezones after approval: owner %q, admin %q", owner.Timezone, admin.Timezone)
	}
}

// Every invite from a token waits for a person, who sees its link once; nothing keeps the link.
func TestEveryTokenInviteWaitsForAPersonWhoSeesTheLinkOnce(t *testing.T) {
	desk := newAdminDesk(t, "Token Invite")
	filed := accepted(t, desk.api, http.MethodPost, "/api/v1/auth/invites", map[string]any{"email": "new@token-invite.example.com", "role_ceiling": "agent"}, desk.token)
	var invites struct {
		Items []struct{ Email string } `json:"items"`
	}
	decode(t, request(t, desk.api, http.MethodGet, "/api/v1/auth/invites", nil, desk.admin, nil), &invites)
	if len(invites.Items) != 0 {
		t.Fatalf("an invite exists before approval: %+v", invites.Items)
	}
	approved := approve(t, desk.api, desk.admin, filed.RequestID, nil)
	if approved.Code != http.StatusOK || !strings.Contains(approved.Body.String(), "token=") {
		t.Fatalf("the approver sees the link: status=%d body=%s", approved.Code, approved.Body.String())
	}
	for _, token := range []string{desk.admin, desk.token} {
		if body := request(t, desk.api, http.MethodGet, "/api/v1/agent-requests/"+filed.RequestID, nil, token, nil).Body.String(); strings.Contains(body, "token=") {
			t.Fatalf("the stored request keeps the link: %s", body)
		}
	}
}

func TestSettingsRequestsRefuseANonObjectBodyAndNameOnlyWhatChanges(t *testing.T) {
	desk := newAdminDesk(t, "Settings Shapes")
	send := issueToken(t, desk.api, desk.admin, "Sender", "send").Token
	for _, body := range []any{[]any{"Asia/Tokyo"}, "Asia/Tokyo", 3} {
		if response := request(t, desk.api, http.MethodPatch, "/api/v1/workspace/timezone", body, send, nil); response.Code != http.StatusBadRequest {
			t.Errorf("body %v: status=%d body=%s, want 400", body, response.Code, response.Body.String())
		}
	}
	if items := listRequests(t, desk.api, desk.admin, ""); len(items) != 0 {
		t.Fatalf("a malformed settings call was stored: %+v", items)
	}
	origins := accepted(t, desk.api, http.MethodPut, "/api/v1/channels/web-form", map[string]any{"enabled": false, "allowed_origins": []string{"https://shop.example.com"}}, send)
	if !strings.Contains(origins.Summary, "none → https://shop.example.com") || strings.Contains(origins.Summary, "off → off") {
		t.Errorf("origins-only summary = %q", origins.Summary)
	}
}

func TestTokenDomainVerificationIsRateLimited(t *testing.T) {
	desk := newAdminDesk(t, "Verify Limit")
	connectEmail(t, desk.api, desk.admin, "support@verify-limit.example.com")
	for attempt := 1; attempt <= 10; attempt++ {
		if response := request(t, desk.api, http.MethodPost, "/api/v1/channels/email/domain-verification", nil, desk.token, nil); response.Code == http.StatusTooManyRequests {
			t.Fatalf("attempt %d was rate limited", attempt)
		}
	}
	limited := request(t, desk.api, http.MethodPost, "/api/v1/channels/email/domain-verification", nil, desk.token, nil)
	if limited.Code != http.StatusTooManyRequests || limited.Header().Get("Retry-After") == "" {
		t.Fatalf("11th attempt: status=%d retry-after=%q, want 429 with Retry-After", limited.Code, limited.Header().Get("Retry-After"))
	}
	if response := request(t, desk.api, http.MethodPost, "/api/v1/channels/email/domain-verification", nil, desk.admin, nil); response.Code == http.StatusTooManyRequests {
		t.Fatal("a person is rate limited by a token's attempts")
	}
}
