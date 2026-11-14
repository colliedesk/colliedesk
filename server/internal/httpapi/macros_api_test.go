package httpapi_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type macroView struct {
	ID           string         `json:"id"`
	Title        string         `json:"title"`
	Body         string         `json:"body"`
	Scope        string         `json:"scope"`
	OwnerAgentID *string        `json:"owner_agent_id"`
	Actions      map[string]any `json:"actions"`
	UpdatedAt    string         `json:"updated_at"`
	DeletedAt    *string        `json:"deleted_at"`
	PurgesAt     *string        `json:"purges_at"`
}

type macroResult struct {
	Application struct {
		ID       string   `json:"id"`
		Title    string   `json:"title"`
		Body     string   `json:"body"`
		UndoneAt *string  `json:"undone_at"`
		Restored []string `json:"restored"`
		Kept     []string `json:"kept"`
	} `json:"application"`
	Conversation struct {
		Status     string  `json:"status"`
		Priority   string  `json:"priority"`
		AssigneeID *string `json:"assignee_id"`
		Tags       []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"tags"`
	} `json:"conversation"`
}

func macroList(t *testing.T, api http.Handler, token, path string) []macroView {
	t.Helper()
	response := request(t, api, http.MethodGet, path, nil, token, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("GET %s = %d %s", path, response.Code, response.Body.String())
	}
	var list struct {
		Items []macroView `json:"items"`
	}
	decode(t, response, &list)
	return list.Items
}

func macroSeen(t *testing.T, api http.Handler, token, id string) string {
	t.Helper()
	for _, macro := range macroList(t, api, token, "/api/v1/macros") {
		if macro.ID == id {
			return macro.UpdatedAt
		}
	}
	t.Fatalf("macro %s is not in the list", id)
	return ""
}

func TestMacrosApplyActionsAndUndoThem(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Macro Desk", "admin@macros.example.com")
	teammate := joinAsAgent(t, api, admin, "agent@macros.example.com")
	var me struct {
		Agent struct {
			ID string `json:"id"`
		} `json:"agent"`
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/auth/me", nil, admin, nil), &me)
	contact := create(t, api, admin, "/api/v1/contacts", map[string]any{"name": "Mai Linh", "email": "mai@macros.example.com"})
	conversation := create(t, api, admin, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Where is my refund"})
	billing := create(t, api, admin, "/api/v1/tags", map[string]any{"name": "billing"})
	waiting := create(t, api, admin, "/api/v1/tags", map[string]any{"name": "waiting"})
	if response := request(t, api, http.MethodPut, "/api/v1/conversations/"+conversation+"/tags/"+waiting, nil, admin, nil); response.Code != http.StatusOK {
		t.Fatalf("add tag = %d %s", response.Code, response.Body.String())
	}

	shared := create(t, api, admin, "/api/v1/macros", map[string]any{
		"title": "Refund and wait", "scope": "shared",
		"body": "Hi {{customer.name}}, this is {{agent.name}} on {{ticket.id}}. {{requester.name}} {{not.a.variable}}",
		"actions": map[string]any{"status": "pending", "priority": "high", "assignee_id": me.Agent.ID,
			"add_tag_ids": []string{billing}, "remove_tag_ids": []string{waiting}},
	})
	personal := create(t, api, teammate, "/api/v1/macros", map[string]any{"title": "My note", "scope": "personal", "body": "Just for me."})
	if got := macroList(t, api, admin, "/api/v1/macros"); len(got) != 1 || got[0].ID != shared || got[0].Scope != "shared" {
		t.Fatalf("admin list = %+v, want only the shared macro", got)
	}
	if got := macroList(t, api, teammate, "/api/v1/macros"); len(got) != 2 {
		t.Fatalf("teammate list = %+v, want the shared macro and their own", got)
	}

	var applied macroResult
	decode(t, request(t, api, http.MethodPost, "/api/v1/conversations/"+conversation+"/macros/"+shared+"/apply", nil, admin, nil), &applied)
	ticket := appliedTicket(t, api, admin, conversation)
	if !strings.Contains(applied.Application.Body, "Hi Mai Linh,") || !strings.Contains(applied.Application.Body, ticket) || !strings.Contains(applied.Application.Body, "{{not.a.variable}}") {
		t.Fatalf("rendered body = %q", applied.Application.Body)
	}
	if applied.Conversation.Status != "pending" || applied.Conversation.Priority != "high" || applied.Conversation.AssigneeID == nil || *applied.Conversation.AssigneeID != me.Agent.ID {
		t.Fatalf("conversation after apply = %+v", applied.Conversation)
	}
	names := map[string]bool{}
	for _, tag := range applied.Conversation.Tags {
		names[tag.Name] = true
	}
	if !names["billing"] || names["waiting"] {
		t.Fatalf("tags after apply = %+v", applied.Conversation.Tags)
	}

	// A later status change is kept; the tags the macro set are put back.
	if response := request(t, api, http.MethodPatch, "/api/v1/conversations/"+conversation, map[string]any{"status": "open", "notify": false}, admin, nil); response.Code != http.StatusOK {
		t.Fatalf("status after the macro = %d %s", response.Code, response.Body.String())
	}
	var undone macroResult
	decode(t, request(t, api, http.MethodPost, "/api/v1/conversations/"+conversation+"/macro-applications/"+applied.Application.ID+"/undo", nil, admin, nil), &undone)
	if undone.Application.UndoneAt == nil || !has(undone.Application.Kept, "status") || !has(undone.Application.Restored, "priority") || !has(undone.Application.Restored, "tags") {
		t.Fatalf("undo = restored %v kept %v", undone.Application.Restored, undone.Application.Kept)
	}
	if undone.Conversation.Status != "open" || undone.Conversation.Priority != "normal" {
		t.Fatalf("conversation after undo = %+v", undone.Conversation)
	}
	again := request(t, api, http.MethodPost, "/api/v1/conversations/"+conversation+"/macro-applications/"+applied.Application.ID+"/undo", nil, admin, nil)
	if again.Code != http.StatusOK {
		t.Fatalf("second undo = %d %s", again.Code, again.Body.String())
	}

	if response := request(t, api, http.MethodPost, "/api/v1/conversations/"+conversation+"/macros/"+personal+"/apply", nil, admin, nil); response.Code != http.StatusNotFound {
		t.Fatalf("another person's personal macro = %d, want 404", response.Code)
	}
	seen := macroSeen(t, api, admin, shared)
	if response := request(t, api, http.MethodPatch, "/api/v1/macros/"+shared, map[string]any{"title": "Refund", "updated_at": "2020-01-01T00:00:00Z"}, admin, nil); response.Code != http.StatusConflict {
		t.Fatalf("stale edit = %d, want 409", response.Code)
	}
	if response := request(t, api, http.MethodPatch, "/api/v1/macros/"+shared, map[string]any{"title": "Refund", "updated_at": seen}, admin, nil); response.Code != http.StatusOK {
		t.Fatalf("edit = %d %s", response.Code, response.Body.String())
	}
	if response := request(t, api, http.MethodDelete, "/api/v1/macros/"+shared+"?updated_at="+url.QueryEscape(macroSeen(t, api, admin, shared)), nil, admin, nil); response.Code != http.StatusOK {
		t.Fatalf("delete = %d %s", response.Code, response.Body.String())
	}
	if got := macroList(t, api, admin, "/api/v1/macros"); len(got) != 0 {
		t.Fatalf("list after delete = %+v", got)
	}
	trash := macroList(t, api, admin, "/api/v1/macros/trash")
	if len(trash) != 1 || trash[0].PurgesAt == nil {
		t.Fatalf("trash = %+v", trash)
	}
	if response := request(t, api, http.MethodPost, "/api/v1/macros/"+shared+"/restore", nil, admin, nil); response.Code != http.StatusOK {
		t.Fatalf("restore = %d %s", response.Code, response.Body.String())
	}
	if got := macroList(t, api, admin, "/api/v1/macros"); len(got) != 1 || got[0].Title != "Refund" {
		t.Fatalf("restored = %+v", got)
	}
}

func TestMacrosNeverCrossWorkspaces(t *testing.T) {
	api, service := testAPI(t)
	adminA := createSession(t, api, service, "Macros A", "admin@macros-a.example.com")
	adminB := createSession(t, api, service, "Macros B", "admin@macros-b.example.com")
	contact := create(t, api, adminA, "/api/v1/contacts", map[string]any{"name": "A", "email": "a@macros-a.example.com"})
	conversation := create(t, api, adminA, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "A"})
	theirs := create(t, api, adminB, "/api/v1/macros", map[string]any{"title": "Theirs", "scope": "shared", "body": "Hello"})
	if got := macroList(t, api, adminA, "/api/v1/macros"); len(got) != 0 {
		t.Fatalf("workspace A lists %d macros", len(got))
	}
	for _, call := range []struct{ method, path string }{
		{http.MethodPatch, "/api/v1/macros/" + theirs},
		{http.MethodDelete, "/api/v1/macros/" + theirs + "?updated_at=2020-01-01T00:00:00Z"},
		{http.MethodPost, "/api/v1/macros/" + theirs + "/restore"},
		{http.MethodPost, "/api/v1/conversations/" + conversation + "/macros/" + theirs + "/apply"},
	} {
		if response := request(t, api, call.method, call.path, map[string]any{"title": "Taken", "updated_at": "2020-01-01T00:00:00Z"}, adminA, nil); response.Code != http.StatusNotFound {
			t.Fatalf("%s %s = %d, want 404", call.method, call.path, response.Code)
		}
	}
}

func appliedTicket(t *testing.T, api http.Handler, token, id string) string {
	t.Helper()
	var conversation struct {
		DisplayRef string `json:"display_ref"`
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/conversations/"+id, nil, token, nil), &conversation)
	return conversation.DisplayRef
}

func has(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
