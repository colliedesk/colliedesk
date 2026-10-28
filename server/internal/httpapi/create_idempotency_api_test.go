package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// postWithKey sends one create request the way the web app does, naming the submit with an
// Idempotency-Key ("" sends none).
func postWithKey(t *testing.T, api http.Handler, token, path, key string, body any) *httptest.ResponseRecorder {
	t.Helper()
	req := newRequest(t, http.MethodPost, path, body, token, nil)
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	return serve(api, req)
}

func idOf(t *testing.T, response *httptest.ResponseRecorder, field string) string {
	t.Helper()
	var body map[string]any
	decode(t, response, &body)
	id, _ := body[field].(string)
	if id == "" {
		t.Fatalf("response has no %s: %s", field, response.Body.String())
	}
	return id
}

// threeAtOnce sends the same keyed request three times at once, as three fast clicks do.
func threeAtOnce(t *testing.T, send func() *httptest.ResponseRecorder) []*httptest.ResponseRecorder {
	t.Helper()
	responses := make([]*httptest.ResponseRecorder, 3)
	var wait sync.WaitGroup
	for index := range responses {
		wait.Add(1)
		go func() {
			defer wait.Done()
			responses[index] = send()
		}()
	}
	wait.Wait()
	created := 0
	for _, response := range responses {
		switch response.Code {
		case http.StatusCreated:
			created++
		case http.StatusOK:
		default:
			t.Fatalf("status=%d body=%s, want 201 for the first and 200 for the repeats", response.Code, response.Body.String())
		}
	}
	if created != 1 {
		t.Fatalf("%d requests answered 201, want exactly 1", created)
	}
	return responses
}

func TestARepeatedKeyCreatesOneKnowledgeBaseArticleAndReturnsIt(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "KB Keys", "admin@kb-keys.example.com")
	collection := kbCollection(t, api, admin, map[string]any{"name": "Guides"})
	article := map[string]any{"collection_id": collection.ID, "title": "Reset a password", "body_md": "Open Settings."}
	first := postWithKey(t, api, admin, "/api/v1/kb/articles", "save-1", article)
	if first.Code != http.StatusCreated {
		t.Fatalf("first: status=%d body=%s", first.Code, first.Body.String())
	}
	created := idOf(t, first, "id")
	again := postWithKey(t, api, admin, "/api/v1/kb/articles", "save-1", article)
	if again.Code != http.StatusOK || idOf(t, again, "id") != created {
		t.Fatalf("repeat: status=%d body=%s, want 200 with article %s", again.Code, again.Body.String(), created)
	}
	threeAtOnce(t, func() *httptest.ResponseRecorder {
		return postWithKey(t, api, admin, "/api/v1/kb/articles", "triple", map[string]any{"collection_id": collection.ID, "title": "Triple click", "body_md": "Once."})
	})
	if count := queryString(t, pool, `SELECT count(*)::text FROM kb_articles WHERE title='Triple click'`); count != "1" {
		t.Fatalf("articles stored for three clicks = %s, want 1", count)
	}
	if count := queryString(t, pool, `SELECT count(*)::text FROM kb_articles WHERE title='Reset a password'`); count != "1" {
		t.Fatalf("articles stored for a repeated key = %s, want 1", count)
	}
	other := postWithKey(t, api, admin, "/api/v1/kb/articles", "save-1", map[string]any{"collection_id": collection.ID, "title": "Another", "body_md": "x"})
	if other.Code != http.StatusUnprocessableEntity || !strings.Contains(other.Body.String(), `"idempotency_key_reused"`) {
		t.Fatalf("another article under a used key: status=%d body=%s, want 422 idempotency_key_reused", other.Code, other.Body.String())
	}
	if plain := postWithKey(t, api, admin, "/api/v1/kb/articles", "", article); plain.Code != http.StatusCreated {
		t.Fatalf("no key keeps the old behaviour: status=%d body=%s", plain.Code, plain.Body.String())
	}
	if bad := postWithKey(t, api, admin, "/api/v1/kb/articles", strings.Repeat("k", 201), article); bad.Code != http.StatusBadRequest {
		t.Fatalf("a key over 200 characters: status=%d, want 400", bad.Code)
	}
}

func TestARepeatedKeyCreatesOneSavedReplyWithoutAFalseDuplicateTitle(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "Reply Keys", "admin@reply-keys.example.com")
	reply := map[string]any{"title": "Refund steps", "body": "We refunded you."}
	responses := threeAtOnce(t, func() *httptest.ResponseRecorder {
		return postWithKey(t, api, admin, "/api/v1/saved-replies", "add-1", reply)
	})
	ids := map[string]bool{}
	for _, response := range responses {
		ids[idOf(t, response, "id")] = true
	}
	if len(ids) != 1 {
		t.Fatalf("three clicks returned %d different replies, want the same one", len(ids))
	}
	if count := queryString(t, pool, `SELECT count(*)::text FROM saved_replies WHERE title='Refund steps'`); count != "1" {
		t.Fatalf("saved replies stored = %s, want 1", count)
	}
	// A new submit (a new key) with the same title is still refused as a duplicate.
	if again := postWithKey(t, api, admin, "/api/v1/saved-replies", "add-2", reply); again.Code != http.StatusConflict {
		t.Fatalf("a second reply with the same title: status=%d body=%s, want 409", again.Code, again.Body.String())
	}
}

func TestARepeatedKeySendsOneTestMessageAndSpendsOneOfTheHourlyFive(t *testing.T) {
	f := newLimitsFixture(t, true)
	path := "/api/v1/channels/" + f.emailChannelID(t, f.workspaceA) + "/email/test-message"
	before := queryString(t, f.pool, `SELECT count(*)::text FROM conversations WHERE workspace_id=$1`, f.workspaceA)
	responses := threeAtOnce(t, func() *httptest.ResponseRecorder {
		return postWithKey(t, f.api, f.adminA, path, "test-1", nil)
	})
	conversation := idOf(t, responses[0], "conversation_id")
	for _, response := range responses[1:] {
		if got := idOf(t, response, "conversation_id"); got != conversation {
			t.Fatalf("a repeat opened conversation %s, want the first one %s", got, conversation)
		}
	}
	after := queryString(t, f.pool, `SELECT (count(*)-$2::int)::text FROM conversations WHERE workspace_id=$1`, f.workspaceA, before)
	if after != "1" {
		t.Fatalf("conversations opened by three clicks = %s, want 1", after)
	}
	// Four more distinct test messages fit in the hour: the repeats cost nothing.
	for attempt := 2; attempt <= 5; attempt++ {
		if got := postWithKey(t, f.api, f.adminA, path, "test-"+string(rune('0'+attempt)), nil); got.Code != http.StatusCreated {
			t.Fatalf("test message %d: status=%d body=%s, want 201", attempt, got.Code, got.Body.String())
		}
	}
	// The default-channel route shares the keys: the same key names the same test message.
	if other := postWithKey(t, f.api, f.adminA, "/api/v1/channels/email/test-message", "test-1", nil); other.Code != http.StatusOK && other.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a used key on the default route: status=%d body=%s, want the first result or 422", other.Code, other.Body.String())
	}
}

func TestCreateKeysBelongToOneWorkspace(t *testing.T) {
	f := newLimitsFixture(t, true)
	reply := map[string]any{"title": "Shared words", "body": "Hello."}
	first := postWithKey(t, f.api, f.adminA, "/api/v1/saved-replies", "shared-key", reply)
	if first.Code != http.StatusCreated {
		t.Fatalf("A: status=%d body=%s", first.Code, first.Body.String())
	}
	inB := postWithKey(t, f.api, f.adminB, "/api/v1/saved-replies", "shared-key", reply)
	if inB.Code != http.StatusCreated {
		t.Fatalf("B with A's key: status=%d body=%s, want its own 201", inB.Code, inB.Body.String())
	}
	if idOf(t, inB, "id") == idOf(t, first, "id") {
		t.Fatal("B's request returned A's saved reply")
	}
	if workspace := queryString(t, f.pool, `SELECT workspace_id::text FROM saved_replies WHERE id=$1`, idOf(t, inB, "id")); workspace != f.workspaceB {
		t.Fatalf("B's reply is in %s, want B", workspace)
	}
}
