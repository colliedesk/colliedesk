package httpapi_test

import (
	"net/http"
	"reflect"
	"testing"
)

type kbReviewDetailView struct {
	kbReviewView
	Reviewers []struct {
		ID     string  `json:"agent_id"`
		SeenAt *string `json:"seen_at"`
	} `json:"reviewers"`
	Live *struct {
		BodyMD string `json:"body_md"`
	} `json:"live"`
	Proposed struct {
		Title  string `json:"title"`
		BodyMD string `json:"body_md"`
	} `json:"proposed"`
	Diff []struct {
		Change string `json:"change"`
		Text   string `json:"text"`
	} `json:"diff"`
}

// A reviewer reads the review with the line diff against the live text, and finds it in their own list;
// another workspace never sees it.
func TestAReviewerReadsTheDiffAndAnotherWorkspaceCannotSeeTheReview(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "KB Review Detail", "admin@kb-review-detail.example.com")
	reviewer := agentSession(t, api, admin, "reviewer@kb-review-detail.example.com")
	collection := kbCollection(t, api, admin, map[string]any{"name": "Docs"})
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Shipping", "body_md": "Ship in two days.\nReturns are free.", "status": "published"})
	kbEditBody(t, api, admin, article.ID, "Ship in three days.\nReturns are free.")
	sent := kbRequestReview(t, api, admin, article.ID, agentID(t, api, reviewer))

	var mine, theirs struct {
		Reviews []kbReviewView `json:"reviews"`
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/reviews?state=open&reviewer=me", nil, reviewer, http.StatusOK, &mine)
	kbCall(t, api, http.MethodGet, "/api/v1/kb/reviews?state=open&reviewer=me", nil, admin, http.StatusOK, &theirs)
	if len(mine.Reviews) != 1 || mine.Reviews[0].ID != sent.ID || len(theirs.Reviews) != 0 {
		t.Fatalf("reviewer's list = %+v, author's list = %+v", mine.Reviews, theirs.Reviews)
	}

	var detail kbReviewDetailView
	kbCall(t, api, http.MethodGet, "/api/v1/kb/reviews/"+sent.ID, nil, reviewer, http.StatusOK, &detail)
	type line struct{ Change, Text string }
	var diff []line
	for _, d := range detail.Diff {
		diff = append(diff, line{d.Change, d.Text})
	}
	want := []line{{"removed", "Ship in two days."}, {"added", "Ship in three days."}, {"same", "Returns are free."}}
	if !reflect.DeepEqual(diff, want) || detail.Live == nil || detail.Live.BodyMD != "Ship in two days.\nReturns are free." ||
		detail.Proposed.BodyMD != "Ship in three days.\nReturns are free." || detail.Proposed.Title != "Shipping" {
		t.Fatalf("review detail = %+v", detail)
	}
	if len(detail.Reviewers) != 1 || detail.Reviewers[0].SeenAt == nil {
		t.Fatalf("the reviewer who read it is marked as having seen it: %+v", detail.Reviewers)
	}

	other := createSession(t, api, service, "KB Review Detail Other", "admin@kb-review-detail-other.example.com")
	kbCall(t, api, http.MethodGet, "/api/v1/kb/reviews/"+sent.ID, nil, other, http.StatusNotFound, nil)
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles/"+article.ID+"/reviews", nil, other, http.StatusNotFound, nil)
	for _, decision := range []string{"approve", "withdraw"} {
		kbCall(t, api, http.MethodPost, "/api/v1/kb/reviews/"+sent.ID+"/"+decision, map[string]any{}, other, http.StatusNotFound, nil)
	}
	kbCall(t, api, http.MethodPost, "/api/v1/kb/reviews/"+sent.ID+"/request-changes", map[string]any{"note": "No"}, other, http.StatusNotFound, nil)
	kbCall(t, api, http.MethodGet, "/api/v1/kb/reviews", nil, other, http.StatusOK, &theirs)
	if len(theirs.Reviews) != 0 {
		t.Fatalf("another workspace lists %+v", theirs.Reviews)
	}
}
