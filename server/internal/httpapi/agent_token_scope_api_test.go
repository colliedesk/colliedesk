package httpapi_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

var tierRank = map[string]int{"read": 1, "draft": 2, "send": 3, "admin": 4}

// routeTiers is the lowest tier that may call each route; a token below it gets an agent request (202).
// "approval" routes give every token an agent request, and "person" routes refuse every token
// (person_only). No route is closed to every token.
// It is written out by hand so a route added later fails here until someone classifies it.
var routeTiers = map[string]string{
	"POST /api/v1/auth/logout":                                   "read",
	"GET /api/v1/auth/me":                                        "read",
	"GET /api/v1/auth/sessions":                                  "read",
	"DELETE /api/v1/auth/sessions/{id}":                          "admin",
	"POST /api/v1/auth/tokens":                                   "approval",
	"POST /api/v1/auth/invites":                                  "approval",
	"GET /api/v1/auth/invites":                                   "read",
	"DELETE /api/v1/auth/invites/{id}":                           "admin",
	"GET /api/v1/conversations":                                  "read",
	"PATCH /api/v1/conversations":                                "draft",
	"GET /api/v1/inbox/activity":                                 "read",
	"PATCH /api/v1/agents/{id}":                                  "admin",
	"GET /api/v1/saved-replies":                                  "read",
	"POST /api/v1/saved-replies":                                 "send",
	"PATCH /api/v1/saved-replies/{id}":                           "send",
	"DELETE /api/v1/saved-replies/{id}":                          "send",
	"GET /api/v1/saved-replies/trash":                            "read",
	"GET /api/v1/saved-replies/{id}/versions":                    "read",
	"POST /api/v1/saved-replies/{id}/versions/{version}/restore": "send",
	"POST /api/v1/saved-replies/{id}/restore":                    "send",
	"POST /api/v1/conversations":                                 "send",
	"GET /api/v1/conversations/{id}":                             "read",
	"PATCH /api/v1/conversations/{id}":                           "draft",
	"PUT /api/v1/conversations/{id}/tags/{tagId}":                "draft",
	"DELETE /api/v1/conversations/{id}/tags/{tagId}":             "draft",
	"GET /api/v1/conversations/{id}/messages":                    "read",
	"POST /api/v1/conversations/{id}/messages":                   "draft",
	"GET /api/v1/conversations/{id}/time-entries":                "read",
	"POST /api/v1/conversations/{id}/messages/{messageId}/retry": "send",
	"GET /api/v1/attachments/{id}":                               "read",
	"GET /api/v1/contacts":                                       "read",
	"POST /api/v1/contacts":                                      "send",
	"GET /api/v1/contacts/{id}":                                  "read",
	"GET /api/v1/agents":                                         "read",
	"PATCH /api/v1/workspace/timezone":                           "admin",
	"PATCH /api/v1/workspace/slug":                               "approval",
	"PATCH /api/v1/agents/me/timezone":                           "draft",
	"PATCH /api/v1/agents/me/display-name":                       "draft",
	"GET /api/v1/tags":                                           "read",
	"POST /api/v1/tags":                                          "draft",
	"PATCH /api/v1/tags/{id}":                                    "send",
	"DELETE /api/v1/tags/{id}":                                   "send",
	"GET /api/v1/channels/email":                                 "read",
	"PUT /api/v1/channels/email":                                 "admin",
	"DELETE /api/v1/channels/email":                              "approval",
	"POST /api/v1/channels/email/domain-verification":            "admin",
	"POST /api/v1/channels/email/secret":                         "approval",
	"GET /api/v1/billing":                                        "admin",
	"POST /api/v1/billing/checkout":                              "approval",
	"POST /api/v1/billing/portal":                                "approval",
	"GET /api/v1/export":                                         "approval",
	"POST /api/v1/export/link":                                   "approval",
	"GET /api/v1/channels/web-form":                              "read",
	"PUT /api/v1/channels/web-form":                              "admin",
	"GET /api/v1/agent-requests":                                 "read",
	"GET /api/v1/agent-requests/{id}":                            "read",
	"POST /api/v1/agent-requests/{id}/cancel":                    "read",
	"POST /api/v1/support/problem-reports":                       "read",
	"GET /api/v1/mail-image":                                     "person",
	"POST /api/v1/agent-requests/{id}/approve":                   "person",
	"POST /api/v1/agent-requests/{id}/reject":                    "person",
}

func TestEveryRouteIsOpenToTokensOnlyFromItsTier(t *testing.T) {
	fixture := newContractFixture(t)
	cases := successfulContractCases(fixture)
	for _, route := range httpapi.Routes() {
		if !sessionRoute(route) {
			continue
		}
		key := route.Method + " " + route.Pattern
		want, ok := routeTiers[key]
		if !ok {
			t.Errorf("%s is not classified in routeTiers", key)
			continue
		}
		item := cases[key]
		for _, tier := range []string{"read", "draft", "send", "admin"} {
			token := issueToken(t, fixture.api, fixture.adminToken, "matrix "+tier, tier).Token
			body := item.body
			if key == "POST /api/v1/tags" { // two tiers may create, and a tag name is unique
				body = map[string]any{"name": "matrix tag " + tier}
			}
			response := request(t, fixture.api, route.Method, item.path, body, token, nil)
			switch {
			case want == "person":
				if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "person_only") {
					t.Errorf("%s with a %s token: status=%d body=%s, want 403 person_only", key, tier, response.Code, response.Body.String())
				}
			case want == "approval" || tierRank[tier] < tierRank[want]:
				if response.Code != http.StatusAccepted {
					t.Errorf("%s with a %s token: status=%d body=%s, want 202 (an agent request)", key, tier, response.Code, response.Body.String())
				}
			case strings.HasPrefix(route.Pattern, "/api/v1/agent-requests/{id}"):
				// The fixture's request belongs to another token, which this one cannot see.
				if response.Code != http.StatusNotFound {
					t.Errorf("%s with a %s token: status=%d body=%s, want 404 for another token's request", key, tier, response.Code, response.Body.String())
				}
			case tier == "admin" && want != "admin" && response.Code != http.StatusAccepted && response.Code != http.StatusForbidden:
				// The send token already ran the call once; the admin token only has to pass the gate.
			case response.Code != item.status:
				t.Errorf("%s with a %s token: status=%d body=%s, want the success status %d", key, tier, response.Code, response.Body.String(), item.status)
			}
		}
	}
	if len(routeTiers) != countSessionRoutes() {
		t.Errorf("routeTiers lists %d routes, API has %d session routes", len(routeTiers), countSessionRoutes())
	}
}

func countSessionRoutes() int {
	count := 0
	for _, route := range httpapi.Routes() {
		if sessionRoute(route) {
			count++
		}
	}
	return count
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

	// tierNeeded names a tier above the token's: the call becomes an agent request and changes nothing.
	step := func(label, token, method, target string, body any, want int, tierNeeded string) {
		t.Helper()
		response := request(t, api, method, target, body, token, nil)
		if tierNeeded != "" {
			if response.Code != http.StatusAccepted || !strings.Contains(response.Body.String(), `"request_id"`) {
				t.Errorf("%s: status=%d body=%s, want 202 with an agent request (the action needs %s)", label, response.Code, response.Body.String(), tierNeeded)
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
		t.Errorf("a reply waiting for approval must store nothing; messages = %+v", messages.Items)
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
			if refused, _ := scopeRefused(t, response); refused {
				continue // a tier below the route's own is refused before the workspace is consulted
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
