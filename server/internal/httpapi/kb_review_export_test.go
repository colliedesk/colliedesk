package httpapi_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

// The export keeps the approval settings and every review with its reviewers, so a restored workspace
// still knows who approved which text and which collections need approval.
func TestTheExportKeepsTheApprovalSettingsAndEveryReview(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Review Export", "admin@review-export.example.com")
	reviewer := agentSession(t, api, admin, "reviewer@review-export.example.com")
	reviewerID := agentID(t, api, reviewer)
	adminID := agentID(t, api, admin)
	collection := kbApprovalCollection(t, api, admin)
	draft := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Refunds", "body_md": "Within 14 days."})
	review := kbRequestReview(t, api, admin, draft.ID, reviewerID)
	kbDecide(t, api, reviewer, review.ID, "approve", map[string]any{"note": "Reads well"}, http.StatusOK)

	exported := request(t, api, http.MethodGet, "/api/v1/export", nil, admin, nil)
	if exported.Code != http.StatusOK {
		t.Fatalf("export status=%d body=%s", exported.Code, exported.Body.String())
	}
	entries := unzip(t, exported.Body.Bytes())
	reviews := ndjsonLines(t, entries["kb-reviews.ndjson"])
	if len(reviews) != 1 {
		t.Fatalf("reviews = %+v, want the one approved review", reviews)
	}
	got := reviews[0]
	if got["id"] != review.ID || got["article_id"] != draft.ID || got["locale"] != nil || got["state"] != "approved" || got["note"] != "Please check the refund window" ||
		got["requested_by_agent_id"] != adminID || got["requested_by_token_label"] != nil || got["decided_by_agent_id"] != reviewerID || got["decision_note"] != "Reads well" ||
		got["content_hash"] != review.ContentHash || got["approves_pending"] != true || got["decided_at"] == nil {
		t.Fatalf("review = %+v", got)
	}
	reviewers := ndjsonLines(t, entries["kb-review-reviewers.ndjson"])
	if len(reviewers) != 1 || reviewers[0]["review_id"] != review.ID || reviewers[0]["agent_id"] != reviewerID {
		t.Fatalf("reviewers = %+v, want the one reviewer", reviewers)
	}
	collections := ndjsonLines(t, entries["kb-collections.ndjson"])
	if len(collections) != 1 || collections[0]["require_approval"] != "on" {
		t.Fatalf("collections = %+v, want require_approval on", collections)
	}
	center := ndjsonLines(t, entries["help-center.ndjson"])
	if len(center) != 1 || center[0]["require_approval"] != false {
		t.Fatalf("help center = %+v, want require_approval false", center)
	}
	var manifest struct {
		Counts map[string]int `json:"counts"`
	}
	if err := json.Unmarshal(entries["manifest.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Counts["kb_reviews"] != 1 || manifest.Counts["kb_review_reviewers"] != 1 {
		t.Fatalf("manifest counts = %+v, want 1 review and 1 reviewer", manifest.Counts)
	}
}
