package httpapi_test

import (
	"github.com/colliedesk/colliedesk/server/internal/autofix"
	"maps"
	"net/http"
	"net/url"
	"strings"
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
	if item.path == githubWebhookPath {
		return signedGitHubRequest([]byte(item.raw))
	}
	if item.path == resendWebhookPath {
		return signedResendRequest("msg_contract_"+method, []byte(item.raw))
	}
	if item.raw != "" {
		return inboundRequest(t, item.raw, secret)
	}
	if item.path == kbImagesPath {
		return kbImageRequest(token, cookie)
	}
	if method == http.MethodPut && strings.HasPrefix(item.path, "/api/v1/kb/media-uploads/") {
		req := kbPartRequest(strings.TrimPrefix(item.path, "/api/v1/kb/media-uploads/"), token, contractVideoPart, 0, len(contractVideoPart)-1, len(contractVideoPart))
		if cookie != nil {
			req.Header.Del("Authorization")
			req.AddCookie(cookie)
		}
		return req
	}
	if method == http.MethodPut && strings.HasSuffix(item.path, "/poster") {
		req := kbPosterRequest(strings.TrimSuffix(strings.TrimPrefix(item.path, "/api/v1/kb/media/"), "/poster"), token, pngImage(209))
		if cookie != nil {
			req.Header.Del("Authorization")
			req.AddCookie(cookie)
		}
		return req
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
		req := contractRequest(t, route.Method, item, "", "", cookie)
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
	cases := map[string]contractCase{
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
		"PUT /api/v1/conversations/{id}/presence":                    {"/api/v1/conversations/" + f.conversationID + "/presence", map[string]any{"client_id": "contract-tab", "writing": true}, admin, 200, ""},
		"DELETE /api/v1/conversations/{id}/presence/{clientId}":      {"/api/v1/conversations/" + f.conversationID + "/presence/contract-tab", nil, admin, 204, ""},
		"GET /api/v1/contacts":                                       {"/api/v1/contacts", nil, admin, 200, ""},
		"POST /api/v1/contacts":                                      {"/api/v1/contacts", map[string]any{"name": "Created", "email": "created@contract.example.com"}, admin, 201, ""},
		"GET /api/v1/contacts/{id}":                                  {"/api/v1/contacts/" + f.contactID, nil, admin, 200, ""},
		"PATCH /api/v1/contacts/{id}":                                {"/api/v1/contacts/" + f.contactID, map[string]any{"name": "Contract Contact"}, admin, 200, ""},
		"POST /api/v1/contacts/{id}/merges":                          {"/api/v1/contacts/" + f.contactID + "/merges", map[string]any{"contact_id": f.mergeDuplicateID}, admin, 201, ""},
		"DELETE /api/v1/contacts/{id}/merges/{mergeId}":              {"/api/v1/contacts/" + f.contactID + "/merges/" + f.undoMergeID, nil, admin, 200, ""},
		"GET /api/v1/agents":                                         {"/api/v1/agents", nil, admin, 200, ""},
		"PATCH /api/v1/workspace/timezone":                           {"/api/v1/workspace/timezone", map[string]any{"timezone": "UTC"}, admin, 200, ""},
		"PATCH /api/v1/workspace/name":                               {"/api/v1/workspace/name", map[string]any{"name": "Contract Desk"}, admin, 200, ""},
		"PATCH /api/v1/workspace/slug":                               {"/api/v1/workspace/slug", map[string]any{"slug": "contract-desk"}, admin, 200, ""},
		"PATCH /api/v1/agents/me/timezone":                           {"/api/v1/agents/me/timezone", map[string]any{"timezone": "UTC"}, admin, 200, ""},
		"PATCH /api/v1/agents/me/display-name":                       {"/api/v1/agents/me/display-name", map[string]any{"display_name": "Contract Admin"}, admin, 200, ""},
		"PATCH /api/v1/agents/{id}":                                  {"/api/v1/agents/" + f.agentID, map[string]any{"role": "agent"}, admin, 200, ""},
		"GET /api/v1/tags":                                           {"/api/v1/tags", nil, admin, 200, ""},
		"GET /api/v1/results":                                        {"/api/v1/results", nil, admin, 200, ""},
		"GET /api/v1/autofix/status":                                 {"/api/v1/autofix/status", nil, admin, 200, ""},
		"GET /api/v1/autofix/fixes":                                  {"/api/v1/autofix/fixes", nil, admin, 200, ""},
		"GET /api/v1/autofix/fixes/{id}":                             {"/api/v1/autofix/fixes/" + f.fixID, nil, admin, 200, ""},
		"GET /api/v1/conversations/{id}/fixes":                       {"/api/v1/conversations/" + f.conversationID + "/fixes", nil, admin, 200, ""},
		"PUT /api/v1/autofix/connectors/{kind}":                      {"/api/v1/autofix/connectors/claude_issue", map[string]any{"consent_version": autofix.ConsentVersion}, admin, 200, ""},
		"DELETE /api/v1/autofix/connectors/{kind}":                   {"/api/v1/autofix/connectors/claude_issue", nil, admin, 200, ""},
		"POST /api/v1/autofix/connectors/{kind}/check":               {"/api/v1/autofix/connectors/claude_issue/check", nil, admin, 200, ""},
		"PUT /api/v1/autofix/connectors/cursor_api/key":              {"/api/v1/autofix/connectors/cursor_api/key", map[string]any{"key": cursorCanaryKey}, admin, 200, ""},
		"DELETE /api/v1/autofix/connectors/cursor_api/key":           {"/api/v1/autofix/connectors/cursor_api/key", nil, admin, 200, ""},
		"GET /api/v1/results/share-link":                             {"/api/v1/results/share-link", nil, admin, 200, ""},
		"POST /api/v1/results/share-link":                            {"/api/v1/results/share-link", nil, admin, 201, ""},
		"DELETE /api/v1/results/share-link":                          {"/api/v1/results/share-link", nil, admin, 200, ""},
		"POST /api/v1/results/shared":                                {"/api/v1/results/shared", map[string]any{"token": "no-such-link"}, "", 404, ""},
		"PATCH /api/v1/agents/me/results-email":                      {"/api/v1/agents/me/results-email", map[string]any{"weekly": true}, admin, 200, ""},
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
		"GET /api/v1/saved-views":                                    {"/api/v1/saved-views", nil, admin, 200, ""},
		"POST /api/v1/saved-views":                                   {"/api/v1/saved-views", map[string]any{"name": "Created contract view", "scope": "personal", "filters": map[string]any{"priority": []string{"urgent"}, "sort": "waiting_longest"}}, admin, 201, ""},
		"GET /api/v1/saved-views/trash":                              {"/api/v1/saved-views/trash", nil, admin, 200, ""},
		"PATCH /api/v1/saved-views/{id}":                             {"/api/v1/saved-views/" + f.updateViewID, map[string]any{"name": "Renamed contract view", "updated_at": f.updateViewSeen}, admin, 200, ""},
		"DELETE /api/v1/saved-views/{id}":                            {"/api/v1/saved-views/" + f.deleteViewID, nil, admin, 200, ""},
		"POST /api/v1/saved-views/{id}/restore":                      {"/api/v1/saved-views/" + f.trashViewID + "/restore", nil, admin, 200, ""},
		"POST /api/v1/conversations/{id}/messages/{messageId}/retry": {path: "/api/v1/conversations/" + f.conversationID + "/messages/" + f.failedReplyID + "/retry", token: admin, status: 200},
		"GET /api/v1/channels/email":                                 {path: "/api/v1/channels/email", token: admin, status: 200},
		"PUT /api/v1/channels/email":                                 {path: "/api/v1/channels/email", body: map[string]any{"support_address": "support@contract-all.example.com"}, token: admin, status: 200},
		"DELETE /api/v1/channels/email":                              {path: "/api/v1/channels/email", token: f.deleteChannelToken, status: 204},
		"POST /api/v1/channels/email/domain-verification":            {path: "/api/v1/channels/email/domain-verification", token: admin, status: 200},
		"POST /api/v1/channels/email/secret":                         {path: "/api/v1/channels/email/secret", token: admin, status: 200},
		"POST /api/v1/channels/email/gmail":                          {path: "/api/v1/channels/email/gmail", token: admin, status: 200},
		"DELETE /api/v1/channels/email/gmail":                        {path: "/api/v1/channels/email/gmail", token: admin, status: 404},
		"GET /api/v1/channels/email/gmail/callback":                  {path: "/api/v1/channels/email/gmail/callback?state=unknown&code=x", status: 303},
		"GET /api/v1/autofix/settings":                               {path: "/api/v1/autofix/settings", token: admin, status: 200},
		"POST /api/v1/autofix/github/installations":                  {path: "/api/v1/autofix/github/installations", token: admin, status: 201},
		"DELETE /api/v1/autofix/github/installations/{id}":           {path: "/api/v1/autofix/github/installations/" + f.github.removableInstallation, token: admin, status: 200},
		"GET /api/v1/autofix/github/installations/{id}/repositories": {path: "/api/v1/autofix/github/installations/" + f.github.installation + "/repositories", token: admin, status: 200},
		"GET /api/v1/autofix/github/callback":                        {path: "/api/v1/autofix/github/callback?state=unknown&code=x&installation_id=1&setup_action=install", status: 303},
		"POST /api/v1/autofix/repos":                                 {path: "/api/v1/autofix/repos", body: map[string]any{"installation_id": f.github.installation, "github_repo_id": contractNewRepo, "default_connector": "copilot_issue"}, token: admin, status: 201},
		"PATCH /api/v1/autofix/repos/{id}":                           {path: "/api/v1/autofix/repos/" + f.github.repo, body: map[string]any{"default_connector": "cursor_api"}, token: admin, status: 200},
		"DELETE /api/v1/autofix/repos/{id}":                          {path: "/api/v1/autofix/repos/" + f.github.removableRepo, token: admin, status: 204},
		"PUT /api/v1/autofix/repos/{id}/include-notes":               {path: "/api/v1/autofix/repos/" + f.github.repo + "/include-notes", token: admin, status: 200},
		"DELETE /api/v1/autofix/repos/{id}/include-notes":            {path: "/api/v1/autofix/repos/" + f.github.repo + "/include-notes", token: admin, status: 200},
		"POST /api/v1/conversations/{id}/fixes/preview": {path: "/api/v1/conversations/" + f.conversationID + "/fixes/preview",
			body: map[string]any{"repo_id": f.github.repo, "connector": "claude_issue"}, token: admin, status: 200},
		// The coding agent is off in the fixture: the send is refused before anything is stored.
		"POST /api/v1/conversations/{id}/fixes": {path: "/api/v1/conversations/" + f.conversationID + "/fixes", body: map[string]any{"repo_id": f.github.repo,
			"connector": "claude_issue", "title": "Checkout broken", "body": "It fails.", "client_key": "contract-send"}, token: admin, status: 409},
		// The fixture's fix is merged: not ended (no retry), not in progress (no cancel), released and taken back.
		"POST /api/v1/autofix/fixes/{id}/retry": {path: "/api/v1/autofix/fixes/" + f.fixID + "/retry", body: map[string]any{"repo_id": f.github.repo,
			"connector": "claude_issue", "title": "Checkout broken", "body": "It fails.", "client_key": "contract-retry"}, token: admin, status: 409},
		"POST /api/v1/autofix/fixes/{id}/cancel":             {path: "/api/v1/autofix/fixes/" + f.fixID + "/cancel", token: admin, status: 409},
		"PUT /api/v1/autofix/fixes/{id}/deployed":            {path: "/api/v1/autofix/fixes/" + f.fixID + "/deployed", token: admin, status: 200},
		"DELETE /api/v1/autofix/fixes/{id}/deployed":         {path: "/api/v1/autofix/fixes/" + f.fixID + "/deployed", token: admin, status: 200},
		"PUT /api/v1/autofix/fixes/{id}/pull-request":        {path: "/api/v1/autofix/fixes/" + f.fixID + "/pull-request", body: map[string]any{"url": "https://github.com/acme/shop-web/pull/7"}, token: admin, status: 409},
		"DELETE /api/v1/autofix/fixes/{id}/pull-request":     {path: "/api/v1/autofix/fixes/" + f.fixID + "/pull-request", token: admin, status: 409},
		"POST /api/v1/webhooks/github":                       {path: githubWebhookPath, raw: `{"zen":"Design for failure.","installation":{"id":` + contractInstallationText + `}}`, status: 200},
		"POST /api/v1/channels/email/microsoft":              {path: "/api/v1/channels/email/microsoft", token: admin, status: 200},
		"DELETE /api/v1/channels/email/microsoft":            {path: "/api/v1/channels/email/microsoft", token: admin, status: 404},
		"GET /api/v1/channels/email/microsoft/callback":      {path: "/api/v1/channels/email/microsoft/callback?state=unknown&code=x", status: 303},
		"GET /api/v1/channels/email/microsoft/admin-consent": {path: "/api/v1/channels/email/microsoft/admin-consent?admin_consent=True&tenant=x&state=y", status: 200},
		"POST /api/v1/channels/email/inbound": {path: inboundPath, status: 200, raw: mail([]string{
			"From: Contract Customer <contract@customer.example>", "Subject: Contract mail", "Message-ID: <contract@customer.example>",
		}, "Contract inbound.")},
		// Routes run in order; PATCH /workspace/slug above renamed the workspace to contract-desk.
		"POST /api/v1/channels/email/inbound/recipient":        {path: recipientPath, body: map[string]any{"recipient": "support@contract-desk." + hostedDomain}, status: 204},
		"GET /api/v1/billing":                                  {path: "/api/v1/billing", token: admin, status: 200},
		"POST /api/v1/billing/checkout":                        {path: "/api/v1/billing/checkout", body: map[string]any{"plan": "team_monthly"}, token: admin, status: 200},
		"POST /api/v1/billing/portal":                          {path: "/api/v1/billing/portal", token: f.portalToken, status: 200},
		"POST " + webhookPath:                                  {path: webhookPath, raw: f.webhookBody, status: 200},
		"POST " + resendWebhookPath:                            {path: resendWebhookPath, raw: string(resendEvent("email.delivered", "Desk <support@contract-all.example.com>", []string{"contact@contract-all.example.com"}, "", time.Now())), status: 200},
		"GET /api/v1/founding":                                 {path: "/api/v1/founding", status: 200},
		"GET /api/v1/conversations/{id}/time-entries":          {path: "/api/v1/conversations/" + f.conversationID + "/time-entries", token: admin, status: 200},
		"GET /api/v1/inbox/activity":                           {path: "/api/v1/inbox/activity?since=2026-01-01T00:00:00Z", token: admin, status: 200},
		"GET /api/v1/attachments/{id}":                         {path: "/api/v1/attachments/" + f.attachmentID, token: admin, status: 200},
		"GET /api/v1/mail-image":                               {path: f.mailImagePath, token: admin, status: 200},
		"GET /api/v1/export":                                   {path: "/api/v1/export", token: admin, status: 200},
		"POST /api/v1/export/link":                             {path: "/api/v1/export/link", token: admin, status: 201},
		"GET /api/v1/export/download/{token}":                  {path: "/api/v1/export/download/not-a-link", status: 303},
		"GET /api/v1/channels/web-form":                        {path: "/api/v1/channels/web-form", token: admin, status: 200},
		"PUT /api/v1/channels/web-form":                        {path: "/api/v1/channels/web-form", body: map[string]any{"enabled": true, "allowed_origins": []string{"https://contract.example.com"}}, token: admin, status: 200},
		"GET /api/v1/channels":                                 {path: "/api/v1/channels", token: admin, status: 200},
		"POST /api/v1/channels":                                {path: "/api/v1/channels", body: map[string]any{"type": "web_form", "config": map[string]any{"allowed_origins": []string{}}}, token: admin, status: 201},
		"GET /api/v1/channels/events":                          {path: "/api/v1/channels/events", token: admin, status: 200},
		"GET /api/v1/channels/{id}":                            {path: "/api/v1/channels/" + f.webFormChannelID, token: admin, status: 200},
		"PATCH /api/v1/channels/{id}":                          {path: "/api/v1/channels/" + f.webFormChannelID, body: map[string]any{"name": "Contact form"}, token: admin, status: 200},
		"DELETE /api/v1/channels/{id}":                         {path: "/api/v1/channels/" + f.emailChannelID, token: admin, status: 204},
		"PUT /api/v1/channels/{id}/credentials":                {path: "/api/v1/channels/" + f.webFormChannelID + "/credentials", body: map[string]any{"name": "token", "value": "x"}, token: admin, status: 400},
		"POST /api/v1/channels/{id}/check":                     {path: "/api/v1/channels/" + f.webFormChannelID + "/check", token: admin, status: 200},
		"POST /api/v1/channels/{id}/held/replay":               {path: "/api/v1/channels/" + f.webFormChannelID + "/held/replay", body: map[string]any{}, token: admin, status: 200},
		"POST /api/v1/channels/{id}/held/discard":              {path: "/api/v1/channels/" + f.webFormChannelID + "/held/discard", body: map[string]any{}, token: admin, status: 200},
		"GET /api/v1/channels/{id}/email":                      {path: "/api/v1/channels/" + f.secondEmailChannelID + "/email", token: admin, status: 200},
		"POST /api/v1/channels/{id}/email/domain-verification": {path: "/api/v1/channels/" + f.secondEmailChannelID + "/email/domain-verification", token: admin, status: 200},
		"POST /api/v1/channels/{id}/email/secret":              {path: "/api/v1/channels/" + f.secondEmailChannelID + "/email/secret", token: admin, status: 200},
		"POST /api/v1/channels/{id}/email/gmail":               {path: "/api/v1/channels/" + f.secondEmailChannelID + "/email/gmail", token: admin, status: 200},
		"DELETE /api/v1/channels/{id}/email/gmail":             {path: "/api/v1/channels/" + f.secondEmailChannelID + "/email/gmail", token: admin, status: 404},
		"POST /api/v1/channels/{id}/email/microsoft":           {path: "/api/v1/channels/" + f.secondEmailChannelID + "/email/microsoft", token: admin, status: 200},
		"DELETE /api/v1/channels/{id}/email/microsoft":         {path: "/api/v1/channels/" + f.secondEmailChannelID + "/email/microsoft", token: admin, status: 404},
		"POST /api/v1/public/web-forms/{channel_id}":           {path: "/api/v1/public/web-forms/" + f.webFormChannelID, body: map[string]any{"email": "visitor@contract.example.com", "source": "site:careers"}, status: 202},
		"GET /api/v1/agent-requests":                           {path: "/api/v1/agent-requests?status=pending", token: admin, status: 200},
		"GET /api/v1/agent-requests/{id}":                      {path: "/api/v1/agent-requests/" + f.approveRequestID, token: f.requestToken, status: 200},
		"POST /api/v1/agent-requests/{id}/approve":             {path: "/api/v1/agent-requests/" + f.approveRequestID + "/approve", token: admin, status: 200},
		"POST /api/v1/agent-requests/{id}/reject":              {path: "/api/v1/agent-requests/" + f.rejectRequestID + "/reject", body: map[string]any{"reason": "Not now"}, token: admin, status: 200},
		"POST /api/v1/agent-requests/{id}/cancel":              {path: "/api/v1/agent-requests/" + f.cancelRequestID + "/cancel", token: f.requestToken, status: 200},
		"POST /api/v1/support/problem-reports":                 {path: "/api/v1/support/problem-reports", body: goodReport(), token: admin, status: 201},
		"GET /api/v1/imports":                                  {path: "/api/v1/imports", token: admin, status: 200},
		"GET /api/v1/imports/{id}":                             {path: "/api/v1/imports/" + f.imports.previewed, token: admin, status: 200},
		"PUT /api/v1/imports/{id}/key":                         {path: "/api/v1/imports/" + f.imports.waitingKey + "/key", body: map[string]any{"api_key": "contract-key"}, token: admin, status: 200},
		"POST /api/v1/imports/{id}/cancel":                     {path: "/api/v1/imports/" + f.imports.waitingKey + "/cancel", token: admin, status: 200},
		"POST /api/v1/imports/{id}/start":                      {path: "/api/v1/imports/" + f.imports.previewed + "/start", token: admin, status: 200},
		"POST /api/v1/imports":                                 {path: "/api/v1/imports", body: map[string]any{"source": "freshdesk", "domain": "contract.freshdesk.com", "api_key": "contract-key"}, token: admin, status: 202},
		"GET /api/v1/imports/{id}/report":                      {path: "/api/v1/imports/" + f.imports.finished + "/report", token: admin, status: 200},
		// The import started above holds the workspace: an undo now answers 409 import_running.
		"POST /api/v1/imports/{id}/undo":                       {path: "/api/v1/imports/" + f.imports.finished + "/undo", token: admin, status: 409},
		"GET /api/v1/imports/{id}/people":                      {path: "/api/v1/imports/" + f.imports.finished + "/people", token: admin, status: 200},
		"PUT /api/v1/imports/{id}/people/{externalId}/link":    {path: "/api/v1/imports/" + f.imports.finished + "/people/102/link", body: map[string]any{"agent_id": f.agentID}, token: admin, status: 200},
		"DELETE /api/v1/imports/{id}/people/{externalId}/link": {path: "/api/v1/imports/" + f.imports.finished + "/people/102/link", token: admin, status: 200},
		"GET /api/v1/ai":                                                   {path: "/api/v1/ai", token: admin, status: 200},
		"POST /api/v1/ai/providers":                                        {path: "/api/v1/ai/providers", body: map[string]any{"kind": "openai_compatible", "label": "Contract router", "base_url": "https://openrouter.ai/api/v1", "api_key": "sk-contract-0002", "models": []string{"openai/gpt-5"}}, token: admin, status: 201},
		"POST /api/v1/ai/models":                                           {path: "/api/v1/ai/models", body: map[string]any{"kind": "openai_compatible", "base_url": "https://openrouter.ai/api/v1", "api_key": "sk-contract-0004"}, token: admin, status: 200},
		"PATCH /api/v1/ai/providers/{id}":                                  {path: "/api/v1/ai/providers/" + f.aiProviderID, body: map[string]any{"label": "Contract key renamed"}, token: admin, status: 200},
		"PUT /api/v1/ai/providers/{id}/key":                                {path: "/api/v1/ai/providers/" + f.aiProviderID + "/key", body: map[string]any{"api_key": "sk-contract-0003"}, token: admin, status: 200},
		"POST /api/v1/ai/providers/{id}/check":                             {path: "/api/v1/ai/providers/" + f.aiProviderID + "/check", token: admin, status: 200},
		"PUT /api/v1/ai/slots/{slot}":                                      {path: "/api/v1/ai/slots/triage", body: map[string]any{"source": "provider", "provider_id": f.aiDraftsProviderID, "enabled": true}, token: admin, status: 200},
		"DELETE /api/v1/ai/providers/{id}":                                 {path: "/api/v1/ai/providers/" + f.aiProviderID, token: admin, status: 200},
		"GET /api/v1/ai/usage":                                             {path: "/api/v1/ai/usage?month=2026-10", token: admin, status: 200},
		"PUT /api/v1/ai/automatic-limits":                                  {path: "/api/v1/ai/automatic-limits", body: map[string]any{"per_day": 200, "per_minute": 20}, token: admin, status: 200},
		"POST /api/v1/conversations/{id}/ai-drafts":                        {path: "/api/v1/conversations/" + f.conversationID + "/ai-drafts", body: map[string]any{"prompt": "Another contract draft"}, token: admin, status: 202},
		"GET /api/v1/conversations/{id}/ai-drafts":                         {path: "/api/v1/conversations/" + f.conversationID + "/ai-drafts", token: admin, status: 200},
		"GET /api/v1/conversations/{id}/ai-drafts/{draftId}":               {path: "/api/v1/conversations/" + f.conversationID + "/ai-drafts/" + f.aiDraftID, token: admin, status: 200},
		"POST /api/v1/conversations/{id}/ai-drafts/{draftId}/discard":      {path: "/api/v1/conversations/" + f.conversationID + "/ai-drafts/" + f.aiDraftID + "/discard", token: admin, status: 200},
		"GET /api/v1/conversations/{id}/ai-triage":                         {path: "/api/v1/conversations/" + f.conversationID + "/ai-triage", token: admin, status: 200},
		"POST /api/v1/conversations/{id}/ai-triage":                        {path: "/api/v1/conversations/" + f.aiTriageNewConversation + "/ai-triage", token: admin, status: 202},
		"POST /api/v1/conversations/{id}/ai-triage/{suggestionId}/accept":  {path: "/api/v1/conversations/" + f.conversationID + "/ai-triage/" + f.aiTriageID + "/accept", token: admin, status: 200},
		"POST /api/v1/conversations/{id}/ai-triage/{suggestionId}/dismiss": {path: "/api/v1/conversations/" + f.aiTriageOtherConversation + "/ai-triage/" + f.aiTriageOtherID + "/dismiss", token: admin, status: 200},
		"POST /api/v1/conversations/{id}/ai-triage/{suggestionId}/undo":    {path: "/api/v1/conversations/" + f.aiTriageAutoConversation + "/ai-triage/" + f.aiTriageAutoID + "/undo", token: admin, status: 200},
		"POST /api/v1/public/forms/{workspace_slug}":                       {path: "/api/v1/public/forms/contract-desk" /* the slug set by PATCH /api/v1/workspace/slug, which runs earlier */, body: map[string]any{"email": "visitor@contract.example.com", "source": "site:updates"}, status: 202},
	}
	maps.Copy(cases, kbContractCases(f))
	return cases
}
