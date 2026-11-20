package mcpserver_test

import "testing"

// An AI agent turns an article's On this page list off and on like a person in the editor: it is
// on unless the create says otherwise, and an update with only show_contents is a change.
func TestKBArticleContentsSwitchThroughMCP(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("KB Contents Desk", "admin@kb-contents.example.com")
	session := e.connect(w.send, "")
	collectionID := at(t, e.ok(session, "colliedesk_kb_collection_create", map[string]any{"name": "Guides"}), "collection", "id").(string)

	on := e.ok(session, "colliedesk_kb_article_create", map[string]any{"collection_id": collectionID, "title": "Set up email", "body": "## One\n\n## Two\n\n## Three"})
	e.requireMatchesOutputSchema(session, "colliedesk_kb_article_create", on)
	if at(t, on, "article", "show_contents") != true {
		t.Fatalf("create without show_contents = %v, want true", at(t, on, "article", "show_contents"))
	}
	off := e.ok(session, "colliedesk_kb_article_update", map[string]any{"article_id": at(t, on, "article", "id"), "updated_at": at(t, on, "article", "updated_at"),
		"show_contents": false})
	if at(t, off, "article", "show_contents") != false {
		t.Fatalf("update show_contents false = %v", off)
	}
	created := e.ok(session, "colliedesk_kb_article_create", map[string]any{"collection_id": collectionID, "title": "Plain", "body": "Text.", "show_contents": false})
	if at(t, created, "article", "show_contents") != false {
		t.Fatalf("create show_contents false = %v", created)
	}
}
