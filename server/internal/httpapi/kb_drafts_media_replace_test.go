package httpapi_test

import (
	"net/http"
	"testing"
)

// TestReplacingAMediaItemAlsoReplacesItInUnpublishedChanges: a replacement reaches the text an author
// has saved but not published, so publishing it later never brings the replaced item back, and the
// undo puts the item back in that text too. An article that names the item only in its unpublished
// changes is reached as well, and its published text is left alone.
func TestReplacingAMediaItemAlsoReplacesItInUnpublishedChanges(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "KB Draft Replace Text", "admin@kb-draft-replace-text.example.com")
	collection := kbCollection(t, api, admin, map[string]any{"name": "Docs"})
	from := uploadKBMedia(t, api, admin, pngImage(170), nil)
	to := uploadKBMedia(t, api, admin, pngImage(171), nil)
	both := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Both", "status": "published", "body_md": "Live ![a](" + from.URL + ")"})
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+both.ID, map[string]any{"updated_at": both.UpdatedAt, "body_md": "Draft ![a](" + from.URL + ") more"}, admin, http.StatusOK, nil)
	only := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Draft only", "status": "published", "body_md": "Plain text"})
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+only.ID, map[string]any{"updated_at": only.UpdatedAt, "body_md": "New ![b](" + from.URL + ")"}, admin, http.StatusOK, nil)

	var done kbReplacementView
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media/"+from.ID+"/replace", map[string]any{"with_media_id": to.ID}, admin, http.StatusOK, &done)
	got := kbGetArticle(t, api, admin, both.ID)
	if got.BodyMD != "Live ![a]("+to.URL+")" || got.Draft == nil || got.Draft.BodyMD != "Draft ![a]("+to.URL+") more" {
		t.Fatalf("after the replacement: published %q, draft %+v; want both to show the new item", got.BodyMD, got.Draft)
	}
	got = kbGetArticle(t, api, admin, only.ID)
	if got.BodyMD != "Plain text" || got.Draft == nil || got.Draft.BodyMD != "New ![b]("+to.URL+")" {
		t.Fatalf("draft-only article after the replacement: published %q, draft %+v", got.BodyMD, got.Draft)
	}

	kbCall(t, api, http.MethodPost, "/api/v1/kb/media-replacements/"+done.ID+"/undo", nil, admin, http.StatusOK, nil)
	got = kbGetArticle(t, api, admin, both.ID)
	if got.BodyMD != "Live ![a]("+from.URL+")" || got.Draft == nil || got.Draft.BodyMD != "Draft ![a]("+from.URL+") more" {
		t.Fatalf("after the undo: published %q, draft %+v; want both to show the old item", got.BodyMD, got.Draft)
	}
	got = kbGetArticle(t, api, admin, only.ID)
	if got.BodyMD != "Plain text" || got.Draft == nil || got.Draft.BodyMD != "New ![b]("+from.URL+")" {
		t.Fatalf("draft-only article after the undo: published %q, draft %+v", got.BodyMD, got.Draft)
	}
}

// An item that only the unpublished changes of a published article name is not on its live page, so
// replacing it needs no kb_articles:publish. One the live text shows still does.
func TestReplacingAnItemOnlyUnpublishedChangesNameNeedsNoPublish(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "KB Draft Replace Gate", "admin@kb-draft-replace-gate.example.com")
	collection := kbCollection(t, api, admin, map[string]any{"name": "Docs"})
	drafted := uploadKBMedia(t, api, admin, pngImage(172), nil)
	live := uploadKBMedia(t, api, admin, pngImage(173), nil)
	to := uploadKBMedia(t, api, admin, pngImage(174), nil)
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Gate", "status": "published", "body_md": "Live ![l](" + live.URL + ")"})
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+article.ID, map[string]any{"updated_at": article.UpdatedAt, "body_md": "Draft ![d](" + drafted.URL + ")"}, admin, http.StatusOK, nil)
	editor := customToken(t, api, admin, "Editor", "kb_articles:read", "kb_articles:update", "kb_media:read", "kb_media:update")

	kbCall(t, api, http.MethodPost, "/api/v1/kb/media/"+drafted.ID+"/replace", map[string]any{"with_media_id": to.ID}, editor, http.StatusOK, nil)
	got := kbGetArticle(t, api, admin, article.ID)
	if got.BodyMD != "Live ![l]("+live.URL+")" || got.Draft == nil || got.Draft.BodyMD != "Draft ![d]("+to.URL+")" {
		t.Fatalf("published %q, draft %+v; want the live text alone and the draft on the new item", got.BodyMD, got.Draft)
	}
	refused(t, api, http.MethodPost, "/api/v1/kb/media/"+live.ID+"/replace", map[string]any{"with_media_id": to.ID}, editor, "kb_articles:publish")
}
