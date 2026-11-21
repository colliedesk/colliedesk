package httpapi_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// kbDraftedTranslation is a translation as GET answers it, with its unpublished changes.
type kbDraftedTranslation struct {
	kbTranslationView
	HasDraft bool         `json:"has_draft"`
	Draft    *kbDraftView `json:"draft"`
}

func kbGetTranslation(t *testing.T, api http.Handler, token, articleID, locale string) kbDraftedTranslation {
	t.Helper()
	var translation kbDraftedTranslation
	kbCall(t, api, http.MethodGet, kbTranslationPath(articleID, locale), nil, token, http.StatusOK, &translation)
	return translation
}

// kbPublishTranslationChange saves change to a published translation and publishes it, as an editor
// does with Save and then Publish changes; into gets the published translation.
func kbPublishTranslationChange(t *testing.T, api http.Handler, token, articleID, locale string, change map[string]any, into any) {
	t.Helper()
	var saved kbDraftedTranslation
	kbCall(t, api, http.MethodPatch, kbTranslationPath(articleID, locale), change, token, http.StatusOK, &saved)
	if saved.Draft == nil {
		t.Fatalf("PATCH %s/%s saved no draft", articleID, locale)
	}
	kbCall(t, api, http.MethodPost, kbTranslationPath(articleID, locale, "/draft/publish"), map[string]any{"updated_at": saved.UpdatedAt}, token, http.StatusOK, into)
}

func kbTranslationVersionKinds(t *testing.T, api http.Handler, token, articleID, locale string) string {
	t.Helper()
	var versions struct {
		Items []struct {
			Kind string `json:"kind"`
		} `json:"items"`
	}
	kbCall(t, api, http.MethodGet, kbTranslationPath(articleID, locale, "/versions"), nil, token, http.StatusOK, &versions)
	kinds := []string{}
	for _, version := range versions.Items {
		kinds = append(kinds, version.Kind)
	}
	return strings.Join(kinds, ",")
}

// Saving a published translation keeps the change as a draft: its help center page, the Markdown copy,
// search, sitemap.xml, llms.txt, AI answers and the help chat keep the published text until someone publishes it;
// Discard drops the change.
func TestSavingAPublishedTranslationKeepsTheHelpCenterOnThePublishedText(t *testing.T) {
	f := newLanguagesFixture(t)
	workspace := workspaceID(t, f.api, f.admin)
	id := f.translated.ID

	var saved kbDraftedTranslation
	kbCall(t, f.api, http.MethodPatch, kbTranslationPath(id, "fr"), map[string]any{"updated_at": f.frSeen,
		"title": "Payer avec une carte", "slug": "payer-avec-une-carte", "body_md": "Toute carte convient : mélèze."}, f.admin, http.StatusOK, &saved)
	if saved.Title != "Payer par carte" || !strings.HasPrefix(saved.BodyMD, "Toute carte convient : sapinette.") || saved.Status != "published" || saved.UpdatedAt == f.frSeen {
		t.Fatalf("the live translation changed, or the updated_at to send back did not move: %+v", saved.kbTranslationView)
	}
	if !saved.HasDraft || saved.Draft == nil || saved.Draft.Title != "Payer avec une carte" || saved.Draft.Slug != "payer-avec-une-carte" ||
		saved.Draft.BodyMD != "Toute carte convient : mélèze." || saved.Draft.SEOTitle != "Paiement par carte" || saved.Draft.UpdatedBy == nil {
		t.Fatalf("draft = %+v", saved.Draft)
	}
	if got := kbTranslationVersionKinds(t, f.api, f.admin, id, "fr"); got != "draft,create" {
		t.Errorf("versions = %s, want draft, create", got)
	}
	if got := kbGetTranslation(t, f.api, f.admin, id, "fr"); !got.HasDraft || got.Draft == nil || got.Draft.BodyMD != "Toute carte convient : mélèze." || got.UpdatedAt != saved.UpdatedAt {
		t.Fatalf("GET = %+v draft=%+v", got.kbTranslationView, got.Draft)
	}

	page := f.base + "/fr/a/1-payer-par-carte"
	unchanged := func(step string) {
		t.Helper()
		for _, path := range []string{page, page + ".md", f.base + "/fr/search?q=sapinette", f.base + "/llms.txt", f.base + "/sitemap.xml"} {
			response := visit(f.site, path, "198.51.100.92")
			if response.Code != http.StatusOK {
				t.Fatalf("%s: %s status=%d", step, path, response.Code)
			}
			body := response.Body.String()
			if strings.Contains(body, "mélèze") || strings.Contains(body, "Payer avec une carte") || strings.Contains(body, "payer-avec-une-carte") {
				t.Errorf("%s: %s shows the draft:\n%s", step, path, body)
			}
		}
		if body := visit(f.site, f.base+"/fr/search?q="+url.QueryEscape("mélèze"), "198.51.100.92").Body.String(); strings.Contains(body, "Payer par carte") {
			t.Errorf("%s: a search for the draft's words found the translation", step)
		}
		buildAIRetrieval(t, f.pool)
		for _, doc := range aiRetrieve(t, f.pool, workspace, store.AIRetrievalQuery{Text: "carte mélèze", KBLimit: 5}) {
			if strings.Contains(doc.Text, "mélèze") || strings.Contains(doc.Title, "Payer avec") {
				t.Errorf("%s: AI reads the draft: %+v", step, doc)
			}
		}
		var chat []store.ChatKBArticle
		if err := database.New(f.pool).InWorkspace(context.Background(), workspace, func(tx pgx.Tx) error {
			var err error
			chat, err = store.ChatKBArticlesIn(context.Background(), tx, workspace, true, "fr")
			return err
		}); err != nil {
			t.Fatalf("%s: chat articles: %v", step, err)
		}
		read := false
		for _, article := range chat {
			if article.ID != id {
				continue
			}
			read = true
			if article.Locale != "fr" || article.Title != "Payer par carte" || !strings.HasPrefix(article.Body, "Toute carte convient : sapinette.") {
				t.Errorf("%s: the French help chat reads %+v, want the published translation", step, article)
			}
		}
		if !read {
			t.Errorf("%s: the French help chat does not read the article: %+v", step, chat)
		}
	}
	unchanged("after a draft save")

	// A second save changes the same draft and needs the token the first save answered.
	stale := request(t, f.api, http.MethodPatch, kbTranslationPath(id, "fr"), map[string]any{"updated_at": f.frSeen, "body_md": "Perdu"}, f.admin, nil)
	if stale.Code != http.StatusConflict || errorCode(t, stale) != "changed_elsewhere" {
		t.Fatalf("a save on the live updated_at status=%d body=%s, want 409 changed_elsewhere", stale.Code, stale.Body.String())
	}
	kbCall(t, f.api, http.MethodPatch, kbTranslationPath(id, "fr"), map[string]any{"updated_at": saved.UpdatedAt, "body_md": "Toute carte convient : mélèze rouge."}, f.admin, http.StatusOK, &saved)
	if saved.Draft == nil || saved.Draft.Title != "Payer avec une carte" || saved.Draft.BodyMD != "Toute carte convient : mélèze rouge." {
		t.Fatalf("second draft save = %+v", saved.Draft)
	}
	unchanged("after a second draft save")

	var published kbDraftedTranslation
	kbCall(t, f.api, http.MethodPost, kbTranslationPath(id, "fr", "/draft/publish"), map[string]any{"updated_at": saved.UpdatedAt}, f.admin, http.StatusOK, &published)
	if published.HasDraft || published.Draft != nil || published.Title != "Payer avec une carte" || published.Slug != "payer-avec-une-carte" ||
		published.BodyMD != "Toute carte convient : mélèze rouge." || published.Status != "published" || published.LastChange == nil || published.LastChange.Kind != "edit" {
		t.Fatalf("published = %+v draft=%+v", published.kbTranslationView, published.Draft)
	}
	if got := kbTranslationVersionKinds(t, f.api, f.admin, id, "fr"); got != "edit,draft,draft,create" {
		t.Errorf("versions = %s, want edit, draft, draft, create", got)
	}
	wantPage(t, visit(f.site, f.base+"/fr/a/1-payer-avec-une-carte", "198.51.100.92"), http.StatusOK, []string{"Payer avec une carte", "mélèze rouge"}, []string{"sapinette"})
	if none := request(t, f.api, http.MethodPost, kbTranslationPath(id, "fr", "/draft/publish"), map[string]any{"updated_at": published.UpdatedAt}, f.admin, nil); none.Code != http.StatusNotFound {
		t.Fatalf("publish without a draft status=%d body=%s, want 404", none.Code, none.Body.String())
	}

	// Discard drops a draft and leaves the live text and its history alone.
	kbCall(t, f.api, http.MethodPatch, kbTranslationPath(id, "fr"), map[string]any{"updated_at": published.UpdatedAt, "body_md": "Jamais publié."}, f.admin, http.StatusOK, &saved)
	var discarded kbDraftedTranslation
	kbCall(t, f.api, http.MethodDelete, kbTranslationPath(id, "fr", "/draft?updated_at="+url.QueryEscape(saved.UpdatedAt)), nil, f.admin, http.StatusOK, &discarded)
	if discarded.HasDraft || discarded.Draft != nil || discarded.BodyMD != "Toute carte convient : mélèze rouge." {
		t.Fatalf("discarded = %+v draft=%+v", discarded.kbTranslationView, discarded.Draft)
	}
	if got := kbTranslationVersionKinds(t, f.api, f.admin, id, "fr"); got != "draft,edit,draft,draft,create" {
		t.Errorf("versions after discard = %s, want draft, edit, draft, draft, create", got)
	}
}

// Restoring an older version of a published translation puts that text in its draft; the trash keeps
// the draft; unpublishing takes the draft's text into the translation, so no change is lost.
func TestARestoreOfAPublishedTranslationWaitsInItsDraftUntilUnpublished(t *testing.T) {
	f := newLanguagesFixture(t)
	id := f.translated.ID
	var live kbDraftedTranslation
	kbPublishTranslationChange(t, f.api, f.admin, id, "fr", map[string]any{"updated_at": f.frSeen, "body_md": "Toute carte convient : mélèze."}, &live)

	var restored kbDraftedTranslation
	response := postWithKey(t, f.api, f.admin, kbTranslationPath(id, "fr", "/versions/1/restore"), "restore-into-draft", map[string]any{"updated_at": live.UpdatedAt})
	if response.Code != http.StatusOK {
		t.Fatalf("restore status=%d body=%s", response.Code, response.Body.String())
	}
	decode(t, response, &restored)
	if restored.BodyMD != "Toute carte convient : mélèze." || restored.Status != "published" || !restored.HasDraft || restored.Draft == nil ||
		!strings.HasPrefix(restored.Draft.BodyMD, "Toute carte convient : sapinette.") || restored.UpdatedAt == live.UpdatedAt {
		t.Fatalf("restored = %+v draft=%+v", restored.kbTranslationView, restored.Draft)
	}
	if got := kbTranslationVersionKinds(t, f.api, f.admin, id, "fr"); got != "draft,edit,draft,create" {
		t.Errorf("versions = %s, want draft, edit, draft, create", got)
	}
	wantPage(t, visit(f.site, f.base+"/fr/a/1-payer-par-carte", "198.51.100.93"), http.StatusOK, []string{"mélèze"}, []string{"sapinette"})

	kbCall(t, f.api, http.MethodDelete, kbTranslationPath(id, "fr", "?updated_at="+url.QueryEscape(restored.UpdatedAt)), nil, f.admin, http.StatusNoContent, nil)
	trashed := kbGetTranslation(t, f.api, f.admin, id, "fr")
	var back kbDraftedTranslation
	response = postWithKey(t, f.api, f.admin, kbTranslationPath(id, "fr", "/restore"), "undelete-with-draft", map[string]any{"updated_at": trashed.UpdatedAt})
	if response.Code != http.StatusOK {
		t.Fatalf("undelete status=%d body=%s", response.Code, response.Body.String())
	}
	decode(t, response, &back)
	if back.InTrash || !back.HasDraft || back.Draft == nil || !strings.HasPrefix(back.Draft.BodyMD, "Toute carte convient : sapinette.") || back.BodyMD != "Toute carte convient : mélèze." {
		t.Fatalf("out of the trash = %+v draft=%+v", back.kbTranslationView, back.Draft)
	}

	var unpublished kbDraftedTranslation
	kbCall(t, f.api, http.MethodPatch, kbTranslationPath(id, "fr"), map[string]any{"updated_at": back.UpdatedAt, "status": "draft"}, f.admin, http.StatusOK, &unpublished)
	if unpublished.Status != "draft" || unpublished.HasDraft || unpublished.Draft != nil || !strings.HasPrefix(unpublished.BodyMD, "Toute carte convient : sapinette.") {
		t.Fatalf("unpublished = %+v draft=%+v", unpublished.kbTranslationView, unpublished.Draft)
	}
	if page := visit(f.site, f.base+"/fr/a/1-payer-par-carte", "198.51.100.93"); page.Code == http.StatusOK && strings.Contains(page.Body.String(), "sapinette") {
		t.Errorf("the unpublished translation is on the help center:\n%s", page.Body.String())
	}
}
