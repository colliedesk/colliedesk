package httpapi_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/jackc/pgx/v5/pgxpool"
)

type teamMemberView struct {
	ID                string  `json:"id"`
	Email             string  `json:"email"`
	Role              string  `json:"role"`
	DeactivatedAt     *string `json:"deactivated_at"`
	LastActiveAt      *string `json:"last_active_at"`
	OpenConversations int     `json:"open_conversations"`
}

// teamFixture is one workspace with an admin and two agents (Ann and Bob), each signed in.
type teamFixture struct {
	api                   http.Handler
	pool                  *pgxpool.Pool
	service               *auth.Service
	admin, ann, bob       string
	annID, bobID, adminID string
	contactID             string
	otherAdmin            string
}

func newTeamFixture(t *testing.T) teamFixture {
	t.Helper()
	api, service, pool := testAPIWithPool(t)
	f := teamFixture{api: api, pool: pool, service: service}
	f.admin = createSession(t, api, service, "Team Desk", "admin@team.example.com")
	_, annURL := createContractInvite(t, api, f.admin, "ann@team.example.com", "agent")
	f.ann = redeemLabelled(t, api, annURL, "Ann laptop")
	_, bobURL := createContractInvite(t, api, f.admin, "bob@team.example.com", "agent")
	f.bob = redeemLabelled(t, api, bobURL, "Bob laptop")
	f.adminID = f.member(t, "admin@team.example.com").ID
	f.annID = f.member(t, "ann@team.example.com").ID
	f.bobID = f.member(t, "bob@team.example.com").ID
	f.contactID = create(t, api, f.admin, "/api/v1/contacts", map[string]any{"name": "Customer", "email": "customer@team.example.com"})
	f.otherAdmin = createSession(t, api, service, "Other Desk", "admin@other.example.com")
	return f
}

func (f teamFixture) members(t *testing.T) []teamMemberView {
	t.Helper()
	response := request(t, f.api, http.MethodGet, "/api/v1/agents", nil, f.admin, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("list agents status=%d body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Items []teamMemberView `json:"items"`
	}
	decode(t, response, &body)
	return body.Items
}

func (f teamFixture) member(t *testing.T, email string) teamMemberView {
	t.Helper()
	for _, item := range f.members(t) {
		if item.Email == email {
			return item
		}
	}
	t.Fatalf("no agent %s", email)
	return teamMemberView{}
}

func (f teamFixture) patch(t *testing.T, token, id string, body map[string]any) (int, apiError) {
	t.Helper()
	response := request(t, f.api, http.MethodPatch, "/api/v1/agents/"+id, body, token, nil)
	var failure apiError
	if response.Code >= 400 {
		decode(t, response, &failure)
	}
	return response.Code, failure
}

// conversation creates a conversation with the given status assigned to agentID.
func (f teamFixture) conversation(t *testing.T, subject, status, agentID string) string {
	t.Helper()
	id := create(t, f.api, f.admin, "/api/v1/conversations", map[string]any{"contact_id": f.contactID, "subject": subject})
	response := request(t, f.api, http.MethodPatch, "/api/v1/conversations/"+id, map[string]any{"status": status, "assignee_id": agentID}, f.admin, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("assign %s status=%d body=%s", subject, response.Code, response.Body.String())
	}
	return id
}

func (f teamFixture) assignee(t *testing.T, conversationID string) *string {
	t.Helper()
	var item struct {
		AssigneeID *string `json:"assignee_id"`
	}
	decode(t, request(t, f.api, http.MethodGet, "/api/v1/conversations/"+conversationID, nil, f.admin, nil), &item)
	return item.AssigneeID
}

func TestTeamListShowsRoleStateActivityAndAssignedOpenWork(t *testing.T) {
	f := newTeamFixture(t)
	f.conversation(t, "Open one", "open", f.annID)
	f.conversation(t, "Pending one", "pending", f.annID)
	f.conversation(t, "Resolved one", "resolved", f.annID)

	ann := f.member(t, "ann@team.example.com")
	if ann.Role != "agent" || ann.DeactivatedAt != nil {
		t.Fatalf("ann=%+v, want an active agent", ann)
	}
	if ann.OpenConversations != 2 {
		t.Fatalf("ann open_conversations=%d, want 2 (open + pending, not resolved)", ann.OpenConversations)
	}
	if ann.LastActiveAt == nil {
		t.Fatalf("ann last_active_at is null after her session was used")
	}
	if got := f.member(t, "bob@team.example.com").OpenConversations; got != 0 {
		t.Fatalf("bob open_conversations=%d, want 0", got)
	}
}

func TestDeactivateMovesOpenAndPendingToUnassignedAndKeepsClosedHistory(t *testing.T) {
	f := newTeamFixture(t)
	open := f.conversation(t, "Open one", "open", f.annID)
	pending := f.conversation(t, "Pending one", "pending", f.annID)
	resolved := f.conversation(t, "Resolved one", "resolved", f.annID)
	closed := f.conversation(t, "Closed one", "closed", f.annID)
	bobs := f.conversation(t, "Bob open", "open", f.bobID)

	if code, failure := f.patch(t, f.admin, f.annID, map[string]any{"active": false}); code != http.StatusOK {
		t.Fatalf("deactivate status=%d error=%+v", code, failure)
	}
	for name, id := range map[string]string{"open": open, "pending": pending} {
		if got := f.assignee(t, id); got != nil {
			t.Errorf("%s conversation assignee=%v, want unassigned", name, *got)
		}
	}
	for name, id := range map[string]string{"resolved": resolved, "closed": closed} {
		if got := f.assignee(t, id); got == nil || *got != f.annID {
			t.Errorf("%s conversation assignee=%v, want ann kept", name, got)
		}
	}
	if got := f.assignee(t, bobs); got == nil || *got != f.bobID {
		t.Errorf("bob's conversation assignee=%v, want untouched", got)
	}
	ann := f.member(t, "ann@team.example.com")
	if ann.DeactivatedAt == nil || ann.OpenConversations != 0 {
		t.Fatalf("ann=%+v, want deactivated with no open work", ann)
	}
}

func TestDeactivateCanHandOpenWorkToAnotherActiveAgent(t *testing.T) {
	f := newTeamFixture(t)
	open := f.conversation(t, "Open one", "open", f.annID)
	pending := f.conversation(t, "Pending one", "pending", f.annID)
	resolved := f.conversation(t, "Resolved one", "resolved", f.annID)

	if code, failure := f.patch(t, f.admin, f.annID, map[string]any{"active": false, "reassign_to": f.bobID}); code != http.StatusOK {
		t.Fatalf("deactivate status=%d error=%+v", code, failure)
	}
	for _, id := range []string{open, pending} {
		if got := f.assignee(t, id); got == nil || *got != f.bobID {
			t.Errorf("assignee=%v, want bob", got)
		}
	}
	if got := f.assignee(t, resolved); got == nil || *got != f.annID {
		t.Errorf("resolved assignee=%v, want ann kept", got)
	}
	if got := f.member(t, "bob@team.example.com").OpenConversations; got != 2 {
		t.Fatalf("bob open_conversations=%d, want 2", got)
	}
}

func TestDeactivatedAgentCannotSignInOrKeepSessionsOrLinks(t *testing.T) {
	f := newTeamFixture(t)
	pairing, err := f.service.Pair(context.Background(), "team-desk", "ann@team.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if code, failure := f.patch(t, f.admin, f.annID, map[string]any{"active": false}); code != http.StatusOK {
		t.Fatalf("deactivate status=%d error=%+v", code, failure)
	}

	if got := request(t, f.api, http.MethodGet, "/api/v1/auth/me", nil, f.ann, nil).Code; got != http.StatusUnauthorized {
		t.Errorf("existing session status=%d, want 401", got)
	}
	if got := request(t, f.api, http.MethodPost, "/api/v1/auth/redeem", map[string]any{"token": linkToken(t, pairing)}, "", nil).Code; got != http.StatusUnauthorized {
		t.Errorf("link made before the deactivation status=%d, want 401", got)
	}
	if _, err := f.service.Pair(context.Background(), "team-desk", "ann@team.example.com"); err == nil {
		t.Errorf("a new pairing link was created for a deactivated agent")
	}
	if _, _, err := f.service.ExchangeCloud(context.Background(), "ann@team.example.com", "", "cloud"); !errors.Is(err, auth.ErrCloudAgentNotFound) {
		t.Errorf("cloud sign-in error=%v, want ErrCloudAgentNotFound", err)
	}
	// Her old session also disappears from the admin's session list as revoked.
	if got := request(t, f.api, http.MethodGet, "/api/v1/auth/me", nil, f.bob, nil).Code; got != http.StatusOK {
		t.Errorf("bob's session status=%d, want untouched", got)
	}
}

func TestInviteRedeemedAfterDeactivationIsRefusedAndNewInviteIsNotCreated(t *testing.T) {
	f := newTeamFixture(t)
	_, staleURL := createContractInvite(t, f.api, f.admin, "ann@team.example.com", "agent")
	if code, failure := f.patch(t, f.admin, f.annID, map[string]any{"active": false}); code != http.StatusOK {
		t.Fatalf("deactivate status=%d error=%+v", code, failure)
	}
	if got := request(t, f.api, http.MethodPost, "/api/v1/auth/redeem", map[string]any{"token": linkToken(t, staleURL)}, "", nil).Code; got != http.StatusUnauthorized {
		t.Errorf("stale invite status=%d, want 401", got)
	}
	response := request(t, f.api, http.MethodPost, "/api/v1/auth/invites", map[string]any{"email": "ann@team.example.com", "role_ceiling": "agent"}, f.admin, nil)
	var failure apiError
	decode(t, response, &failure)
	if response.Code != http.StatusConflict || failure.Error.Code != "agent_deactivated" {
		t.Fatalf("invite for a deactivated agent status=%d error=%+v, want 409 agent_deactivated", response.Code, failure.Error)
	}
}

func TestDeactivatedAgentCannotBeAssignedButStaysVisibleInHistory(t *testing.T) {
	f := newTeamFixture(t)
	resolved := f.conversation(t, "Resolved one", "resolved", f.annID)
	f.patch(t, f.admin, f.annID, map[string]any{"active": false})

	other := create(t, f.api, f.admin, "/api/v1/conversations", map[string]any{"contact_id": f.contactID, "subject": "New"})
	response := request(t, f.api, http.MethodPatch, "/api/v1/conversations/"+other, map[string]any{"assignee_id": f.annID}, f.admin, nil)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("assign to a deactivated agent status=%d body=%s, want 400", response.Code, response.Body.String())
	}
	if got := f.assignee(t, resolved); got == nil || *got != f.annID {
		t.Fatalf("resolved assignee=%v, want ann still named", got)
	}
	if f.member(t, "ann@team.example.com").ID != f.annID {
		t.Fatalf("a deactivated agent must stay in the agent list so old work can name her")
	}
}

func TestRoleChangeAndLastAdminRule(t *testing.T) {
	f := newTeamFixture(t)

	if code, failure := f.patch(t, f.admin, f.annID, map[string]any{"role": "admin"}); code != http.StatusOK {
		t.Fatalf("promote status=%d error=%+v", code, failure)
	}
	if got := f.member(t, "ann@team.example.com").Role; got != "admin" {
		t.Fatalf("ann role=%q, want admin", got)
	}
	// Two admins now: the first may demote herself.
	if code, failure := f.patch(t, f.admin, f.adminID, map[string]any{"role": "agent"}); code != http.StatusOK {
		t.Fatalf("self demote with a second admin status=%d error=%+v", code, failure)
	}
	// Ann is the last admin: she can neither demote nor deactivate herself. Her old session is
	// capped at the agent role of her invite, so she signs in again with a pairing link.
	link, err := f.service.Pair(context.Background(), "team-desk", "ann@team.example.com")
	if err != nil {
		t.Fatal(err)
	}
	annAdmin := redeemLabelled(t, f.api, link, "Ann admin")
	for name, body := range map[string]map[string]any{"demote": {"role": "agent"}, "deactivate": {"active": false}} {
		code, failure := f.patch(t, annAdmin, f.annID, body)
		if code != http.StatusConflict || failure.Error.Code != "last_admin" {
			t.Errorf("last admin %s status=%d error=%+v, want 409 last_admin", name, code, failure.Error)
		}
	}
	if got := request(t, f.api, http.MethodGet, "/api/v1/auth/me", nil, annAdmin, nil).Code; got != http.StatusOK {
		t.Fatalf("the refused deactivation revoked the admin's session: status=%d", got)
	}
	// A request that also restates the active flag cannot sneak past the rule.
	code, failure := f.patch(t, annAdmin, f.annID, map[string]any{"role": "agent", "active": true})
	if code != http.StatusConflict || failure.Error.Code != "last_admin" {
		t.Errorf("combined demote status=%d error=%+v, want 409 last_admin", code, failure.Error)
	}
}

func TestAgentRoleCannotManageTheTeam(t *testing.T) {
	f := newTeamFixture(t)
	for _, body := range []map[string]any{{"role": "admin"}, {"active": false}} {
		if code, _ := f.patch(t, f.ann, f.bobID, body); code != http.StatusForbidden {
			t.Errorf("agent patch %v status=%d, want 403", body, code)
		}
	}
	if got := f.member(t, "bob@team.example.com"); got.Role != "agent" || got.DeactivatedAt != nil {
		t.Fatalf("bob=%+v, want unchanged", got)
	}
}

func TestAgentOfAnotherWorkspaceIsNotFoundAndUnchanged(t *testing.T) {
	f := newTeamFixture(t)
	f.conversation(t, "Open one", "open", f.annID)
	for _, body := range []map[string]any{{"role": "admin"}, {"active": false}} {
		if code, _ := f.patch(t, f.otherAdmin, f.annID, body); code != http.StatusNotFound {
			t.Errorf("foreign patch %v status=%d, want 404", body, code)
		}
	}
	ann := f.member(t, "ann@team.example.com")
	if ann.Role != "agent" || ann.DeactivatedAt != nil || ann.OpenConversations != 1 {
		t.Fatalf("ann=%+v, want untouched", ann)
	}
	if got := request(t, f.api, http.MethodGet, "/api/v1/auth/me", nil, f.ann, nil).Code; got != http.StatusOK {
		t.Fatalf("ann's session status=%d, want untouched", got)
	}
	// A foreign agent is also not a valid hand-over target.
	var me struct {
		Agent struct {
			ID string `json:"id"`
		} `json:"agent"`
	}
	decode(t, request(t, f.api, http.MethodGet, "/api/v1/auth/me", nil, f.otherAdmin, nil), &me)
	code, failure := f.patch(t, f.admin, f.annID, map[string]any{"active": false, "reassign_to": me.Agent.ID})
	if code != http.StatusBadRequest || failure.Error.Code != "invalid_reassign_target" {
		t.Fatalf("foreign reassign target status=%d error=%+v, want 400 invalid_reassign_target", code, failure.Error)
	}
}

func TestInvalidReassignTargetChangesNothing(t *testing.T) {
	f := newTeamFixture(t)
	open := f.conversation(t, "Open one", "open", f.annID)
	f.patch(t, f.admin, f.bobID, map[string]any{"active": false})
	unknown := "00000000-0000-4000-8000-000000000000"
	for name, target := range map[string]string{"unknown": unknown, "deactivated": f.bobID, "self": f.annID} {
		code, failure := f.patch(t, f.admin, f.annID, map[string]any{"role": "admin", "active": false, "reassign_to": target})
		if code != http.StatusBadRequest || failure.Error.Code != "invalid_reassign_target" {
			t.Errorf("%s target status=%d error=%+v, want 400 invalid_reassign_target", name, code, failure.Error)
		}
	}
	ann := f.member(t, "ann@team.example.com")
	if ann.Role != "agent" || ann.DeactivatedAt != nil || ann.OpenConversations != 1 {
		t.Fatalf("ann=%+v, want role, state and assignments unchanged", ann)
	}
	if got := f.assignee(t, open); got == nil || *got != f.annID {
		t.Fatalf("assignee=%v, want ann", got)
	}
	if got := request(t, f.api, http.MethodGet, "/api/v1/auth/me", nil, f.ann, nil).Code; got != http.StatusOK {
		t.Fatalf("session status=%d, want not revoked", got)
	}
}

func TestPatchValidatesTheBody(t *testing.T) {
	f := newTeamFixture(t)
	bodies := map[string]map[string]any{
		"empty":                  {},
		"unknown role":           {"role": "owner"},
		"reassign without leave": {"reassign_to": f.bobID},
		"reassign on reactivate": {"active": true, "reassign_to": f.bobID},
		"unknown field":          {"name": "x"},
	}
	for name, body := range bodies {
		if code, failure := f.patch(t, f.admin, f.annID, body); code != http.StatusBadRequest {
			t.Errorf("%s status=%d error=%+v, want 400", name, code, failure.Error)
		}
	}
}

func TestReactivateRestoresSignInButNotAssignments(t *testing.T) {
	f := newTeamFixture(t)
	open := f.conversation(t, "Open one", "open", f.annID)
	f.patch(t, f.admin, f.annID, map[string]any{"active": false})

	if code, failure := f.patch(t, f.admin, f.annID, map[string]any{"active": true}); code != http.StatusOK {
		t.Fatalf("reactivate status=%d error=%+v", code, failure)
	}
	if ann := f.member(t, "ann@team.example.com"); ann.DeactivatedAt != nil {
		t.Fatalf("ann=%+v, want active", ann)
	}
	if got := f.assignee(t, open); got != nil {
		t.Fatalf("assignee=%v, want the conversation to stay unassigned", *got)
	}
	// The old session stays revoked; she signs in again through a new link.
	if got := request(t, f.api, http.MethodGet, "/api/v1/auth/me", nil, f.ann, nil).Code; got != http.StatusUnauthorized {
		t.Errorf("old session status=%d, want 401", got)
	}
	link, err := f.service.Pair(context.Background(), "team-desk", "ann@team.example.com")
	if err != nil {
		t.Fatalf("pair after reactivation: %v", err)
	}
	if got := request(t, f.api, http.MethodPost, "/api/v1/auth/redeem", map[string]any{"token": linkToken(t, link)}, "", nil).Code; got != http.StatusOK {
		t.Errorf("redeem after reactivation status=%d, want 200", got)
	}
	// Repeating either transition changes nothing.
	if code, _ := f.patch(t, f.admin, f.annID, map[string]any{"active": true}); code != http.StatusOK {
		t.Errorf("reactivate an active agent status=%d, want 200", code)
	}
}

func TestDeactivationFreesASeatAndReactivationTakesItAgain(t *testing.T) {
	f := newLimitsFixture(t, true)
	f.upgrade(t, f.workspaceA, "sub_deactivation") // Team: ten seats
	ann := fmt.Sprintf("ann@%s", "limits-alpha.example.com")
	_, annURL := createContractInvite(t, f.api, f.adminA, ann, "agent")
	redeemLabelled(t, f.api, annURL, "Ann laptop")
	// Admin + Ann + eight pending invites fill the ten seats of the Team plan.
	for number := 1; number <= 8; number++ {
		if got := invite(t, f, f.adminA, number); got.Code != http.StatusCreated {
			t.Fatalf("invite %d status=%d body=%s", number, got.Code, got.Body)
		}
	}
	if got := invite(t, f, f.adminA, 9); got.Code != http.StatusPaymentRequired {
		t.Fatalf("eleventh seat status=%d, want 402", got.Code)
	}
	annID := memberIDByEmail(t, f.api, f.adminA, ann)

	response := request(t, f.api, http.MethodPatch, "/api/v1/agents/"+annID, map[string]any{"active": false}, f.adminA, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("deactivate status=%d body=%s", response.Code, response.Body.String())
	}
	if got := f.usage(t, f.adminA).Usage.Agents; got != 1 {
		t.Fatalf("usage.agents=%d, want 1: a deactivated agent holds no seat", got)
	}
	if got := invite(t, f, f.adminA, 9); got.Code != http.StatusCreated {
		t.Fatalf("invite into the freed seat status=%d body=%s, want 201", got.Code, got.Body)
	}

	// The freed seat is taken again, so reactivation is refused until a seat opens.
	response = request(t, f.api, http.MethodPatch, "/api/v1/agents/"+annID, map[string]any{"active": true}, f.adminA, nil)
	var failure apiError
	decode(t, response, &failure)
	if response.Code != http.StatusPaymentRequired || failure.Error.Code != "plan_limit_reached" {
		t.Fatalf("reactivate on a full plan status=%d error=%+v, want 402 plan_limit_reached", response.Code, failure.Error)
	}
	if f.stillDeactivated(t, annID) != true {
		t.Fatalf("a refused reactivation must leave the agent deactivated")
	}
	var list struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	decode(t, request(t, f.api, http.MethodGet, "/api/v1/auth/invites", nil, f.adminA, nil), &list)
	request(t, f.api, http.MethodDelete, "/api/v1/auth/invites/"+list.Items[0].ID, nil, f.adminA, nil)
	if got := request(t, f.api, http.MethodPatch, "/api/v1/agents/"+annID, map[string]any{"active": true}, f.adminA, nil).Code; got != http.StatusOK {
		t.Fatalf("reactivate after freeing a seat status=%d, want 200", got)
	}
	if got := f.usage(t, f.adminA).Usage.Agents; got != 2 {
		t.Fatalf("usage.agents=%d, want 2 after reactivation", got)
	}
}

func (f limitsFixture) stillDeactivated(t *testing.T, id string) bool {
	t.Helper()
	var body struct {
		Items []teamMemberView `json:"items"`
	}
	decode(t, request(t, f.api, http.MethodGet, "/api/v1/agents", nil, f.adminA, nil), &body)
	for _, item := range body.Items {
		if item.ID == id {
			return item.DeactivatedAt != nil
		}
	}
	return false
}

func TestExportKeepsDeactivatedAgentsWithTheirState(t *testing.T) {
	f := newTeamFixture(t)
	f.patch(t, f.admin, f.annID, map[string]any{"active": false})
	response := request(t, f.api, http.MethodGet, "/api/v1/export", nil, f.admin, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("export status=%d body=%s", response.Code, response.Body.String())
	}
	byEmail := map[string]map[string]any{}
	for _, line := range ndjsonLines(t, unzip(t, response.Body.Bytes())["agents.ndjson"]) {
		byEmail[line["email"].(string)] = line
	}
	if len(byEmail) != 3 {
		t.Fatalf("exported agents=%d, want all 3 including the deactivated one", len(byEmail))
	}
	if got := byEmail["ann@team.example.com"]["deactivated_at"]; got == nil {
		t.Errorf("ann deactivated_at=nil, want a timestamp")
	}
	if got, present := byEmail["bob@team.example.com"]["deactivated_at"]; !present || got != nil {
		t.Errorf("bob deactivated_at=%v present=%v, want an explicit null", got, present)
	}
}
