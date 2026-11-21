package httpapi_test

import (
	"net/http"
	"net/url"
	"testing"
)

// Out of the trash comes the text that was published before, so an undelete needs no new approval;
// while it is in the trash the text cannot be changed, so nothing unreviewed comes back with it.
func TestUndeletingPublishedTextNeedsNoNewApprovalAndTrashedTextCannotChange(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "KB Review Trash", "admin@kb-review-trash.example.com")
	collection := kbCollection(t, api, admin, map[string]any{"name": "Docs"})
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Refunds", "body_md": "Within 14 days.", "status": "published"})
	kbCall(t, api, http.MethodPut, "/api/v1/help-center/languages", map[string]any{"languages": []map[string]any{{"locale": "fr", "visible": true}}}, admin, http.StatusOK, nil)
	var translation kbTranslationView
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles/"+article.ID+"/translations", map[string]any{"locale": "fr", "title": "Remboursements", "body_md": "Sous 14 jours.", "status": "published"},
		admin, http.StatusCreated, &translation)
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/collections/"+collection.ID, map[string]any{"updated_at": collection.UpdatedAt, "require_approval": "on"}, admin, http.StatusOK, nil)

	kbCall(t, api, http.MethodDelete, kbTranslationPath(article.ID, "fr", "?updated_at="+url.QueryEscape(translation.UpdatedAt)), nil, admin, http.StatusNoContent, nil)
	var trashed kbTranslationView
	kbCall(t, api, http.MethodGet, kbTranslationPath(article.ID, "fr"), nil, admin, http.StatusOK, &trashed)
	for _, call := range []struct {
		name, method, path string
		body               any
	}{
		{"edit", http.MethodPatch, kbTranslationPath(article.ID, "fr"), map[string]any{"updated_at": trashed.UpdatedAt, "body_md": "Sous 90 jours."}},
		{"restore a version", http.MethodPost, kbTranslationPath(article.ID, "fr", "/versions/1/restore"), map[string]any{"updated_at": trashed.UpdatedAt}},
	} {
		if response := request(t, api, call.method, call.path, call.body, admin, nil); response.Code == http.StatusOK {
			t.Fatalf("%s of a trashed translation succeeded: %s", call.name, response.Body.String())
		}
	}
	var restored kbTranslationView
	kbCall(t, api, http.MethodPost, kbTranslationPath(article.ID, "fr", "/restore"), map[string]any{"updated_at": trashed.UpdatedAt}, admin, http.StatusOK, &restored)
	if restored.InTrash || restored.Status != "published" || restored.BodyMD != "Sous 14 jours." {
		t.Fatalf("undeleted translation = %+v", restored)
	}

	live := kbGetArticle(t, api, admin, article.ID)
	kbCall(t, api, http.MethodDelete, "/api/v1/kb/articles/"+article.ID+"?updated_at="+url.QueryEscape(live.UpdatedAt), nil, admin, http.StatusNoContent, nil)
	gone := kbGetArticle(t, api, admin, article.ID)
	if response := request(t, api, http.MethodPatch, "/api/v1/kb/articles/"+article.ID, map[string]any{"updated_at": gone.UpdatedAt, "body_md": "Within 90 days."}, admin, nil); response.Code == http.StatusOK {
		t.Fatalf("an edit of a trashed article succeeded: %s", response.Body.String())
	}
	var back kbArticleView
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles/"+article.ID+"/restore", map[string]any{"updated_at": gone.UpdatedAt}, admin, http.StatusOK, &back)
	if back.Status != "published" || back.BodyMD != "Within 14 days." {
		t.Fatalf("undeleted article = %+v", back)
	}
}
