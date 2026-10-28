package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func postKeyed(t *testing.T, api http.Handler, token, conversation, key string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	req := newRequest(t, http.MethodPost, "/api/v1/conversations/"+conversation+"/messages", body, token, nil)
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	return serve(api, req)
}

func messageIDOf(t *testing.T, response *httptest.ResponseRecorder) string {
	t.Helper()
	var message struct {
		ID string `json:"id"`
	}
	decode(t, response, &message)
	return message.ID
}

func TestARepeatedIdempotencyKeyReturnsTheMessageItAlreadyCreated(t *testing.T) {
	f := newLimitsFixture(t, false)
	conversation := f.conversationA(t)
	note := map[string]any{"kind": "note", "body": "Checked the order"}
	first := postKeyed(t, f.api, f.adminA, conversation, "submit-1", note)
	if first.Code != http.StatusCreated {
		t.Fatalf("first: status=%d body=%s", first.Code, first.Body.String())
	}
	created := messageIDOf(t, first)
	again := postKeyed(t, f.api, f.adminA, conversation, "submit-1", note)
	if again.Code != http.StatusOK {
		t.Fatalf("repeat: status=%d body=%s, want 200 with the earlier message", again.Code, again.Body.String())
	}
	if id := messageIDOf(t, again); id != created {
		t.Fatalf("repeat returned %s, want %s", id, created)
	}
	if count := queryString(t, f.pool, `SELECT count(*)::text FROM messages WHERE workspace_id=$1 AND body='Checked the order'`, f.workspaceA); count != "1" {
		t.Fatalf("notes stored = %s, want 1", count)
	}
	if next := postKeyed(t, f.api, f.adminA, conversation, "submit-2", note); next.Code != http.StatusCreated {
		t.Fatalf("a new key is a new message: status=%d body=%s", next.Code, next.Body.String())
	}
	if plain := postKeyed(t, f.api, f.adminA, conversation, "", note); plain.Code != http.StatusCreated {
		t.Fatalf("no key keeps the old behaviour: status=%d body=%s", plain.Code, plain.Body.String())
	}
}

func TestConcurrentRequestsWithOneIdempotencyKeyStoreOneMessage(t *testing.T) {
	f := newLimitsFixture(t, false)
	conversation := f.conversationA(t)
	reply := map[string]any{"kind": "outgoing", "body": "On its way"}
	var wait sync.WaitGroup
	codes := make([]int, 3)
	for index := range codes {
		wait.Add(1)
		go func() {
			defer wait.Done()
			codes[index] = postKeyed(t, f.api, f.adminA, conversation, "triple-click", reply).Code
		}()
	}
	wait.Wait()
	for _, code := range codes {
		if code != http.StatusCreated && code != http.StatusOK {
			t.Fatalf("codes=%v, want each 201 or 200", codes)
		}
	}
	if count := queryString(t, f.pool, `SELECT count(*)::text FROM messages WHERE workspace_id=$1 AND body='On its way'`, f.workspaceA); count != "1" {
		t.Fatalf("replies stored = %s, want 1", count)
	}
	if outbox := f.outboxCount(t, f.workspaceA); outbox != "1" {
		t.Fatalf("outbox rows = %s, want 1: the reply is emailed once", outbox)
	}
}

func TestAnIdempotencyKeyReusedForAnotherMessageIsRefused(t *testing.T) {
	f := newLimitsFixture(t, false)
	conversation := f.conversationA(t)
	if first := postKeyed(t, f.api, f.adminA, conversation, "reused", map[string]any{"kind": "note", "body": "First text"}); first.Code != http.StatusCreated {
		t.Fatalf("first: status=%d body=%s", first.Code, first.Body.String())
	}
	other := postKeyed(t, f.api, f.adminA, conversation, "reused", map[string]any{"kind": "note", "body": "Other text"})
	if other.Code != http.StatusUnprocessableEntity || !strings.Contains(other.Body.String(), `"idempotency_key_reused"`) {
		t.Fatalf("other body under the same key: status=%d body=%s, want 422 idempotency_key_reused", other.Code, other.Body.String())
	}
	if bad := postKeyed(t, f.api, f.adminA, conversation, strings.Repeat("k", 201), map[string]any{"kind": "note", "body": "Long key"}); bad.Code != http.StatusBadRequest {
		t.Fatalf("a key over 200 characters: status=%d, want 400", bad.Code)
	}
}

func TestIdempotencyKeysBelongToOneWorkspace(t *testing.T) {
	f := newLimitsFixture(t, false)
	conversationA := f.conversationA(t)
	resultB := f.receive(t, true, 2, "Bravo question")
	note := map[string]any{"kind": "note", "body": "Same words"}
	if first := postKeyed(t, f.api, f.adminA, conversationA, "shared-key", note); first.Code != http.StatusCreated {
		t.Fatalf("A: status=%d body=%s", first.Code, first.Body.String())
	}
	inB := postKeyed(t, f.api, f.adminB, *resultB.ConversationID, "shared-key", note)
	if inB.Code != http.StatusCreated {
		t.Fatalf("B with A's key: status=%d body=%s, want its own 201", inB.Code, inB.Body.String())
	}
	if conversation := queryString(t, f.pool, `SELECT conversation_id::text FROM messages WHERE workspace_id=$1 AND id=$2`, f.workspaceB, messageIDOf(t, inB)); conversation != *resultB.ConversationID {
		t.Fatalf("B's message is on %s, want B's conversation", conversation)
	}
	// A's key never reaches into A's conversation from B.
	if cross := postKeyed(t, f.api, f.adminB, conversationA, "shared-key", note); cross.Code != http.StatusNotFound {
		t.Fatalf("B posting to A's conversation: status=%d, want 404", cross.Code)
	}
}

func TestAMultipartMessageHonoursItsIdempotencyKey(t *testing.T) {
	f := newLimitsFixture(t, false)
	conversation := f.conversationA(t)
	send := func() *httptest.ResponseRecorder {
		req := multipartMessage(t, conversation, f.adminA, "note", "With a file", []upload{{name: "a.txt", contentType: "text/plain", data: []byte("hello")}})
		req.Header.Set("Idempotency-Key", "files-1")
		return serve(f.api, req)
	}
	if first := send(); first.Code != http.StatusCreated {
		t.Fatalf("first: status=%d body=%s", first.Code, first.Body.String())
	}
	if again := send(); again.Code != http.StatusOK {
		t.Fatalf("repeat: status=%d body=%s, want 200", again.Code, again.Body.String())
	}
	if count := queryString(t, f.pool, `SELECT count(*)::text FROM attachments WHERE workspace_id=$1 AND filename='a.txt'`, f.workspaceA); count != "1" {
		t.Fatalf("attachments stored = %s, want 1", count)
	}
}
