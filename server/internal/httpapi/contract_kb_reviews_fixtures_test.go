package httpapi_test

import (
	"net/http"
	"testing"
)

// contractKBReviews are the reviews the contract cases act on: an article to send for review, and
// reviews the agent sent to the admin, one for each decision a case makes.
type contractKBReviews struct {
	requestArticleID string
	approveID        string
}

func seedContractKBReviews(t *testing.T, api http.Handler, admin, agent, collectionID string) contractKBReviews {
	t.Helper()
	var reviews contractKBReviews
	adminID := agentID(t, api, admin)
	reviews.requestArticleID = kbArticle(t, api, admin, map[string]any{"collection_id": collectionID, "title": "Contract article to review", "body_md": "x"}).ID
	review := func(title string) string {
		article := kbArticle(t, api, agent, map[string]any{"collection_id": collectionID, "title": title, "body_md": "x"})
		return kbRequestReview(t, api, agent, article.ID, adminID).ID
	}
	reviews.approveID = review("Contract article to approve")
	return reviews
}

func kbReviewContractCases(f contractFixture) map[string]contractCase {
	admin, reviews := f.adminToken, f.kbReviews
	return map[string]contractCase{
		"POST /api/v1/kb/articles/{id}/reviews": {"/api/v1/kb/articles/" + reviews.requestArticleID + "/reviews", map[string]any{"reviewer_ids": []string{f.agentID}, "note": "Please check"}, admin, 201, ""},
		"POST /api/v1/kb/reviews/{id}/approve":  {"/api/v1/kb/reviews/" + reviews.approveID + "/approve", map[string]any{"note": "Looks right"}, admin, 200, ""},
	}
}
