package httpapi_test

import (
	"fmt"
	"net/http"
	"testing"
)

type kbBatchItem struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Outcome  string `json:"outcome"`
	Undoable bool   `json:"undoable"`
	Error    *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// kbBatch is the answer to a bulk action on articles or to its undo.
type kbBatch struct {
	Operation *struct {
		ID        string `json:"id"`
		UndoUntil string `json:"undo_until"`
	} `json:"operation"`
	Counts  map[string]int `json:"counts"`
	Results []kbBatchItem  `json:"results"`
}

// outcomes is each id's outcome, or its error code when refused.
func (b kbBatch) outcomes() map[string]string {
	got := map[string]string{}
	for _, item := range b.Results {
		got[item.ID] = item.Outcome
		if item.Error != nil {
			got[item.ID] = item.Error.Code
		}
	}
	return got
}

func kbBulkAction(t *testing.T, api http.Handler, token string, body map[string]any) kbBatch {
	t.Helper()
	var answer kbBatch
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles/bulk", body, token, http.StatusOK, &answer)
	return answer
}

// A bulk publish checks each article as publishing it alone would: in a collection that needs approval
// only the approved one goes live, and an id from another workspace is answered exactly as an id that
// exists nowhere, so the answer does not tell that it exists.
func TestABulkPublishDoesOnlyWhatPublishingEachAloneWould(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "KB Bulk Publish", "admin@kb-bulk-publish.example.com")
	reviewer := agentSession(t, api, admin, "reviewer@kb-bulk-publish.example.com")
	collection := kbApprovalCollection(t, api, admin)
	refunds := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Refunds", "body_md": "Within 14 days."})
	shipping := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Shipping", "body_md": "Two days."})
	returns := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Returns", "body_md": "By post."})
	review := kbRequestReview(t, api, admin, refunds.ID, agentID(t, api, reviewer))
	kbDecide(t, api, reviewer, review.ID, "approve", map[string]any{}, http.StatusOK)

	other := createSession(t, api, service, "KB Bulk Publish Other", "admin@kb-bulk-publish-other.example.com")
	foreign := kbArticle(t, api, other, map[string]any{"collection_id": kbCollection(t, api, other, map[string]any{"name": "Theirs"}).ID, "title": "Theirs", "body_md": "Not yours."})
	nowhere := "00000000-0000-4000-8000-000000000679"

	answer := kbBulkAction(t, api, admin, map[string]any{"action": "publish", "ids": []string{refunds.ID, shipping.ID, returns.ID, foreign.ID, nowhere}})
	got := answer.outcomes()
	want := map[string]string{refunds.ID: "done", shipping.ID: "approval_required", returns.ID: "approval_required", foreign.ID: "not_found", nowhere: "not_found"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("bulk publish = %v, want %v", got, want)
	}
	if answer.Counts["done"] != 1 || answer.Counts["refused"] != 4 || answer.Counts["unchanged"] != 0 {
		t.Fatalf("counts = %v, want 1 done and 4 refused", answer.Counts)
	}
	byID := map[string]kbBatchItem{}
	for _, item := range answer.Results {
		byID[item.ID] = item
	}
	if byID[foreign.ID].Error.Message != byID[nowhere].Error.Message || byID[foreign.ID].Title != "" {
		t.Fatalf("another workspace's id is told apart from a missing one: %+v vs %+v", byID[foreign.ID], byID[nowhere])
	}
	if byID[refunds.ID].Title != "Refunds" || !byID[refunds.ID].Undoable {
		t.Fatalf("the published article's result = %+v, want its title and undoable", byID[refunds.ID])
	}
	for id, status := range map[string]string{refunds.ID: "published", shipping.ID: "draft", returns.ID: "draft"} {
		if got := kbGetArticle(t, api, admin, id).Status; got != status {
			t.Fatalf("%s is %s, want %s", id, got, status)
		}
	}
	if got := kbGetArticle(t, api, other, foreign.ID).Status; got != "draft" {
		t.Fatalf("the other workspace's article became %s", got)
	}
}

// A token sends drafts for review in bulk and then tries to approve them in bulk: four-eyes holds, each
// approval is refused, and the named teammate's token approves them.
func TestABulkApproveByATokenNeverApprovesItsOwnRequest(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "KB Bulk Four Eyes", "admin@kb-bulk-four-eyes.example.com")
	reviewer := agentSession(t, api, admin, "reviewer@kb-bulk-four-eyes.example.com")
	collection := kbApprovalCollection(t, api, admin)
	writer := issueCustomToken(t, api, admin, "Docs bot", "kb_articles:read", "kb_articles:create", "kb_articles:update", "kb_articles:publish")
	reviewerBot := issueCustomToken(t, api, reviewer, "Reviewer bot", "kb_articles:read", "kb_articles:publish")
	first := kbArticle(t, api, writer.Token, map[string]any{"collection_id": collection.ID, "title": "Refunds", "body_md": "Within 14 days."})
	second := kbArticle(t, api, writer.Token, map[string]any{"collection_id": collection.ID, "title": "Shipping", "body_md": "Two days."})
	ids := []string{first.ID, second.ID}

	sent := kbBulkAction(t, api, writer.Token, map[string]any{"action": "send_for_review", "ids": ids, "reviewer_ids": []string{agentID(t, api, reviewer)}})
	if got := sent.outcomes(); got[first.ID] != "done" || got[second.ID] != "done" {
		t.Fatalf("bulk send for review = %v", got)
	}
	for name, token := range map[string]string{"the token that sent them": writer.Token, "the person who made it": admin} {
		refused := kbBulkAction(t, api, token, map[string]any{"action": "approve", "ids": ids})
		if got := refused.outcomes(); got[first.ID] != "not_reviewer" || got[second.ID] != "not_reviewer" || refused.Counts["done"] != 0 {
			t.Fatalf("%s approving in bulk = %v", name, got)
		}
	}
	if got := kbArticleIDs(t, api, admin, "state=approved"); len(got) != 0 {
		t.Fatalf("approved after refused approvals = %v", got)
	}
	approved := kbBulkAction(t, api, reviewerBot.Token, map[string]any{"action": "approve", "ids": ids})
	if got := approved.outcomes(); got[first.ID] != "done" || got[second.ID] != "done" {
		t.Fatalf("the reviewer approving in bulk = %v", got)
	}
}

// A filter picks every article that matches it, not only the first page a list shows; a filter key the
// list does not know is refused before anything changes, so a typo never widens the action.
func TestABulkFilterReachesEveryMatchingArticleBeyondTheFirstPage(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "KB Bulk Filter", "admin@kb-bulk-filter.example.com")
	collection := kbCollection(t, api, admin, map[string]any{"name": "Billing"})
	kbArticle(t, api, admin, map[string]any{"collection_id": kbCollection(t, api, admin, map[string]any{"name": "Other"}).ID, "title": "Elsewhere", "body_md": "Stays."})
	for i := range 55 {
		kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": fmt.Sprintf("Invoice %02d", i), "body_md": "Monthly."})
	}
	var first struct {
		Items      []kbArticleView `json:"items"`
		NextCursor *string         `json:"next_cursor"`
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles?collection_id="+collection.ID, nil, admin, http.StatusOK, &first)
	if len(first.Items) != 50 || first.NextCursor == nil {
		t.Fatalf("the list's first page holds %d articles, next=%v; want 50 and a next page", len(first.Items), first.NextCursor)
	}

	// The contract refuses this body too; the server must refuse it on its own.
	typo := serve(api, newRequest(t, http.MethodPost, "/api/v1/kb/articles/bulk", map[string]any{"action": "tags_add", "tags": []string{"billing"},
		"filter": map[string]any{"collection": collection.ID}}, admin, nil))
	if code := errorCode(t, typo); typo.Code != http.StatusBadRequest || code != "invalid_request" {
		t.Fatalf("an unknown filter key: status=%d code=%s", typo.Code, code)
	}
	answer := kbBulkAction(t, api, admin, map[string]any{"action": "tags_add", "tags": []string{"billing"}, "filter": map[string]any{"collection_id": collection.ID}})
	if answer.Counts["done"] != 55 || len(answer.Results) != 55 {
		t.Fatalf("bulk tag by filter: counts=%v results=%d, want 55 done", answer.Counts, len(answer.Results))
	}
	if got := kbArticleIDs(t, api, admin, "tag=billing&limit=200"); len(got) != 55 {
		t.Fatalf("articles tagged billing = %d, want 55", len(got))
	}
	again := kbBulkAction(t, api, admin, map[string]any{"action": "tags_add", "tags": []string{"billing"}, "filter": map[string]any{"collection_id": collection.ID}})
	if again.Counts["unchanged"] != 55 || again.Operation != nil {
		t.Fatalf("tagging them again: counts=%v operation=%v, want 55 unchanged and nothing to undo", again.Counts, again.Operation)
	}
}

// A token holds only what it was given: a bulk action needs the permission its action needs, and the
// refusal names it.
func TestABulkActionNeedsThePermissionOfItsAction(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "KB Bulk Scope", "admin@kb-bulk-scope.example.com")
	article := kbArticle(t, api, admin, map[string]any{"collection_id": kbCollection(t, api, admin, map[string]any{"name": "Docs"}).ID, "title": "Refunds", "body_md": "Within 14 days."})
	reader := issueCustomToken(t, api, admin, "Reader bot", "kb_articles:read")
	ids := []string{article.ID}
	for action, permission := range map[string]string{"trash": "kb_articles:delete", "publish": "kb_articles:publish", "visibility": "kb_articles:update"} {
		refused(t, api, http.MethodPost, "/api/v1/kb/articles/bulk", map[string]any{"action": action, "ids": ids, "visibility": "agents"}, reader.Token, permission)
	}
	refused(t, api, http.MethodPost, "/api/v1/kb/bulk-operations/00000000-0000-4000-8000-000000000679/undo", map[string]any{}, reader.Token, "kb_articles:update")
	if got := kbGetArticle(t, api, admin, article.ID); got.Status != "draft" || got.DeletedAt != nil {
		t.Fatalf("a refused bulk action changed the article: %+v", got)
	}
}
