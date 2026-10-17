package mcpserver_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
)

// A step that fails after an earlier step wrote must not tell the model that nothing changed.
func TestConversationUpdateSaysWhatMayBeAppliedWhenALaterStepFails(t *testing.T) {
	e := newEnv(t)
	f := seedInbox(t, e)
	draft := e.connect(f.w.draft, "")
	// PostgreSQL refuses a NUL character in text, so creating the second tag fails after the first exists.
	args := map[string]any{"conversation_id": f.c3, "status": "pending", "add_tags": []string{"fresh-tag", "bad\u0000name"}}

	message := e.fails(draft, "colliedesk_conversation_update", args)
	for _, want := range []string{"may already be applied", "safe to repeat"} {
		if !strings.Contains(message, want) {
			t.Errorf("message %q lacks %q", message, want)
		}
	}
	if strings.Contains(message, "Nothing is known to have changed") {
		t.Errorf("message %q claims nothing changed after a tag was created", message)
	}
	if got := workspaceTagNames(t, e, f.w.admin); !slices.Equal(got, []string{"billing", "fresh-tag"}) {
		t.Fatalf("workspace tags = %v, want the first new tag to exist (that is what 'may already be applied' refers to)", got)
	}

	// Repeating the corrected call finishes the job and reuses the tag the first call created.
	e.ok(draft, "colliedesk_conversation_update", map[string]any{"conversation_id": f.c3, "status": "pending", "add_tags": []string{"fresh-tag"}})
	if got := tagNames(t, restConversation(t, e, f.w, f.c3)); !slices.Equal(got, []string{"fresh-tag"}) {
		t.Errorf("after the repeat the conversation has tags %v, want fresh-tag", got)
	}
	if got := workspaceTagNames(t, e, f.w.admin); !slices.Equal(got, []string{"billing", "fresh-tag"}) {
		t.Errorf("the repeat created another tag: %v", got)
	}
}

// Two callers can create the same new tag name at once; the loser must use the winner's tag.
func TestConversationUpdateUsesATagAnotherCallerCreatedMeanwhile(t *testing.T) {
	var (
		once  sync.Once
		admin string
		fail  func(format string, args ...any)
	)
	e := newEnvBuilt(t, nil, func(real http.Handler) http.Handler {
		return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			if request.Method == http.MethodPost && request.URL.Path == "/api/v1/tags" {
				once.Do(func() {
					// The other caller's request reaches the real API first, with the same name.
					raw, _ := io.ReadAll(request.Body)
					request.Body = io.NopCloser(bytes.NewReader(raw))
					rival := httptest.NewRequest(http.MethodPost, "/api/v1/tags", bytes.NewReader(raw))
					rival.Header.Set("Content-Type", "application/json")
					rival.Header.Set("Authorization", "Bearer "+admin)
					recorder := httptest.NewRecorder()
					real.ServeHTTP(recorder, rival)
					if recorder.Code != http.StatusCreated {
						fail("the other caller's tag was not created: %d %s", recorder.Code, recorder.Body.String())
					}
				})
			}
			real.ServeHTTP(response, request)
		})
	})
	fail = t.Errorf
	f := seedInbox(t, e)
	admin = f.w.admin

	out := e.ok(e.connect(f.w.draft, ""), "colliedesk_conversation_update", map[string]any{"conversation_id": f.c3, "add_tags": []string{"race-tag"}})
	if tags := at(t, out, "conversation", "tags").([]any); len(tags) != 1 || tags[0] != "race-tag" {
		t.Errorf("result tags = %v, want race-tag", tags)
	}
	if got := workspaceTagNames(t, e, f.w.admin); !slices.Equal(got, []string{"billing", "race-tag"}) {
		t.Errorf("workspace tags = %v, want exactly one race-tag", got)
	}
	if got := tagNames(t, restConversation(t, e, f.w, f.c3)); !slices.Equal(got, []string{"race-tag"}) {
		t.Errorf("REST shows tags %v on the conversation, want race-tag", got)
	}
}

// REST keeps "Refund" and "refund" as two tags, so a name must never be taken for the other one.
func TestTagNamesMatchExactlyFirstAndAmbiguousCaseVariantsAreRefused(t *testing.T) {
	e := newEnv(t)
	f := seedInbox(t, e)
	e.created("/api/v1/tags", map[string]any{"name": "Refund"}, f.w.admin)
	e.created("/api/v1/tags", map[string]any{"name": "refund"}, f.w.admin)
	draft, read := e.connect(f.w.draft, ""), e.connect(f.w.read, "")

	e.ok(draft, "colliedesk_conversation_update", map[string]any{"conversation_id": f.c1, "add_tags": []string{"refund"}})
	e.ok(draft, "colliedesk_conversation_update", map[string]any{"conversation_id": f.c2, "add_tags": []string{"Refund"}})
	if got := tagNames(t, restConversation(t, e, f.w, f.c1)); !slices.Contains(got, "refund") || slices.Contains(got, "Refund") {
		t.Errorf("asked for refund, the conversation got %v", got)
	}
	if got := tagNames(t, restConversation(t, e, f.w, f.c2)); !slices.Equal(got, []string{"Refund"}) {
		t.Errorf("asked for Refund, the conversation got %v", got)
	}

	for name, call := range map[string]func() string{
		"add": func() string {
			return e.fails(draft, "colliedesk_conversation_update", map[string]any{"conversation_id": f.c3, "add_tags": []string{"REFUND"}})
		},
		"remove": func() string {
			return e.fails(draft, "colliedesk_conversation_update", map[string]any{"conversation_id": f.c1, "remove_tags": []string{"REFUND"}})
		},
		"filter": func() string { return e.fails(read, "colliedesk_inbox_search", map[string]any{"tag": "REFUND"}) },
	} {
		message := call()
		for _, want := range []string{"Refund", "refund", "exact"} {
			if !strings.Contains(message, want) {
				t.Errorf("%s with REFUND: message %q lacks %q", name, message, want)
			}
		}
	}
	if got := tagNames(t, restConversation(t, e, f.w, f.c3)); len(got) != 0 {
		t.Errorf("a refused add tagged the conversation: %v", got)
	}
	if got := workspaceTagNames(t, e, f.w.admin); !slices.Equal(got, []string{"Refund", "billing", "refund"}) {
		t.Errorf("a refused call created or removed a tag: %v", got)
	}

	// The exact spelling still works for remove and filter.
	listed := e.ok(read, "colliedesk_inbox_search", map[string]any{"tag": "refund"})
	if ids := conversationIDs(t, listed); !slices.Equal(ids, []string{f.c1}) {
		t.Errorf("filter refund returned %v, want only %s", ids, f.c1)
	}
	e.ok(draft, "colliedesk_conversation_update", map[string]any{"conversation_id": f.c1, "remove_tags": []string{"refund"}})
	if got := tagNames(t, restConversation(t, e, f.w, f.c1)); slices.Contains(got, "refund") {
		t.Errorf("remove refund left %v", got)
	}
}
