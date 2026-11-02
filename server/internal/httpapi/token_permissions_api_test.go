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

// Every permission opens at least one route (or a handler's check), and every route names a known one.
func TestEveryPermissionOpensARoute(t *testing.T) {
	used := map[string]bool{}
	for key, access := range httpapi.RouteAccessTable() {
		permission := access.Permission
		if access.PersonNeeds != "" {
			permission = access.PersonNeeds
		}
		if permission == "" || access.Tier == auth.ScopeOperator || strings.Contains(key, "/api/v1/operator/") {
			continue
		}
		used[permission] = true
		if !auth.ValidPermission(permission) {
			t.Errorf("%s names unknown permission %q", key, permission)
		}
	}
	// Publishing is checked by the handlers that change what the help center shows; messages:send by the
	// reply and status handlers too.
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
	if writer.Session.Token == nil || writer.Session.Token.Kind != "custom" {
		t.Fatalf("session token = %+v, want custom", writer.Session.Token)
	}
	var me struct {
		Token struct {
			Kind string `json:"kind"`
		} `json:"token"`
		Permissions []string `json:"permissions"`
	}
	decode(t, request(t, desk.api, http.MethodGet, "/api/v1/auth/me", nil, writer.Token, nil), &me)
	if want := []string{"kb_articles:read", "kb_articles:create", "kb_articles:update"}; me.Token.Kind != "custom" || !slices.Equal(me.Permissions, want) {
		t.Fatalf("me = %+v, want custom with %v (in the matrix order, once each)", me, want)
	}
	if response := request(t, desk.api, http.MethodGet, "/api/v1/kb/articles", nil, writer.Token, nil); response.Code != http.StatusOK {
		t.Errorf("listing articles: status=%d body=%s", response.Code, response.Body.String())
	}
	// What it was not given is refused, even what its person holds; nothing changes.
	refused(t, desk.api, http.MethodGet, "/api/v1/conversations", nil, writer.Token, "conversations:read")
	if from := refused(t, desk.api, http.MethodPatch, "/api/v1/conversations/"+desk.conversation, map[string]any{"status": "pending"}, writer.Token, "conversations:update"); from != "token" {
		t.Errorf("missing from %q, want token: its person holds it", from)
	}
	if got := conversationStatus(t, desk.api, desk.admin, desk.conversation); got != "open" {
		t.Errorf("status = %s, want open", got)
	}
	// Its own session and sign-out are every token's.
	if response := request(t, desk.api, http.MethodPost, "/api/v1/auth/logout", nil, writer.Token, nil); response.Code >= 300 {
		t.Errorf("logout: status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestTokenRequestsAreChecked(t *testing.T) {
	desk := newRequestDesk(t, "Custom Checks")
	agent := joinAsAgent(t, desk.api, desk.admin, "agent@custom-checks.example.com")
	adminRole := roleID(t, desk.api, desk.admin, "Admin")
	for _, item := range []struct {
		name  string
		token string
		body  map[string]any
		want  int
		code  string
	}{
		{"unknown permission", desk.admin, map[string]any{"label": "x", "permissions": []string{"conversations:fly"}}, http.StatusBadRequest, "invalid_request"},
		{"no permission", desk.admin, map[string]any{"label": "x", "permissions": []string{}}, http.StatusBadRequest, "invalid_request"},
		{"a scope (the old tiers)", desk.admin, map[string]any{"label": "x", "scope": "read"}, http.StatusBadRequest, "invalid_request"},
		{"role and permissions", desk.admin, map[string]any{"label": "x", "role_id": adminRole, "permissions": []string{"tags:read"}}, http.StatusBadRequest, "invalid_request"},
		{"agent gives a permission they lack", agent, map[string]any{"label": "x", "permissions": []string{"kb_articles:read", "kb_media:purge"}}, http.StatusForbidden, "role_exceeds_yours"},
		{"agent follows a role they lack", agent, map[string]any{"label": "x", "role_id": adminRole}, http.StatusForbidden, "role_not_yours"},
		{"agent gives the agent's permissions", agent, map[string]any{"label": "x", "permissions": []string{"kb_articles:read", "messages:send"}}, http.StatusCreated, ""},
		{"agent follows the Agent role", agent, map[string]any{"label": "y", "role_id": roleID(t, desk.api, agent, "Agent")}, http.StatusCreated, ""},
	} {
		response := request(t, desk.api, http.MethodPost, "/api/v1/auth/tokens", item.body, item.token, nil)
		if response.Code != item.want || (item.code != "" && !strings.Contains(response.Body.String(), item.code)) {
			t.Errorf("%s: status=%d body=%s, want %d %s", item.name, response.Code, response.Body.String(), item.want, item.code)
		}
	}
	// A token never makes tokens: it would leave them behind when it is revoked.
	admin := issueToken(t, desk.api, desk.admin, "Ops", "admin").Token
	response := request(t, desk.api, http.MethodPost, "/api/v1/auth/tokens", map[string]any{"label": "Child", "permissions": []string{"tags:read"}}, admin, nil)
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "person_only") || !strings.Contains(response.Body.String(), "/settings") {
		t.Errorf("a token making a token: status=%d body=%s, want 403 person_only that says where a person does it", response.Code, response.Body.String())
	}
}

// A token without kb_articles:publish writes drafts; anything that changes what the public help
// center shows is refused.
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
		{"reorder a collection", http.MethodPut, "/api/v1/kb/order", kbOrderBody(collection.ID, []string{}, []string{live.ID, draft.ID})},
		{"rename a collection", http.MethodPatch, "/api/v1/kb/collections/" + collection.ID, map[string]any{"updated_at": collection.UpdatedAt, "name": "Renamed"}},
		{"replace an image a published article shows", http.MethodPost, "/api/v1/kb/media/" + image.ID + "/replace", map[string]any{"with_media_id": other.ID}},
	} {
		response := request(t, api, call.method, call.path, call.body, writer, nil)
		if missing, names, _ := permissionMissing(t, response); !missing || !slices.Contains(names, "kb_articles:publish") {
			t.Errorf("%s without publish: status=%d body=%s, want 403 permission_missing kb_articles:publish", call.name, response.Code, response.Body.String())
		}
	}
	var after kbArticleView
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles/"+live.ID, nil, admin, http.StatusOK, &after)
	if after.Title != "Live" || !strings.Contains(after.BodyMD, image.ID) {
		t.Errorf("a refused call changed the published article: %+v", after)
	}
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+live.ID, map[string]any{"updated_at": live.UpdatedAt, "title": "Changed"}, publisher, http.StatusOK, nil)
}

// Accepting or undoing a triage suggestion changes the conversation: it needs conversations:update.
func TestAcceptingTriageNeedsConversationUpdate(t *testing.T) {
	desk := newRequestDesk(t, "Triage Gate")
	assist := issueCustomToken(t, desk.api, desk.admin, "Assist only", auth.PermConversationsRead, auth.PermAIAssistRead, auth.PermAIAssistCreate).Token
	base := "/api/v1/conversations/" + desk.conversation + "/ai-triage/00000000-0000-4000-8000-000000000001"
	refused(t, desk.api, http.MethodPost, base+"/accept", map[string]any{"priority": "high"}, assist, "conversations:update")
	refused(t, desk.api, http.MethodPost, base+"/undo", nil, assist, "conversations:update")
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
