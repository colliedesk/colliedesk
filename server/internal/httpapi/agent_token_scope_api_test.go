package httpapi_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

var tierRank = map[string]int{"read": 1, "draft": 2, "send": 3}

// routeTiers is the lowest tier that may call each route. "closed" routes are never open to a token.
// It is written out by hand so a route added later fails here until someone classifies it.
var routeTiers = map[string]string{
	"POST /api/v1/auth/logout":                                   "read",
	"GET /api/v1/auth/me":                                        "read",
	"GET /api/v1/auth/sessions":                                  "read",
	"DELETE /api/v1/auth/sessions/{id}":                          "closed",
	"POST /api/v1/auth/tokens":                                   "closed",
	"POST /api/v1/auth/invites":                                  "closed",
	"GET /api/v1/auth/invites":                                   "read",
	"DELETE /api/v1/auth/invites/{id}":                           "closed",
	"GET /api/v1/conversations":                                  "read",
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
	"PATCH /api/v1/workspace/timezone":                           "closed",
	"PATCH /api/v1/workspace/slug":                               "closed",
	"PATCH /api/v1/agents/me/timezone":                           "closed",
	"GET /api/v1/tags":                                           "read",
	"POST /api/v1/tags":                                          "send",
	"PATCH /api/v1/tags/{id}":                                    "send",
	"DELETE /api/v1/tags/{id}":                                   "send",
	"GET /api/v1/channels/email":                                 "read",
	"PUT /api/v1/channels/email":                                 "closed",
	"DELETE /api/v1/channels/email":                              "closed",
	"POST /api/v1/channels/email/domain-verification":            "closed",
	"POST /api/v1/channels/email/secret":                         "closed",
	"GET /api/v1/billing":                                        "closed",
	"POST /api/v1/billing/checkout":                              "closed",
	"POST /api/v1/billing/portal":                                "closed",
	"GET /api/v1/export":                                         "closed",
	"POST /api/v1/export/link":                                   "closed",
	"GET /api/v1/channels/web-form":                              "read",
	"PUT /api/v1/channels/web-form":                              "closed",
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
		for _, tier := range []string{"read", "draft", "send"} {
			token := issueToken(t, fixture.api, fixture.adminToken, "matrix "+tier, tier).Token
			response := request(t, fixture.api, route.Method, item.path, item.body, token, nil)
			refused, _ := scopeRefused(t, response)
			if wantRefused := want == "closed" || tierRank[tier] < tierRank[want]; refused != wantRefused {
				t.Errorf("%s with a %s token: refused=%v, want %v (status=%d body=%s)", key, tier, refused, wantRefused, response.Code, response.Body.String())
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

	step := func(label, token, method, target string, body any, want int, tierNeeded string) {
		t.Helper()
		response := request(t, api, method, target, body, token, nil)
		if tierNeeded != "" {
			refused, message := scopeRefused(t, response)
			if !refused || !strings.Contains(message, "needs the "+tierNeeded+" scope") {
				t.Errorf("%s: status=%d body=%s, want scope_insufficient naming the %s scope", label, response.Code, response.Body.String(), tierNeeded)
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
	step("draft creates a tag", draft, http.MethodPost, "/api/v1/tags", map[string]any{"name": "new"}, 0, "send")

	var messages struct {
		Items []struct {
			Kind string `json:"kind"`
		} `json:"items"`
	}
	decode(t, request(t, api, http.MethodGet, path+"/messages", nil, admin, nil), &messages)
	if len(messages.Items) != 1 || messages.Items[0].Kind != "note" {
		t.Errorf("refused reply must store nothing; messages = %+v", messages.Items)
	}

	step("send replies", send, http.MethodPost, path+"/messages", reply, 201, "")
	step("send changes status", send, http.MethodPatch, path, map[string]any{"status": "resolved"}, 200, "")
	step("send creates a tag", send, http.MethodPost, "/api/v1/tags", map[string]any{"name": "new"}, 201, "")
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
