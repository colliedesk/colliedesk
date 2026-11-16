package mcpserver_test

import (
	"strings"
	"testing"
)

// A new workspace has nothing in its inbox until the team's mail reaches it. An AI app that only lists
// the inbox sees an empty list and stops, so the answer itself names the step that opens a first
// conversation.
func TestEmptyWorkspaceInboxSearchNamesTheFirstStep(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("Brand New Desk", "admin@brand-new.example.com")
	session := e.connect(w.read, "")

	out := e.ok(session, "colliedesk_inbox_search", nil)
	if count := out["count"]; count != float64(0) {
		t.Fatalf("count = %v, want 0", count)
	}
	next, _ := out["getting_started"].(string)
	for _, tool := range []string{"colliedesk_setup_progress_get", "colliedesk_email_test_message_send"} {
		if !strings.Contains(next, tool) {
			t.Errorf("getting_started does not name %s: %q", tool, next)
		}
	}
}

// An empty answer to a filter or a search says nothing about the workspace being new, and a workspace
// that has conversations is past the first step; neither gets the hint.
func TestInboxSearchGivesTheFirstStepOnlyToAnEmptyWorkspace(t *testing.T) {
	e := newEnv(t)
	empty := e.newWorkspace("Filtered Desk", "admin@filtered.example.com")
	filtered := e.ok(e.connect(empty.read, ""), "colliedesk_inbox_search", map[string]any{"q": "no-such-words"})
	if _, ok := filtered["getting_started"]; ok {
		t.Errorf("a search with no match got getting_started: %v", filtered)
	}

	f := seedInbox(t, e)
	session := e.connect(f.w.read, "")
	for label, args := range map[string]map[string]any{"all": nil, "no match": {"q": "no-such-words"}} {
		if out := e.ok(session, "colliedesk_inbox_search", args); out["getting_started"] != nil {
			t.Errorf("%s: a workspace with conversations got getting_started: %v", label, out["getting_started"])
		}
	}
}
