package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
)

type namedMember struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
}

func displayNameOf(t *testing.T, f teamFixture, token, email string) string {
	t.Helper()
	var body struct {
		Items []namedMember `json:"items"`
	}
	decode(t, request(t, f.api, http.MethodGet, "/api/v1/agents", nil, token, nil), &body)
	for _, item := range body.Items {
		if item.Email == email {
			return item.DisplayName
		}
	}
	t.Fatalf("no agent %s in the team list", email)
	return ""
}

func myDisplayName(t *testing.T, f teamFixture, token string) string {
	t.Helper()
	var me struct {
		Agent namedMember `json:"agent"`
	}
	decode(t, request(t, f.api, http.MethodGet, "/api/v1/auth/me", nil, token, nil), &me)
	return me.Agent.DisplayName
}

func TestAgentSetsAndClearsTheirOwnDisplayName(t *testing.T) {
	f := newTeamFixture(t)
	path := "/api/v1/agents/me/display-name"

	set := newRequest(t, http.MethodPatch, path, map[string]any{"display_name": "  Ann Lee  "}, f.ann, nil)
	response := serve(f.api, set)
	if response.Code != http.StatusOK {
		t.Fatalf("set own display name status=%d body=%s", response.Code, response.Body.String())
	}
	validateContractResponse(t, newRequest(t, http.MethodPatch, path, map[string]any{"display_name": "  Ann Lee  "}, f.ann, nil), response)
	var agent namedMember
	decode(t, response, &agent)
	if agent.DisplayName != "Ann Lee" || agent.Email != "ann@team.example.com" {
		t.Fatalf("answer = %+v, want the trimmed name Ann Lee on Ann", agent)
	}
	if got := myDisplayName(t, f, f.ann); got != "Ann Lee" {
		t.Fatalf("GET /auth/me display_name = %q, want Ann Lee", got)
	}
	if got := displayNameOf(t, f, f.admin, "ann@team.example.com"); got != "Ann Lee" {
		t.Fatalf("team list display_name = %q, want Ann Lee", got)
	}
	if got := displayNameOf(t, f, f.admin, "bob@team.example.com"); got != "" {
		t.Fatalf("Bob's display name changed to %q", got)
	}

	cleared := request(t, f.api, http.MethodPatch, path, map[string]any{"display_name": "   "}, f.ann, nil)
	if cleared.Code != http.StatusOK {
		t.Fatalf("clear own display name status=%d body=%s", cleared.Code, cleared.Body.String())
	}
	if got := myDisplayName(t, f, f.ann); got != "" {
		t.Fatalf("after clearing, display_name = %q, want empty (the email is shown)", got)
	}
}

func TestDisplayNameIsValidatedOnEveryRoute(t *testing.T) {
	f := newTeamFixture(t)
	eighty := strings.Repeat("é", 80)
	routes := []struct{ name, path string }{
		{"self", "/api/v1/agents/me/display-name"},
		{"admin", "/api/v1/agents/" + f.annID},
	}
	for _, route := range routes {
		token := f.admin
		for _, bad := range []any{strings.Repeat("a", 81), "Ann\nLee", "Ann\x00", "Ann‮eeL", 42} {
			response := request(t, f.api, http.MethodPatch, route.path, map[string]any{"display_name": bad}, token, nil)
			if response.Code != http.StatusBadRequest {
				t.Errorf("%s display_name %q status=%d body=%s, want 400", route.name, bad, response.Code, response.Body.String())
			}
		}
		ok := request(t, f.api, http.MethodPatch, route.path, map[string]any{"display_name": eighty}, token, nil)
		if ok.Code != http.StatusOK {
			t.Errorf("%s display_name of 80 characters status=%d body=%s, want 200", route.name, ok.Code, ok.Body.String())
		}
	}
	missing := request(t, f.api, http.MethodPatch, "/api/v1/agents/me/display-name", map[string]any{}, f.ann, nil)
	if missing.Code != http.StatusBadRequest {
		t.Errorf("missing display_name status=%d, want 400", missing.Code)
	}
	null := request(t, f.api, http.MethodPatch, "/api/v1/agents/me/display-name", map[string]any{"display_name": nil}, f.ann, nil)
	if null.Code != http.StatusBadRequest {
		t.Errorf("null display_name status=%d, want 400 (send an empty string to clear)", null.Code)
	}
	if got := myDisplayName(t, f, f.ann); got != eighty {
		t.Fatalf("a refused change was stored: display_name = %q", got)
	}
}

func TestAdminRenamesAnotherAgentAndClearsTheName(t *testing.T) {
	f := newTeamFixture(t)
	status, failure := f.patch(t, f.admin, f.annID, map[string]any{"display_name": " Ann from Support "})
	if status != http.StatusOK {
		t.Fatalf("admin rename status=%d error=%+v", status, failure)
	}
	if got := displayNameOf(t, f, f.admin, "ann@team.example.com"); got != "Ann from Support" {
		t.Fatalf("team list display_name = %q", got)
	}
	if got := myDisplayName(t, f, f.ann); got != "Ann from Support" {
		t.Fatalf("Ann's /auth/me display_name = %q", got)
	}
	if member := f.member(t, "ann@team.example.com"); member.Role.main() != "agent" || member.DeactivatedAt != nil {
		t.Fatalf("a rename changed the role or state: %+v", member)
	}
	if status, failure := f.patch(t, f.admin, f.annID, map[string]any{"display_name": ""}); status != http.StatusOK {
		t.Fatalf("admin clear status=%d error=%+v", status, failure)
	}
	if got := displayNameOf(t, f, f.admin, "ann@team.example.com"); got != "" {
		t.Fatalf("after clearing, display_name = %q", got)
	}
}

func TestAgentRoleCannotRenameSomeoneElse(t *testing.T) {
	f := newTeamFixture(t)
	if status, _ := f.patch(t, f.ann, f.bobID, map[string]any{"display_name": "Bobby"}); status != http.StatusForbidden {
		t.Fatalf("agent renaming a teammate status=%d, want 403", status)
	}
	if got := displayNameOf(t, f, f.admin, "bob@team.example.com"); got != "" {
		t.Fatalf("Bob was renamed to %q by another agent", got)
	}
}

func TestAdminCannotRenameAnAgentOfAnotherWorkspace(t *testing.T) {
	f := newTeamFixture(t)
	if status, _ := f.patch(t, f.otherAdmin, f.annID, map[string]any{"display_name": "Taken over"}); status != http.StatusNotFound {
		t.Fatalf("cross-workspace rename status=%d, want 404", status)
	}
	if got := displayNameOf(t, f, f.admin, "ann@team.example.com"); got != "" {
		t.Fatalf("another workspace renamed Ann to %q", got)
	}
}

func TestDisplayNameThroughAgentTokens(t *testing.T) {
	f := newTeamFixture(t)
	// The token owner's own name: profile:update sets it; a token without it is refused and nothing changes.
	draft := issueToken(t, f.api, f.ann, "Ann's assistant", "draft").Token
	if response := request(t, f.api, http.MethodPatch, "/api/v1/agents/me/display-name", map[string]any{"display_name": "Ann L."}, draft, nil); response.Code != http.StatusOK {
		t.Fatalf("draft token self rename status=%d body=%s", response.Code, response.Body.String())
	}
	read := issueToken(t, f.api, f.ann, "Ann's reader", "read").Token
	if from := refused(t, f.api, http.MethodPatch, "/api/v1/agents/me/display-name", map[string]any{"display_name": "Annie"}, read, "profile:update"); from != "token" {
		t.Errorf("a read token's rename is missing from %q, want token (Ann holds profile:update)", from)
	}
	if got := myDisplayName(t, f, f.ann); got != "Ann L." {
		t.Fatalf("a refused rename changed the name: %q", got)
	}

	// An Admin token renames anyone, an admin included, at once; a role sent with it is refused.
	adminToken := issueToken(t, f.api, f.admin, "Ops", "admin").Token
	for _, id := range []string{f.bobID, f.adminID} {
		if status, failure := f.patch(t, adminToken, id, map[string]any{"display_name": "Renamed"}); status != http.StatusOK {
			t.Fatalf("admin token rename of %s status=%d error=%+v", id, status, failure)
		}
	}
	if response := request(t, f.api, http.MethodPatch, "/api/v1/agents/"+f.bobID, map[string]any{"role": "admin", "display_name": "Boss Bob"}, adminToken, nil); response.Code != http.StatusBadRequest {
		t.Fatalf("a role sent to PATCH /agents/{id} status=%d, want 400 (roles change with PUT /agents/{id}/roles)", response.Code)
	}
	send := issueToken(t, f.api, f.admin, "Sender", "send").Token
	refused(t, f.api, http.MethodPatch, "/api/v1/agents/"+f.annID, map[string]any{"display_name": "Ann Q."}, send, "team:manage_roles")
	if got := displayNameOf(t, f, f.admin, "ann@team.example.com"); got != "Ann L." {
		t.Fatalf("a refused rename renamed Ann to %q", got)
	}
}
