package httpapi_test

import (
	"net/http"
	"net/url"
	"testing"
)

func kbArticleReviews(t *testing.T, api http.Handler, token, articleID string) []kbReviewView {
	t.Helper()
	var list struct {
		Reviews []kbReviewView `json:"reviews"`
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles/"+articleID+"/reviews", nil, token, http.StatusOK, &list)
	return list.Reviews
}

func kbEditBody(t *testing.T, api http.Handler, token, articleID, body string) {
	t.Helper()
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+articleID, map[string]any{"updated_at": kbGetArticle(t, api, token, articleID).UpdatedAt, "body_md": body},
		token, http.StatusOK, nil)
}

// An edit while a text is in review supersedes the review and asks the same people about the new text;
// a reviewer who edits it is no longer one of them, and can never be named again for that text.
func TestAnEditInReviewReopensTheReviewWithoutTheReviewerWhoEdited(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "KB Review Edits", "admin@kb-review-edits.example.com")
	first := agentSession(t, api, admin, "first@kb-review-edits.example.com")
	second := agentSession(t, api, admin, "second@kb-review-edits.example.com")
	firstID, secondID := agentID(t, api, first), agentID(t, api, second)
	collection := kbApprovalCollection(t, api, admin)
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Shipping", "body_md": "Two days."})

	sent := kbRequestReview(t, api, admin, article.ID, firstID, secondID)
	kbEditBody(t, api, admin, article.ID, "Three days.")
	reviews := kbArticleReviews(t, api, admin, article.ID)
	if len(reviews) != 2 || reviews[0].State != "open" || reviews[0].SupersedesReviewID == nil || *reviews[0].SupersedesReviewID != sent.ID ||
		len(reviews[0].Reviewers) != 2 || reviews[1].ID != sent.ID || reviews[1].State != "superseded" {
		t.Fatalf("reviews after the author's edit = %+v", reviews)
	}
	if reviews[0].RequestedBy == nil || reviews[0].RequestedBy.ID != agentID(t, api, admin) {
		t.Fatalf("the reopened review keeps who sent it: %+v", reviews[0].RequestedBy)
	}
	response := kbCall(t, api, http.MethodPost, "/api/v1/kb/reviews/"+sent.ID+"/approve", map[string]any{}, first, http.StatusConflict, nil)
	if code := errorCode(t, response); code != "review_state" {
		t.Fatalf("approving the superseded review: code=%s", code)
	}

	kbEditBody(t, api, first, article.ID, "Four days.")
	reviews = kbArticleReviews(t, api, admin, article.ID)
	if len(reviews) != 3 || len(reviews[0].Reviewers) != 1 || reviews[0].Reviewers[0].ID != secondID {
		t.Fatalf("reviews after a reviewer's edit = %+v", reviews)
	}
	response = kbCall(t, api, http.MethodPost, "/api/v1/kb/reviews/"+reviews[0].ID+"/approve", map[string]any{}, first, http.StatusForbidden, nil)
	if code := errorCode(t, response); code != "not_reviewer" {
		t.Fatalf("the reviewer who edited approves: code=%s", code)
	}

	if withdrawn := kbDecide(t, api, admin, reviews[0].ID, "withdraw", map[string]any{}, http.StatusOK); withdrawn.State != "withdrawn" {
		t.Fatalf("withdrawn = %+v", withdrawn)
	}
	response = kbCall(t, api, http.MethodPost, "/api/v1/kb/articles/"+article.ID+"/reviews", map[string]any{"reviewer_ids": []string{firstID}}, admin, http.StatusBadRequest, nil)
	if code := errorCode(t, response); code != "invalid_request" {
		t.Fatalf("naming the reviewer who edited the text again: code=%s", code)
	}
	again := kbRequestReview(t, api, admin, article.ID, secondID)
	kbCall(t, api, http.MethodPost, "/api/v1/kb/reviews/"+again.ID+"/request-changes", map[string]any{"note": "  "}, second, http.StatusBadRequest, nil)
	changes := kbDecide(t, api, second, again.ID, "request-changes", map[string]any{"note": "Say working days"}, http.StatusOK)
	if changes.State != "changes_requested" || changes.DecisionNote != "Say working days" {
		t.Fatalf("changes requested = %+v", changes)
	}
}

// Discarding the draft or deleting the article leaves nothing to review: the open review is closed.
func TestDiscardingOrDeletingTheTextClosesItsReview(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "KB Review Close", "admin@kb-review-close.example.com")
	reviewerID := agentID(t, api, agentSession(t, api, admin, "reviewer@kb-review-close.example.com"))
	collection := kbCollection(t, api, admin, map[string]any{"name": "Docs"})
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Returns", "body_md": "Free.", "status": "published"})

	kbEditBody(t, api, admin, article.ID, "Free within a month.")
	kbRequestReview(t, api, admin, article.ID, reviewerID)
	drafted := kbGetArticle(t, api, admin, article.ID)
	kbCall(t, api, http.MethodDelete, "/api/v1/kb/articles/"+article.ID+"/draft?updated_at="+url.QueryEscape(drafted.UpdatedAt), nil, admin, http.StatusOK, nil)
	if reviews := kbArticleReviews(t, api, admin, article.ID); len(reviews) != 1 || reviews[0].State != "superseded" {
		t.Fatalf("reviews after the draft was discarded = %+v", reviews)
	}

	kbEditBody(t, api, admin, article.ID, "Free within a year.")
	kbRequestReview(t, api, admin, article.ID, reviewerID)
	kbCall(t, api, http.MethodDelete, "/api/v1/kb/articles/"+article.ID+"?updated_at="+url.QueryEscape(kbGetArticle(t, api, admin, article.ID).UpdatedAt), nil, admin, http.StatusNoContent, nil)
	var trashed struct {
		Reviews []kbReviewView `json:"reviews"`
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/reviews?state=open", nil, admin, http.StatusOK, &trashed)
	if len(trashed.Reviews) != 0 {
		t.Fatalf("open reviews after the article was deleted = %+v", trashed.Reviews)
	}
}
