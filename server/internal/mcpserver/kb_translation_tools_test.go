package mcpserver_test

import (
	"strings"
	"testing"
)

// An AI agent translates the knowledge base in a few calls: the languages say what is missing, the
// list names the articles, one read gives the translation and its original, one write each.
func TestAnAgentTranslatesAnArticleMarksItUpToDateAndUndoesEveryStep(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("KB Translation Tools", "admin@kb-translation-tools.example.com")
	session := e.connect(w.adminTier, "")
	collection := e.ok(session, "colliedesk_kb_collection_create", map[string]any{"name": "Billing"})
	collectionID := at(t, collection, "collection", "id").(string)
	article := e.ok(session, "colliedesk_kb_article_create", map[string]any{"collection_id": collectionID, "title": "Pay an invoice", "body": "Open **Billing**.", "status": "published"})
	articleID := at(t, article, "article", "id").(string)

	// A refused call says how to fix it.
	refused := e.fails(session, "colliedesk_kb_translation_create", map[string]any{"article_id": articleID, "locale": "fr", "title": "Payer", "body": "Ouvrez."})
	if !strings.Contains(refused, "/api/v1/help-center/languages") && !strings.Contains(refused, "colliedesk_help_center_languages_set") {
		t.Fatalf("unlisted language message = %q", refused)
	}
	languages := e.ok(session, "colliedesk_help_center_languages_set", map[string]any{"languages": []any{map[string]any{"locale": "fr", "visible": false}}})
	if at(t, languages, "languages", "primary") != "en" || at(t, languages, "languages", "languages", 0, "missing") != float64(1) {
		t.Fatalf("languages = %v", languages)
	}
	missing := e.ok(session, "colliedesk_kb_translations_list", map[string]any{"locale": "fr", "state": "missing"})
	if at(t, missing, "articles", 0, "team_text", "title") != "Pay an invoice" || at(t, missing, "articles", 0, "collection") != "Billing" || at(t, missing, "articles", 0, "translation") != nil {
		t.Fatalf("missing = %v", missing)
	}

	created := e.ok(session, "colliedesk_kb_translation_create", map[string]any{"article_id": articleID, "locale": "fr", "title": "Payer une facture", "body": "Ouvrez **Facturation**."})
	if at(t, created, "translation", "status") != "draft" || at(t, created, "translation", "original", "team_text", "body") != "Open **Billing**." ||
		at(t, created, "translation", "original", "collection") != "Billing" || at(t, created, "translation", "last_change", "via_token") != "test admin" {
		t.Fatalf("created = %v", created)
	}
	// The article changes; its translation is outdated, and one read shows both texts.
	e.ok(session, "colliedesk_kb_article_update", map[string]any{"article_id": articleID, "updated_at": at(t, article, "article", "updated_at"), "body": "Open **Billing**, then Pay."})
	read := e.ok(session, "colliedesk_kb_translation_get", map[string]any{"article_id": articleID, "locale": "fr"})
	if at(t, read, "translation", "outdated") != true || at(t, read, "translation", "original", "team_text", "body") != "Open **Billing**, then Pay." {
		t.Fatalf("read = %v", read)
	}
	updated := e.ok(session, "colliedesk_kb_translation_update", map[string]any{"article_id": articleID, "locale": "fr", "updated_at": at(t, read, "translation", "updated_at"),
		"body": "Ouvrez **Facturation**, puis Payer.", "outdated": false, "status": "published"})
	if at(t, updated, "translation", "outdated") != false || at(t, updated, "translation", "status") != "published" {
		t.Fatalf("updated = %v", updated)
	}
	got := e.ok(session, "colliedesk_kb_article_get", map[string]any{"article_id": articleID})
	if at(t, got, "article", "translations", 0, "locale") != "fr" || at(t, got, "article", "translations", 0, "team_text", "title") != "Payer une facture" {
		t.Fatalf("article translations = %v", got)
	}
	history := e.ok(session, "colliedesk_kb_translation_history", map[string]any{"article_id": articleID, "locale": "fr"})
	if len(history["versions"].([]any)) != 2 {
		t.Fatalf("history = %v", history)
	}
	restored := e.ok(session, "colliedesk_kb_translation_version_restore", map[string]any{"article_id": articleID, "locale": "fr", "version": 1, "updated_at": at(t, updated, "translation", "updated_at")})
	if at(t, restored, "translation", "team_text", "body") != "Ouvrez **Facturation**." {
		t.Fatalf("restored = %v", restored)
	}
	deleted := e.ok(session, "colliedesk_kb_translation_delete", map[string]any{"article_id": articleID, "locale": "fr", "updated_at": at(t, restored, "translation", "updated_at")})
	if !strings.Contains(at(t, deleted, "next").(string), "colliedesk_kb_translation_undelete") {
		t.Fatalf("deleted = %v", deleted)
	}
	trash := e.ok(session, "colliedesk_kb_translations_trash", nil)
	if at(t, trash, "translations", 0, "article_title") != "Pay an invoice" {
		t.Fatalf("trash = %v", trash)
	}
	back := e.ok(session, "colliedesk_kb_translation_undelete", map[string]any{"article_id": articleID, "locale": "fr", "updated_at": at(t, trash, "translations", 0, "updated_at")})
	if at(t, back, "translation", "in_trash") != false {
		t.Fatalf("undeleted = %v", back)
	}
	if message := e.fails(session, "colliedesk_kb_translation_get", map[string]any{"article_id": articleID, "locale": "de"}); !strings.Contains(message, "colliedesk_kb_translation_create") {
		t.Fatalf("missing translation message = %q", message)
	}

	named := e.ok(session, "colliedesk_kb_collection_translation_set", map[string]any{"collection_id": collectionID, "locale": "fr", "name": "Facturation"})
	if at(t, named, "collection", "translations", 0, "name") != "Facturation" {
		t.Fatalf("named = %v", named)
	}
	unnamed := e.ok(session, "colliedesk_kb_collection_translation_delete", map[string]any{"collection_id": collectionID, "locale": "fr"})
	if len(at(t, unnamed, "collection", "translations").([]any)) != 0 {
		t.Fatalf("unnamed = %v", unnamed)
	}
	if message := e.fails(session, "colliedesk_help_center_languages_set", map[string]any{"languages": []any{}}); !strings.Contains(message, "visible false") {
		t.Fatalf("dropping a language in use = %q", message)
	}
	got = e.ok(session, "colliedesk_help_center_languages_get", nil)
	if at(t, got, "languages", "languages", 0, "published") != float64(1) {
		t.Fatalf("languages = %v", got)
	}
}
