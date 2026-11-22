package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

// A bulk action by filter sent again with the same Idempotency-Key answers what the first one did, even
// though the articles it trashed no longer match; the same key with another filter is refused.
func TestABulkFilterActionRepeatedWithItsKeyAnswersTheFirstAnswer(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "KB Bulk Filter Key", "admin@kb-bulk-filter-key.example.com")
	collection := kbCollection(t, api, admin, map[string]any{"name": "Docs"})
	for _, title := range []string{"Refunds", "Shipping"} {
		kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": title, "body_md": "Text.", "tags": []string{"old"}})
	}
	kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Returns", "body_md": "Text.", "tags": []string{"new"}})
	send := func(body map[string]any) *httptest.ResponseRecorder {
		t.Helper()
		call := newRequest(t, http.MethodPost, "/api/v1/kb/articles/bulk", body, admin, nil)
		call.Header.Set("Idempotency-Key", "trash-old")
		return serve(api, call)
	}
	trashOld := map[string]any{"action": "trash", "filter": map[string]any{"tag": []string{"old"}}}
	first := send(trashOld)
	if first.Code != http.StatusOK {
		t.Fatalf("first trash by filter = %d %s", first.Code, first.Body.String())
	}
	again := send(trashOld)
	var firstAnswer, againAnswer map[string]any
	decode(t, first, &firstAnswer)
	if again.Code == http.StatusOK {
		decode(t, again, &againAnswer)
	}
	if again.Code != http.StatusOK || !reflect.DeepEqual(againAnswer, firstAnswer) {
		t.Fatalf("repeat with the same key = %d %s, want the first answer %s", again.Code, again.Body.String(), first.Body.String())
	}
	other := send(map[string]any{"action": "trash", "filter": map[string]any{"tag": []string{"new"}}})
	if other.Code != http.StatusUnprocessableEntity || errorCode(t, other) != "idempotency_key_reused" {
		t.Fatalf("same key, another filter = %d %s, want 422 idempotency_key_reused", other.Code, other.Body.String())
	}
}

// A filter that names nothing to narrow by is refused: it would change every article in the workspace.
func TestABulkActionRefusesAnEmptyFilter(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "KB Bulk Empty Filter", "admin@kb-bulk-empty-filter.example.com")
	collection := kbCollection(t, api, admin, map[string]any{"name": "Docs"})
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Refunds", "body_md": "Text."})
	for _, filter := range []map[string]any{{}, {"tag": []string{}}} {
		response := kbCall(t, api, http.MethodPost, "/api/v1/kb/articles/bulk", map[string]any{"action": "trash", "filter": filter}, admin, http.StatusBadRequest, nil)
		if code := errorCode(t, response); code != "invalid_request" {
			t.Fatalf("filter %v answered %s, want invalid_request", filter, code)
		}
	}
	if got := kbGetArticle(t, api, admin, article.ID); got.DeletedAt != nil {
		t.Fatalf("an empty filter trashed %s", article.ID)
	}
}
