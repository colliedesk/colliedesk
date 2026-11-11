package mcpserver_test

import (
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/mcpserver"
)

const routeEffectFile = "server/internal/httpapi/route-effect.go"

// A tool's readOnlyHint and destructiveHint come from what its routes do, so a route that changes what
// it does changes the hints of every tool that calls it. Every route must say what it does.
func TestEveryRouteSaysWhatItDoes(t *testing.T) {
	routes := map[string]bool{}
	for _, route := range httpapi.Routes() {
		key := route.Method + " " + route.Pattern
		routes[key] = true
		if httpapi.RouteEffect(key) == httpapi.EffectUnknown {
			t.Errorf("%s does not say what it does: add it to routeEffects in %s as Reads, Changes, Final or ByBody", key, routeEffectFile)
		}
	}
	for key := range httpapi.RouteEffectTable() {
		if !routes[key] {
			t.Errorf("routeEffects in %s lists %s, which is not a route", routeEffectFile, key)
		}
	}
	for key := range httpapi.RoutesReachingOutsideTable() {
		if !routes[key] {
			t.Errorf("routesReachingOutside in %s lists %s, which is not a route", routeEffectFile, key)
		}
	}
}

// A route whose effect depends on the body (a note or a reply on the same route) cannot give a tool its
// hints: the tool says what the body it always sends does.
func TestToolsOnABodyDecidedRouteSayWhatTheirBodyDoes(t *testing.T) {
	for _, spec := range mcpserver.Catalog() {
		for _, key := range spec.REST {
			if httpapi.RouteEffect(key) == httpapi.ByBody && spec.BodyEffect == httpapi.EffectUnknown {
				t.Errorf("%s calls %s, whose effect depends on the body: set BodyEffect to what its body does (Changes or Final)", spec.Name, key)
			}
		}
	}
}

// The hints follow from the routes: worked examples of each rule, written from what the tools do.
func TestToolHintsFollowFromTheirRoutes(t *testing.T) {
	want := map[string]struct{ readOnly, destructive bool }{
		"colliedesk_inbox_search":              {readOnly: true},    // GET only
		"colliedesk_ai_models_list":            {readOnly: true},    // a POST that stores nothing
		"colliedesk_docs_search":               {readOnly: true},    // no route at all
		"colliedesk_note_add":                  {},                  // the messages route with a note body
		"colliedesk_reply_send":                {destructive: true}, // the same route with a reply body
		"colliedesk_conversation_create":       {},
		"colliedesk_conversation_start":        {destructive: true},
		"colliedesk_saved_reply_delete":        {},                  // a DELETE into the trash
		"colliedesk_tag_delete":                {destructive: true}, // a DELETE for good
		"colliedesk_token_revoke":              {destructive: true}, // a POST that revokes
		"colliedesk_operator_revoke":           {},                  // a DELETE that a token only asks for
		"colliedesk_results_share_link_revoke": {destructive: true},
	}
	found := 0
	for _, spec := range mcpserver.Catalog() {
		w, ok := want[spec.Name]
		if !ok {
			continue
		}
		found++
		if spec.ReadOnly() != w.readOnly || spec.Destructive() != w.destructive {
			t.Errorf("%s: readOnly=%v destructive=%v, want %v %v", spec.Name, spec.ReadOnly(), spec.Destructive(), w.readOnly, w.destructive)
		}
	}
	if found != len(want) {
		t.Errorf("found %d of the %d tools of this test in the catalog", found, len(want))
	}
}

// openWorldHint is true when a tool may reach people or systems outside the workspace: it emails or
// messages a customer, calls a third party (an AI provider, GitHub, Cursor, Freshdesk, DNS, billing),
// or changes what the public help center shows. Worked examples, from what each handler does.
func TestToolOpenWorldFollowsFromTheirRoutes(t *testing.T) {
	want := map[string]bool{
		"colliedesk_inbox_search":              false, // reads the workspace
		"colliedesk_note_add":                  false, // a note stays in the team
		"colliedesk_reply_send":                true,  // emails the customer
		"colliedesk_conversation_create":       false,
		"colliedesk_conversation_start":        true,
		"colliedesk_conversation_update":       true,  // a status change may tell the customer in their chat
		"colliedesk_ai_models_list":            true,  // read-only, but asks the AI provider
		"colliedesk_github_repositories_list":  true,  // read-only, but asks GitHub
		"colliedesk_kb_article_update":         true,  // may change a published article
		"colliedesk_results_share_link_create": true,  // a public link
		"colliedesk_email_test_message_send":   false, // the message goes from the agent to its own channel
		"colliedesk_saved_reply_create":        false,
		"colliedesk_ai_slot_set":               false, // switches AI on; the calls come later, not from this tool
	}
	found := 0
	for _, spec := range mcpserver.Catalog() {
		w, ok := want[spec.Name]
		if !ok {
			continue
		}
		found++
		if spec.OpenWorld() != w {
			t.Errorf("%s: openWorld=%v, want %v", spec.Name, spec.OpenWorld(), w)
		}
	}
	if found != len(want) {
		t.Errorf("found %d of the %d tools of this test in the catalog", found, len(want))
	}
}
