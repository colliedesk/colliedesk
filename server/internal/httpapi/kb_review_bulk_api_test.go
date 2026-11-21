package httpapi_test

import (
	"net/http"
	"slices"
	"testing"
)

type kbBulkResult struct {
	ID    string `json:"id"`
	Error *struct {
		Code string `json:"code"`
	} `json:"error"`
}

func kbBulk(t *testing.T, api http.Handler, token, path string, body map[string]any) map[string]string {
	t.Helper()
	var answer struct {
		Results []kbBulkResult `json:"results"`
	}
	kbCall(t, api, http.MethodPost, path, body, token, http.StatusOK, &answer)
	codes := map[string]string{}
	for _, result := range answer.Results {
		codes[result.ID] = "ok"
		if result.Error != nil {
			codes[result.ID] = result.Error.Code
		}
	}
	return codes
}

func kbArticleIDs(t *testing.T, api http.Handler, token, query string) []string {
	t.Helper()
	var page struct {
		Items []kbArticleView `json:"items"`
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles?"+query, nil, token, http.StatusOK, &page)
	ids := []string{}
	for _, item := range page.Items {
		ids = append(ids, item.ID)
	}
	slices.Sort(ids)
	return ids
}

// The In review and Approved lists show the drafts waiting for a reviewer and the ones approved as they
// are; a reviewer approves several at once and the author publishes several at once, each article
// answered on its own: one without an approval of its current text stays a draft.
func TestReviewsAreApprovedAndArticlesPublishedInBulkEachOnItsOwn(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "KB Review Bulk", "admin@kb-review-bulk.example.com")
	reviewer := agentSession(t, api, admin, "reviewer@kb-review-bulk.example.com")
	reviewerID := agentID(t, api, reviewer)
	collection := kbApprovalCollection(t, api, admin)
	refunds := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Refunds", "body_md": "Within 14 days."})
	shipping := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Shipping", "body_md": "Two days."})
	returns := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Returns", "body_md": "By post."})
	first := kbRequestReview(t, api, admin, refunds.ID, reviewerID)
	second := kbRequestReview(t, api, admin, shipping.ID, reviewerID)

	inReview := []string{refunds.ID, shipping.ID}
	slices.Sort(inReview)
	if got := kbArticleIDs(t, api, admin, "state=in_review"); !slices.Equal(got, inReview) {
		t.Fatalf("in review = %v, want %v", got, inReview)
	}
	if got := kbArticleIDs(t, api, admin, "state=approved"); len(got) != 0 {
		t.Fatalf("approved before any approval = %v", got)
	}
	if response := request(t, api, http.MethodGet, "/api/v1/kb/articles?state=waiting", nil, admin, nil); response.Code != http.StatusBadRequest {
		t.Fatalf("unknown state: status=%d", response.Code)
	}

	other := createSession(t, api, service, "KB Review Bulk Other", "admin@kb-review-bulk-other.example.com")
	otherCollection := kbApprovalCollection(t, api, other)
	foreign := kbArticle(t, api, other, map[string]any{"collection_id": otherCollection.ID, "title": "Theirs", "body_md": "Not yours."})
	foreignReview := kbRequestReview(t, api, other, foreign.ID, agentID(t, api, agentSession(t, api, other, "reviewer@kb-review-bulk-other.example.com")))

	approved := kbBulk(t, api, reviewer, "/api/v1/kb/reviews/approve", map[string]any{"review_ids": []string{first.ID, second.ID, foreignReview.ID}})
	if approved[first.ID] != "ok" || approved[second.ID] != "ok" || approved[foreignReview.ID] != "not_found" {
		t.Fatalf("bulk approve = %v", approved)
	}
	if got := kbArticleIDs(t, api, admin, "state=approved"); !slices.Equal(got, inReview) {
		t.Fatalf("approved = %v, want %v", got, inReview)
	}
	if got := kbArticleIDs(t, api, admin, "state=in_review"); len(got) != 0 {
		t.Fatalf("in review after the approvals = %v", got)
	}

	// An edit after the approval: the text is no longer the approved one.
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+shipping.ID, map[string]any{"updated_at": kbGetArticle(t, api, admin, shipping.ID).UpdatedAt, "body_md": "Three days."},
		admin, http.StatusOK, nil)
	if got := kbArticleIDs(t, api, admin, "state=approved"); !slices.Equal(got, []string{refunds.ID}) {
		t.Fatalf("approved after an edit = %v, want only %s", got, refunds.ID)
	}

	published := kbBulk(t, api, admin, "/api/v1/kb/articles/publish", map[string]any{"article_ids": []string{refunds.ID, shipping.ID, returns.ID, foreign.ID}})
	if published[refunds.ID] != "ok" || published[shipping.ID] != "approval_required" || published[returns.ID] != "approval_required" || published[foreign.ID] != "not_found" {
		t.Fatalf("bulk publish = %v", published)
	}
	for id, want := range map[string]string{refunds.ID: "published", shipping.ID: "draft", returns.ID: "draft"} {
		if got := kbGetArticle(t, api, admin, id); got.Status != want {
			t.Fatalf("article %s status = %s, want %s", id, got.Status, want)
		}
	}
	if got := kbGetArticle(t, api, other, foreign.ID); got.Status != "draft" {
		t.Fatalf("another workspace's article was published: %+v", got)
	}
	if got := kbArticleIDs(t, api, admin, "state=approved"); len(got) != 0 {
		t.Fatalf("approved after publishing = %v", got)
	}

	// Approving and publishing in bulk are publish decisions.
	writer := issueCustomToken(t, api, admin, "Writer", "kb_articles:read", "kb_articles:update")
	refused(t, api, http.MethodPost, "/api/v1/kb/reviews/approve", map[string]any{"review_ids": []string{first.ID}}, writer.Token, "kb_articles:publish")
	refused(t, api, http.MethodPost, "/api/v1/kb/articles/publish", map[string]any{"article_ids": []string{returns.ID}}, writer.Token, "kb_articles:publish")
	for _, path := range []string{"/api/v1/kb/reviews/approve", "/api/v1/kb/articles/publish"} {
		if response := request(t, api, http.MethodPost, path, map[string]any{}, admin, nil); response.Code != http.StatusBadRequest {
			t.Fatalf("%s without ids: status=%d", path, response.Code)
		}
	}
}
