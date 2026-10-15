package mcpserver_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func TestDocsSearchRanksTheEmbeddedUserGuides(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("Docs Desk", "admin@docs.example.com")
	for name, token := range map[string]string{"read token": w.read, "person session": w.admin} {
		session := e.connect(token, "")
		cases := []struct {
			query string
			pages []string
		}{
			{"revoke an unused invite", []string{"invites"}},
			{"agent tokens draft tier", []string{"sessions", "mcp"}},
			{"renaming and deleting tags", []string{"tags"}},
		}
		for _, c := range cases {
			out := e.ok(session, "colliedesk_docs_search", map[string]any{"query": c.query})
			results, _ := out["results"].([]any)
			if len(results) == 0 {
				t.Errorf("%s: %q found nothing", name, c.query)
				continue
			}
			if got, _ := at(t, results[0], "page").(string); !slices.Contains(c.pages, got) {
				t.Errorf("%s: %q top page=%v, want one of %v (%v)", name, c.query, got, c.pages, results[0])
			}
			if at(t, results[0], "excerpt") == "" || at(t, results[0], "title") == "" {
				t.Errorf("%s: %q result has no excerpt or title: %v", name, c.query, results[0])
			}
		}
	}
	session := e.connect(w.read, "")
	first := e.ok(session, "colliedesk_docs_search", map[string]any{"query": "inbox"})
	second := e.ok(session, "colliedesk_docs_search", map[string]any{"query": "inbox"})
	if text := stringify(first); text != stringify(second) {
		t.Errorf("the same query ranked differently:\n%s\n%s", text, stringify(second))
	}
	if limited := e.ok(session, "colliedesk_docs_search", map[string]any{"query": "inbox", "limit": 1}); len(limited["results"].([]any)) != 1 {
		t.Errorf("limit 1 returned %v", limited["results"])
	}
	none := e.ok(session, "colliedesk_docs_search", map[string]any{"query": "zzqqxx"})
	if len(none["results"].([]any)) != 0 || !strings.Contains(none["hint"].(string), "different words") {
		t.Errorf("a query without matches must say so: %v", none)
	}
	if message := e.fails(session, "colliedesk_docs_search", map[string]any{"query": ""}); !strings.Contains(message, "query") {
		t.Errorf("empty query message=%q", message)
	}
}

func stringify(value map[string]any) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
