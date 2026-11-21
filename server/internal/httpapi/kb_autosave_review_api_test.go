package httpapi_test

import (
	"net/http"
	"slices"
	"testing"
)

func kbAutosaveBody(t *testing.T, api http.Handler, token, articleID, body string) {
	t.Helper()
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+articleID, map[string]any{"updated_at": kbGetArticle(t, api, token, articleID).UpdatedAt,
		"autosave": true, "body_md": body}, token, http.StatusOK, nil)
}

// An autosave of a published article's draft changes the text in review as a Save does: the open review
// is superseded and reopened for the new text, an approval of the old text stops counting, and the
// history and the help center stay as they were.
func TestAnAutosaveInReviewSettlesTheReviewLikeASave(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "KB Autosave Review", "admin@kb-autosave-review.example.com")
	reviewer := agentSession(t, api, admin, "reviewer@kb-autosave-review.example.com")
	reviewerID := agentID(t, api, reviewer)
	collection := kbApprovalCollection(t, api, admin)
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Refunds", "body_md": "Within 14 days."})
	kbDecide(t, api, reviewer, kbRequestReview(t, api, admin, article.ID, reviewerID).ID, "approve", map[string]any{}, http.StatusOK)
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+article.ID, map[string]any{"updated_at": kbGetArticle(t, api, admin, article.ID).UpdatedAt, "status": "published"},
		admin, http.StatusOK, nil)

	kbEditBody(t, api, admin, article.ID, "Within 30 days.")
	sent := kbRequestReview(t, api, admin, article.ID, reviewerID)
	versions := kbVersionKinds(t, api, admin, article.ID)
	kbAutosaveBody(t, api, admin, article.ID, "Within 45 days.")
	reviews := kbArticleReviews(t, api, admin, article.ID)
	if len(reviews) < 2 || reviews[0].State != "open" || reviews[0].SupersedesReviewID == nil || *reviews[0].SupersedesReviewID != sent.ID ||
		reviews[1].ID != sent.ID || reviews[1].State != "superseded" || len(reviews[0].Reviewers) != 1 || reviews[0].Reviewers[0].ID != reviewerID {
		t.Fatalf("reviews after an autosave = %+v", reviews)
	}
	if got := kbVersionKinds(t, api, admin, article.ID); got != versions {
		t.Errorf("versions after reopening the review = %s, want %s", got, versions)
	}

	kbDecide(t, api, reviewer, reviews[0].ID, "approve", map[string]any{}, http.StatusOK)
	if got := kbArticleIDs(t, api, admin, "state=approved"); !slices.Equal(got, []string{article.ID}) {
		t.Fatalf("approved before the autosave = %v", got)
	}
	versions = kbVersionKinds(t, api, admin, article.ID)
	kbAutosaveBody(t, api, admin, article.ID, "Within 60 days.")
	if got := kbArticleIDs(t, api, admin, "state=approved"); len(got) != 0 {
		t.Fatalf("approved after an autosave of other text = %v", got)
	}
	if got := kbArticleReviews(t, api, admin, article.ID); len(got) != len(reviews) || got[0].State != "approved" {
		t.Fatalf("reviews after autosaving an approved text = %+v", got)
	}
	saved := kbGetArticle(t, api, admin, article.ID)
	response := kbCall(t, api, http.MethodPost, "/api/v1/kb/articles/"+article.ID+"/draft/publish", map[string]any{"updated_at": saved.UpdatedAt}, admin, http.StatusConflict, nil)
	if code := errorCode(t, response); code != "approval_required" {
		t.Fatalf("publishing an autosaved draft after its approval: code=%s", code)
	}
	if got := kbVersionKinds(t, api, admin, article.ID); got != versions {
		t.Errorf("versions after the autosaves = %s, want %s", got, versions)
	}
	if got := kbGetArticle(t, api, admin, article.ID); got.Status != "published" || got.BodyMD != "Within 14 days." {
		t.Fatalf("help center text after the autosaves = %s %q", got.Status, got.BodyMD)
	}
}

// An autosave of an article that was never published settles its review the same way, and leaves it a draft.
func TestAnAutosaveOfADraftInReviewSettlesTheReview(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "KB Autosave Draft Review", "admin@kb-autosave-draft-review.example.com")
	reviewer := agentSession(t, api, admin, "reviewer@kb-autosave-draft-review.example.com")
	reviewerID := agentID(t, api, reviewer)
	collection := kbApprovalCollection(t, api, admin)
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Shipping", "body_md": "Two days."})
	sent := kbRequestReview(t, api, admin, article.ID, reviewerID)

	kbAutosaveBody(t, api, admin, article.ID, "Three days.")
	reviews := kbArticleReviews(t, api, admin, article.ID)
	if len(reviews) != 2 || reviews[0].State != "open" || reviews[1].ID != sent.ID || reviews[1].State != "superseded" {
		t.Fatalf("reviews after an autosave = %+v", reviews)
	}
	kbDecide(t, api, reviewer, reviews[0].ID, "approve", map[string]any{}, http.StatusOK)
	versions := kbVersionKinds(t, api, admin, article.ID)
	kbAutosaveBody(t, api, admin, article.ID, "Four days.")
	if got := kbArticleIDs(t, api, admin, "state=approved"); len(got) != 0 {
		t.Fatalf("approved after an autosave of other text = %v", got)
	}
	if got := kbVersionKinds(t, api, admin, article.ID); got != versions {
		t.Errorf("versions after the autosave = %s, want %s", got, versions)
	}
	if got := kbGetArticle(t, api, admin, article.ID); got.Status != "draft" || got.BodyMD != "Four days." {
		t.Fatalf("article after the autosaves = %s %q", got.Status, got.BodyMD)
	}
}
