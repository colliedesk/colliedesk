package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type savedReplyView struct {
	ID               string  `json:"id"`
	Title            string  `json:"title"`
	Body             string  `json:"body"`
	CreatedByAgentID *string `json:"created_by_agent_id"`
	CreatedAt        string  `json:"created_at"`
	UpdatedAt        string  `json:"updated_at"`
}

func agentSession(t *testing.T, api http.Handler, adminToken, email string) string {
	t.Helper()
	_, link := createContractInvite(t, api, adminToken, email, "agent")
	redeemed := request(t, api, http.MethodPost, "/api/v1/auth/redeem", map[string]any{"token": linkToken(t, link)}, "", nil)
	var session struct {
		Token string `json:"token"`
		Agent struct {
			Role string `json:"role"`
		} `json:"agent"`
	}
	decode(t, redeemed, &session)
	if session.Agent.Role != "agent" {
		t.Fatalf("redeemed role=%q, want agent", session.Agent.Role)
	}
	return session.Token
}

func listSavedReplies(t *testing.T, api http.Handler, token string) []savedReplyView {
	t.Helper()
	response := request(t, api, http.MethodGet, "/api/v1/saved-replies", nil, token, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("list saved replies status=%d body=%s", response.Code, response.Body.String())
	}
	var list struct {
		Items []savedReplyView `json:"items"`
	}
	decode(t, response, &list)
	return list.Items
}

// seenAt is the updated_at a client holds for a reply: what an edit must send back.
func seenAt(t *testing.T, api http.Handler, token, id string) string {
	t.Helper()
	for _, item := range listSavedReplies(t, api, token) {
		if item.ID == id {
			return item.UpdatedAt
		}
	}
	t.Fatalf("saved reply %s not listed", id)
	return ""
}

// patchReply edits a reply the way the web app does, sending back the updated_at it last saw.
func patchReply(t *testing.T, api http.Handler, token, id string, fields map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	body := map[string]any{"updated_at": seenAt(t, api, token, id)}
	for key, value := range fields {
		body[key] = value
	}
	return request(t, api, http.MethodPatch, "/api/v1/saved-replies/"+id, body, token, nil)
}

func TestAnyAgentCreatesEditsAndDeletesSharedSavedReplies(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Replies Desk", "admin@replies.example.com")
	agent := agentSession(t, api, admin, "agent@replies.example.com")

	created := request(t, api, http.MethodPost, "/api/v1/saved-replies", map[string]any{"title": "  Refund steps  ", "body": "Hi {{customer.name}},\n\nWe refunded you.\n{{agent.name}}"}, agent, nil)
	if created.Code != http.StatusCreated {
		t.Fatalf("agent create status=%d body=%s", created.Code, created.Body.String())
	}
	var reply savedReplyView
	decode(t, created, &reply)
	if reply.Title != "Refund steps" || reply.Body != "Hi {{customer.name}},\n\nWe refunded you.\n{{agent.name}}" || reply.CreatedByAgentID == nil || reply.CreatedAt == "" {
		t.Fatalf("created reply = %+v", reply)
	}
	create(t, api, admin, "/api/v1/saved-replies", map[string]any{"title": "apology", "body": "Sorry."})
	create(t, api, admin, "/api/v1/saved-replies", map[string]any{"title": "Closing", "body": "Bye."})

	// Ordered by title ignoring case, and the admin sees what the agent wrote.
	var titles []string
	for _, item := range listSavedReplies(t, api, admin) {
		titles = append(titles, item.Title)
	}
	if got := strings.Join(titles, "|"); got != "apology|Closing|Refund steps" {
		t.Fatalf("titles = %s, want apology|Closing|Refund steps", got)
	}

	// Another agent edits and deletes a reply an admin wrote.
	adminReply := listSavedReplies(t, api, agent)[0]
	edited := patchReply(t, api, agent, adminReply.ID, map[string]any{"body": "We are sorry."})
	if edited.Code != http.StatusOK {
		t.Fatalf("agent edit status=%d body=%s", edited.Code, edited.Body.String())
	}
	var after savedReplyView
	decode(t, edited, &after)
	if after.Title != "apology" || after.Body != "We are sorry." || after.CreatedByAgentID == nil || *after.CreatedByAgentID != *adminReply.CreatedByAgentID {
		t.Fatalf("edited reply = %+v (before %+v)", after, adminReply)
	}
	renamed := patchReply(t, api, agent, adminReply.ID, map[string]any{"title": "Apology"})
	if renamed.Code != http.StatusOK || !strings.Contains(renamed.Body.String(), `"title":"Apology"`) || !strings.Contains(renamed.Body.String(), `"body":"We are sorry."`) {
		t.Fatalf("rename status=%d body=%s", renamed.Code, renamed.Body.String())
	}
	if deleted := request(t, api, http.MethodDelete, "/api/v1/saved-replies/"+adminReply.ID, nil, agent, nil); deleted.Code != http.StatusNoContent {
		t.Fatalf("agent delete status=%d body=%s", deleted.Code, deleted.Body.String())
	}
	if again := request(t, api, http.MethodDelete, "/api/v1/saved-replies/"+adminReply.ID, nil, agent, nil); again.Code != http.StatusNotFound {
		t.Fatalf("second delete status=%d, want 404", again.Code)
	}
	if got := len(listSavedReplies(t, api, admin)); got != 2 {
		t.Fatalf("replies after delete = %d, want 2", got)
	}
}

func TestSavedReplyValidation(t *testing.T) {
	api, service := testAPI(t)
	token := createSession(t, api, service, "Validation Desk", "admin@reply-validation.example.com")
	existing := create(t, api, token, "/api/v1/saved-replies", map[string]any{"title": "Existing", "body": "Kept"})
	create(t, api, token, "/api/v1/saved-replies", map[string]any{"title": "Other", "body": "Kept"})

	for name, input := range map[string]map[string]any{
		"empty title":        {"title": "", "body": "x"},
		"blank title":        {"title": "   ", "body": "x"},
		"missing title":      {"body": "x"},
		"empty body":         {"title": "T1", "body": ""},
		"blank body":         {"title": "T2", "body": " \n\t "},
		"missing body":       {"title": "T3"},
		"title too long":     {"title": strings.Repeat("a", 201), "body": "x"},
		"body too long":      {"title": "T4", "body": strings.Repeat("a", 20001)},
		"wrong type":         {"title": 5, "body": "x"},
		"unknown field only": {"colour": "red"},
	} {
		if response := request(t, api, http.MethodPost, "/api/v1/saved-replies", input, token, nil); response.Code != http.StatusBadRequest {
			t.Errorf("create with %s: status=%d body=%s, want 400", name, response.Code, response.Body.String())
		}
	}
	// Limits count characters, not bytes: 20 000 Vietnamese letters and 200 of them in a title fit.
	long := request(t, api, http.MethodPost, "/api/v1/saved-replies", map[string]any{"title": strings.Repeat("ế", 200), "body": strings.Repeat("ạ", 20000)}, token, nil)
	if long.Code != http.StatusCreated {
		t.Fatalf("limit-sized reply status=%d body=%s", long.Code, long.Body.String())
	}

	for name, input := range map[string]map[string]any{
		"empty patch":    {},
		"blank title":    {"title": " "},
		"blank body":     {"body": ""},
		"body too long":  {"body": strings.Repeat("a", 20001)},
		"title too long": {"title": strings.Repeat("a", 201)},
	} {
		input["updated_at"] = seenAt(t, api, token, existing)
		if response := request(t, api, http.MethodPatch, "/api/v1/saved-replies/"+existing, input, token, nil); response.Code != http.StatusBadRequest {
			t.Errorf("patch with %s: status=%d body=%s, want 400", name, response.Code, response.Body.String())
		}
	}
	for _, item := range listSavedReplies(t, api, token) {
		if item.ID == existing && (item.Title != "Existing" || item.Body != "Kept") {
			t.Fatalf("a refused edit changed the reply: %+v", item)
		}
	}
}

func TestSavedReplyTitleIsUniqueIgnoringCase(t *testing.T) {
	api, service := testAPI(t)
	token := createSession(t, api, service, "Duplicate Desk", "admin@reply-duplicate.example.com")
	first := create(t, api, token, "/api/v1/saved-replies", map[string]any{"title": "Refund", "body": "one"})
	second := create(t, api, token, "/api/v1/saved-replies", map[string]any{"title": "Shipping", "body": "two"})

	duplicate := request(t, api, http.MethodPost, "/api/v1/saved-replies", map[string]any{"title": " rEFUND ", "body": "three"}, token, nil)
	if duplicate.Code != http.StatusConflict || !strings.Contains(duplicate.Body.String(), `"duplicate_title"`) {
		t.Fatalf("duplicate create status=%d body=%s, want 409 duplicate_title", duplicate.Code, duplicate.Body.String())
	}
	clash := patchReply(t, api, token, second, map[string]any{"title": "REFUND"})
	if clash.Code != http.StatusConflict || !strings.Contains(clash.Body.String(), `"duplicate_title"`) {
		t.Fatalf("rename into a taken title status=%d body=%s, want 409", clash.Code, clash.Body.String())
	}
	// Changing only the case of its own title is not a clash.
	own := patchReply(t, api, token, first, map[string]any{"title": "REFUND"})
	if own.Code != http.StatusOK {
		t.Fatalf("recase own title status=%d body=%s", own.Code, own.Body.String())
	}
	if got := len(listSavedReplies(t, api, token)); got != 2 {
		t.Fatalf("replies = %d, want 2", got)
	}
}

func TestSavedReplyCountIsLimitedPerWorkspace(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	alpha := createSession(t, api, service, "Limit Alpha", "admin@reply-limit-a.example.com")
	beta := createSession(t, api, service, "Limit Beta", "admin@reply-limit-b.example.com")
	if _, err := pool.Exec(t.Context(), `INSERT INTO saved_replies(workspace_id,title,body)
		SELECT $1, 'Reply ' || n, 'Body' FROM generate_series(1,500) n`, workspaceID(t, api, alpha)); err != nil {
		t.Fatal(err)
	}
	full := request(t, api, http.MethodPost, "/api/v1/saved-replies", map[string]any{"title": "One too many", "body": "x"}, alpha, nil)
	if full.Code != http.StatusConflict || !strings.Contains(full.Body.String(), `"saved_reply_limit"`) {
		t.Fatalf("501st reply status=%d body=%s, want 409 saved_reply_limit", full.Code, full.Body.String())
	}
	if ok := request(t, api, http.MethodPost, "/api/v1/saved-replies", map[string]any{"title": "Fits", "body": "x"}, beta, nil); ok.Code != http.StatusCreated {
		t.Fatalf("other workspace status=%d body=%s", ok.Code, ok.Body.String())
	}
	// Deleting one makes room again, and editing a full workspace's reply still works.
	first := listSavedReplies(t, api, alpha)[0]
	if edit := patchReply(t, api, alpha, first.ID, map[string]any{"body": "edited"}); edit.Code != http.StatusOK {
		t.Fatalf("edit in a full workspace status=%d body=%s", edit.Code, edit.Body.String())
	}
	if gone := request(t, api, http.MethodDelete, "/api/v1/saved-replies/"+first.ID, nil, alpha, nil); gone.Code != http.StatusNoContent {
		t.Fatalf("delete status=%d", gone.Code)
	}
	if again := request(t, api, http.MethodPost, "/api/v1/saved-replies", map[string]any{"title": "One too many", "body": "x"}, alpha, nil); again.Code != http.StatusCreated {
		t.Fatalf("create after delete status=%d body=%s", again.Code, again.Body.String())
	}
}

func TestSavedRepliesStayWorkspaceScoped(t *testing.T) {
	api, service := testAPI(t)
	alpha := createSession(t, api, service, "Scope Alpha", "admin@reply-scope-a.example.com")
	beta := createSession(t, api, service, "Scope Beta", "admin@reply-scope-b.example.com")
	alphaID := create(t, api, alpha, "/api/v1/saved-replies", map[string]any{"title": "Shared title", "body": "Alpha text"})
	betaID := create(t, api, beta, "/api/v1/saved-replies", map[string]any{"title": "Shared title", "body": "Beta text"})
	if alphaID == betaID {
		t.Fatal("two workspaces got the same reply id")
	}

	// Beta cannot see, edit or delete Alpha's reply by id; Alpha's reply is unchanged.
	for _, item := range listSavedReplies(t, api, beta) {
		if item.ID == alphaID || item.Body == "Alpha text" {
			t.Fatalf("beta list shows alpha's reply: %+v", item)
		}
	}
	for _, id := range []string{alphaID, "not-a-uuid", "99999999-9999-4999-8999-999999999999"} {
		patch := request(t, api, http.MethodPatch, "/api/v1/saved-replies/"+id, map[string]any{"title": "Hijacked", "body": "Hijacked", "updated_at": "2026-01-01T00:00:00Z"}, beta, nil)
		if patch.Code != http.StatusNotFound {
			t.Errorf("PATCH %s status=%d body=%s, want 404", id, patch.Code, patch.Body.String())
		}
		if del := request(t, api, http.MethodDelete, "/api/v1/saved-replies/"+id, nil, beta, nil); del.Code != http.StatusNotFound {
			t.Errorf("DELETE %s status=%d, want 404", id, del.Code)
		}
	}
	got := listSavedReplies(t, api, alpha)
	if len(got) != 1 || got[0].ID != alphaID || got[0].Title != "Shared title" || got[0].Body != "Alpha text" {
		t.Fatalf("alpha's reply changed: %+v", got)
	}
	if beta := listSavedReplies(t, api, beta); len(beta) != 1 || beta[0].Body != "Beta text" {
		t.Fatalf("beta's reply changed: %+v", beta)
	}
}

func TestSavedRepliesRequireSignIn(t *testing.T) {
	api, _ := testAPI(t)
	for _, call := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/saved-replies"}, {http.MethodPost, "/api/v1/saved-replies"},
		{http.MethodPatch, "/api/v1/saved-replies/99999999-9999-4999-8999-999999999999"},
		{http.MethodDelete, "/api/v1/saved-replies/99999999-9999-4999-8999-999999999999"},
	} {
		if response := request(t, api, call.method, call.path, map[string]any{"title": "x", "body": "x"}, "", nil); response.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without a session: status=%d, want 401", call.method, call.path, response.Code)
		}
	}
}

func TestConversationsCarryTheNameOfTheirContact(t *testing.T) {
	api, service := testAPI(t)
	token := createSession(t, api, service, "Name Desk", "admin@contact-name.example.com")
	contact := create(t, api, token, "/api/v1/contacts", map[string]any{"name": "Nguyễn Thị Ánh", "email": "anh@contact-name.example.com"})
	conversation := create(t, api, token, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Hello"})

	type view struct {
		ID          string `json:"id"`
		ContactName string `json:"contact_name"`
	}
	var one view
	decode(t, request(t, api, http.MethodGet, "/api/v1/conversations/"+conversation, nil, token, nil), &one)
	if one.ContactName != "Nguyễn Thị Ánh" {
		t.Fatalf("GET conversation contact_name = %q", one.ContactName)
	}
	var list struct {
		Items []view `json:"items"`
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/conversations", nil, token, nil), &list)
	if len(list.Items) != 1 || list.Items[0].ContactName != "Nguyễn Thị Ánh" {
		t.Fatalf("list = %+v", list.Items)
	}
	var updated view
	decode(t, request(t, api, http.MethodPatch, "/api/v1/conversations/"+conversation, map[string]any{"status": "closed"}, token, nil), &updated)
	if updated.ContactName != "Nguyễn Thị Ánh" {
		t.Fatalf("PATCH conversation contact_name = %q", updated.ContactName)
	}
}

func TestSavedReplyEditNeedsTheVersionTheClientSaw(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Version Desk", "admin@reply-version.example.com")
	agent := agentSession(t, api, admin, "agent@reply-version.example.com")
	id := create(t, api, admin, "/api/v1/saved-replies", map[string]any{"title": "Greeting", "body": "Original"})
	opened := seenAt(t, api, admin, id)

	// Another agent saves first; the first agent's edit, made from the older version, is refused.
	if first := patchReply(t, api, agent, id, map[string]any{"body": "Agent's version"}); first.Code != http.StatusOK {
		t.Fatalf("first edit status=%d body=%s", first.Code, first.Body.String())
	}
	stale := request(t, api, http.MethodPatch, "/api/v1/saved-replies/"+id, map[string]any{"body": "Admin's version", "updated_at": opened}, admin, nil)
	if stale.Code != http.StatusConflict || !strings.Contains(stale.Body.String(), `"changed_elsewhere"`) {
		t.Fatalf("stale edit status=%d body=%s, want 409 changed_elsewhere", stale.Code, stale.Body.String())
	}
	if got := listSavedReplies(t, api, admin)[0].Body; got != "Agent's version" {
		t.Fatalf("a refused edit changed the reply: %q", got)
	}
	// After reloading, the same edit goes through.
	if again := patchReply(t, api, admin, id, map[string]any{"body": "Admin's version"}); again.Code != http.StatusOK {
		t.Fatalf("edit after reload status=%d body=%s", again.Code, again.Body.String())
	}
	// Without the version the request is refused, and so is one that is not a time.
	for name, input := range map[string]map[string]any{"missing": {"body": "x"}, "not a time": {"body": "x", "updated_at": "yesterday"}} {
		if response := request(t, api, http.MethodPatch, "/api/v1/saved-replies/"+id, input, admin, nil); response.Code != http.StatusBadRequest {
			t.Errorf("%s updated_at: status=%d body=%s, want 400", name, response.Code, response.Body.String())
		}
	}
}
