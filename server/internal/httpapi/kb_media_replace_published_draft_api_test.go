package httpapi_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// An edited copy of an image in a published article goes into its unpublished changes and those of
// its published translations: the help center keeps the image until someone publishes them, so a
// teammate who may update articles but not publish them can do it. The image stays as the earlier
// version, which a purge refuses, naming the article.
func TestAnnotatingAPublishedArticleChangesItsDraftUntilPublished(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "KB Annotate Draft", "admin@kb-annotate-draft.example.com")
	enableHelpCenter(t, api, admin, "Annotate Draft", true)
	base := "/help/" + workspaceSlug(t, api, admin)
	collection := kbCollection(t, api, admin, map[string]any{"name": "Docs"})
	original := uploadKBMedia(t, api, admin, pngImage(180), nil)
	annotated := uploadKBMedia(t, api, admin, pngImage(181), nil)
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Returns", "status": "published", "body_md": "See ![a](" + original.URL + ")"})
	kbCall(t, api, http.MethodPut, "/api/v1/help-center/languages", map[string]any{"languages": []map[string]any{{"locale": "fr", "visible": true}}}, admin, http.StatusOK, nil)
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles/"+article.ID+"/translations", map[string]any{"locale": "fr", "title": "Retours",
		"body_md": "Voir ![a](" + original.URL + ")", "status": "published"}, admin, http.StatusCreated, nil)
	pages := []string{fmt.Sprintf("%s/a/%d-%s", base, article.Number, article.Slug), fmt.Sprintf("%s/fr/a/%d-retours", base, article.Number)}
	for _, page := range pages {
		wantPage(t, visit(site, page, "198.51.100.61"), http.StatusOK, []string{original.ID}, []string{annotated.ID})
	}

	editor := customToken(t, api, admin, "Editor", "kb_articles:read", "kb_articles:update", "kb_media:read", "kb_media:update")
	var done kbReplacementView
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media/"+original.ID+"/replace", map[string]any{"with_media_id": annotated.ID, "article_id": article.ID}, editor, http.StatusOK, &done)
	if len(done.ArticleIDs) != 1 || done.ArticleIDs[0] != article.ID || len(done.Translations) != 0 {
		t.Fatalf("replacement = %+v, want the article and no published translation", done)
	}
	got := kbGetArticle(t, api, admin, article.ID)
	if got.BodyMD != "See ![a]("+original.URL+")" || got.Draft == nil || got.Draft.BodyMD != "See ![a]("+annotated.URL+")" {
		t.Fatalf("article: published %q, draft %+v; want the copy in the draft only", got.BodyMD, got.Draft)
	}
	translation := kbGetTranslation(t, api, admin, article.ID, "fr")
	if translation.BodyMD != "Voir ![a]("+original.URL+")" || translation.Draft == nil || translation.Draft.BodyMD != "Voir ![a]("+annotated.URL+")" {
		t.Fatalf("translation: published %q, draft %+v; want the copy in the draft only", translation.BodyMD, translation.Draft)
	}
	for _, page := range pages {
		wantPage(t, visit(site, page, "198.51.100.61"), http.StatusOK, []string{original.ID}, []string{annotated.ID})
	}

	kbPublishDraft(t, api, admin, article.ID, got.Draft.UpdatedAt, nil)
	kbCall(t, api, http.MethodPost, kbTranslationPath(article.ID, "fr")+"/draft/publish", map[string]any{"updated_at": translation.Draft.UpdatedAt}, admin, http.StatusOK, nil)
	for _, page := range pages {
		wantPage(t, visit(site, page, "198.51.100.61"), http.StatusOK, []string{annotated.ID}, []string{original.ID})
	}

	kbCall(t, api, http.MethodDelete, "/api/v1/kb/media/"+original.ID, nil, admin, http.StatusOK, nil)
	var kept kbMediaKeptView
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media/purge", map[string]any{"ids": []string{original.ID}}, admin, http.StatusConflict, &kept)
	if kept.Error.Code != "media_kept" || !strings.Contains(kept.Error.Message, `"Returns"`) || len(kept.Error.KeptBy) != 1 ||
		len(kept.Error.KeptBy[0].Articles) != 1 || kept.Error.KeptBy[0].Articles[0].ID != article.ID {
		t.Fatalf("purge refusal = %+v", kept.Error)
	}
}

// Undoing the edited copy of a published article puts the image back in its unpublished changes, so
// the teammate who made it without kb_articles:publish undoes it too; the published text never changed.
func TestATeammateWhoCannotPublishUndoesAnAnnotationOfAPublishedArticle(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "KB Annotate Draft Undo", "admin@kb-annotate-draft-undo.example.com")
	collection := kbCollection(t, api, admin, map[string]any{"name": "Docs"})
	original := uploadKBMedia(t, api, admin, pngImage(182), nil)
	annotated := uploadKBMedia(t, api, admin, pngImage(183), nil)
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Billing", "status": "published", "body_md": "See ![a](" + original.URL + ")"})
	editor := customToken(t, api, admin, "Editor", "kb_articles:read", "kb_articles:update", "kb_media:read", "kb_media:update")
	var done, undone kbReplacementView
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media/"+original.ID+"/replace", map[string]any{"with_media_id": annotated.ID, "article_id": article.ID}, editor, http.StatusOK, &done)
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media-replacements/"+done.ID+"/undo", nil, editor, http.StatusOK, &undone)
	got := kbGetArticle(t, api, admin, article.ID)
	if undone.UndoneAt == nil || got.BodyMD != "See ![a]("+original.URL+")" || got.Draft == nil || got.Draft.BodyMD != "See ![a]("+original.URL+")" {
		t.Fatalf("after undo: undone %v, published %q, draft %+v", undone.UndoneAt, got.BodyMD, got.Draft)
	}
}

// Replacing in one article rewrites that article's text, so a token given kb_media:update but not
// kb_articles:update is refused, naming the permission, and nothing changes.
func TestReplacingInOneArticleNeedsKBArticlesUpdate(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "KB Replace Scope", "admin@kb-replace-scope.example.com")
	collection := kbCollection(t, api, admin, map[string]any{"name": "Docs"})
	original := uploadKBMedia(t, api, admin, pngImage(184), nil)
	annotated := uploadKBMedia(t, api, admin, pngImage(185), nil)
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Draft", "body_md": "See ![a](" + original.URL + ")"})
	media := customToken(t, api, admin, "Media", "kb_articles:read", "kb_media:read", "kb_media:update")
	refused(t, api, http.MethodPost, "/api/v1/kb/media/"+original.ID+"/replace", map[string]any{"with_media_id": annotated.ID, "article_id": article.ID}, media, "kb_articles:update")
	if got := kbGetArticle(t, api, admin, article.ID); got.BodyMD != "See ![a]("+original.URL+")" {
		t.Fatalf("refused call changed the article: %q", got.BodyMD)
	}
}
