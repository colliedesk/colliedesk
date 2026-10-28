package httpapi_test

import (
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

// issueCustomToken creates a token that holds exactly permissions.
func issueCustomToken(t *testing.T, api http.Handler, sessionToken, label string, permissions ...string) issuedToken {
	t.Helper()
	response := request(t, api, http.MethodPost, "/api/v1/auth/tokens", map[string]any{"label": label, "permissions": permissions}, sessionToken, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create custom token status=%d body=%s", response.Code, response.Body.String())
	}
	var issued issuedToken
	decode(t, response, &issued)
	return issued
}

// Each route's permission is held by the tier the route had before permissions, so a preset token
// reaches exactly what its tier reached (routeTiers is that list, written out by hand), and every
// permission a token can hold opens at least one route.
func TestEveryRoutePermissionIsHeldByTheTierTheRouteHad(t *testing.T) {
	used := map[string]bool{}
	for key, access := range httpapi.TokenAccessTable() {
		if access.Permission == "" {
			continue
		}
		used[access.Permission] = true
		if !auth.ValidPermission(access.Permission) {
			t.Errorf("%s names unknown permission %q", key, access.Permission)
			continue
		}
		want := routeTiers[key]
		if access.Approval || access.Tier == "handler" || want == "approval" || want == "handler" {
			continue
		}
		if got := auth.PermissionPreset(access.Permission); got != want {
			t.Errorf("%s needs %s, held from the %s tier; the route was %s", key, access.Permission, got, want)
		}
	}
	// Publishing is checked by the handlers that change what the help center shows.
	used[auth.PermKBArticlesPublish] = true
	for _, permission := range auth.TokenPermissions() {
		if !used[permission.Name] {
			t.Errorf("permission %s opens no route", permission.Name)
		}
	}
}

func TestCustomTokenHoldsExactlyItsPermissions(t *testing.T) {
	desk := newRequestDesk(t, "Custom Token")
	writer := issueCustomToken(t, desk.api, desk.admin, "KB writer", auth.PermKBArticlesUpdate, auth.PermKBArticlesRead, auth.PermKBArticlesCreate, auth.PermKBArticlesRead)
	if writer.Session.Scope == nil || *writer.Session.Scope != auth.ScopeCustom {
		t.Fatalf("session scope = %v, want custom", writer.Session.Scope)
	}
	var me struct {
		Scope       string   `json:"scope"`
		Permissions []string `json:"permissions"`
	}
	decode(t, request(t, desk.api, http.MethodGet, "/api/v1/auth/me", nil, writer.Token, nil), &me)
	if want := []string{"kb_articles:create", "kb_articles:read", "kb_articles:update"}; me.Scope != "custom" || !slices.Equal(me.Permissions, want) {
		t.Fatalf("me = %+v, want custom with %v (sorted, once each)", me, want)
	}
	if response := request(t, desk.api, http.MethodGet, "/api/v1/kb/articles", nil, writer.Token, nil); response.Code != http.StatusOK {
		t.Errorf("listing articles: status=%d body=%s", response.Code, response.Body.String())
	}
	// What it does not hold, even what every read token may do, waits for a person.
	accepted(t, desk.api, http.MethodGet, "/api/v1/conversations", nil, writer.Token)
	asked := accepted(t, desk.api, http.MethodPatch, "/api/v1/conversations/"+desk.conversation, map[string]any{"status": "pending"}, writer.Token)
	if got := getRequest(t, desk.api, desk.admin, asked.RequestID); got.RequestedBy.TokenScope != "custom" || !slices.Equal(got.RequestedBy.TokenPermissions, me.Permissions) {
		t.Errorf("request keeps %q %v, want the token's custom permissions", got.RequestedBy.TokenScope, got.RequestedBy.TokenPermissions)
	}
	if got := conversationStatus(t, desk.api, desk.admin, desk.conversation); got != "open" {
		t.Errorf("status = %s, want open until a person approves", got)
	}
	// Its own session and sign-out are every token's.
	if response := request(t, desk.api, http.MethodPost, "/api/v1/auth/logout", nil, writer.Token, nil); response.Code >= 300 {
		t.Errorf("logout: status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestCustomTokenRequestsAreChecked(t *testing.T) {
	desk := newRequestDesk(t, "Custom Checks")
	agent := joinAsAgent(t, desk.api, desk.admin, "agent@custom-checks.example.com")
	for _, item := range []struct {
		name  string
		token string
		body  map[string]any
		want  int
		code  string
	}{
		{"unknown permission", desk.admin, map[string]any{"label": "x", "permissions": []string{"conversations:fly"}}, http.StatusBadRequest, "invalid_request"},
		{"no permission", desk.admin, map[string]any{"label": "x", "permissions": []string{}}, http.StatusBadRequest, "invalid_request"},
		{"scope and permissions", desk.admin, map[string]any{"label": "x", "scope": "read", "permissions": []string{"tags:read"}}, http.StatusBadRequest, "invalid_request"},
		// An admin-only permission acts as the admin who created it: only an admin may give one.
		{"agent gives an admin-only permission", agent, map[string]any{"label": "x", "permissions": []string{"kb_articles:read", "kb_media:purge"}}, http.StatusForbidden, "admin_token_needs_admin"},
		{"agent gives the agent's permissions", agent, map[string]any{"label": "x", "permissions": []string{"kb_articles:read", "messages:send"}}, http.StatusCreated, ""},
	} {
		response := request(t, desk.api, http.MethodPost, "/api/v1/auth/tokens", item.body, item.token, nil)
		if response.Code != item.want || (item.code != "" && !strings.Contains(response.Body.String(), item.code)) {
			t.Errorf("%s: status=%d body=%s, want %d %s", item.name, response.Code, response.Body.String(), item.want, item.code)
		}
	}
	// A token asking for a token with an admin-only permission waits for an admin.
	send := issueToken(t, desk.api, desk.admin, "Sender", "send").Token
	asked := accepted(t, desk.api, http.MethodPost, "/api/v1/auth/tokens", map[string]any{"label": "Purger", "permissions": []string{"kb_media:purge"}}, send)
	if got := getRequest(t, desk.api, desk.admin, asked.RequestID).ApprovableBy; got != "admin" {
		t.Errorf("a custom token with an admin-only permission is approvable by %q, want admin", got)
	}
}

// A token without kb_articles:publish writes drafts; anything that changes what the public help
// center shows waits for a person.
func TestKBChangesThatReachTheHelpCenterNeedPublish(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "Publish Gate", "admin@publish-gate.example.com")
	collection := kbCollection(t, api, admin, map[string]any{"name": "Guides"})
	image := uploadKBImage(t, api, admin, pngImage(211), http.StatusCreated)
	other := uploadKBImage(t, api, admin, pngImage(212), http.StatusCreated)
	live := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Live", "status": "published", "body_md": "![x](/api/v1/kb/images/" + image.ID + ")"})
	writer := issueCustomToken(t, api, admin, "Drafts only", auth.PermKBArticlesRead, auth.PermKBArticlesCreate, auth.PermKBArticlesUpdate,
		auth.PermKBArticlesDelete, auth.PermKBMediaRead, auth.PermKBMediaUpdate).Token
	publisher := issueCustomToken(t, api, admin, "Publisher", auth.PermKBArticlesRead, auth.PermKBArticlesUpdate, auth.PermKBArticlesPublish).Token

	// A draft is the writer's to make and change.
	var draft kbArticleView
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles", map[string]any{"collection_id": collection.ID, "title": "Draft", "body_md": "Soon"}, writer, http.StatusCreated, &draft)
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+draft.ID, map[string]any{"updated_at": draft.UpdatedAt, "title": "Draft 2"}, writer, http.StatusOK, nil)

	for _, call := range []struct {
		name, method, path string
		body               any
	}{
		{"create a published article", http.MethodPost, "/api/v1/kb/articles", map[string]any{"collection_id": collection.ID, "title": "Now", "body_md": "Out", "status": "published"}},
		{"publish a draft", http.MethodPatch, "/api/v1/kb/articles/" + draft.ID, map[string]any{"updated_at": articleSeen(t, api, admin, draft.ID), "status": "published"}},
		{"edit a published article", http.MethodPatch, "/api/v1/kb/articles/" + live.ID, map[string]any{"updated_at": live.UpdatedAt, "title": "Changed"}},
		{"trash a published article", http.MethodDelete, "/api/v1/kb/articles/" + live.ID + "?updated_at=" + url.QueryEscape(live.UpdatedAt), nil},
		{"rename a collection", http.MethodPatch, "/api/v1/kb/collections/" + collection.ID, map[string]any{"updated_at": collection.UpdatedAt, "name": "Renamed"}},
		{"replace an image a published article shows", http.MethodPost, "/api/v1/kb/media/" + image.ID + "/replace", map[string]any{"with_media_id": other.ID}},
	} {
		response := request(t, api, call.method, call.path, call.body, writer, nil)
		if response.Code != http.StatusAccepted || !strings.Contains(response.Body.String(), `"request_id"`) {
			t.Errorf("%s without publish: status=%d body=%s, want 202 with an agent request", call.name, response.Code, response.Body.String())
		}
	}
	var after kbArticleView
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles/"+live.ID, nil, admin, http.StatusOK, &after)
	if after.Title != "Live" || !strings.Contains(after.BodyMD, image.ID) {
		t.Errorf("the published article changed before a person approved: %+v", after)
	}
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+live.ID, map[string]any{"updated_at": live.UpdatedAt, "title": "Changed"}, publisher, http.StatusOK, nil)
}

// Accepting or undoing a triage suggestion changes the conversation: it needs conversations:update.
func TestAcceptingTriageNeedsConversationUpdate(t *testing.T) {
	desk := newRequestDesk(t, "Triage Gate")
	assist := issueCustomToken(t, desk.api, desk.admin, "Assist only", auth.PermConversationsRead, auth.PermAIAssistRead, auth.PermAIAssistCreate).Token
	base := "/api/v1/conversations/" + desk.conversation + "/ai-triage/00000000-0000-4000-8000-000000000001"
	accepted(t, desk.api, http.MethodPost, base+"/accept", map[string]any{"priority": "high"}, assist)
	accepted(t, desk.api, http.MethodPost, base+"/undo", nil, assist)
}

// A custom token reaches only its own workspace.
func TestCustomTokenOfOneWorkspaceCannotReachAnother(t *testing.T) {
	alpha := newRequestDesk(t, "Custom Alpha")
	beta := createSession(t, alpha.api, alpha.service, "Custom Beta", "admin@custom-beta.example.com")
	betaContact := create(t, alpha.api, beta, "/api/v1/contacts", map[string]any{"name": "Bo", "email": "bo@custom-beta.example.com"})
	betaConversation := create(t, alpha.api, beta, "/api/v1/conversations", map[string]any{"contact_id": betaContact, "subject": "Beta only"})
	token := issueCustomToken(t, alpha.api, alpha.admin, "Reader", auth.PermConversationsRead, auth.PermMessagesRead).Token
	for _, path := range []string{"/api/v1/conversations/" + betaConversation, "/api/v1/conversations/" + betaConversation + "/messages"} {
		if response := request(t, alpha.api, http.MethodGet, path, nil, token, nil); response.Code != http.StatusNotFound {
			t.Errorf("GET %s of another workspace: status=%d body=%s, want 404", path, response.Code, response.Body.String())
		}
	}
}

// articleSeen is the updated_at a change to the article sends.
func articleSeen(t *testing.T, api http.Handler, token, id string) string {
	t.Helper()
	var article kbArticleView
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles/"+id, nil, token, http.StatusOK, &article)
	return article.UpdatedAt
}
