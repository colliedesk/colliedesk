package httpapi_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

// Every session route needs one permission (or none, or a person), the same for people and tokens:
// a token given everything but it is refused with permission_missing (missing from the token, since its
// person holds it); a token given just it gets past the check, and the call answers what a person's does.
func TestEveryRouteNeedsOnePermissionForPeopleAndTokens(t *testing.T) {
	fixture := newContractFixture(t)
	cases := successfulContractCases(fixture)
	table := httpapi.RouteAccessTable()
	all := auth.AllPermissions()
	tokens := map[string]string{}
	tokenWith := func(names ...string) string {
		key := strings.Join(names, ",")
		if tokens[key] == "" {
			tokens[key] = customToken(t, fixture.api, fixture.adminToken, fmt.Sprintf("matrix %d", len(tokens)), names...)
		}
		return tokens[key]
	}
	for _, route := range httpapi.Routes() {
		if !sessionRoute(route) || route.Permission == auth.PermissionOperator {
			continue
		}
		key := route.Method + " " + route.Pattern
		access, ok := table[key]
		if !ok {
			t.Errorf("%s has no row in routeAccess", key)
			continue
		}
		item, ok := cases[key]
		if !ok {
			continue
		}
		call := func(token string) *httptest.ResponseRecorder {
			if key == "POST "+kbImagesPath { // the body is the raw image
				return serve(fixture.api, kbImageRequest(token, nil))
			}
			return request(t, fixture.api, route.Method, item.path, item.body, token, nil)
		}
		switch {
		case access.Closed == httpapi.PersonOnly:
			response := call(tokenWith(all...))
			if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "person_only") {
				t.Errorf("%s with a token: status=%d body=%s, want 403 person_only", key, response.Code, response.Body.String())
			}
		case access.Anyone:
			if missing, _, _ := permissionMissing(t, call(tokenWith(auth.PermTagsRead))); missing {
				t.Errorf("%s refused a token, want it open to every token", key)
			}
		case access.ByHandler:
			if missing, _, _ := permissionMissing(t, call(tokenWith(access.Permission))); missing && key != "POST /api/v1/conversations/{id}/messages" {
				t.Errorf("%s refused a token holding %s", key, access.Permission)
			}
		default:
			var others []string
			for _, name := range all {
				if name != access.Permission {
					others = append(others, name)
				}
			}
			response := call(tokenWith(others...))
			missing, names, from := permissionMissing(t, response)
			if !missing || !slices.Contains(names, access.Permission) || from != "token" {
				t.Errorf("%s with every permission but %s: status=%d body=%s, want 403 permission_missing from the token", key, access.Permission, response.Code, response.Body.String())
			}
		}
	}
}

// The two built-in roles let a person through every route exactly as the agent and admin roles did
// before roles (testdata/old-route-roles.json, frozen from the release before).
func TestSystemRolesReproduceTheOldRoles(t *testing.T) {
	data, err := os.ReadFile("testdata/old-route-roles.json")
	if err != nil {
		t.Fatal(err)
	}
	var old map[string]struct {
		Agent bool `json:"agent"`
		Admin bool `json:"admin"`
	}
	if err := json.Unmarshal(data, &old); err != nil {
		t.Fatal(err)
	}
	// What changed on purpose: one's own sessions are anyone's (another's needs team:revoke in the
	// handler, which the agent role never had), and a channel's AI level was refused to agents by its handler.
	changed := map[string]bool{"DELETE /api/v1/auth/sessions/{id}": true, "PUT /api/v1/channels/{id}/ai/{slot}": true}
	table := httpapi.RouteAccessTable()
	agent := auth.PrincipalForTest(domain.Workspace{}, domain.Agent{}, auth.AgentRolePermissions())
	admin := auth.PrincipalForTest(domain.Workspace{}, domain.Agent{}, auth.AllPermissions())
	passes := func(who auth.Principal, access httpapi.TokenAccess) bool {
		switch {
		case access.Anyone, access.ByHandler && who.Can(access.Permission):
			return true
		case access.Closed == httpapi.PersonOnly:
			return access.PersonNeeds == "" || who.Can(access.PersonNeeds)
		}
		return access.Permission != "" && who.Can(access.Permission)
	}
	for key, before := range old {
		access, ok := table[key]
		if !ok || strings.Contains(key, "/agent-requests") {
			continue // removed with agent requests
		}
		if got := passes(admin, access); got != before.Admin {
			t.Errorf("%s: the Admin role passes=%v, the admin role did %v", key, got, before.Admin)
		}
		if got := passes(agent, access); got != before.Agent && !changed[key] {
			t.Errorf("%s: the Agent role passes=%v, the agent role did %v", key, got, before.Agent)
		}
	}
	if passes(agent, table["PUT /api/v1/channels/{id}/ai/{slot}"]) {
		t.Error("the Agent role may change a channel's AI level; the agent role never could")
	}
}

func TestTokenTiersGateWhatAnAgentMayChange(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Tier Desk", "admin@tier.example.com")
	contact := create(t, api, admin, "/api/v1/contacts", map[string]any{"name": "Ada", "email": "ada@tier.example.com"})
	conversation := create(t, api, admin, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Printer jam"})
	tag := create(t, api, admin, "/api/v1/tags", map[string]any{"name": "billing"})
	read := issueToken(t, api, admin, "reader", "read").Token
	draft := issueToken(t, api, admin, "drafter", "draft").Token
	send := issueToken(t, api, admin, "sender", "send").Token

	path := "/api/v1/conversations/" + conversation
	status := map[string]any{"status": "pending"}
	note := map[string]any{"kind": "note", "body": "Internal note"}
	reply := map[string]any{"kind": "outgoing", "body": "Hello customer"}
	tagPath := path + "/tags/" + tag

	// tierNeeded names a tier above the token's: the call is refused and changes nothing.
	step := func(label, token, method, target string, body any, want int, tierNeeded string) {
		t.Helper()
		response := request(t, api, method, target, body, token, nil)
		if tierNeeded != "" {
			if missing, _, _ := permissionMissing(t, response); !missing {
				t.Errorf("%s: status=%d body=%s, want 403 permission_missing (the action needs %s)", label, response.Code, response.Body.String(), tierNeeded)
			}
			return
		}
		if response.Code != want {
			t.Errorf("%s: status=%d body=%s, want %d", label, response.Code, response.Body.String(), want)
		}
	}

	step("read lists conversations", read, http.MethodGet, "/api/v1/conversations", nil, 200, "")
	step("read opens a conversation", read, http.MethodGet, path, nil, 200, "")
	step("read changes status", read, http.MethodPatch, path, status, 0, "draft")
	step("read tags", read, http.MethodPut, tagPath, nil, 0, "draft")
	step("read adds a note", read, http.MethodPost, path+"/messages", note, 0, "draft")
	step("read replies", read, http.MethodPost, path+"/messages", reply, 0, "send")

	step("draft changes status", draft, http.MethodPatch, path, status, 200, "")
	step("draft tags", draft, http.MethodPut, tagPath, nil, 200, "")
	step("draft untags", draft, http.MethodDelete, tagPath, nil, 200, "")
	step("draft adds a note", draft, http.MethodPost, path+"/messages", note, 201, "")
	step("draft replies", draft, http.MethodPost, path+"/messages", reply, 0, "send")
	step("read creates a tag", read, http.MethodPost, "/api/v1/tags", map[string]any{"name": "from read"}, 0, "draft")
	step("draft creates a tag", draft, http.MethodPost, "/api/v1/tags", map[string]any{"name": "from draft"}, 201, "")
	step("draft renames a tag", draft, http.MethodPatch, "/api/v1/tags/"+tag, map[string]any{"name": "renamed"}, 0, "send")
	step("draft deletes a tag", draft, http.MethodDelete, "/api/v1/tags/"+tag, nil, 0, "send")

	var messages struct {
		Items []struct {
			Kind string `json:"kind"`
		} `json:"items"`
	}
	decode(t, request(t, api, http.MethodGet, path+"/messages", nil, admin, nil), &messages)
	if len(messages.Items) != 1 || messages.Items[0].Kind != "note" {
		t.Errorf("a refused reply must store nothing; messages = %+v", messages.Items)
	}

	step("send replies", send, http.MethodPost, path+"/messages", reply, 201, "")
	step("send changes status", send, http.MethodPatch, path, map[string]any{"status": "resolved"}, 200, "")
	step("send creates a tag", send, http.MethodPost, "/api/v1/tags", map[string]any{"name": "from send"}, 201, "")
	step("send renames a tag", send, http.MethodPatch, "/api/v1/tags/"+tag, map[string]any{"name": "renamed"}, 200, "")
	step("send deletes a tag", send, http.MethodDelete, "/api/v1/tags/"+tag, nil, 204, "")
}

// Every tier reads the invite and session lists and the channel settings, without links or secrets;
// what the role may not read stays refused by the role.
func TestTokensReadSettingsWithoutLinksOrSecrets(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Settings Reads", "admin@settings-reads.example.com")
	secret := connectEmail(t, api, admin, "support@settings-reads.example.com")
	_, link := createContractInvite(t, api, admin, "invitee@settings-reads.example.com", "agent")
	reads := []string{"/api/v1/auth/invites", "/api/v1/auth/sessions", "/api/v1/channels/email", "/api/v1/channels/web-form",
		"/api/v1/agents", "/api/v1/tags", "/api/v1/conversations", "/api/v1/contacts", "/api/v1/auth/me"}
	for _, tier := range []string{"read", "draft", "send", "admin"} {
		token := issueToken(t, api, admin, tier+" token", tier).Token
		for _, path := range reads {
			response := request(t, api, http.MethodGet, path, nil, token, nil)
			if response.Code != http.StatusOK {
				t.Errorf("%s token GET %s: status=%d body=%s, want 200", tier, path, response.Code, response.Body.String())
			}
			for _, leak := range []string{secret, linkToken(t, link), "cdk_agent_"} {
				if strings.Contains(response.Body.String(), leak) {
					t.Errorf("%s token GET %s leaks a credential: %s", tier, path, response.Body.String())
				}
			}
		}
	}
}

func TestTokenOfOneWorkspaceCannotReachAnotherWorkspace(t *testing.T) {
	api, service := testAPI(t)
	adminA := createSession(t, api, service, "Scope Alpha", "admin@scope-alpha.example.com")
	adminB := createSession(t, api, service, "Scope Beta", "admin@scope-beta.example.com")
	contactB := create(t, api, adminB, "/api/v1/contacts", map[string]any{"name": "Beta", "email": "beta@scope.example.com"})
	conversationB := create(t, api, adminB, "/api/v1/conversations", map[string]any{"contact_id": contactB, "subject": "Beta only"})
	tagB := create(t, api, adminB, "/api/v1/tags", map[string]any{"name": "beta tag"})

	for _, tier := range []string{"read", "draft", "send"} {
		token := issueToken(t, api, adminA, tier+" token", tier).Token
		path := "/api/v1/conversations/" + conversationB
		for _, probe := range []struct {
			method, target string
			body           any
		}{
			{http.MethodGet, path, nil},
			{http.MethodGet, path + "/messages", nil},
			{http.MethodGet, "/api/v1/contacts/" + contactB, nil},
			{http.MethodPatch, path, map[string]any{"status": "closed"}},
			{http.MethodPut, path + "/tags/" + tagB, nil},
			{http.MethodPost, path + "/messages", map[string]any{"kind": "note", "body": "intrusion"}},
		} {
			response := request(t, api, probe.method, probe.target, probe.body, token, nil)
			if missing, _, _ := permissionMissing(t, response); missing {
				continue // a token without the route's permission is refused before the workspace is consulted
			}
			if response.Code != http.StatusNotFound {
				t.Errorf("%s token %s %s: status=%d body=%s, want 404", tier, probe.method, probe.target, response.Code, response.Body.String())
			}
		}
		listed := request(t, api, http.MethodGet, "/api/v1/conversations", nil, token, nil)
		if strings.Contains(listed.Body.String(), conversationB) {
			t.Errorf("%s token lists a conversation of another workspace", tier)
		}
	}
	var messages struct {
		Items []struct{} `json:"items"`
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/conversations/"+conversationB+"/messages", nil, adminB, nil), &messages)
	if len(messages.Items) != 0 {
		t.Fatalf("another workspace's token wrote %d messages", len(messages.Items))
	}
}
