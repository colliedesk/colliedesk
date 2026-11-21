package httpapi_test

import (
	"net/http"
	"testing"
)

// A replacement reaches the unpublished changes of a published translation too, so publishing them
// later never brings the replaced item back, and the undo puts the item back there. The translation's
// published text, which does not show the item, is left alone, and since no live page shows it a
// teammate who may not publish can do both.
func TestReplacingAMediaItemAlsoReplacesItInATranslationsUnpublishedChanges(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "KB Translation Draft Replace", "admin@kb-translation-draft-replace.example.com")
	collection := kbCollection(t, api, admin, map[string]any{"name": "Docs"})
	from := uploadKBMedia(t, api, admin, pngImage(175), nil)
	to := uploadKBMedia(t, api, admin, pngImage(176), nil)
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Returns", "status": "published", "body_md": "Plain text"})
	kbCall(t, api, http.MethodPut, "/api/v1/help-center/languages", map[string]any{"languages": []map[string]any{{"locale": "fr", "visible": true}}}, admin, http.StatusOK, nil)
	var live kbTranslationWithFiles
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles/"+article.ID+"/translations", map[string]any{"locale": "fr", "title": "Retours", "body_md": "Texte publié", "status": "published"},
		admin, http.StatusCreated, &live)
	kbCall(t, api, http.MethodPatch, kbTranslationPath(article.ID, "fr"), map[string]any{"updated_at": live.UpdatedAt, "body_md": "Brouillon ![a](" + from.URL + ")"}, admin, http.StatusOK, nil)
	editor := customToken(t, api, admin, "Editor", "kb_articles:read", "kb_articles:update", "kb_media:read", "kb_media:update")

	var done kbReplacementView
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media/"+from.ID+"/replace", map[string]any{"with_media_id": to.ID}, editor, http.StatusOK, &done)
	got := kbGetTranslation(t, api, admin, article.ID, "fr")
	if got.BodyMD != "Texte publié" || got.Draft == nil || got.Draft.BodyMD != "Brouillon ![a]("+to.URL+")" {
		t.Fatalf("after the replacement: published %q, draft %+v; want the draft on the new item", got.BodyMD, got.Draft)
	}

	kbCall(t, api, http.MethodPost, "/api/v1/kb/media-replacements/"+done.ID+"/undo", nil, editor, http.StatusOK, nil)
	got = kbGetTranslation(t, api, admin, article.ID, "fr")
	if got.BodyMD != "Texte publié" || got.Draft == nil || got.Draft.BodyMD != "Brouillon ![a]("+from.URL+")" {
		t.Fatalf("after the undo: published %q, draft %+v; want the draft on the old item", got.BodyMD, got.Draft)
	}
}
