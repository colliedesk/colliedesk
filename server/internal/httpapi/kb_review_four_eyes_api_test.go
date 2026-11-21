package httpapi_test

import (
	"net/http"
	"testing"
)

type kbVersionEntry struct {
	Version   int    `json:"version"`
	Kind      string `json:"kind"`
	Status    string `json:"status"`
	ChangedBy *struct {
		ID string `json:"agent_id"`
	} `json:"changed_by"`
	Token *struct {
		Label string `json:"label"`
	} `json:"token"`
}

// A token that may publish sends its own draft for review: neither it, another token of the person who
// made it, nor that person approves it. The named teammate approves through their own token, and the
// history says who did each step, through which token, at which version.
func TestATokensDraftIsApprovedOnlyByAnotherTeammate(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "KB Four Eyes", "admin@kb-four-eyes.example.com")
	adminID := agentID(t, api, admin)
	reviewer := agentSession(t, api, admin, "reviewer@kb-four-eyes.example.com")
	reviewerID := agentID(t, api, reviewer)
	collection := kbApprovalCollection(t, api, admin)
	writer := issueCustomToken(t, api, admin, "Docs bot", "kb_articles:read", "kb_articles:create", "kb_articles:update", "kb_articles:publish")
	sibling := issueCustomToken(t, api, admin, "Other bot", "kb_articles:read", "kb_articles:publish")
	reviewerBot := issueCustomToken(t, api, reviewer, "Reviewer bot", "kb_articles:read", "kb_articles:publish")

	draft := kbArticle(t, api, writer.Token, map[string]any{"collection_id": collection.ID, "title": "Refunds", "body_md": "Within 14 days."})
	response := kbCall(t, api, http.MethodPost, "/api/v1/kb/articles/"+draft.ID+"/reviews", map[string]any{"reviewer_ids": []string{adminID}}, writer.Token, http.StatusBadRequest, nil)
	if code := errorCode(t, response); code != "invalid_request" {
		t.Fatalf("a token naming the person who made it: code=%s", code)
	}
	review := kbRequestReview(t, api, writer.Token, draft.ID, reviewerID)
	if review.RequestedBy == nil || review.RequestedBy.ID != adminID || review.RequestedByToken == nil || review.RequestedByToken.Label != "Docs bot" {
		t.Fatalf("review sent by a token = %+v", review)
	}

	for name, token := range map[string]string{"the token that sent it": writer.Token, "another token of its maker": sibling.Token, "its maker": admin} {
		response := kbCall(t, api, http.MethodPost, "/api/v1/kb/reviews/"+review.ID+"/approve", map[string]any{}, token, http.StatusForbidden, nil)
		if code := errorCode(t, response); code != "not_reviewer" {
			t.Fatalf("%s approving: code=%s", name, code)
		}
	}
	approved := kbDecide(t, api, reviewerBot.Token, review.ID, "approve", map[string]any{}, http.StatusOK)
	if approved.State != "approved" || approved.DecidedBy == nil || approved.DecidedBy.ID != reviewerID || approved.DecidedByToken == nil || approved.DecidedByToken.Label != "Reviewer bot" {
		t.Fatalf("approved through the reviewer's token = %+v", approved)
	}
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+draft.ID, map[string]any{"updated_at": kbGetArticle(t, api, admin, draft.ID).UpdatedAt, "status": "published"},
		admin, http.StatusOK, nil)

	var history struct {
		Items []kbVersionEntry `json:"items"`
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles/"+draft.ID+"/versions", nil, admin, http.StatusOK, &history)
	type step struct {
		Version             int
		Kind, Status, Actor string
		Token               string
	}
	var got []step
	for _, item := range history.Items {
		entry := step{Version: item.Version, Kind: item.Kind, Status: item.Status}
		if item.ChangedBy != nil {
			entry.Actor = item.ChangedBy.ID
		}
		if item.Token != nil {
			entry.Token = item.Token.Label
		}
		got = append(got, entry)
	}
	want := []step{
		{4, "edit", "published", adminID, ""},
		{3, "review_approve", "draft", reviewerID, "Reviewer bot"},
		{2, "review_request", "draft", adminID, "Docs bot"},
		{1, "create", "draft", adminID, "Docs bot"},
	}
	if len(got) != len(want) {
		t.Fatalf("history = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("history[%d] = %+v, want %+v (all: %+v)", i, got[i], want[i], got)
		}
	}
}

// Only an active teammate who may publish can be named a reviewer, and never the person who sends it.
func TestAReviewerMustBeAnotherTeammateWhoMayPublish(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "KB Reviewer Choice", "admin@kb-reviewer-choice.example.com")
	reader := agentSession(t, api, admin, "reader@kb-reviewer-choice.example.com")
	readerID := agentID(t, api, reader)
	kbCall(t, api, http.MethodPost, "/api/v1/roles", map[string]any{"name": "KB reader", "permissions": []string{"kb_articles:read"}}, admin, http.StatusCreated, nil)
	if got := setRoles(t, api, admin, readerID, "KB reader"); got.code != http.StatusOK {
		t.Fatalf("give the reader role: %d %s", got.code, got.body)
	}
	collection := kbApprovalCollection(t, api, admin)
	draft := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Refunds", "body_md": "Within 14 days."})

	for name, reviewers := range map[string][]string{"a teammate who may not publish": {readerID}, "the sender": {agentID(t, api, admin)}} {
		response := kbCall(t, api, http.MethodPost, "/api/v1/kb/articles/"+draft.ID+"/reviews", map[string]any{"reviewer_ids": reviewers}, admin, http.StatusBadRequest, nil)
		if code := errorCode(t, response); code != "invalid_request" {
			t.Fatalf("naming %s: code=%s", name, code)
		}
	}
	if reviews := kbArticleReviews(t, api, admin, draft.ID); len(reviews) != 0 {
		t.Fatalf("a refused request left reviews %+v", reviews)
	}
}
