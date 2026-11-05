package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/store"
)

type kbTranslationView struct {
	ArticleID       string           `json:"article_id"`
	Locale          string           `json:"locale"`
	Title           string           `json:"title"`
	Slug            string           `json:"slug"`
	Status          string           `json:"status"`
	BodyMD          string           `json:"body_md"`
	SEOTitle        string           `json:"seo_title"`
	Outdated        bool             `json:"outdated"`
	InTrash         bool             `json:"in_trash"`
	UpdatedAt       string           `json:"updated_at"`
	URL             string           `json:"url"`
	LanguageVisible bool             `json:"language_visible"`
	LastChange      *replyChangeView `json:"last_change"`
	Original        struct {
		Title          string `json:"title"`
		BodyMD         string `json:"body_md"`
		Locale         string `json:"locale"`
		CollectionName string `json:"collection_name"`
		Version        int    `json:"version"`
	} `json:"original"`
}

type kbLanguagesView struct {
	Primary   string `json:"primary"`
	Languages []struct {
		Locale    string `json:"locale"`
		Visible   bool   `json:"visible"`
		Published int    `json:"published"`
		Drafts    int    `json:"drafts"`
		Outdated  int    `json:"outdated"`
		Missing   int    `json:"missing"`
	} `json:"languages"`
	Unlisted []string `json:"unlisted"`
}

type kbLanguagePage struct {
	Items []struct {
		ArticleID      string             `json:"article_id"`
		Title          string             `json:"title"`
		CollectionName string             `json:"collection_name"`
		Translation    *kbTranslationView `json:"translation"`
	} `json:"items"`
}

func kbTranslationPath(articleID, locale string, rest ...string) string {
	return "/api/v1/kb/articles/" + articleID + "/translations/" + locale + strings.Join(rest, "")
}

func kbLanguageTitles(t *testing.T, api http.Handler, token, locale, state string) string {
	t.Helper()
	var page kbLanguagePage
	kbCall(t, api, http.MethodGet, "/api/v1/kb/translations?locale="+locale+"&state="+state, nil, token, http.StatusOK, &page)
	titles := []string{}
	for _, item := range page.Items {
		titles = append(titles, item.Title)
	}
	return strings.Join(titles, ",")
}

func TestHelpCenterLanguagesAreListedHiddenFirstAndNeverDroppedWithTheirTranslations(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "KB Languages", "admin@kb-languages.example.com")
	var languages kbLanguagesView
	kbCall(t, api, http.MethodGet, "/api/v1/help-center/languages", nil, admin, http.StatusOK, &languages)
	if languages.Primary != "en" || len(languages.Languages) != 0 || len(languages.Unlisted) != 0 {
		t.Fatalf("languages = %+v", languages)
	}
	// The primary language is the articles' own: it is never listed.
	response := kbCall(t, api, http.MethodPut, "/api/v1/help-center/languages", map[string]any{"languages": []map[string]any{{"locale": "en", "visible": true}}}, admin, http.StatusBadRequest, nil)
	if !strings.Contains(response.Body.String(), "primary language") {
		t.Fatalf("primary listed: %s", response.Body.String())
	}
	kbCall(t, api, http.MethodPut, "/api/v1/help-center/languages", map[string]any{"languages": []map[string]any{{"locale": "fr", "visible": false}, {"locale": "de", "visible": true}}},
		admin, http.StatusOK, &languages)
	if len(languages.Languages) != 2 || languages.Languages[0].Locale != "fr" || languages.Languages[0].Visible || !languages.Languages[1].Visible {
		t.Fatalf("languages = %+v", languages)
	}
	collection := kbCollection(t, api, admin, map[string]any{"name": "Docs"})
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Reset", "body_md": "Steps."})
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles/"+article.ID+"/translations", map[string]any{"locale": "fr", "title": "Réinitialiser", "body_md": "Étapes."}, admin, http.StatusCreated, nil)
	response = kbCall(t, api, http.MethodPut, "/api/v1/help-center/languages", map[string]any{"languages": []map[string]any{{"locale": "de", "visible": true}}}, admin, http.StatusConflict, nil)
	if errorCode(t, response) != "language_in_use" || !strings.Contains(response.Body.String(), "fr") {
		t.Fatalf("dropping fr: %s", response.Body.String())
	}
	kbCall(t, api, http.MethodGet, "/api/v1/help-center/languages", nil, admin, http.StatusOK, &languages)
	if fr := languages.Languages[0]; fr.Locale != "fr" || fr.Drafts != 1 || fr.Missing != 0 || languages.Languages[1].Missing != 1 {
		t.Fatalf("counts = %+v", languages.Languages)
	}
}

func TestKnowledgeBaseTranslationsHaveTheirOwnTextStatusHistoryTrashAndOutdatedFlag(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "KB Translations", "admin@kb-translations.example.com")
	collection := kbCollection(t, api, admin, map[string]any{"name": "Docs"})
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Reset your password", "body_md": "Open Settings.", "status": "published"})
	other := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Invoices", "body_md": "Billing."})

	response := kbCall(t, api, http.MethodPost, "/api/v1/kb/articles/"+article.ID+"/translations", map[string]any{"locale": "fr", "title": "Réinitialiser", "body_md": "Ouvrez."}, admin, http.StatusBadRequest, nil)
	if errorCode(t, response) != "language_not_listed" || !strings.Contains(response.Body.String(), "/api/v1/help-center/languages") {
		t.Fatalf("unlisted language: %s", response.Body.String())
	}
	kbCall(t, api, http.MethodPut, "/api/v1/help-center/languages", map[string]any{"languages": []map[string]any{{"locale": "fr", "visible": true}}}, admin, http.StatusOK, nil)

	var created kbTranslationView
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles/"+article.ID+"/translations",
		map[string]any{"locale": "fr", "title": "Réinitialiser le mot de passe", "body_md": "Ouvrez les réglages.", "seo_title": "Mot de passe"}, admin, http.StatusCreated, &created)
	if created.Status != "draft" || created.Slug != "réinitialiser-le-mot-de-passe" || created.Outdated || !created.LanguageVisible || created.SEOTitle != "Mot de passe" ||
		created.Original.Title != "Reset your password" || created.Original.BodyMD != "Open Settings." || created.Original.Locale != "en" || created.Original.CollectionName != "Docs" ||
		!strings.HasSuffix(created.URL, "/fr/a/1-r%C3%A9initialiser-le-mot-de-passe") || created.LastChange == nil || created.LastChange.Kind != "create" {
		t.Fatalf("created = %+v", created)
	}
	if response := kbCall(t, api, http.MethodPost, "/api/v1/kb/articles/"+article.ID+"/translations", map[string]any{"locale": "fr", "title": "Encore", "body_md": "x"}, admin, http.StatusConflict, nil); errorCode(t, response) != "translation_exists" {
		t.Fatalf("second fr translation: %s", response.Body.String())
	}
	var read kbArticleView
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles/"+article.ID, nil, admin, http.StatusOK, &read)
	if got := kbLanguageTitles(t, api, admin, "fr", "missing"); got != "Invoices" {
		t.Fatalf("missing in fr = %s", got)
	}

	// The article's own words change: its translation is outdated.
	var edited kbArticleView
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+article.ID, map[string]any{"updated_at": read.UpdatedAt, "body_md": "Open Settings, then Security."}, admin, http.StatusOK, &edited)
	if got := kbLanguageTitles(t, api, admin, "fr", "outdated"); got != "Reset your password" {
		t.Fatalf("outdated in fr = %s", got)
	}
	var current kbTranslationView
	kbCall(t, api, http.MethodGet, kbTranslationPath(article.ID, "fr"), nil, admin, http.StatusOK, &current)
	if !current.Outdated || current.Original.BodyMD != "Open Settings, then Security." {
		t.Fatalf("after the article changed: %+v", current)
	}
	// Moving the article only (no words) leaves an up to date translation alone; marking it up to date is a version.
	var marked kbTranslationView
	kbCall(t, api, http.MethodPatch, kbTranslationPath(article.ID, "fr"), map[string]any{"updated_at": current.UpdatedAt, "body_md": "Ouvrez les réglages, puis Sécurité.", "outdated": false},
		admin, http.StatusOK, &marked)
	if marked.Outdated || marked.LastChange.Kind != "edit" {
		t.Fatalf("marked = %+v", marked)
	}
	kbCall(t, api, http.MethodPatch, kbTranslationPath(article.ID, "fr"), map[string]any{"updated_at": marked.UpdatedAt, "outdated": true}, admin, http.StatusOK, &marked)
	if !marked.Outdated || marked.LastChange.Kind != "mark_outdated" {
		t.Fatalf("marked outdated = %+v", marked)
	}
	kbCall(t, api, http.MethodPatch, kbTranslationPath(article.ID, "fr"), map[string]any{"updated_at": marked.UpdatedAt, "outdated": false, "status": "published"}, admin, http.StatusOK, &marked)
	if marked.Outdated || marked.Status != "published" {
		t.Fatalf("published = %+v", marked)
	}
	if response := kbCall(t, api, http.MethodPatch, kbTranslationPath(article.ID, "fr"), map[string]any{"updated_at": current.UpdatedAt, "title": "Stale"}, admin, http.StatusConflict, nil); errorCode(t, response) != "changed_elsewhere" {
		t.Fatalf("stale write: %s", response.Body.String())
	}

	// History and restore.
	var versions struct {
		Items []struct {
			Version int    `json:"version"`
			Kind    string `json:"kind"`
			BodyMD  string `json:"body_md"`
		} `json:"items"`
	}
	kbCall(t, api, http.MethodGet, kbTranslationPath(article.ID, "fr", "/versions"), nil, admin, http.StatusOK, &versions)
	if len(versions.Items) != 4 || versions.Items[3].Kind != "create" || versions.Items[2].Kind != "edit" || versions.Items[1].Kind != "mark_outdated" {
		t.Fatalf("versions = %+v", versions.Items)
	}
	var restored kbTranslationView
	// A repeated press (same Idempotency-Key) answers the first restore again and adds no version.
	first := postWithKey(t, api, admin, kbTranslationPath(article.ID, "fr", "/versions/1/restore"), "restore-version-1", map[string]any{"updated_at": marked.UpdatedAt})
	again := postWithKey(t, api, admin, kbTranslationPath(article.ID, "fr", "/versions/1/restore"), "restore-version-1", map[string]any{"updated_at": marked.UpdatedAt})
	if first.Code != http.StatusOK || again.Code != http.StatusOK || sameUpdate(t, first) != sameUpdate(t, again) {
		t.Fatalf("restore version, repeated: %d %s / %d %s", first.Code, first.Body.String(), again.Code, again.Body.String())
	}
	decode(t, first, &restored)
	if restored.BodyMD != "Ouvrez les réglages." || restored.Status != "published" || restored.LastChange.Kind != "restore" {
		t.Fatalf("restored = %+v", restored)
	}
	kbCall(t, api, http.MethodGet, kbTranslationPath(article.ID, "fr", "/versions"), nil, admin, http.StatusOK, &versions)
	if len(versions.Items) != 5 {
		t.Fatalf("versions after a repeated restore = %d", len(versions.Items))
	}

	// Trash and back.
	kbCall(t, api, http.MethodDelete, kbTranslationPath(article.ID, "fr", "?updated_at="+url.QueryEscape(restored.UpdatedAt)), nil, admin, http.StatusNoContent, nil)
	kbCall(t, api, http.MethodGet, kbTranslationPath(article.ID, "fr"), nil, admin, http.StatusOK, &current)
	if !current.InTrash {
		t.Fatalf("deleted translation = %+v", current)
	}
	if got := kbLanguageTitles(t, api, admin, "fr", "missing"); got != "Reset your password,Invoices" && got != "Invoices,Reset your password" {
		t.Fatalf("missing in fr after the delete = %s", got)
	}
	var trash struct {
		Items []struct {
			ArticleTitle string `json:"article_title"`
			Locale       string `json:"locale"`
			UpdatedAt    string `json:"updated_at"`
		} `json:"items"`
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/translations/trash", nil, admin, http.StatusOK, &trash)
	if len(trash.Items) != 1 || trash.Items[0].ArticleTitle != "Reset your password" || trash.Items[0].Locale != "fr" {
		t.Fatalf("trash = %+v", trash.Items)
	}
	if response := kbCall(t, api, http.MethodPost, "/api/v1/kb/articles/"+article.ID+"/translations", map[string]any{"locale": "fr", "title": "Nouvelle", "body_md": "x"}, admin, http.StatusConflict, nil); errorCode(t, response) != "translation_exists" {
		t.Fatalf("new translation over one in the trash: %s", response.Body.String())
	}
	first = postWithKey(t, api, admin, kbTranslationPath(article.ID, "fr", "/restore"), "undelete-1", map[string]any{"updated_at": trash.Items[0].UpdatedAt})
	again = postWithKey(t, api, admin, kbTranslationPath(article.ID, "fr", "/restore"), "undelete-1", map[string]any{"updated_at": trash.Items[0].UpdatedAt})
	if first.Code != http.StatusOK || again.Code != http.StatusOK || sameUpdate(t, first) != sameUpdate(t, again) {
		t.Fatalf("restore from the trash, repeated: %d %s / %d %s", first.Code, first.Body.String(), again.Code, again.Body.String())
	}
	if reused := postWithKey(t, api, admin, kbTranslationPath(article.ID, "fr", "/restore"), "undelete-1", map[string]any{"updated_at": restored.UpdatedAt}); reused.Code != http.StatusUnprocessableEntity {
		t.Fatalf("key reused for another restore: %d %s", reused.Code, reused.Body.String())
	}
	decode(t, first, &restored)
	if restored.InTrash || restored.BodyMD != "Ouvrez les réglages." || restored.LastChange.Kind != "undelete" {
		t.Fatalf("undeleted = %+v", restored)
	}
	_ = other
}

func TestKnowledgeBaseCollectionsHaveNamesPerLanguage(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "KB Collection Names", "admin@kb-collection-names.example.com")
	collection := kbCollection(t, api, admin, map[string]any{"name": "Billing"})
	if response := kbCall(t, api, http.MethodPut, "/api/v1/kb/collections/"+collection.ID+"/translations/fr", map[string]any{"name": "Facturation"}, admin, http.StatusBadRequest, nil); errorCode(t, response) != "language_not_listed" {
		t.Fatalf("unlisted language: %s", response.Body.String())
	}
	kbCall(t, api, http.MethodPut, "/api/v1/help-center/languages", map[string]any{"languages": []map[string]any{{"locale": "fr", "visible": false}}}, admin, http.StatusOK, nil)
	var named struct {
		Translations []struct {
			Locale, Name, Description string
			Imported                  bool
		} `json:"translations"`
	}
	kbCall(t, api, http.MethodPut, "/api/v1/kb/collections/"+collection.ID+"/translations/fr", map[string]any{"name": "Facturation", "description": "Vos factures"}, admin, http.StatusOK, &named)
	if len(named.Translations) != 1 || named.Translations[0].Name != "Facturation" || named.Translations[0].Description != "Vos factures" || named.Translations[0].Imported {
		t.Fatalf("named = %+v", named)
	}
	var list struct {
		Items []struct {
			Translations []struct{ Locale, Name string } `json:"translations"`
		} `json:"items"`
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/collections", nil, admin, http.StatusOK, &list)
	if len(list.Items) != 1 || len(list.Items[0].Translations) != 1 || list.Items[0].Translations[0].Name != "Facturation" {
		t.Fatalf("collections = %+v", list.Items)
	}
	kbCall(t, api, http.MethodDelete, "/api/v1/kb/collections/"+collection.ID+"/translations/fr", nil, admin, http.StatusOK, &named)
	if len(named.Translations) != 0 {
		t.Fatalf("after the delete = %+v", named)
	}
	kbCall(t, api, http.MethodDelete, "/api/v1/kb/collections/"+collection.ID+"/translations/fr", nil, admin, http.StatusNotFound, nil)
}

func TestKnowledgeBaseTranslationsNeverCrossWorkspaces(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	alpha := createSession(t, api, service, "KB Tr Alpha", "admin@kb-tr-alpha.example.com")
	beta := createSession(t, api, service, "KB Tr Beta", "admin@kb-tr-beta.example.com")
	for _, token := range []string{alpha, beta} {
		kbCall(t, api, http.MethodPut, "/api/v1/help-center/languages", map[string]any{"languages": []map[string]any{{"locale": "fr", "visible": true}}}, token, http.StatusOK, nil)
	}
	collection := kbCollection(t, api, alpha, map[string]any{"name": "Alpha docs"})
	article := kbArticle(t, api, alpha, map[string]any{"collection_id": collection.ID, "title": "Alpha secret", "body_md": "Secret."})
	var translation kbTranslationView
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles/"+article.ID+"/translations", map[string]any{"locale": "fr", "title": "Secret alpha", "body_md": "Secret."}, alpha, http.StatusCreated, &translation)

	for _, call := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, kbTranslationPath(article.ID, "fr"), nil},
		{http.MethodGet, kbTranslationPath(article.ID, "fr", "/versions"), nil},
		{http.MethodPatch, kbTranslationPath(article.ID, "fr"), map[string]any{"updated_at": translation.UpdatedAt, "title": "Taken"}},
		{http.MethodDelete, kbTranslationPath(article.ID, "fr", "?updated_at="+url.QueryEscape(translation.UpdatedAt)), nil},
		{http.MethodPost, kbTranslationPath(article.ID, "fr", "/restore"), map[string]any{"updated_at": translation.UpdatedAt}},
		{http.MethodPost, kbTranslationPath(article.ID, "fr", "/versions/1/restore"), map[string]any{"updated_at": translation.UpdatedAt}},
		{http.MethodPost, "/api/v1/kb/articles/" + article.ID + "/translations", map[string]any{"locale": "fr", "title": "Mine", "body_md": "x"}},
		{http.MethodPut, "/api/v1/kb/collections/" + collection.ID + "/translations/fr", map[string]any{"name": "Taken"}},
		{http.MethodDelete, "/api/v1/kb/collections/" + collection.ID + "/translations/fr", nil},
	} {
		if response := request(t, api, call.method, call.path, call.body, beta, nil); response.Code != http.StatusNotFound {
			t.Errorf("beta %s %s status=%d body=%s, want 404", call.method, call.path, response.Code, response.Body.String())
		}
	}
	if got := kbLanguageTitles(t, api, beta, "fr", "translated"); got != "" {
		t.Fatalf("beta sees alpha's articles: %s", got)
	}
	var trash struct {
		Items []any `json:"items"`
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/translations/trash", nil, beta, http.StatusOK, &trash)
	if len(trash.Items) != 0 {
		t.Fatalf("beta trash = %+v", trash.Items)
	}
	var languages kbLanguagesView
	kbCall(t, api, http.MethodGet, "/api/v1/help-center/languages", nil, beta, http.StatusOK, &languages)
	if languages.Languages[0].Drafts != 0 || languages.Languages[0].Missing != 0 {
		t.Fatalf("beta counts alpha's translations: %+v", languages.Languages)
	}
	kbCall(t, api, http.MethodGet, kbTranslationPath(article.ID, "fr"), nil, alpha, http.StatusOK, &translation)
	if translation.Title != "Secret alpha" {
		t.Fatalf("alpha's translation changed: %+v", translation)
	}
}

func TestKnowledgeBaseTranslationWritesNeedTheirPermission(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "KB Tr Tokens", "admin@kb-tr-tokens.example.com")
	read := issueToken(t, api, admin, "Reader", "read").Token
	send := issueToken(t, api, admin, "Writer", "send").Token
	kbCall(t, api, http.MethodPut, "/api/v1/help-center/languages", map[string]any{"languages": []map[string]any{{"locale": "fr", "visible": false}}}, admin, http.StatusOK, nil)
	collection := kbCollection(t, api, admin, map[string]any{"name": "Docs"})
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Reset", "body_md": "x"})
	refused(t, api, http.MethodPost, "/api/v1/kb/articles/"+article.ID+"/translations", map[string]any{"locale": "fr", "title": "R", "body_md": "x"}, read, "kb_articles:create")
	var draft kbTranslationView
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles/"+article.ID+"/translations", map[string]any{"locale": "fr", "title": "Réinitialiser", "body_md": "x"}, send, http.StatusCreated, &draft)
	if draft.LastChange == nil || draft.LastChange.Token == nil || draft.LastChange.Token.Label != "Writer" {
		t.Fatalf("agent version = %+v", draft.LastChange)
	}
	// Setting the help center's languages takes help_center:update, which a send token was not given.
	refused(t, api, http.MethodPut, "/api/v1/help-center/languages", map[string]any{"languages": []map[string]any{{"locale": "fr", "visible": true}}}, send, "help_center:update")
	refused(t, api, http.MethodPatch, kbTranslationPath(article.ID, "fr"), map[string]any{"updated_at": draft.UpdatedAt, "title": "Lu"}, read, "kb_articles:update")
}

func TestExportCarriesTranslationsTheirHistoryLanguagesAndCollectionNames(t *testing.T) {
	api, service, pool, storage := testAPIWithStorage(t)
	admin := createSession(t, api, service, "KB Tr Export", "admin@kb-tr-export.example.com")
	kbCall(t, api, http.MethodPut, "/api/v1/help-center/languages", map[string]any{"languages": []map[string]any{{"locale": "fr", "visible": true}, {"locale": "de", "visible": false}}}, admin, http.StatusOK, nil)
	collection := kbCollection(t, api, admin, map[string]any{"name": "Guides"})
	kbCall(t, api, http.MethodPut, "/api/v1/kb/collections/"+collection.ID+"/translations/fr", map[string]any{"name": "Guides FR"}, admin, http.StatusOK, nil)
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Guide", "body_md": "Text"})
	var translation kbTranslationView
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles/"+article.ID+"/translations", map[string]any{"locale": "fr", "title": "Guide FR", "body_md": "Texte", "seo_title": "SEO FR"}, admin, http.StatusCreated, &translation)
	var trashed kbTranslationView
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles/"+article.ID+"/translations", map[string]any{"locale": "de", "title": "Anleitung", "body_md": "Text"}, admin, http.StatusCreated, &trashed)
	kbCall(t, api, http.MethodDelete, kbTranslationPath(article.ID, "de", "?updated_at="+url.QueryEscape(trashed.UpdatedAt)), nil, admin, http.StatusNoContent, nil)

	response := request(t, api, http.MethodGet, "/api/v1/export", nil, admin, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("export status=%d", response.Code)
	}
	entries := unzip(t, response.Body.Bytes())
	translations := ndjsonLines(t, entries["kb-article-translations.ndjson"])
	if len(translations) != 2 || translations[1]["locale"] != "fr" || translations[1]["slug"] != "guide-fr" || translations[1]["seo_title"] != "SEO FR" ||
		translations[1]["status"] != "draft" || translations[0]["deleted_at"] == nil {
		t.Fatalf("kb-article-translations.ndjson = %v", translations)
	}
	if versions := ndjsonLines(t, entries["kb-article-translation-versions.ndjson"]); len(versions) != 3 {
		t.Fatalf("kb-article-translation-versions.ndjson = %v", versions)
	}
	collections := ndjsonLines(t, entries["kb-collections.ndjson"])
	if names, _ := collections[0]["translations"].([]any); len(names) != 1 || names[0].(map[string]any)["name"] != "Guides FR" {
		t.Fatalf("collection names = %v", collections[0]["translations"])
	}
	center := ndjsonLines(t, entries["help-center.ndjson"])
	if languages, _ := center[0]["languages"].([]any); len(languages) != 2 || languages[0].(map[string]any)["locale"] != "fr" || languages[0].(map[string]any)["visible"] != true {
		t.Fatalf("help center languages = %v", center[0]["languages"])
	}

	// A translation in the trash for 30 days is deleted for good; the article and its other translation stay.
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `UPDATE kb_article_translations SET deleted_at=now()-interval '31 days' WHERE locale='de'`); err != nil {
		t.Fatal(err)
	}
	purged, err := store.New(pool).WithObjects(storage).PurgeKBTrash(ctx)
	if err != nil || purged.Translations != 1 {
		t.Fatalf("purged = %+v, %v", purged, err)
	}
	kbCall(t, api, http.MethodGet, kbTranslationPath(article.ID, "de"), nil, admin, http.StatusNotFound, nil)
	kbCall(t, api, http.MethodGet, kbTranslationPath(article.ID, "fr"), nil, admin, http.StatusOK, nil)
}

// A translation's address is its page only while customers see its language; in a hidden language
// it is the article's own page, so nobody pastes an address that answers 404.
func TestATranslationInAHiddenLanguageGivesTheArticlesAddress(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "KB Hidden Address", "admin@kb-hidden-address.example.com")
	kbCall(t, api, http.MethodPut, "/api/v1/help-center/languages", map[string]any{"languages": []map[string]any{{"locale": "de", "visible": false}}}, admin, http.StatusOK, nil)
	collection := kbCollection(t, api, admin, map[string]any{"name": "Docs"})
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Reset", "body_md": "x", "status": "published"})
	var hidden kbTranslationView
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles/"+article.ID+"/translations", map[string]any{"locale": "de", "title": "Zurücksetzen", "body_md": "x", "status": "published"},
		admin, http.StatusCreated, &hidden)
	home := strings.TrimSuffix(article.URL, "/a/1-reset")
	if hidden.URL != home+"/a/1" {
		t.Fatalf("hidden language url = %q, want %q", hidden.URL, home+"/a/1")
	}
	var read struct {
		Translations []struct{ URL string } `json:"translations"`
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles/"+article.ID, nil, admin, http.StatusOK, &read)
	if len(read.Translations) != 1 || read.Translations[0].URL != home+"/a/1" {
		t.Fatalf("article translations = %+v", read.Translations)
	}
	kbCall(t, api, http.MethodPut, "/api/v1/help-center/languages", map[string]any{"languages": []map[string]any{{"locale": "de", "visible": true}}}, admin, http.StatusOK, nil)
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles/"+article.ID+"/translations/de", nil, admin, http.StatusOK, &hidden)
	if hidden.URL != home+"/de/a/1-zur%C3%BCcksetzen" {
		t.Fatalf("shown language url = %q", hidden.URL)
	}
}

// sameUpdate is the updated_at an answer carries: a repeat answers the first one's.
func sameUpdate(t *testing.T, response *httptest.ResponseRecorder) string {
	t.Helper()
	var view kbTranslationView
	if err := json.Unmarshal(response.Body.Bytes(), &view); err != nil || view.UpdatedAt == "" {
		return "unreadable: " + response.Body.String()
	}
	return view.UpdatedAt
}

// Three presses of one restore at once (same Idempotency-Key): one runs it, the others wait for its
// answer and answer it again. The text is the same answer; its bytes may differ (the repeat is read back
// from jsonb, which orders keys its own way). Nothing runs twice: one version, the same updated_at.
func TestARestoreOfATranslationPressedThreeTimesAtOnceRunsOnce(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "KB Restore Race", "admin@kb-restore-race.example.com")
	collection := kbCollection(t, api, admin, map[string]any{"name": "Docs"})
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Reset", "body_md": "Open.", "status": "published"})
	kbCall(t, api, http.MethodPut, "/api/v1/help-center/languages", map[string]any{"languages": []map[string]any{{"locale": "fr", "visible": true}}}, admin, http.StatusOK, nil)
	var translation kbTranslationView
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles/"+article.ID+"/translations", map[string]any{"locale": "fr", "title": "Réinitialiser", "body_md": "Ouvrez."}, admin, http.StatusCreated, &translation)
	kbCall(t, api, http.MethodPatch, kbTranslationPath(article.ID, "fr"), map[string]any{"updated_at": translation.UpdatedAt, "body_md": "Ouvrez les réglages."}, admin, http.StatusOK, &translation)

	for round := range 6 {
		key := "race-" + strconv.Itoa(round)
		path := kbTranslationPath(article.ID, "fr", "/versions/1/restore")
		before := translation.UpdatedAt
		responses := make([]*httptest.ResponseRecorder, 3)
		var wait sync.WaitGroup
		for index := range responses {
			wait.Add(1)
			go func() {
				defer wait.Done()
				responses[index] = postWithKey(t, api, admin, path, key, map[string]any{"updated_at": before})
			}()
		}
		wait.Wait()
		var first map[string]any
		for index, response := range responses {
			if response.Code != http.StatusOK {
				t.Fatalf("round %d press %d: %d %s", round, index, response.Code, response.Body.String())
			}
			var answer map[string]any
			decode(t, response, &answer)
			if first == nil {
				first = answer
			} else if !reflect.DeepEqual(first, answer) {
				t.Fatalf("round %d: press %d answered another restore:\n%s\n%s", round, index, responses[0].Body.String(), response.Body.String())
			}
		}
		var versions struct {
			Items []struct {
				Version int `json:"version"`
			} `json:"items"`
		}
		kbCall(t, api, http.MethodGet, kbTranslationPath(article.ID, "fr", "/versions"), nil, admin, http.StatusOK, &versions)
		if want := 3 + round; len(versions.Items) != want {
			t.Fatalf("round %d: %d versions, want %d (the restore ran more than once)", round, len(versions.Items), want)
		}
		kbCall(t, api, http.MethodGet, kbTranslationPath(article.ID, "fr"), nil, admin, http.StatusOK, &translation)
		if translation.UpdatedAt != first["updated_at"] {
			t.Fatalf("round %d: updated_at %s, answered %v", round, translation.UpdatedAt, first["updated_at"])
		}
	}
}
