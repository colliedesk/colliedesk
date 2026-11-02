package httpapi_test

import (
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/auth"
)

type roleView struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	System      *string  `json:"system"`
	Permissions []string `json:"permissions"`
	Defaults    []string `json:"default_permissions"`
	Editable    bool     `json:"editable"`
	Deletable   bool     `json:"deletable"`
	Assignable  bool     `json:"assignable"`
	Members     []struct {
		Name string `json:"name"`
	} `json:"members"`
	Tokens []struct {
		Label   string `json:"label"`
		Owner   string `json:"owner"`
		OwnerID string `json:"owner_id"`
	} `json:"tokens"`
	UpdatedAt string `json:"updated_at"`
}

func roleOf(t *testing.T, api http.Handler, token, key string) roleView {
	t.Helper()
	var role roleView
	kbCall(t, api, http.MethodGet, "/api/v1/roles/"+url.PathEscape(key), nil, token, http.StatusOK, &role)
	return role
}

func setRoles(t *testing.T, api http.Handler, token, agentID string, names ...string) *recorderView {
	t.Helper()
	response := request(t, api, http.MethodPut, "/api/v1/agents/"+agentID+"/roles", map[string]any{"roles": names}, token, nil)
	return &recorderView{code: response.Code, body: response.Body.String()}
}

type recorderView struct {
	code int
	body string
}

func myPermissions(t *testing.T, api http.Handler, token string) []string {
	t.Helper()
	var me struct {
		Permissions []string `json:"permissions"`
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/auth/me", nil, token, nil), &me)
	return me.Permissions
}

func TestEveryWorkspaceStartsWithAdminAndAgentAndMembersHoldWhatTheyHad(t *testing.T) {
	desk := newRequestDesk(t, "Roles Start")
	agent := joinAsAgent(t, desk.api, desk.admin, "agent@roles-start.example.com")
	var roles struct {
		Items []roleView `json:"items"`
	}
	kbCall(t, desk.api, http.MethodGet, "/api/v1/roles", nil, agent, http.StatusOK, &roles)
	if len(roles.Items) != 2 || roles.Items[0].Name != "Admin" || roles.Items[1].Name != "Agent" ||
		len(roles.Items[0].Permissions) != len(auth.AllPermissions()) || len(roles.Items[1].Permissions) != 42 {
		t.Fatalf("roles = %+v, want Admin with every permission and Agent with 42", roles.Items)
	}
	if roles.Items[0].Defaults != nil || !slices.Equal(roles.Items[1].Defaults, auth.AgentRolePermissions()) {
		t.Fatalf("defaults: Admin %v, Agent %v; want none and the Agent role's built-in list", roles.Items[0].Defaults, roles.Items[1].Defaults)
	}
	if roles.Items[0].Editable || roles.Items[1].Editable || roles.Items[1].Assignable {
		t.Fatalf("an agent may change or give a role: %+v", roles.Items)
	}
	if got := myPermissions(t, desk.api, agent); !slices.Equal(slices.Sorted(slices.Values(got)), slices.Sorted(slices.Values(auth.AgentRolePermissions()))) {
		t.Fatalf("an agent may do %v, want the Agent role's", got)
	}
	if got := myPermissions(t, desk.api, desk.admin); len(got) != len(auth.AllPermissions()) {
		t.Fatalf("the admin may do %d permissions, want every one", len(got))
	}
}

func TestARoleGivesItsPermissionsAndAMemberHoldsEveryRole(t *testing.T) {
	desk := newRequestDesk(t, "Roles Give")
	agent := joinAsAgent(t, desk.api, desk.admin, "agent@roles-give.example.com")
	agentID := memberIDByEmail(t, desk.api, desk.admin, "agent@roles-give.example.com")
	refused(t, desk.api, http.MethodPost, "/api/v1/kb/media/purge", map[string]any{"ids": []string{}}, agent, "kb_media:purge")
	kbCall(t, desk.api, http.MethodPost, "/api/v1/roles", map[string]any{"name": "Media cleaner", "permissions": []string{"kb_media:purge"}}, desk.admin, http.StatusCreated, nil)
	if got := setRoles(t, desk.api, desk.admin, agentID, "Agent", "media cleaner"); got.code != http.StatusOK || !strings.Contains(got.body, "Media cleaner") {
		t.Fatalf("give the role: %d %s", got.code, got.body)
	}
	if got := myPermissions(t, desk.api, agent); !slices.Contains(got, "kb_media:purge") || !slices.Contains(got, "messages:send") {
		t.Fatalf("the agent may do %v, want the Agent role's and kb_media:purge", got)
	}
	// Changing the role changes what its members may do at once.
	role := roleOf(t, desk.api, desk.admin, "Media cleaner")
	kbCall(t, desk.api, http.MethodPatch, "/api/v1/roles/"+role.ID, map[string]any{"permissions": []string{"kb_media:read"}}, desk.admin, http.StatusOK, nil)
	if got := myPermissions(t, desk.api, agent); slices.Contains(got, "kb_media:purge") {
		t.Fatalf("after the change the agent may still purge: %v", got)
	}
	// A role in use is not deleted; once nobody holds it, it is, and it comes back.
	response := request(t, desk.api, http.MethodDelete, "/api/v1/roles/"+role.ID, nil, desk.admin, nil)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "role_in_use") || !strings.Contains(response.Body.String(), "in use by 1 person (agent@roles-give.example.com)") {
		t.Fatalf("delete a role in use: %d %s, want 409 role_in_use naming who holds it", response.Code, response.Body.String())
	}
	if got := setRoles(t, desk.api, desk.admin, agentID, "Agent"); got.code != http.StatusOK {
		t.Fatalf("take the role: %d %s", got.code, got.body)
	}
	kbCall(t, desk.api, http.MethodDelete, "/api/v1/roles/"+role.ID, nil, desk.admin, http.StatusNoContent, nil)
	kbCall(t, desk.api, http.MethodPost, "/api/v1/roles/"+role.ID+"/restore", nil, desk.admin, http.StatusOK, nil)
	for _, system := range []string{"Admin", "Agent"} {
		if response := request(t, desk.api, http.MethodDelete, "/api/v1/roles/"+system, nil, desk.admin, nil); response.Code != http.StatusConflict ||
			!strings.Contains(response.Body.String(), "system_role_locked") {
			t.Errorf("delete %s: %d %s, want 409 system_role_locked", system, response.Code, response.Body.String())
		}
	}
	if response := request(t, desk.api, http.MethodPatch, "/api/v1/roles/Admin", map[string]any{"permissions": []string{}}, desk.admin, nil); response.Code != http.StatusConflict {
		t.Errorf("change Admin: %d %s, want 409", response.Code, response.Body.String())
	}
	// Names are unique without regard to case.
	if response := request(t, desk.api, http.MethodPost, "/api/v1/roles", map[string]any{"name": "MEDIA cleaner", "permissions": []string{}}, desk.admin, nil); response.Code != http.StatusConflict ||
		!strings.Contains(response.Body.String(), "role_name_taken") {
		t.Errorf("a taken name: %d %s", response.Code, response.Body.String())
	}
}

func TestNoOneGrantsWhatTheyDoNotHold(t *testing.T) {
	desk := newRequestDesk(t, "Roles Grant")
	lead := joinAsAgent(t, desk.api, desk.admin, "lead@roles-grant.example.com")
	leadID := memberIDByEmail(t, desk.api, desk.admin, "lead@roles-grant.example.com")
	agentID := memberIDByEmail(t, desk.api, desk.admin, memberEmail(t, desk.api, desk.admin, joinAsAgent(t, desk.api, desk.admin, "agent@roles-grant.example.com")))
	kbCall(t, desk.api, http.MethodPost, "/api/v1/roles", map[string]any{"name": "Team lead", "permissions": []string{"team:manage_roles", "team:read", "kb_articles:read"}}, desk.admin, http.StatusCreated, nil)
	kbCall(t, desk.api, http.MethodPost, "/api/v1/roles", map[string]any{"name": "Exporter", "permissions": []string{"workspace:export"}}, desk.admin, http.StatusCreated, nil)
	if got := setRoles(t, desk.api, desk.admin, leadID, "Agent", "Team lead"); got.code != http.StatusOK {
		t.Fatalf("make a lead: %d %s", got.code, got.body)
	}
	for name, call := range map[string]struct {
		method, path string
		body         any
	}{
		"create a role with more":     {http.MethodPost, "/api/v1/roles", map[string]any{"name": "Sneaky", "permissions": []string{"workspace:export"}}},
		"widen a role":                {http.MethodPatch, "/api/v1/roles/Team%20lead", map[string]any{"permissions": []string{"team:manage_roles", "team:read", "billing:manage"}}},
		"give a role beyond theirs":   {http.MethodPut, "/api/v1/agents/" + agentID + "/roles", map[string]any{"roles": []string{"Agent", "Exporter"}}},
		"give themselves Admin":       {http.MethodPut, "/api/v1/agents/" + leadID + "/roles", map[string]any{"roles": []string{"Admin"}}},
		"change a role beyond theirs": {http.MethodPatch, "/api/v1/roles/Exporter", map[string]any{"name": "Exports"}},
	} {
		response := request(t, desk.api, call.method, call.path, call.body, lead, nil)
		if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "role_exceeds_yours") || !strings.Contains(response.Body.String(), "missing_permissions") {
			t.Errorf("%s: %d %s, want 403 role_exceeds_yours naming what is missing", name, response.Code, response.Body.String())
		}
	}
	// Within what they hold, the lead gives roles.
	kbCall(t, desk.api, http.MethodPost, "/api/v1/roles", map[string]any{"name": "Readers", "permissions": []string{"kb_articles:read"}}, lead, http.StatusCreated, nil)
	if got := setRoles(t, desk.api, lead, agentID, "Agent", "Readers"); got.code != http.StatusOK {
		t.Fatalf("the lead gives a role within theirs: %d %s", got.code, got.body)
	}
	// A token makes no token, and a custom token never holds more than its person.
	response := request(t, desk.api, http.MethodPost, "/api/v1/auth/tokens", map[string]any{"label": "x", "permissions": []string{"workspace:export"}}, lead, nil)
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "role_exceeds_yours") {
		t.Fatalf("a token beyond the lead: %d %s", response.Code, response.Body.String())
	}
}

func TestNoOneTouchesSomeoneWhoOutranksThem(t *testing.T) {
	desk := newRequestDesk(t, "Roles Outrank")
	lead := joinAsAgent(t, desk.api, desk.admin, "lead@roles-outrank.example.com")
	leadID := memberIDByEmail(t, desk.api, desk.admin, "lead@roles-outrank.example.com")
	adminID := memberIDByEmail(t, desk.api, desk.admin, "admin@roles-outrank.example.com")
	kbCall(t, desk.api, http.MethodPost, "/api/v1/roles", map[string]any{"name": "Lead", "permissions": []string{"team:manage_roles", "team:read", "team:revoke"}}, desk.admin, http.StatusCreated, nil)
	setRoles(t, desk.api, desk.admin, leadID, "Agent", "Lead")
	for name, call := range map[string]struct {
		method, path string
		body         any
	}{
		"rename the admin":      {http.MethodPatch, "/api/v1/agents/" + adminID, map[string]any{"display_name": "Gone"}},
		"deactivate the admin":  {http.MethodPatch, "/api/v1/agents/" + adminID, map[string]any{"active": false}},
		"change the admin role": {http.MethodPut, "/api/v1/agents/" + adminID + "/roles", map[string]any{"roles": []string{"Agent"}}},
		"sign the admin out":    {http.MethodDelete, "/api/v1/auth/sessions/" + queryString(t, desk.pool, `SELECT id::text FROM sessions WHERE agent_id=$1 LIMIT 1`, adminID), nil},
	} {
		response := request(t, desk.api, call.method, call.path, call.body, lead, nil)
		if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "member_outranks_you") {
			t.Errorf("%s: %d %s, want 403 member_outranks_you", name, response.Code, response.Body.String())
		}
	}
	var team struct {
		Items []struct {
			Email      string `json:"email"`
			Manageable bool   `json:"manageable"`
		} `json:"items"`
	}
	kbCall(t, desk.api, http.MethodGet, "/api/v1/agents", nil, lead, http.StatusOK, &team)
	for _, member := range team.Items {
		if want := member.Email != "admin@roles-outrank.example.com"; member.Manageable != want {
			t.Errorf("%s manageable=%v for the lead, want %v", member.Email, member.Manageable, want)
		}
	}
}

func TestAWorkspaceAlwaysKeepsAnActiveAdmin(t *testing.T) {
	desk := newRequestDesk(t, "Roles Last")
	adminID := memberIDByEmail(t, desk.api, desk.admin, "admin@roles-last.example.com")
	if got := setRoles(t, desk.api, desk.admin, adminID, "Agent"); got.code != http.StatusConflict || !strings.Contains(got.body, "last_admin") {
		t.Fatalf("take Admin from the last admin: %d %s", got.code, got.body)
	}
	// Two admins who each take the role from the other at the same moment: one keeps it.
	second := joinAsAgent(t, desk.api, desk.admin, "second@roles-last.example.com")
	secondID := memberIDByEmail(t, desk.api, desk.admin, "second@roles-last.example.com")
	if got := setRoles(t, desk.api, desk.admin, secondID, "Admin"); got.code != http.StatusOK {
		t.Fatalf("make a second admin: %d %s", got.code, got.body)
	}
	var wait sync.WaitGroup
	codes := make([]int, 2)
	for index, pair := range [][2]string{{desk.admin, secondID}, {second, adminID}} {
		wait.Add(1)
		go func() {
			defer wait.Done()
			response := request(t, desk.api, http.MethodPut, "/api/v1/agents/"+pair[1]+"/roles", map[string]any{"roles": []string{"Agent"}}, pair[0], nil)
			codes[index] = response.Code
		}()
	}
	wait.Wait()
	admins := queryString(t, desk.pool, `SELECT count(*)::text FROM agent_roles ar JOIN roles r ON r.id=ar.role_id JOIN agents a ON a.id=ar.agent_id
		WHERE r.system='admin' AND a.workspace_id=(SELECT workspace_id FROM agents WHERE id=$1) AND a.deactivated_at IS NULL`, adminID)
	if admins == "0" {
		t.Fatalf("both admins took the role from each other (%v): the workspace has no admin", codes)
	}
}

func TestRolesStayInTheirWorkspace(t *testing.T) {
	alpha := newRequestDesk(t, "Roles Alpha")
	beta := createSession(t, alpha.api, alpha.service, "Roles Beta", "admin@roles-beta.example.com")
	var betaRole roleView
	kbCall(t, alpha.api, http.MethodPost, "/api/v1/roles", map[string]any{"name": "Beta only", "permissions": []string{"tags:read"}}, beta, http.StatusCreated, &betaRole)
	alphaAgentID := memberIDByEmail(t, alpha.api, alpha.admin, memberEmail(t, alpha.api, alpha.admin, joinAsAgent(t, alpha.api, alpha.admin, "agent@roles-alpha.example.com")))
	for _, call := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/api/v1/roles/" + betaRole.ID, nil},
		{http.MethodPatch, "/api/v1/roles/" + betaRole.ID, map[string]any{"name": "Taken"}},
		{http.MethodDelete, "/api/v1/roles/" + betaRole.ID, nil},
		{http.MethodPost, "/api/v1/roles/" + betaRole.ID + "/restore", nil},
		{http.MethodPost, "/api/v1/roles/" + betaRole.ID + "/reset", nil},
		{http.MethodPut, "/api/v1/agents/" + alphaAgentID + "/roles", map[string]any{"role_ids": []string{betaRole.ID}}},
		{http.MethodPost, "/api/v1/auth/invites", map[string]any{"email": "x@roles-alpha.example.com", "role_ids": []string{betaRole.ID}}},
		{http.MethodPost, "/api/v1/auth/tokens", map[string]any{"label": "x", "role_id": betaRole.ID}},
	} {
		response := request(t, alpha.api, call.method, call.path, call.body, alpha.admin, nil)
		if response.Code != http.StatusNotFound && response.Code != http.StatusForbidden || strings.Contains(response.Body.String(), "Beta only") {
			t.Errorf("%s %s with another workspace's role: %d %s, want 404 (or 403 role_not_yours) and nothing of it", call.method, call.path, response.Code, response.Body.String())
		}
	}
	var roles struct {
		Items []roleView `json:"items"`
	}
	kbCall(t, alpha.api, http.MethodGet, "/api/v1/roles", nil, alpha.admin, http.StatusOK, &roles)
	for _, role := range roles.Items {
		if role.ID == betaRole.ID {
			t.Fatal("a workspace lists another workspace's role")
		}
	}
	if got := roleOf(t, alpha.api, beta, betaRole.ID); got.Name != "Beta only" {
		t.Fatalf("beta's role changed: %+v", got)
	}
}

func TestPermissionMissingSaysWhatAndHow(t *testing.T) {
	desk := newRequestDesk(t, "Roles Missing")
	agent := joinAsAgent(t, desk.api, desk.admin, "agent@roles-missing.example.com")
	response := request(t, desk.api, http.MethodGet, "/api/v1/export", nil, agent, nil)
	missing, names, from := permissionMissing(t, response)
	if !missing || !slices.Equal(names, []string{"workspace:export"}) || from != "person" || !strings.Contains(response.Body.String(), `"ask":["admin@roles-missing.example.com"]`) {
		t.Fatalf("a person without the permission: %d %s, want missing_from person and who to ask", response.Code, response.Body.String())
	}
	token := customToken(t, desk.api, desk.admin, "Reader", "conversations:read")
	response = request(t, desk.api, http.MethodPost, "/api/v1/conversations/"+desk.conversation+"/messages", map[string]any{"kind": "outgoing", "body": "Hi"}, token, nil)
	missing, names, from = permissionMissing(t, response)
	if !missing || !slices.Equal(names, []string{"messages:send"}) || from != "token" || !strings.Contains(response.Body.String(), "External AI agents") ||
		!strings.Contains(response.Body.String(), "reply-drafts") {
		t.Fatalf("a token without messages:send: %d %s, want missing_from token, where to make one, and the draft route", response.Code, response.Body.String())
	}
	validateContractResponse(t, newRequest(t, http.MethodPost, "/api/v1/conversations/"+desk.conversation+"/messages", map[string]any{"kind": "outgoing", "body": "Hi"}, token, nil), response)
}

func TestARoleTokenStopsWhileItsPersonLacksTheRoleAndStartsAgain(t *testing.T) {
	desk := newRequestDesk(t, "Roles Token")
	agent := joinAsAgent(t, desk.api, desk.admin, "agent@roles-token.example.com")
	agentID := memberIDByEmail(t, desk.api, desk.admin, "agent@roles-token.example.com")
	kbCall(t, desk.api, http.MethodPost, "/api/v1/roles", map[string]any{"name": "Tag keeper", "permissions": []string{"tags:read", "tags:create"}}, desk.admin, http.StatusCreated, nil)
	setRoles(t, desk.api, desk.admin, agentID, "Agent", "Tag keeper")
	token := issueTokenWith(t, desk.api, agent, map[string]any{"label": "Tagger", "role_id": roleID(t, desk.api, agent, "Tag keeper")}).Token
	if got := myPermissions(t, desk.api, token); !slices.Equal(got, []string{"tags:read", "tags:create"}) {
		t.Fatalf("a token following Tag keeper may do %v", got)
	}
	refused(t, desk.api, http.MethodGet, "/api/v1/conversations", nil, token, "conversations:read")
	var role roleView
	kbCall(t, desk.api, http.MethodGet, "/api/v1/roles/Tag%20keeper", nil, desk.admin, http.StatusOK, &role)
	if len(role.Tokens) != 1 || role.Tokens[0].Label != "Tagger" || role.Tokens[0].Owner != "agent@roles-token.example.com" || role.Tokens[0].OwnerID != agentID {
		t.Fatalf("the role names %+v, want the token following it", role.Tokens)
	}
	setRoles(t, desk.api, desk.admin, agentID, "Agent")
	response := request(t, desk.api, http.MethodGet, "/api/v1/tags", nil, token, nil)
	if missing, _, from := permissionMissing(t, response); !missing || from != "person" || !strings.Contains(response.Body.String(), "no longer holds") {
		t.Fatalf("a token whose person lost its role: %d %s", response.Code, response.Body.String())
	}
	var sessions struct {
		Items []struct {
			Label string `json:"label"`
			Token *struct {
				Kind     string `json:"kind"`
				RoleHeld bool   `json:"role_held"`
			} `json:"token"`
		} `json:"items"`
	}
	kbCall(t, desk.api, http.MethodGet, "/api/v1/auth/sessions", nil, agent, http.StatusOK, &sessions)
	for _, item := range sessions.Items {
		if item.Label == "Tagger" && (item.Token == nil || item.Token.Kind != "role" || item.Token.RoleHeld) {
			t.Fatalf("the stopped token lists as %+v", item.Token)
		}
	}
	setRoles(t, desk.api, desk.admin, agentID, "Agent", "Tag keeper")
	kbCall(t, desk.api, http.MethodGet, "/api/v1/tags", nil, token, http.StatusOK, nil)
}

func TestAWrittenReplyDraftWaitsForAPersonToSend(t *testing.T) {
	desk := newRequestDesk(t, "Roles Drafts")
	path := "/api/v1/conversations/" + desk.conversation + "/reply-drafts"
	reader := customToken(t, desk.api, desk.admin, "Reader", "conversations:read")
	refused(t, desk.api, http.MethodPost, path, map[string]any{"text": "Hi"}, reader, "ai_assist:create")
	writer := issueToken(t, desk.api, desk.admin, "Writer", "draft").Token
	if response := request(t, desk.api, http.MethodPost, path, map[string]any{"text": "Hi", "cc": []string{"stranger@elsewhere.example"}}, writer, nil); response.Code != http.StatusBadRequest {
		t.Fatalf("a draft token copies someone new: %d %s, want 400", response.Code, response.Body.String())
	}
	var draft struct {
		ID        string `json:"id"`
		Status    string `json:"status"`
		Source    string `json:"source"`
		WrittenBy *struct {
			TokenLabel *string `json:"token_label"`
			AgentName  string  `json:"agent_name"`
		} `json:"written_by"`
	}
	kbCall(t, desk.api, http.MethodPost, path, map[string]any{"text": "Thanks, it is fixed."}, writer, http.StatusCreated, &draft)
	if draft.Status != "ready" || draft.Source != "written" || draft.WrittenBy == nil || draft.WrittenBy.TokenLabel == nil || *draft.WrittenBy.TokenLabel != "Writer" {
		t.Fatalf("draft = %+v", draft)
	}
	var messages struct {
		Items []struct{} `json:"items"`
	}
	decode(t, request(t, desk.api, http.MethodGet, "/api/v1/conversations/"+desk.conversation+"/messages", nil, desk.admin, nil), &messages)
	if len(messages.Items) != 0 {
		t.Fatal("a draft sent something")
	}
	var filtered struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	decode(t, request(t, desk.api, http.MethodGet, "/api/v1/conversations?has_pending_draft=true", nil, desk.admin, nil), &filtered)
	if len(filtered.Items) != 1 || filtered.Items[0].ID != desk.conversation {
		t.Fatalf("has_pending_draft = %+v", filtered.Items)
	}
	kbCall(t, desk.api, http.MethodPost, "/api/v1/conversations/"+desk.conversation+"/messages", map[string]any{"kind": "outgoing", "body": "Thanks, it is fixed.", "ai_draft_id": draft.ID}, desk.admin, http.StatusCreated, nil)
}

// A member's permissions are read with the session: holding several roles costs no extra statement.
func TestAuthenticateWithRolesStaysOneQuery(t *testing.T) {
	counter := &queryCounter{}
	f := newLimitsFixtureTraced(t, false, counter)
	base, _ := counter.measure(t, f, "/api/v1/auth/me", f.adminA)
	for _, name := range []string{"One", "Two", "Three"} {
		kbCall(t, f.api, http.MethodPost, "/api/v1/roles", map[string]any{"name": name, "permissions": []string{"tags:read"}}, f.adminA, http.StatusCreated, nil)
	}
	adminID := memberIDByEmail(t, f.api, f.adminA, memberEmail(t, f.api, f.adminA, f.adminA))
	if got := setRoles(t, f.api, f.adminA, adminID, "Admin", "One", "Two", "Three"); got.code != http.StatusOK {
		t.Fatalf("give roles: %d %s", got.code, got.body)
	}
	if after, _ := counter.measure(t, f, "/api/v1/auth/me", f.adminA); after != base {
		t.Fatalf("GET /auth/me with four roles costs %d statements, %d with one", after, base)
	}
}

// No way of adding someone leaves them without a role.
func TestNoActiveMemberIsLeftWithoutARole(t *testing.T) {
	desk := newRequestDesk(t, "Roles Everyone")
	joinAsAgent(t, desk.api, desk.admin, "agent@roles-everyone.example.com")
	_, link := createContractInvite(t, desk.api, desk.admin, "admin2@roles-everyone.example.com", "admin")
	request(t, desk.api, http.MethodPost, "/api/v1/auth/redeem", map[string]any{"token": linkToken(t, link)}, "", nil)
	if n := queryString(t, desk.pool, `SELECT count(*)::text FROM agents a WHERE a.deactivated_at IS NULL
		AND NOT EXISTS (SELECT 1 FROM agent_roles ar WHERE ar.workspace_id=a.workspace_id AND ar.agent_id=a.id)`); n != "0" {
		t.Fatalf("%s active members hold no role", n)
	}
	if got := queryString(t, desk.pool, `SELECT string_agg(r.name, ',' ORDER BY r.name) FROM agent_roles ar JOIN roles r ON r.id=ar.role_id JOIN agents a ON a.id=ar.agent_id
		WHERE a.email='admin2@roles-everyone.example.com'`); got != "Admin" {
		t.Fatalf("an invite with the Admin role gave %q, want only Admin", got)
	}

}

// memberEmail is the email of the person signed in with token.
func memberEmail(t *testing.T, api http.Handler, admin, token string) string {
	t.Helper()
	var me struct {
		Agent struct {
			Email string `json:"email"`
		} `json:"agent"`
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/auth/me", nil, token, nil), &me)
	return me.Agent.Email
}

// A role named like another role's id never answers for that id: an id finds its role, a name its own.
func TestARoleNamedLikeAnIDDoesNotTakeThatID(t *testing.T) {
	desk := newRequestDesk(t, "Roles Lookalike")
	agentRole := roleOf(t, desk.api, desk.admin, "Agent")
	var impostor roleView
	kbCall(t, desk.api, http.MethodPost, "/api/v1/roles", map[string]any{"name": strings.ToUpper(agentRole.ID), "permissions": []string{"tags:read"}}, desk.admin, http.StatusCreated, &impostor)
	for range 5 {
		if got := roleOf(t, desk.api, desk.admin, agentRole.ID); got.ID != agentRole.ID || got.Name != "Agent" {
			t.Fatalf("GET /roles/%s answered %s (%s), want the Agent role", agentRole.ID, got.Name, got.ID)
		}
	}
	if got := request(t, desk.api, http.MethodDelete, "/api/v1/roles/"+agentRole.ID, nil, desk.admin, nil); got.Code != http.StatusConflict || !strings.Contains(got.Body.String(), "system_role_locked") {
		t.Fatalf("DELETE by the Agent role's id: %d %s, want 409 system_role_locked", got.Code, got.Body.String())
	}
	if got := roleOf(t, desk.api, desk.admin, impostor.ID); got.ID != impostor.ID {
		t.Fatalf("the lookalike by its own id answered %s", got.ID)
	}
}
