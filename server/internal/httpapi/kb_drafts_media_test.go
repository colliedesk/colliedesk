package httpapi_test

import (
	"net/http"
	"testing"
)

// TestUndoingAMediaReplacementPutsBackThePublishedTextNotTheDraft: unpublished changes saved before
// and after a replacement never become the published text when it is undone, and they do not stop the
// undo either, since they did not change the published text.
func TestUndoingAMediaReplacementPutsBackThePublishedTextNotTheDraft(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "KB Draft Replace", "admin@kb-draft-replace.example.com")
	collection := kbCollection(t, api, admin, map[string]any{"name": "Docs"})
	from := uploadKBMedia(t, api, admin, pngImage(160), nil)
	to := uploadKBMedia(t, api, admin, pngImage(161), nil)
	published := "Before ![a](" + from.URL + ") after"
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Shows it", "status": "published", "body_md": published})
	var saved kbDraftedArticle
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+article.ID, map[string]any{"updated_at": article.UpdatedAt, "body_md": "Draft text"}, admin, http.StatusOK, &saved)

	var done kbReplacementView
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media/"+from.ID+"/replace", map[string]any{"with_media_id": to.ID}, admin, http.StatusOK, &done)
	if len(done.ArticleIDs) != 1 || done.ArticleIDs[0] != article.ID {
		t.Fatalf("replacement = %+v", done)
	}
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+article.ID, map[string]any{"updated_at": kbSeen(saved), "body_md": "Draft text, edited"}, admin, http.StatusOK, nil)

	var undone struct {
		Skipped []string `json:"skipped_article_ids"`
	}
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media-replacements/"+done.ID+"/undo", nil, admin, http.StatusOK, &undone)
	if len(undone.Skipped) != 0 {
		t.Fatalf("skipped = %v, want none: only the draft changed since", undone.Skipped)
	}
	after := kbGetArticle(t, api, admin, article.ID)
	if after.BodyMD != published {
		t.Fatalf("published text after undo = %q, want %q", after.BodyMD, published)
	}
	if after.Draft == nil || after.Draft.BodyMD != "Draft text, edited" {
		t.Fatalf("draft after undo = %+v, want it unchanged", after.Draft)
	}
}
