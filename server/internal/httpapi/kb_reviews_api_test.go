package httpapi_test

import (
	"net/http"
	"testing"
)

type kbReviewView struct {
	ID          string  `json:"id"`
	ArticleID   string  `json:"article_id"`
	Locale      *string `json:"locale"`
	State       string  `json:"state"`
	Note        string  `json:"note"`
	ContentHash string  `json:"content_hash"`
	Reviewers   []struct {
		ID   string `json:"agent_id"`
		Name string `json:"name"`
	} `json:"reviewers"`
	RequestedBy *struct {
		ID   string `json:"agent_id"`
		Name string `json:"name"`
	} `json:"requested_by"`
	RequestedByToken *struct {
		Label string `json:"label"`
	} `json:"requested_by_token"`
	DecidedBy *struct {
		ID string `json:"agent_id"`
	} `json:"decided_by"`
	DecidedByToken *struct {
		Label string `json:"label"`
	} `json:"decided_by_token"`
	SupersedesReviewID *string `json:"supersedes_review_id"`
}

// kbApprovalCollection is a collection that needs an approved version before an article in it is
// published.
func kbApprovalCollection(t *testing.T, api http.Handler, admin string) kbCollectionView {
	t.Helper()
	collection := kbCollection(t, api, admin, map[string]any{"name": "Docs"})
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/collections/"+collection.ID, map[string]any{"updated_at": collection.UpdatedAt, "require_approval": "on"},
		admin, http.StatusOK, &collection)
	return collection
}

func kbRequestReview(t *testing.T, api http.Handler, token, articleID string, reviewers ...string) kbReviewView {
	t.Helper()
	var review kbReviewView
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles/"+articleID+"/reviews", map[string]any{"reviewer_ids": reviewers, "note": "Please check the refund window"},
		token, http.StatusCreated, &review)
	return review
}

func kbDecide(t *testing.T, api http.Handler, token, reviewID, decision string, body map[string]any, want int) kbReviewView {
	t.Helper()
	var review kbReviewView
	into := any(&review)
	if want != http.StatusOK {
		into = nil
	}
	kbCall(t, api, http.MethodPost, "/api/v1/kb/reviews/"+reviewID+"/"+decision, body, token, want, into)
	return review
}

// In a collection that needs approval, an article goes on the help center only once a teammate who may
// publish approved the exact text: before, publishing is refused; after, it publishes; an edit after
// the approval makes the text unapproved again.
func TestAnArticleThatNeedsApprovalPublishesOnlyTheApprovedText(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "KB Reviews", "admin@kb-reviews.example.com")
	reviewer := agentSession(t, api, admin, "reviewer@kb-reviews.example.com")
	reviewerID := agentID(t, api, reviewer)
	collection := kbApprovalCollection(t, api, admin)

	response := kbCall(t, api, http.MethodPost, "/api/v1/kb/articles", map[string]any{"collection_id": collection.ID, "title": "Refunds", "body_md": "Within 14 days.", "status": "published"},
		admin, http.StatusConflict, nil)
	if code := errorCode(t, response); code != "approval_required" {
		t.Fatalf("publishing on create: code=%s", code)
	}
	draft := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Refunds", "body_md": "Within 14 days."})
	response = kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+draft.ID, map[string]any{"updated_at": draft.UpdatedAt, "status": "published"}, admin, http.StatusConflict, nil)
	if code := errorCode(t, response); code != "approval_required" {
		t.Fatalf("publishing an unapproved draft: code=%s", code)
	}

	review := kbRequestReview(t, api, admin, draft.ID, reviewerID)
	if review.State != "open" || len(review.Reviewers) != 1 || review.Reviewers[0].ID != reviewerID || review.Note != "Please check the refund window" {
		t.Fatalf("review = %+v", review)
	}
	if approved := kbDecide(t, api, reviewer, review.ID, "approve", map[string]any{}, http.StatusOK); approved.State != "approved" || approved.DecidedBy == nil || approved.DecidedBy.ID != reviewerID {
		t.Fatalf("approved = %+v", approved)
	}
	var published kbDraftedArticle
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+draft.ID, map[string]any{"updated_at": kbGetArticle(t, api, admin, draft.ID).UpdatedAt, "status": "published"},
		admin, http.StatusOK, &published)
	if published.Status != "published" || published.BodyMD != "Within 14 days." {
		t.Fatalf("published = %+v", published.kbArticleView)
	}

	// The next change is a draft of the published article: approved, then edited again, it is refused.
	var saved kbDraftedArticle
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+draft.ID, map[string]any{"updated_at": published.UpdatedAt, "body_md": "Within 30 days."}, admin, http.StatusOK, &saved)
	second := kbRequestReview(t, api, admin, draft.ID, reviewerID)
	kbDecide(t, api, reviewer, second.ID, "approve", map[string]any{}, http.StatusOK)
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+draft.ID, map[string]any{"updated_at": saved.UpdatedAt, "body_md": "Within 45 days."}, admin, http.StatusOK, &saved)
	response = kbCall(t, api, http.MethodPost, "/api/v1/kb/articles/"+draft.ID+"/draft/publish", map[string]any{"updated_at": saved.UpdatedAt}, admin, http.StatusConflict, nil)
	if code := errorCode(t, response); code != "approval_required" {
		t.Fatalf("publishing a draft edited after its approval: code=%s", code)
	}
	if got := kbGetArticle(t, api, admin, draft.ID); got.BodyMD != "Within 14 days." {
		t.Fatalf("help center text after the refusal = %q", got.BodyMD)
	}
}
