package mcpserver_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// kbVersionKinds lists the kinds of an article's versions, newest first, joined by commas.
func kbVersionKinds(t *testing.T, e *env, session *mcp.ClientSession, articleID string) string {
	t.Helper()
	var kinds []string
	for _, version := range e.ok(session, "colliedesk_kb_article_history", map[string]any{"article_id": articleID})["versions"].([]any) {
		kinds = append(kinds, at(t, version, "kind").(string))
	}
	return strings.Join(kinds, ",")
}

// An AI agent edits a published article as a person does in the editor: the change waits in a draft,
// the live text stays, and publishing or discarding the draft is one call each.
func TestAnAgentChangesAPublishedArticleWithoutTouchingTheLivePage(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("KB Draft Tools", "admin@kb-draft-tools.example.com")
	session := e.connect(w.send, "")
	collectionID := at(t, e.ok(session, "colliedesk_kb_collection_create", map[string]any{"name": "Billing"}), "collection", "id").(string)
	created := e.ok(session, "colliedesk_kb_article_create", map[string]any{"collection_id": collectionID, "title": "Refunds", "body": "Within 14 days.", "status": "published"})
	articleID := at(t, created, "article", "id").(string)
	live := at(t, created, "article", "updated_at").(string)

	saved := e.ok(session, "colliedesk_kb_article_update", map[string]any{"article_id": articleID, "updated_at": live, "body": "Within 30 days."})
	if at(t, saved, "article", "team_text", "body") != "Within 14 days." || at(t, saved, "article", "has_draft") != true ||
		at(t, saved, "article", "draft", "team_text", "body") != "Within 30 days." || at(t, saved, "article", "draft", "updated_by") != "admin@kb-draft-tools.example.com" {
		t.Fatalf("saved = %v", saved)
	}
	// The updated_at an answer gives is the one to send back; a draft save moves it.
	if at(t, saved, "article", "updated_at") == live {
		t.Fatalf("updated_at after a draft save is still the live one: %v", saved)
	}
	again := e.ok(session, "colliedesk_kb_article_update", map[string]any{"article_id": articleID, "updated_at": at(t, saved, "article", "updated_at"), "title": "Refunds policy"})
	if at(t, again, "article", "team_text", "title") != "Refunds" || at(t, again, "article", "draft", "team_text", "title") != "Refunds policy" ||
		at(t, again, "article", "draft", "team_text", "body") != "Within 30 days." {
		t.Fatalf("second save = %v", again)
	}
	got := e.ok(session, "colliedesk_kb_article_get", map[string]any{"article_id": articleID})
	if at(t, got, "article", "updated_at") != at(t, again, "article", "updated_at") || at(t, got, "article", "draft", "team_text", "title") != "Refunds policy" {
		t.Fatalf("get = %v", got)
	}

	rows := e.ok(session, "colliedesk_kb_articles_search", map[string]any{"has_draft": true})["articles"].([]any)
	if len(rows) != 1 || at(t, rows[0], "id") != articleID || at(t, rows[0], "has_draft") != true || at(t, rows[0], "team_text", "title") != "Refunds" {
		t.Fatalf("has_draft search = %v", rows)
	}

	published := e.ok(session, "colliedesk_kb_article_draft_publish", map[string]any{"article_id": articleID, "updated_at": at(t, got, "article", "updated_at")})
	if at(t, published, "article", "team_text", "title") != "Refunds policy" || at(t, published, "article", "has_draft") != false {
		t.Fatalf("published = %v", published)
	}
	if live := e.ok(session, "colliedesk_kb_article_get", map[string]any{"article_id": articleID}); at(t, live, "article", "team_text", "body") != "Within 30 days." ||
		at(t, live, "article", "draft") != nil || at(t, live, "article", "updated_at") != at(t, published, "article", "updated_at") {
		t.Fatalf("article after publish = %v", live)
	}
	if kinds := kbVersionKinds(t, e, session, articleID); kinds != "edit,draft,draft,create" {
		t.Fatalf("versions after publish = %s", kinds)
	}

	pending := e.ok(session, "colliedesk_kb_article_update", map[string]any{"article_id": articleID, "updated_at": at(t, published, "article", "updated_at"), "body": "Within 60 days."})
	discarded := e.ok(session, "colliedesk_kb_article_draft_discard", map[string]any{"article_id": articleID, "updated_at": at(t, pending, "article", "updated_at")})
	if at(t, discarded, "article", "has_draft") != false || at(t, discarded, "article", "updated_at") != at(t, published, "article", "updated_at") {
		t.Fatalf("discarded = %v", discarded)
	}
	if live := e.ok(session, "colliedesk_kb_article_get", map[string]any{"article_id": articleID}); at(t, live, "article", "team_text", "body") != "Within 30 days." {
		t.Fatalf("article after discard = %v", live)
	}
	if message := e.fails(session, "colliedesk_kb_article_draft_discard", map[string]any{"article_id": articleID, "updated_at": at(t, published, "article", "updated_at")}); !strings.Contains(message, "no unpublished changes") {
		t.Fatalf("second discard = %q", message)
	}
	// The discarded draft stays in the history, so version_restore can bring it back.
	if kinds := kbVersionKinds(t, e, session, articleID); kinds != "draft,edit,draft,draft,create" {
		t.Fatalf("versions after discard = %s", kinds)
	}
}

// A token that may edit articles but not publish them saves drafts and is refused on publish.
func TestAnEditorTokenSavesADraftButCannotPublishIt(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("KB Draft Editor", "admin@kb-draft-editor.example.com")
	editor := e.connect(e.token(w.admin, "test editor", "", slices.DeleteFunc(slices.Clone(sendPermissions), func(p string) bool { return p == "kb_articles:publish" })), "")
	collectionID := e.created("/api/v1/kb/collections", map[string]any{"name": "Billing"}, w.admin)
	articleID := e.created("/api/v1/kb/articles", map[string]any{"collection_id": collectionID, "title": "Refunds", "body_md": "Within 14 days.", "status": "published"}, w.admin)
	live := at(t, e.ok(editor, "colliedesk_kb_article_get", map[string]any{"article_id": articleID}), "article", "updated_at")

	saved := e.ok(editor, "colliedesk_kb_article_update", map[string]any{"article_id": articleID, "updated_at": live, "body": "Within 30 days."})
	if at(t, saved, "article", "draft", "team_text", "body") != "Within 30 days." {
		t.Fatalf("editor save = %v", saved)
	}
	e.refused(editor, "colliedesk_kb_article_draft_publish", map[string]any{"article_id": articleID, "updated_at": at(t, saved, "article", "updated_at")})
	if got := e.ok(editor, "colliedesk_kb_article_get", map[string]any{"article_id": articleID}); at(t, got, "article", "team_text", "body") != "Within 14 days." {
		t.Fatalf("a refused publish changed the live text: %v", got)
	}
	e.ok(editor, "colliedesk_kb_article_draft_discard", map[string]any{"article_id": articleID, "updated_at": at(t, saved, "article", "updated_at")})
}

// Another workspace can neither publish nor discard an article's draft.
func TestDraftToolsStayInTheirWorkspace(t *testing.T) {
	e := newEnv(t)
	alpha := e.newWorkspace("KB Draft Alpha Tools", "admin@kb-draft-alpha-tools.example.com")
	beta := e.newWorkspace("KB Draft Beta Tools", "admin@kb-draft-beta-tools.example.com")
	collectionID := e.created("/api/v1/kb/collections", map[string]any{"name": "Alpha"}, alpha.admin)
	articleID := e.created("/api/v1/kb/articles", map[string]any{"collection_id": collectionID, "title": "Alpha", "body_md": "Live.", "status": "published"}, alpha.admin)
	owner := e.connect(alpha.send, "")
	live := at(t, e.ok(owner, "colliedesk_kb_article_get", map[string]any{"article_id": articleID}), "article", "updated_at")
	seen := at(t, e.ok(owner, "colliedesk_kb_article_update", map[string]any{"article_id": articleID, "updated_at": live, "body": "Draft."}), "article", "updated_at")

	session := e.connect(beta.send, "")
	for _, tool := range []string{"colliedesk_kb_article_draft_publish", "colliedesk_kb_article_draft_discard"} {
		if message := e.fails(session, tool, map[string]any{"article_id": articleID, "updated_at": seen}); !strings.Contains(message, "No knowledge base article") {
			t.Errorf("beta %s: %q", tool, message)
		}
	}
	if got := e.ok(owner, "colliedesk_kb_article_get", map[string]any{"article_id": articleID}); at(t, got, "article", "team_text", "body") != "Live." || at(t, got, "article", "draft", "team_text", "body") != "Draft." {
		t.Fatalf("alpha's article after beta's calls = %v", got)
	}
}
