package mcpserver_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// kbPublishedFrench adds a published article and its published fr translation, and returns the
// article id and the translation's updated_at.
func kbPublishedFrench(t *testing.T, e *env, session *mcp.ClientSession) (string, string) {
	t.Helper()
	e.ok(session, "colliedesk_help_center_languages_set", map[string]any{"languages": []any{map[string]any{"locale": "fr", "visible": true}}})
	collectionID := at(t, e.ok(session, "colliedesk_kb_collection_create", map[string]any{"name": "Billing"}), "collection", "id").(string)
	articleID := at(t, e.ok(session, "colliedesk_kb_article_create", map[string]any{"collection_id": collectionID, "title": "Refunds", "body": "Within 14 days.", "status": "published"}),
		"article", "id").(string)
	created := e.ok(session, "colliedesk_kb_translation_create", map[string]any{"article_id": articleID, "locale": "fr", "title": "Remboursements", "body": "Sous 14 jours.", "status": "published"})
	return articleID, at(t, created, "translation", "updated_at").(string)
}

// An AI agent edits a published translation as a person does in the editor: the change waits in the
// translation's draft, the help center keeps the published text, and the same two draft tools as for
// an article publish or discard it, given the locale.
func TestAnAgentPublishesAndDiscardsTheChangesOfAPublishedTranslation(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("KB Translation Draft Tools", "admin@kb-translation-draft-tools.example.com")
	session := e.connect(w.adminTier, "")
	articleID, live := kbPublishedFrench(t, e, session)

	saved := e.ok(session, "colliedesk_kb_translation_update", map[string]any{"article_id": articleID, "locale": "fr", "updated_at": live, "body": "Sous 30 jours."})
	if at(t, saved, "translation", "team_text", "body") != "Sous 14 jours." || at(t, saved, "translation", "has_draft") != true ||
		at(t, saved, "translation", "draft", "team_text", "body") != "Sous 30 jours." || at(t, saved, "translation", "status") != "published" {
		t.Fatalf("saved = %v", saved)
	}
	listed := e.ok(session, "colliedesk_kb_article_get", map[string]any{"article_id": articleID})
	if at(t, listed, "article", "translations", 0, "has_draft") != true {
		t.Fatalf("article translations = %v", listed)
	}

	published := e.ok(session, "colliedesk_kb_article_draft_publish", map[string]any{"article_id": articleID, "locale": "fr", "updated_at": at(t, saved, "translation", "updated_at")})
	if at(t, published, "translation", "locale") != "fr" || at(t, published, "translation", "has_draft") != false || at(t, published, "translation", "team_text", "title") != "Remboursements" {
		t.Fatalf("published = %v", published)
	}
	if got := e.ok(session, "colliedesk_kb_translation_get", map[string]any{"article_id": articleID, "locale": "fr"}); at(t, got, "translation", "team_text", "body") != "Sous 30 jours." ||
		at(t, got, "translation", "draft") != nil {
		t.Fatalf("translation after publish = %v", got)
	}

	pending := e.ok(session, "colliedesk_kb_translation_update", map[string]any{"article_id": articleID, "locale": "fr", "updated_at": at(t, published, "translation", "updated_at"), "body": "Sous 60 jours."})
	discarded := e.ok(session, "colliedesk_kb_article_draft_discard", map[string]any{"article_id": articleID, "locale": "fr", "updated_at": at(t, pending, "translation", "updated_at")})
	if at(t, discarded, "translation", "has_draft") != false || at(t, discarded, "translation", "updated_at") != at(t, published, "translation", "updated_at") {
		t.Fatalf("discarded = %v", discarded)
	}
	if got := e.ok(session, "colliedesk_kb_translation_get", map[string]any{"article_id": articleID, "locale": "fr"}); at(t, got, "translation", "team_text", "body") != "Sous 30 jours." {
		t.Fatalf("translation after discard = %v", got)
	}
	if message := e.fails(session, "colliedesk_kb_article_draft_discard", map[string]any{"article_id": articleID, "locale": "fr", "updated_at": at(t, published, "translation", "updated_at")}); !strings.Contains(message, "no unpublished changes") {
		t.Fatalf("second discard = %q", message)
	}
	// The article itself never had a draft: the calls with a locale left it alone.
	if got := e.ok(session, "colliedesk_kb_article_get", map[string]any{"article_id": articleID}); at(t, got, "article", "has_draft") != false || at(t, got, "article", "team_text", "body") != "Within 14 days." {
		t.Fatalf("article = %v", got)
	}
}

// A token that may edit articles but not publish them saves a translation's draft and is refused on publish.
func TestAnEditorTokenSavesATranslationDraftButCannotPublishIt(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("KB Translation Draft Editor", "admin@kb-translation-draft-editor.example.com")
	articleID, live := kbPublishedFrench(t, e, e.connect(w.adminTier, ""))
	editor := e.connect(e.token(w.admin, "test editor", "", slices.DeleteFunc(slices.Clone(sendPermissions), func(p string) bool { return p == "kb_articles:publish" })), "")

	saved := e.ok(editor, "colliedesk_kb_translation_update", map[string]any{"article_id": articleID, "locale": "fr", "updated_at": live, "body": "Sous 30 jours."})
	if at(t, saved, "translation", "draft", "team_text", "body") != "Sous 30 jours." {
		t.Fatalf("editor save = %v", saved)
	}
	e.refused(editor, "colliedesk_kb_article_draft_publish", map[string]any{"article_id": articleID, "locale": "fr", "updated_at": at(t, saved, "translation", "updated_at")})
	if got := e.ok(editor, "colliedesk_kb_translation_get", map[string]any{"article_id": articleID, "locale": "fr"}); at(t, got, "translation", "team_text", "body") != "Sous 14 jours." {
		t.Fatalf("a refused publish changed the live text: %v", got)
	}
	e.ok(editor, "colliedesk_kb_article_draft_discard", map[string]any{"article_id": articleID, "locale": "fr", "updated_at": at(t, saved, "translation", "updated_at")})
}

// Another workspace can neither publish nor discard a translation's draft.
func TestTranslationDraftToolsStayInTheirWorkspace(t *testing.T) {
	e := newEnv(t)
	alpha := e.newWorkspace("KB Translation Draft Alpha", "admin@kb-translation-draft-alpha.example.com")
	beta := e.newWorkspace("KB Translation Draft Beta", "admin@kb-translation-draft-beta.example.com")
	owner := e.connect(alpha.adminTier, "")
	articleID, live := kbPublishedFrench(t, e, owner)
	seen := at(t, e.ok(owner, "colliedesk_kb_translation_update", map[string]any{"article_id": articleID, "locale": "fr", "updated_at": live, "body": "Brouillon."}), "translation", "updated_at")

	session := e.connect(beta.adminTier, "")
	for _, tool := range []string{"colliedesk_kb_article_draft_publish", "colliedesk_kb_article_draft_discard"} {
		if message := e.fails(session, tool, map[string]any{"article_id": articleID, "locale": "fr", "updated_at": seen}); !strings.Contains(message, "colliedesk_kb_translation_get") {
			t.Errorf("beta %s: %q", tool, message)
		}
	}
	if got := e.ok(owner, "colliedesk_kb_translation_get", map[string]any{"article_id": articleID, "locale": "fr"}); at(t, got, "translation", "team_text", "body") != "Sous 14 jours." ||
		at(t, got, "translation", "draft", "team_text", "body") != "Brouillon." {
		t.Fatalf("alpha's translation after beta's calls = %v", got)
	}
}
