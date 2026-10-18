package httpapi_test

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

type contractCase struct {
	path   string
	body   any
	token  string
	status int
	raw    string
}

// contractRequest builds the request for a case; raw cases carry an RFC 5322 message and the inbound secret.
func contractRequest(t *testing.T, method string, item contractCase, token, secret string, cookie *http.Cookie) *http.Request {
	t.Helper()
	if item.path == webhookPath {
		return signedWebhookRequest("msg_contract_"+method, []byte(item.raw))
	}
	if item.path == resendWebhookPath {
		return signedResendRequest("msg_contract_"+method, []byte(item.raw))
	}
	if item.raw != "" {
		return inboundRequest(t, item.raw, secret)
	}
	if item.path == recipientPath {
		// The MTA authenticates with the server-wide secret; an empty secret exercises the 401.
		req := newRequest(t, method, item.path, item.body, "", nil)
		if secret != "" {
			req.Header.Set("X-Colliedesk-Inbound-Secret", platformSecret)
		}
		return req
	}
	return newRequest(t, method, item.path, item.body, token, cookie)
}

// sessionRoute reports whether a route is authenticated by an agent session (not public, not a channel secret).
func sessionRoute(route httpapi.Route) bool {
	return route.Permission != auth.PermissionPublic && route.Permission != auth.PermissionChannelInbound
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
			req := contractRequest(t, route.Method, item, item.token, fixture.inboundSecret, nil)
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
		req := contractRequest(t, route.Method, item, "", "", nil)
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
		if !sessionRoute(route) || auth.HasPermission("agent", route.Permission) {
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
		if !sessionRoute(route) || route.Method == http.MethodGet {
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
		"GET /api/v1/config":                                         {"/api/v1/config", nil, "", 200, ""},
		"POST /api/v1/auth/cloud/exchange":                           {"/api/v1/auth/cloud/exchange", map[string]any{}, "disabled-cloud-token", 404, ""},
		"POST /api/v1/auth/cloud/signup":                             {"/api/v1/auth/cloud/signup", map[string]any{"workspace_name": "Acme"}, "disabled-cloud-token", 404, ""},
		"POST /api/v1/auth/redeem":                                   {"/api/v1/auth/redeem", map[string]any{"token": f.redeemToken}, "", 200, ""},
		"POST /api/v1/auth/logout":                                   {"/api/v1/auth/logout", nil, f.logoutToken, 204, ""},
		"GET /api/v1/auth/me":                                        {"/api/v1/auth/me", nil, admin, 200, ""},
		"GET /api/v1/auth/sessions":                                  {"/api/v1/auth/sessions", nil, admin, 200, ""},
		"DELETE /api/v1/auth/sessions/{id}":                          {"/api/v1/auth/sessions/" + f.sessionID, nil, admin, 204, ""},
		"POST /api/v1/auth/tokens":                                   {"/api/v1/auth/tokens", map[string]any{"label": "Contract agent", "scope": "draft"}, admin, 201, ""},
		"POST /api/v1/auth/invites":                                  {"/api/v1/auth/invites", map[string]any{"email": "new@contract.example.com", "role_ceiling": "agent"}, admin, 201, ""},
		"GET /api/v1/auth/invites":                                   {"/api/v1/auth/invites", nil, admin, 200, ""},
		"DELETE /api/v1/auth/invites/{id}":                           {"/api/v1/auth/invites/" + f.inviteID, nil, admin, 204, ""},
		"GET /api/v1/conversations":                                  {"/api/v1/conversations", nil, admin, 200, ""},
		"POST /api/v1/conversations":                                 {"/api/v1/conversations", map[string]any{"contact_id": f.contactID, "subject": "Created by contract test"}, admin, 201, ""},
		"PATCH /api/v1/conversations":                                {"/api/v1/conversations", map[string]any{"ids": []string{f.conversationID}, "changes": map[string]any{"status": "open"}}, admin, 200, ""},
		"GET /api/v1/conversations/{id}":                             {"/api/v1/conversations/" + f.conversationID, nil, admin, 200, ""},
		"PATCH /api/v1/conversations/{id}":                           {"/api/v1/conversations/" + f.conversationID, map[string]any{"priority": "high"}, admin, 200, ""},
		"PUT /api/v1/conversations/{id}/tags/{tagId}":                {"/api/v1/conversations/" + f.conversationID + "/tags/" + f.updateTagID, nil, admin, 200, ""},
		"DELETE /api/v1/conversations/{id}/tags/{tagId}":             {"/api/v1/conversations/" + f.conversationID + "/tags/" + f.updateTagID, nil, admin, 200, ""},
		"GET /api/v1/conversations/{id}/messages":                    {"/api/v1/conversations/" + f.conversationID + "/messages", nil, admin, 200, ""},
		"POST /api/v1/conversations/{id}/messages":                   {"/api/v1/conversations/" + f.conversationID + "/messages", map[string]any{"kind": "note", "body": "Contract"}, admin, 201, ""},
		"GET /api/v1/contacts":                                       {"/api/v1/contacts", nil, admin, 200, ""},
		"POST /api/v1/contacts":                                      {"/api/v1/contacts", map[string]any{"name": "Created", "email": "created@contract.example.com"}, admin, 201, ""},
		"GET /api/v1/contacts/{id}":                                  {"/api/v1/contacts/" + f.contactID, nil, admin, 200, ""},
		"GET /api/v1/agents":                                         {"/api/v1/agents", nil, admin, 200, ""},
		"PATCH /api/v1/workspace/timezone":                           {"/api/v1/workspace/timezone", map[string]any{"timezone": "UTC"}, admin, 200, ""},
		"PATCH /api/v1/workspace/slug":                               {"/api/v1/workspace/slug", map[string]any{"slug": "contract-desk"}, admin, 200, ""},
		"PATCH /api/v1/agents/me/timezone":                           {"/api/v1/agents/me/timezone", map[string]any{"timezone": "UTC"}, admin, 200, ""},
		"PATCH /api/v1/agents/me/display-name":                       {"/api/v1/agents/me/display-name", map[string]any{"display_name": "Contract Admin"}, admin, 200, ""},
		"PATCH /api/v1/agents/{id}":                                  {"/api/v1/agents/" + f.agentID, map[string]any{"role": "agent"}, admin, 200, ""},
		"GET /api/v1/tags":                                           {"/api/v1/tags", nil, admin, 200, ""},
		"POST /api/v1/tags":                                          {"/api/v1/tags", map[string]any{"name": "Created contract tag"}, admin, 201, ""},
		"PATCH /api/v1/tags/{id}":                                    {"/api/v1/tags/" + f.updateTagID, map[string]any{"name": "Updated contract tag"}, admin, 200, ""},
		"DELETE /api/v1/tags/{id}":                                   {"/api/v1/tags/" + f.deleteTagID, nil, admin, 204, ""},
		"GET /api/v1/saved-replies":                                  {"/api/v1/saved-replies", nil, admin, 200, ""},
		"POST /api/v1/saved-replies":                                 {"/api/v1/saved-replies", map[string]any{"title": "Created contract reply", "body": "Thanks"}, admin, 201, ""},
		"PATCH /api/v1/saved-replies/{id}":                           {"/api/v1/saved-replies/" + f.updateReplyID, map[string]any{"body": "Updated", "updated_at": f.updateReplySeen}, admin, 200, ""},
		"DELETE /api/v1/saved-replies/{id}":                          {"/api/v1/saved-replies/" + f.deleteReplyID + "?updated_at=" + url.QueryEscape(f.deleteReplySeen), nil, admin, 204, ""},
		"GET /api/v1/saved-replies/trash":                            {"/api/v1/saved-replies/trash", nil, admin, 200, ""},
		"GET /api/v1/saved-replies/{id}/versions":                    {"/api/v1/saved-replies/" + f.restoreReplyID + "/versions", nil, admin, 200, ""},
		"POST /api/v1/saved-replies/{id}/versions/{version}/restore": {"/api/v1/saved-replies/" + f.restoreReplyID + "/versions/1/restore", map[string]any{"updated_at": f.restoreReplySeen}, admin, 200, ""},
		"POST /api/v1/saved-replies/{id}/restore":                    {"/api/v1/saved-replies/" + f.trashReplyID + "/restore", map[string]any{"updated_at": f.trashReplySeen}, admin, 200, ""},
		"POST /api/v1/conversations/{id}/messages/{messageId}/retry": {path: "/api/v1/conversations/" + f.conversationID + "/messages/" + f.failedReplyID + "/retry", token: admin, status: 200},
		"GET /api/v1/channels/email":                                 {path: "/api/v1/channels/email", token: admin, status: 200},
		"PUT /api/v1/channels/email":                                 {path: "/api/v1/channels/email", body: map[string]any{"support_address": "support@contract-all.example.com"}, token: admin, status: 200},
		"DELETE /api/v1/channels/email":                              {path: "/api/v1/channels/email", token: f.deleteChannelToken, status: 204},
		"POST /api/v1/channels/email/domain-verification":            {path: "/api/v1/channels/email/domain-verification", token: admin, status: 200},
		"POST /api/v1/channels/email/secret":                         {path: "/api/v1/channels/email/secret", token: admin, status: 200},
		"POST /api/v1/channels/email/inbound": {path: inboundPath, status: 200, raw: mail([]string{
			"From: Contract Customer <contract@customer.example>", "Subject: Contract mail", "Message-ID: <contract@customer.example>",
		}, "Contract inbound.")},
		// Routes run in order; PATCH /workspace/slug above renamed the workspace to contract-desk.
		"POST /api/v1/channels/email/inbound/recipient": {path: recipientPath, body: map[string]any{"recipient": "support@contract-desk." + hostedDomain}, status: 204},
		"GET /api/v1/billing":                           {path: "/api/v1/billing", token: admin, status: 200},
		"POST /api/v1/billing/checkout":                 {path: "/api/v1/billing/checkout", body: map[string]any{"plan": "team_monthly"}, token: admin, status: 200},
		"POST /api/v1/billing/portal":                   {path: "/api/v1/billing/portal", token: f.portalToken, status: 200},
		"POST " + webhookPath:                           {path: webhookPath, raw: f.webhookBody, status: 200},
		"POST " + resendWebhookPath:                     {path: resendWebhookPath, raw: string(resendEvent("email.delivered", "Desk <support@contract-all.example.com>", []string{"contact@contract-all.example.com"}, "", time.Now())), status: 200},
		"GET /api/v1/founding":                          {path: "/api/v1/founding", status: 200},
		"GET /api/v1/conversations/{id}/time-entries":   {path: "/api/v1/conversations/" + f.conversationID + "/time-entries", token: admin, status: 200},
		"GET /api/v1/inbox/activity":                    {path: "/api/v1/inbox/activity?since=2026-01-01T00:00:00Z", token: admin, status: 200},
		"GET /api/v1/attachments/{id}":                  {path: "/api/v1/attachments/" + f.attachmentID, token: admin, status: 200},
		"GET /api/v1/export":                            {path: "/api/v1/export", token: admin, status: 200},
		"POST /api/v1/export/link":                      {path: "/api/v1/export/link", token: admin, status: 201},
		"GET /api/v1/export/download/{token}":           {path: "/api/v1/export/download/not-a-link", status: 303},
		"GET /api/v1/channels/web-form":                 {path: "/api/v1/channels/web-form", token: admin, status: 200},
		"PUT /api/v1/channels/web-form":                 {path: "/api/v1/channels/web-form", body: map[string]any{"enabled": true, "allowed_origins": []string{"https://contract.example.com"}}, token: admin, status: 200},
		"GET /api/v1/agent-requests":                    {path: "/api/v1/agent-requests?status=pending", token: admin, status: 200},
		"GET /api/v1/agent-requests/{id}":               {path: "/api/v1/agent-requests/" + f.approveRequestID, token: f.requestToken, status: 200},
		"POST /api/v1/agent-requests/{id}/approve":      {path: "/api/v1/agent-requests/" + f.approveRequestID + "/approve", token: admin, status: 200},
		"POST /api/v1/agent-requests/{id}/reject":       {path: "/api/v1/agent-requests/" + f.rejectRequestID + "/reject", body: map[string]any{"reason": "Not now"}, token: admin, status: 200},
		"POST /api/v1/agent-requests/{id}/cancel":       {path: "/api/v1/agent-requests/" + f.cancelRequestID + "/cancel", token: f.requestToken, status: 200},
		"POST /api/v1/public/forms/{workspace_slug}":    {path: "/api/v1/public/forms/contract-desk" /* the slug set by PATCH /api/v1/workspace/slug, which runs earlier */, body: map[string]any{"email": "visitor@contract.example.com", "source": "site:updates"}, status: 202},
	}
}
