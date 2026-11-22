package httpapi_test

import (
	"net/http"
	"slices"
	"testing"
)

// In a collection that needs approval, a tag change is a change of the reviewed text: tags approved
// and then changed again are refused on publish, and the approved tags publish.
func TestATagChangeNeedsApprovalLikeATextChange(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "KB Tag Reviews", "admin@kb-tag-reviews.example.com")
	reviewer := agentSession(t, api, admin, "reviewer@kb-tag-reviews.example.com")
	reviewerID := agentID(t, api, reviewer)
	collection := kbApprovalCollection(t, api, admin)

	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Refunds", "body_md": "Within 14 days."})
	kbDecide(t, api, reviewer, kbRequestReview(t, api, admin, article.ID, reviewerID).ID, "approve", map[string]any{}, http.StatusOK)
	var published kbTaggedArticle
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+article.ID, map[string]any{"updated_at": kbGetArticle(t, api, admin, article.ID).UpdatedAt, "status": "published"},
		admin, http.StatusOK, &published)

	var saved kbTaggedArticle
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+article.ID, map[string]any{"updated_at": published.UpdatedAt, "tags": []string{"billing"}}, admin, http.StatusOK, &saved)
	response := kbCall(t, api, http.MethodPost, "/api/v1/kb/articles/"+article.ID+"/draft/publish", map[string]any{"updated_at": saved.UpdatedAt}, admin, http.StatusConflict, nil)
	if code := errorCode(t, response); code != "approval_required" {
		t.Fatalf("publishing an unapproved tag change: code=%s", code)
	}

	kbDecide(t, api, reviewer, kbRequestReview(t, api, admin, article.ID, reviewerID).ID, "approve", map[string]any{}, http.StatusOK)
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+article.ID, map[string]any{"updated_at": saved.UpdatedAt, "tags": []string{"refunds"}}, admin, http.StatusOK, &saved)
	response = kbCall(t, api, http.MethodPost, "/api/v1/kb/articles/"+article.ID+"/draft/publish", map[string]any{"updated_at": saved.UpdatedAt}, admin, http.StatusConflict, nil)
	if code := errorCode(t, response); code != "approval_required" {
		t.Fatalf("publishing tags changed after their approval: code=%s", code)
	}

	kbDecide(t, api, reviewer, kbRequestReview(t, api, admin, article.ID, reviewerID).ID, "approve", map[string]any{}, http.StatusOK)
	var live kbTaggedArticle
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles/"+article.ID+"/draft/publish", map[string]any{"updated_at": saved.UpdatedAt}, admin, http.StatusOK, &live)
	if !slices.Equal(live.Tags, []string{"refunds"}) || live.HasDraft {
		t.Fatalf("published tags = %q has_draft=%v, want [refunds] and no draft", live.Tags, live.HasDraft)
	}
}
