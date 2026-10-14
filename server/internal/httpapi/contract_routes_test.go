package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/hclabsco/colliedesk/server/internal/auth"
	"github.com/hclabsco/colliedesk/server/internal/httpapi"
)

type contractCase struct {
	path   string
	body   any
	token  string
	status int
}

func TestEveryRouteResponseMatchesOpenAPIContract(t *testing.T) {
	fixture := newContractFixture(t)
	cases := successfulContractCases(fixture)
	if len(cases) != len(httpapi.Routes()) {
		t.Fatalf("contract cases=%d, registered routes=%d", len(cases), len(httpapi.Routes()))
	}
	for _, route := range httpapi.Routes() {
		key := route.Method + " " + route.Pattern
		t.Run(key, func(t *testing.T) {
			item, ok := cases[key]
			if !ok {
				t.Fatalf("missing contract case for %s", key)
			}
			req := newRequest(t, route.Method, item.path, item.body, item.token, nil)
			validateContractRequest(t, req)
			response := serve(fixture.api, req)
			if response.Code != item.status {
				t.Fatalf("status=%d body=%s, want %d", response.Code, response.Body.String(), item.status)
			}
			validateContractResponse(t, req, response)
		})
	}
}

func TestEveryProtectedRouteReturnsContractValidUnauthorized(t *testing.T) {
	fixture := newContractFixture(t)
	cases := successfulContractCases(fixture)
	for _, route := range httpapi.Routes() {
		if route.Permission == auth.PermissionPublic {
			continue
		}
		item := cases[route.Method+" "+route.Pattern]
		req := newRequest(t, route.Method, item.path, item.body, "", nil)
		validateContractRequest(t, req)
		response := serve(fixture.api, req)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s status=%d, want 401", route.Method, route.Pattern, response.Code)
		}
		validateContractResponse(t, req, response)
	}
}

func TestAgentGetsContractValidForbiddenOnEveryAdminRoute(t *testing.T) {
	fixture := newContractFixture(t)
	cases := successfulContractCases(fixture)
	for _, route := range httpapi.Routes() {
		if auth.HasPermission("agent", route.Permission) {
			continue
		}
		item := cases[route.Method+" "+route.Pattern]
		req := newRequest(t, route.Method, item.path, item.body, fixture.agentToken, nil)
		validateContractRequest(t, req)
		response := serve(fixture.api, req)
		if response.Code != http.StatusForbidden {
			t.Fatalf("%s %s status=%d, want 403", route.Method, route.Pattern, response.Code)
		}
		validateContractResponse(t, req, response)
	}
}

func TestCookieWriteRoutesReturnContractValidForbidden(t *testing.T) {
	fixture := newContractFixture(t)
	cases := successfulContractCases(fixture)
	cookie := &http.Cookie{Name: "colliedesk_session", Value: fixture.adminToken}
	for _, route := range httpapi.Routes() {
		if route.Permission == auth.PermissionPublic || route.Method == http.MethodGet {
			continue
		}
		item := cases[route.Method+" "+route.Pattern]
		req := newRequest(t, route.Method, item.path, item.body, "", cookie)
		validateContractRequest(t, req)
		response := serve(fixture.api, req)
		if response.Code != http.StatusForbidden {
			t.Fatalf("%s %s status=%d, want 403", route.Method, route.Pattern, response.Code)
		}
		validateContractResponse(t, req, response)
	}
}

func successfulContractCases(f contractFixture) map[string]contractCase {
	admin := f.adminToken
	return map[string]contractCase{
		"POST /api/v1/auth/redeem":                       {"/api/v1/auth/redeem", map[string]any{"token": f.redeemToken}, "", 200},
		"POST /api/v1/auth/logout":                       {"/api/v1/auth/logout", nil, f.logoutToken, 204},
		"GET /api/v1/auth/me":                            {"/api/v1/auth/me", nil, admin, 200},
		"GET /api/v1/auth/sessions":                      {"/api/v1/auth/sessions", nil, admin, 200},
		"DELETE /api/v1/auth/sessions/{id}":              {"/api/v1/auth/sessions/" + f.sessionID, nil, admin, 204},
		"POST /api/v1/auth/invites":                      {"/api/v1/auth/invites", map[string]any{"email": "new@contract.example.com", "role_ceiling": "agent"}, admin, 201},
		"GET /api/v1/auth/invites":                       {"/api/v1/auth/invites", nil, admin, 200},
		"DELETE /api/v1/auth/invites/{id}":               {"/api/v1/auth/invites/" + f.inviteID, nil, admin, 204},
		"GET /api/v1/conversations":                      {"/api/v1/conversations", nil, admin, 200},
		"POST /api/v1/conversations":                     {"/api/v1/conversations", map[string]any{"contact_id": f.contactID, "subject": "Created by contract test"}, admin, 201},
		"GET /api/v1/conversations/{id}":                 {"/api/v1/conversations/" + f.conversationID, nil, admin, 200},
		"PATCH /api/v1/conversations/{id}":               {"/api/v1/conversations/" + f.conversationID, map[string]any{"priority": "high"}, admin, 200},
		"PUT /api/v1/conversations/{id}/tags/{tagId}":    {"/api/v1/conversations/" + f.conversationID + "/tags/" + f.updateTagID, nil, admin, 200},
		"DELETE /api/v1/conversations/{id}/tags/{tagId}": {"/api/v1/conversations/" + f.conversationID + "/tags/" + f.updateTagID, nil, admin, 200},
		"GET /api/v1/conversations/{id}/messages":        {"/api/v1/conversations/" + f.conversationID + "/messages", nil, admin, 200},
		"POST /api/v1/conversations/{id}/messages":       {"/api/v1/conversations/" + f.conversationID + "/messages", map[string]any{"kind": "note", "body": "Contract"}, admin, 201},
		"GET /api/v1/contacts":                           {"/api/v1/contacts", nil, admin, 200},
		"POST /api/v1/contacts":                          {"/api/v1/contacts", map[string]any{"name": "Created", "email": "created@contract.example.com"}, admin, 201},
		"GET /api/v1/contacts/{id}":                      {"/api/v1/contacts/" + f.contactID, nil, admin, 200},
		"GET /api/v1/agents":                             {"/api/v1/agents", nil, admin, 200},
		"PATCH /api/v1/workspace/timezone":               {"/api/v1/workspace/timezone", map[string]any{"timezone": "UTC"}, admin, 200},
		"PATCH /api/v1/agents/me/timezone":               {"/api/v1/agents/me/timezone", map[string]any{"timezone": "UTC"}, admin, 200},
		"GET /api/v1/tags":                               {"/api/v1/tags", nil, admin, 200},
		"POST /api/v1/tags":                              {"/api/v1/tags", map[string]any{"name": "Created contract tag"}, admin, 201},
		"PATCH /api/v1/tags/{id}":                        {"/api/v1/tags/" + f.updateTagID, map[string]any{"name": "Updated contract tag"}, admin, 200},
		"DELETE /api/v1/tags/{id}":                       {"/api/v1/tags/" + f.deleteTagID, nil, admin, 204},
	}
}
