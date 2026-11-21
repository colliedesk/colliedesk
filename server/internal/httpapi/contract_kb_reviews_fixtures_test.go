package httpapi_test

import (
	"net/http"
	"testing"
)

// contractKBReviews are the reviews the contract cases act on: an article to send for review, and
// reviews the agent sent to the admin, one for each decision a case makes.
type contractKBReviews struct {
	requestArticleID string
	reviewedArticle  string
	translatedID     string
	approveID        string
	changesID        string
	withdrawID       string
	bulkApproveID    string
	bulkPublishID    string
}

func seedContractKBReviews(t *testing.T, api http.Handler, admin, agent, collectionID string) contractKBReviews {
	t.Helper()
	var reviews contractKBReviews
	adminID := agentID(t, api, admin)
	reviews.requestArticleID = kbArticle(t, api, admin, map[string]any{"collection_id": collectionID, "title": "Contract article to review", "body_md": "x"}).ID
	reviews.translatedID = kbArticle(t, api, admin, map[string]any{"collection_id": collectionID, "title": "Contract translation to review", "body_md": "x", "status": "published"}).ID
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles/"+reviews.translatedID+"/translations", map[string]any{"locale": "fr", "title": "Traduction", "body_md": "x"}, admin, http.StatusCreated, nil)
	review := func(title string) (string, string) {
		article := kbArticle(t, api, agent, map[string]any{"collection_id": collectionID, "title": title, "body_md": "x"})
		return article.ID, kbRequestReview(t, api, agent, article.ID, adminID).ID
	}
	reviews.reviewedArticle, reviews.approveID = review("Contract article to approve")
	_, reviews.changesID = review("Contract article to change")
	_, reviews.withdrawID = review("Contract article to withdraw")
	_, reviews.bulkApproveID = review("Contract article to approve in bulk")
	reviews.bulkPublishID = kbArticle(t, api, admin, map[string]any{"collection_id": collectionID, "title": "Contract article to publish in bulk", "body_md": "x"}).ID
	return reviews
}

func kbReviewContractCases(f contractFixture) map[string]contractCase {
	admin, reviews := f.adminToken, f.kbReviews
	return map[string]contractCase{
		"POST /api/v1/kb/articles/{id}/reviews":        {"/api/v1/kb/articles/" + reviews.requestArticleID + "/reviews", map[string]any{"reviewer_ids": []string{f.agentID}, "note": "Please check"}, admin, 201, ""},
		"GET /api/v1/kb/articles/{id}/reviews":         {"/api/v1/kb/articles/" + reviews.reviewedArticle + "/reviews", nil, admin, 200, ""},
		"GET /api/v1/kb/reviews":                       {"/api/v1/kb/reviews?state=open&reviewer=me", nil, admin, 200, ""},
		"GET /api/v1/kb/reviews/{id}":                  {"/api/v1/kb/reviews/" + reviews.approveID, nil, admin, 200, ""},
		"POST /api/v1/kb/reviews/{id}/approve":         {"/api/v1/kb/reviews/" + reviews.approveID + "/approve", map[string]any{"note": "Looks right"}, admin, 200, ""},
		"POST /api/v1/kb/reviews/{id}/request-changes": {"/api/v1/kb/reviews/" + reviews.changesID + "/request-changes", map[string]any{"note": "Name the plan"}, admin, 200, ""},
		"POST /api/v1/kb/reviews/{id}/withdraw":        {"/api/v1/kb/reviews/" + reviews.withdrawID + "/withdraw", map[string]any{}, admin, 200, ""},
		"POST /api/v1/kb/articles/{id}/translations/{locale}/reviews": {"/api/v1/kb/articles/" + reviews.translatedID + "/translations/fr/reviews",
			map[string]any{"reviewer_ids": []string{f.agentID}}, admin, 201, ""},
		"GET /api/v1/kb/articles/{id}/translations/{locale}/reviews": {"/api/v1/kb/articles/" + reviews.translatedID + "/translations/fr/reviews", nil, admin, 200, ""},
		"POST /api/v1/kb/reviews/approve":                            {"/api/v1/kb/reviews/approve", map[string]any{"review_ids": []string{reviews.bulkApproveID}}, admin, 200, ""},
		"POST /api/v1/kb/articles/publish":                           {"/api/v1/kb/articles/publish", map[string]any{"article_ids": []string{reviews.bulkPublishID}}, admin, 200, ""},
	}
}
