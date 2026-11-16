package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConversationReverseStatesAndTagLifecycle(t *testing.T) {
	api, authService := testAPI(t)
	token := createSession(t, api, authService, "Tag Desk", "admin@tags.example.com")
	contactID := create(t, api, token, "/api/v1/contacts", map[string]any{
		"name": "Tag Customer", "email": "customer@tags.example.com",
	})
	conversationID := create(t, api, token, "/api/v1/conversations", map[string]any{
		"contact_id": contactID, "subject": "Tag lifecycle",
	})

	me := request(t, api, http.MethodGet, "/api/v1/auth/me", nil, token, nil)
	var identity struct {
		Agent struct {
			ID string `json:"id"`
		} `json:"agent"`
	}
	decode(t, me, &identity)
	assertConversationUpdate(t, api, token, conversationID, map[string]any{"status": "closed"}, "closed", identity.Agent.ID, false)
	assertConversationUpdate(t, api, token, conversationID, map[string]any{"status": "open"}, "open", identity.Agent.ID, false)
	assertConversationUpdate(t, api, token, conversationID, map[string]any{"assignee_id": identity.Agent.ID}, "open", identity.Agent.ID, true)
	assertConversationUpdate(t, api, token, conversationID, map[string]any{"assignee_id": nil}, "open", identity.Agent.ID, false)

	created := request(t, api, http.MethodPost, "/api/v1/tags", map[string]any{"name": "Billing", "color": "#2563eb"}, token, nil)
	if created.Code != http.StatusCreated {
		t.Fatalf("create tag status=%d body=%s", created.Code, created.Body.String())
	}
	var tag struct {
		ID string `json:"id"`
	}
	decode(t, created, &tag)
	renamed := request(t, api, http.MethodPatch, "/api/v1/tags/"+tag.ID, map[string]any{"name": "Accounts"}, token, nil)
	if renamed.Code != http.StatusOK || !strings.Contains(renamed.Body.String(), `"name":"Accounts"`) {
		t.Fatalf("rename tag status=%d body=%s", renamed.Code, renamed.Body.String())
	}
	added := request(t, api, http.MethodPatch, "/api/v1/conversations/"+conversationID, map[string]any{"tag_ids": []string{tag.ID}}, token, nil)
	if added.Code != http.StatusOK || !bodyHasTag(t, added, tag.ID) {
		t.Fatalf("add tag status=%d body=%s", added.Code, added.Body.String())
	}
	removed := request(t, api, http.MethodPatch, "/api/v1/conversations/"+conversationID, map[string]any{"tag_ids": []string{}}, token, nil)
	if removed.Code != http.StatusOK || bodyHasTag(t, removed, tag.ID) {
		t.Fatalf("remove tag status=%d body=%s", removed.Code, removed.Body.String())
	}
	deleted := request(t, api, http.MethodDelete, "/api/v1/tags/"+tag.ID, nil, token, nil)
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete tag status=%d body=%s", deleted.Code, deleted.Body.String())
	}
}

func TestTagReplacementRejectsUnknownAndForeignIDsAtomically(t *testing.T) {
	api, authService := testAPI(t)
	tokenA := createSession(t, api, authService, "Tag Alpha", "admin@tag-alpha.example.com")
	tokenB := createSession(t, api, authService, "Tag Beta", "admin@tag-beta.example.com")
	contactID := create(t, api, tokenA, "/api/v1/contacts", map[string]any{"name": "Alpha", "email": "alpha@customer.example.com"})
	conversationID := create(t, api, tokenA, "/api/v1/conversations", map[string]any{"contact_id": contactID, "subject": "Keep tag"})
	validTag := create(t, api, tokenA, "/api/v1/tags", map[string]any{"name": "Keep"})
	foreignTag := create(t, api, tokenB, "/api/v1/tags", map[string]any{"name": "Foreign"})
	assigned := request(t, api, http.MethodPatch, "/api/v1/conversations/"+conversationID, map[string]any{"tag_ids": []string{validTag}}, tokenA, nil)
	if assigned.Code != http.StatusOK {
		t.Fatalf("seed tag status=%d body=%s", assigned.Code, assigned.Body.String())
	}

	for name, invalidID := range map[string]string{
		"unknown": "99999999-9999-4999-8999-999999999999",
		"foreign": foreignTag,
	} {
		t.Run(name, func(t *testing.T) {
			response := request(t, api, http.MethodPatch, "/api/v1/conversations/"+conversationID, map[string]any{
				"status": "closed", "tag_ids": []string{validTag, invalidID},
			}, tokenA, nil)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("replace status=%d body=%s, want 400", response.Code, response.Body.String())
			}
			current := request(t, api, http.MethodGet, "/api/v1/conversations/"+conversationID, nil, tokenA, nil)
			if !strings.Contains(current.Body.String(), `"status":"open"`) || !bodyHasTag(t, current, validTag) {
				t.Fatalf("rejected replacement changed conversation: %s", current.Body.String())
			}
		})
	}
}

func TestConversationTagEndpointsAreIdempotentAndWorkspaceScoped(t *testing.T) {
	api, service := testAPI(t)
	tokenA := createSession(t, api, service, "Atomic Tags Alpha", "admin@atomic-tags-a.example.com")
	tokenB := createSession(t, api, service, "Atomic Tags Beta", "admin@atomic-tags-b.example.com")
	contactA := create(t, api, tokenA, "/api/v1/contacts", map[string]any{"name": "Alpha", "email": "alpha@atomic.example.com"})
	contactB := create(t, api, tokenB, "/api/v1/contacts", map[string]any{"name": "Beta", "email": "beta@atomic.example.com"})
	conversationA := create(t, api, tokenA, "/api/v1/conversations", map[string]any{"contact_id": contactA, "subject": "Alpha tags"})
	conversationB := create(t, api, tokenB, "/api/v1/conversations", map[string]any{"contact_id": contactB, "subject": "Beta tags"})
	tagA := create(t, api, tokenA, "/api/v1/tags", map[string]any{"name": "Alpha tag"})
	tagB := create(t, api, tokenB, "/api/v1/tags", map[string]any{"name": "Beta tag"})

	pathA := "/api/v1/conversations/" + conversationA + "/tags/" + tagA
	for attempt := 1; attempt <= 2; attempt++ {
		added := request(t, api, http.MethodPut, pathA, nil, tokenA, nil)
		if added.Code != http.StatusOK || !bodyHasTag(t, added, tagA) {
			t.Fatalf("add attempt %d status=%d body=%s", attempt, added.Code, added.Body.String())
		}
	}
	for _, path := range []string{
		"/api/v1/conversations/" + conversationA + "/tags/" + tagB,
		"/api/v1/conversations/" + conversationB + "/tags/" + tagA,
	} {
		for _, method := range []string{http.MethodPut, http.MethodDelete} {
			response := request(t, api, method, path, nil, tokenA, nil)
			if response.Code != http.StatusNotFound {
				t.Fatalf("%s foreign path status=%d body=%s, want 404", method, response.Code, response.Body.String())
			}
		}
	}
	for attempt := 1; attempt <= 2; attempt++ {
		removed := request(t, api, http.MethodDelete, pathA, nil, tokenA, nil)
		if removed.Code != http.StatusOK || bodyHasTag(t, removed, tagA) {
			t.Fatalf("remove attempt %d status=%d body=%s", attempt, removed.Code, removed.Body.String())
		}
	}
}

func assertConversationUpdate(t *testing.T, api http.Handler, token, id string, update map[string]any, wantStatus, assigneeID string, wantAssigned bool) {
	t.Helper()
	response := request(t, api, http.MethodPatch, "/api/v1/conversations/"+id, update, token, nil)
	var conversation struct {
		Status     string  `json:"status"`
		AssigneeID *string `json:"assignee_id"`
	}
	decode(t, response, &conversation)
	if response.Code != http.StatusOK || conversation.Status != wantStatus {
		t.Fatalf("update status=%d body=%s", response.Code, response.Body.String())
	}
	if wantAssigned != (conversation.AssigneeID != nil && *conversation.AssigneeID == assigneeID) {
		t.Fatalf("assignee=%v, want assigned=%v", conversation.AssigneeID, wantAssigned)
	}
}

func bodyHasTag(t *testing.T, response *httptest.ResponseRecorder, tagID string) bool {
	t.Helper()
	var conversation struct {
		Tags []struct {
			ID string `json:"id"`
		} `json:"tags"`
	}
	decode(t, response, &conversation)
	for _, tag := range conversation.Tags {
		if tag.ID == tagID {
			return true
		}
	}
	return false
}

func TestTagListCountsConversationsOnlyWhenAskedAndOnlyInItsWorkspace(t *testing.T) {
	api, service := testAPI(t)
	tokenA := createSession(t, api, service, "Tag Count Alpha", "admin@tag-count-a.example.com")
	tokenB := createSession(t, api, service, "Tag Count Beta", "admin@tag-count-b.example.com")
	contactA := create(t, api, tokenA, "/api/v1/contacts", map[string]any{"name": "Alpha", "email": "alpha@tag-count.example.com"})
	contactB := create(t, api, tokenB, "/api/v1/contacts", map[string]any{"name": "Beta", "email": "beta@tag-count.example.com"})
	billing := create(t, api, tokenA, "/api/v1/tags", map[string]any{"name": "billing"})
	create(t, api, tokenA, "/api/v1/tags", map[string]any{"name": "unused"})
	foreign := create(t, api, tokenB, "/api/v1/tags", map[string]any{"name": "billing"})
	for _, subject := range []string{"One", "Two"} {
		conversation := create(t, api, tokenA, "/api/v1/conversations", map[string]any{"contact_id": contactA, "subject": subject})
		tagged := request(t, api, http.MethodPut, "/api/v1/conversations/"+conversation+"/tags/"+billing, nil, tokenA, nil)
		if tagged.Code != http.StatusOK {
			t.Fatalf("tag %s status=%d body=%s", subject, tagged.Code, tagged.Body.String())
		}
	}
	other := create(t, api, tokenB, "/api/v1/conversations", map[string]any{"contact_id": contactB, "subject": "Other workspace"})
	if tagged := request(t, api, http.MethodPut, "/api/v1/conversations/"+other+"/tags/"+foreign, nil, tokenB, nil); tagged.Code != http.StatusOK {
		t.Fatalf("tag in other workspace status=%d body=%s", tagged.Code, tagged.Body.String())
	}

	counted := request(t, api, http.MethodGet, "/api/v1/tags?counts=true", nil, tokenA, nil)
	if counted.Code != http.StatusOK {
		t.Fatalf("counted list status=%d body=%s", counted.Code, counted.Body.String())
	}
	var page struct {
		Items []struct {
			Name              string `json:"name"`
			ConversationCount *int   `json:"conversation_count"`
		} `json:"items"`
	}
	decode(t, counted, &page)
	got := map[string]int{}
	for _, item := range page.Items {
		if item.ConversationCount == nil {
			t.Fatalf("tag %q has no conversation_count: %s", item.Name, counted.Body.String())
		}
		got[item.Name] = *item.ConversationCount
	}
	if len(got) != 2 || got["billing"] != 2 || got["unused"] != 0 {
		t.Fatalf("counts=%v, want billing 2 and unused 0", got)
	}

	plain := request(t, api, http.MethodGet, "/api/v1/tags", nil, tokenA, nil)
	if plain.Code != http.StatusOK || strings.Contains(plain.Body.String(), "conversation_count") {
		t.Fatalf("plain list status=%d body=%s, want no conversation_count", plain.Code, plain.Body.String())
	}
}

func TestRenamingATagToAnotherTagsNameIsRefusedWithTheReason(t *testing.T) {
	api, service := testAPI(t)
	token := createSession(t, api, service, "Tag Rename Clash", "admin@tag-rename.example.com")
	create(t, api, token, "/api/v1/tags", map[string]any{"name": "billing"})
	refund := create(t, api, token, "/api/v1/tags", map[string]any{"name": "Refund"})

	clash := request(t, api, http.MethodPatch, "/api/v1/tags/"+refund, map[string]any{"name": "billing"}, token, nil)
	if clash.Code != http.StatusConflict {
		t.Fatalf("rename onto an existing name status=%d body=%s, want 409", clash.Code, clash.Body.String())
	}
	var refused struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	decode(t, clash, &refused)
	if refused.Error.Code != "tag_name_taken" || refused.Error.Message != `Another tag is already named "billing". Pick another name.` {
		t.Fatalf("refusal=%+v", refused.Error)
	}
	list := request(t, api, http.MethodGet, "/api/v1/tags", nil, token, nil)
	if !strings.Contains(list.Body.String(), `"name":"Refund"`) {
		t.Fatalf("refused rename changed the tag: %s", list.Body.String())
	}
}
