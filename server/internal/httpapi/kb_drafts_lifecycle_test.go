package httpapi_test

import (
	"net/http"
	"net/url"
	"testing"
)

// kbDraftedFixture is a published article "Refunds" ("Within 14 days.") with a saved draft
// "Refunds policy" ("Within 30 days.").
func kbDraftedFixture(t *testing.T, api http.Handler, token string) (kbDraftedArticle, kbDraftedArticle) {
	t.Helper()
	collection := kbCollection(t, api, token, map[string]any{"name": "Billing"})
	live := kbArticle(t, api, token, map[string]any{"collection_id": collection.ID, "title": "Refunds", "body_md": "Within 14 days.", "status": "published"})
	var saved kbDraftedArticle
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+live.ID, map[string]any{"updated_at": live.UpdatedAt,
		"title": "Refunds policy", "body_md": "Within 30 days."}, token, http.StatusOK, &saved)
	return kbDraftedArticle{kbArticleView: live}, saved
}

// Discarding the draft deletes only the draft: the live article, its updated_at and its versions,
// the draft save among them, stay as they were.
func TestDiscardingADraftLeavesThePublishedArticleAndItsVersions(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "KB Discard", "admin@kb-discard.example.com")
	live, saved := kbDraftedFixture(t, api, admin)

	stale := request(t, api, http.MethodDelete, "/api/v1/kb/articles/"+live.ID+"/draft?updated_at="+url.QueryEscape(live.UpdatedAt), nil, admin, nil)
	if stale.Code != http.StatusConflict || errorCode(t, stale) != "changed_elsewhere" {
		t.Fatalf("discard on the live updated_at status=%d body=%s, want 409 changed_elsewhere", stale.Code, stale.Body.String())
	}
	var discarded kbDraftedArticle
	kbCall(t, api, http.MethodDelete, "/api/v1/kb/articles/"+live.ID+"/draft?updated_at="+url.QueryEscape(saved.Draft.UpdatedAt), nil, admin, http.StatusOK, &discarded)
	if discarded.HasDraft || discarded.Draft != nil || discarded.Title != "Refunds" || discarded.BodyMD != "Within 14 days." ||
		discarded.UpdatedAt != live.UpdatedAt || discarded.Status != "published" {
		t.Fatalf("after discard = %+v draft=%+v", discarded.kbArticleView, discarded.Draft)
	}
	if got := kbVersionKinds(t, api, admin, live.ID); got != "draft,create" {
		t.Errorf("versions = %s, want draft, create", got)
	}
	again := request(t, api, http.MethodDelete, "/api/v1/kb/articles/"+live.ID+"/draft?updated_at="+url.QueryEscape(live.UpdatedAt), nil, admin, nil)
	if again.Code != http.StatusNotFound || errorCode(t, again) != "not_found" {
		t.Fatalf("second discard status=%d body=%s, want 404 not_found", again.Code, again.Body.String())
	}
}

// The trash takes the draft along with the article, and taking the article out brings the draft back.
func TestTrashingAnArticleKeepsItsDraftForUndelete(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "KB Draft Trash", "admin@kb-draft-trash.example.com")
	live, saved := kbDraftedFixture(t, api, admin)

	kbCall(t, api, http.MethodDelete, "/api/v1/kb/articles/"+live.ID+"?updated_at="+url.QueryEscape(saved.Draft.UpdatedAt), nil, admin, http.StatusNoContent, nil)
	var trash struct {
		Items []kbTrashItem `json:"items"`
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles/trash", nil, admin, http.StatusOK, &trash)
	if len(trash.Items) != 1 || trash.Items[0].ID != live.ID {
		t.Fatalf("trash = %+v", trash.Items)
	}
	var back kbDraftedArticle
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles/"+live.ID+"/restore", map[string]any{"updated_at": trash.Items[0].UpdatedAt}, admin, http.StatusOK, &back)
	if back.DeletedAt != nil || back.BodyMD != "Within 14 days." || !back.HasDraft || back.Draft == nil ||
		back.Draft.Title != "Refunds policy" || back.Draft.BodyMD != "Within 30 days." {
		t.Fatalf("back from the trash = %+v draft=%+v", back.kbArticleView, back.Draft)
	}
}

// Unpublishing an article with a draft makes the draft its text: nothing is lost, and there is no
// draft of an unpublished article.
func TestUnpublishingFoldsTheDraftIntoTheArticle(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "KB Unpublish", "admin@kb-unpublish.example.com")
	live, saved := kbDraftedFixture(t, api, admin)

	var unpublished kbDraftedArticle
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+live.ID, map[string]any{"updated_at": saved.Draft.UpdatedAt, "status": "draft"}, admin, http.StatusOK, &unpublished)
	if unpublished.Status != "draft" || unpublished.HasDraft || unpublished.Draft != nil || unpublished.Title != "Refunds policy" || unpublished.BodyMD != "Within 30 days." {
		t.Fatalf("unpublished = %+v draft=%+v", unpublished.kbArticleView, unpublished.Draft)
	}
	if got := kbVersionKinds(t, api, admin, live.ID); got != "edit,draft,create" {
		t.Errorf("versions = %s, want edit, draft, create", got)
	}
	// An unpublished article is saved as it is, with no draft.
	var edited kbDraftedArticle
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+live.ID, map[string]any{"updated_at": unpublished.UpdatedAt, "body_md": "Within 60 days."}, admin, http.StatusOK, &edited)
	if edited.HasDraft || edited.BodyMD != "Within 60 days." {
		t.Fatalf("edit of an unpublished article = %+v draft=%+v", edited.kbArticleView, edited.Draft)
	}
}

// Restoring an old version of a published article puts its text in the draft; the live text stays
// until someone publishes it.
func TestRestoringAVersionOfAPublishedArticleWritesTheDraft(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "KB Draft Restore", "admin@kb-draft-restore.example.com")
	live, saved := kbDraftedFixture(t, api, admin)
	var published kbDraftedArticle
	kbPublishDraft(t, api, admin, live.ID, saved.Draft.UpdatedAt, &published)

	var restored kbDraftedArticle
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles/"+live.ID+"/versions/1/restore", map[string]any{"updated_at": published.UpdatedAt}, admin, http.StatusOK, &restored)
	if restored.Title != "Refunds policy" || restored.BodyMD != "Within 30 days." || restored.UpdatedAt != published.UpdatedAt ||
		restored.Draft == nil || restored.Draft.Title != "Refunds" || restored.Draft.BodyMD != "Within 14 days." {
		t.Fatalf("restored = %+v draft=%+v", restored.kbArticleView, restored.Draft)
	}
	if got := kbVersionKinds(t, api, admin, live.ID); got != "draft,edit,draft,create" {
		t.Errorf("versions = %s, want draft, edit, draft, create", got)
	}
}
