package mcpserver_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// reviewer invites a teammate with the Agent role (it may publish) and answers their session and agent id.
func reviewer(t *testing.T, e *env, w workspace, email string) (string, string) {
	t.Helper()
	status, created := e.rest(http.MethodPost, "/api/v1/auth/invites", map[string]any{"email": email, "roles": []string{"Agent"}}, w.admin)
	if status != http.StatusCreated {
		t.Fatalf("invite %s: status=%d body=%v", email, status, created)
	}
	parsed, err := url.Parse(created["url"].(string))
	if err != nil {
		t.Fatal(err)
	}
	status, redeemed := e.rest(http.MethodPost, "/api/v1/auth/redeem", map[string]any{"token": parsed.Query().Get("token")}, "")
	if status != http.StatusOK {
		t.Fatalf("redeem %s: status=%d body=%v", email, status, redeemed)
	}
	session := redeemed["token"].(string)
	status, me := e.rest(http.MethodGet, "/api/v1/auth/me", nil, session)
	if status != http.StatusOK {
		t.Fatalf("me %s: status=%d body=%v", email, status, me)
	}
	return session, me["agent"].(map[string]any)["id"].(string)
}

// reviewIDs are the ids of the reviews in a colliedesk_kb_reviews_list answer.
func reviewIDs(t *testing.T, listed map[string]any) []string {
	t.Helper()
	var ids []string
	for _, row := range listed["reviews"].([]any) {
		ids = append(ids, at(t, row, "id").(string))
	}
	return ids
}

func TestAnAgentSendsAnArticleForReviewAndATeammateApprovesBeforeItIsPublished(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("KB Review Desk", "admin@kb-review.example.com")
	reviewerSession, reviewerID := reviewer(t, e, w, "reviewer@kb-review.example.com")
	send, adminTier := e.connect(w.send, ""), e.connect(w.adminTier, "")
	center := e.ok(adminTier, "colliedesk_help_center_update", map[string]any{"enabled": true, "title": "Help", "locale": "en", "require_approval": true})
	if at(t, center, "help_center", "require_approval") != true {
		t.Fatalf("help center = %v, want require_approval true", center)
	}
	collection := at(t, e.ok(send, "colliedesk_kb_collection_create", map[string]any{"name": "Billing"}), "collection", "id").(string)
	created := e.ok(send, "colliedesk_kb_article_create", map[string]any{"collection_id": collection, "title": "Refund rules", "body": "Within five days."})
	articleID, updatedAt := at(t, created, "article", "id").(string), at(t, created, "article", "updated_at").(string)

	refused := e.ok(send, "colliedesk_kb_articles_publish", map[string]any{"article_ids": []string{articleID}})
	e.requireMatchesOutputSchema(send, "colliedesk_kb_articles_publish", refused)
	if code := at(t, refused, "results", 0, "error", "code"); code != "approval_required" {
		t.Fatalf("bulk publish without approval = %v, want approval_required", refused)
	}
	if message := e.fails(send, "colliedesk_kb_article_update", map[string]any{"article_id": articleID, "updated_at": updatedAt, "status": "published"}); !strings.Contains(message, "colliedesk_kb_article_send_for_review") {
		t.Errorf("publish without approval: %q, want it to name colliedesk_kb_article_send_for_review", message)
	}

	requested := e.ok(send, "colliedesk_kb_article_send_for_review", map[string]any{"article_id": articleID, "updated_at": updatedAt,
		"body": "Within seven days.", "reviewer_ids": []string{reviewerID}, "note": "Changed the window"})
	e.requireMatchesOutputSchema(send, "colliedesk_kb_article_send_for_review", requested)
	reviewID := at(t, requested, "review", "id").(string)
	if at(t, requested, "review", "state") != "open" || at(t, requested, "review", "written_by_agent_token", "label") != "test send" ||
		at(t, requested, "review", "reviewers", 0, "agent_id") != reviewerID || at(t, requested, "review", "team_text", "note") != "Changed the window" {
		t.Fatalf("review request = %v, want open, by the send token, to the reviewer, with the note", requested)
	}
	for _, own := range []string{w.send, w.adminTier} {
		if message := e.fails(e.connect(own, ""), "colliedesk_kb_review_approve", map[string]any{"review_id": reviewID}); !strings.Contains(message, "another reviewer must") {
			t.Errorf("the author's own token approved or failed otherwise: %q", message)
		}
	}

	bot := e.connect(e.token(reviewerSession, "Reviewer bot", "", []string{"kb_articles:read", "kb_articles:publish"}), "")
	mine := e.ok(bot, "colliedesk_kb_reviews_list", map[string]any{"reviewer": "me", "state": "open"})
	e.requireMatchesOutputSchema(bot, "colliedesk_kb_reviews_list", mine)
	if ids := reviewIDs(t, mine); len(ids) != 1 || ids[0] != reviewID {
		t.Fatalf("reviews for me = %v, want only %s", ids, reviewID)
	}
	detail := e.ok(bot, "colliedesk_kb_review_get", map[string]any{"review_id": reviewID})
	e.requireMatchesOutputSchema(bot, "colliedesk_kb_review_get", detail)
	if at(t, detail, "proposed", "team_text", "body") != "Within seven days." || at(t, detail, "live") != nil ||
		!strings.Contains(stringify(detail), `"change":"added","text":"Within seven days."`) {
		t.Fatalf("review detail = %v, want the proposed body, nothing live, and the added line", detail)
	}

	approved := e.ok(bot, "colliedesk_kb_review_approve", map[string]any{"review_id": reviewID, "note": "Good"})
	e.requireMatchesOutputSchema(bot, "colliedesk_kb_review_approve", approved)
	if at(t, approved, "review", "state") != "approved" || at(t, approved, "review", "decided_by_agent_token", "label") != "Reviewer bot" ||
		at(t, approved, "review", "team_text", "decision_note") != "Good" {
		t.Fatalf("approve = %v, want approved by the reviewer's token with the note", approved)
	}
	found := e.ok(send, "colliedesk_kb_articles_search", map[string]any{"state": "approved"})
	if !strings.Contains(stringify(found), articleID) {
		t.Errorf("articles in state approved = %v, want %s", found, articleID)
	}
	published := e.ok(send, "colliedesk_kb_articles_publish", map[string]any{"article_ids": []string{articleID}})
	if at(t, published, "results", 0, "article", "status") != "published" {
		t.Fatalf("publish after approval = %v, want published", published)
	}
	history := e.ok(send, "colliedesk_kb_article_history", map[string]any{"article_id": articleID})
	for _, kind := range []string{"review_request", "review_approve"} {
		if !strings.Contains(stringify(history), `"kind":"`+kind+`"`) {
			t.Errorf("history = %v, want a %s version", history, kind)
		}
	}

	other := e.newWorkspace("KB Review Elsewhere", "admin@kb-review-elsewhere.example.com")
	stranger := e.connect(other.adminTier, "")
	for _, tool := range []string{"colliedesk_kb_review_get", "colliedesk_kb_review_approve", "colliedesk_kb_review_withdraw"} {
		if message := e.fails(stranger, tool, map[string]any{"review_id": reviewID}); !strings.Contains(message, "No knowledge base review "+reviewID+" in this workspace") {
			t.Errorf("%s with another workspace's review: %q, want not found", tool, message)
		}
	}
	if listed := e.ok(stranger, "colliedesk_kb_reviews_list", map[string]any{"state": "approved"}); len(reviewIDs(t, listed)) != 0 {
		t.Errorf("another workspace lists %v", listed)
	}
}

func TestAnEditAfterApprovalNeedsANewApprovalBeforeTheDraftIsPublished(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("KB Review Edits", "admin@kb-review-edits.example.com")
	reviewerSession, reviewerID := reviewer(t, e, w, "reviewer@kb-review-edits.example.com")
	send := e.connect(w.send, "")
	e.ok(e.connect(w.adminTier, ""), "colliedesk_help_center_update", map[string]any{"enabled": true, "title": "Help", "locale": "en"})
	madeCollection := e.ok(send, "colliedesk_kb_collection_create", map[string]any{"name": "Billing"})
	collection := at(t, madeCollection, "collection", "id").(string)
	created := e.ok(send, "colliedesk_kb_article_create", map[string]any{"collection_id": collection, "title": "Refund rules", "body": "Within five days.", "status": "published"})
	articleID := at(t, created, "article", "id").(string)
	set := e.ok(e.connect(w.adminTier, ""), "colliedesk_kb_collection_update", map[string]any{"collection_id": collection, "require_approval": "on",
		"updated_at": at(t, madeCollection, "collection", "updated_at")})
	if at(t, set, "collection", "require_approval") != "on" {
		t.Fatalf("collection = %v, want require_approval on", set)
	}
	bot := e.connect(e.token(reviewerSession, "Reviewer bot", "", []string{"kb_articles:read", "kb_articles:publish"}), "")
	updatedAt := func() string {
		return at(t, e.ok(send, "colliedesk_kb_article_get", map[string]any{"article_id": articleID}), "article", "updated_at").(string)
	}
	request := func(body string) string {
		requested := e.ok(send, "colliedesk_kb_article_send_for_review", map[string]any{"article_id": articleID, "updated_at": updatedAt(), "body": body, "reviewer_ids": []string{reviewerID}})
		return at(t, requested, "review", "id").(string)
	}

	first := request("Within seven days.")
	e.ok(bot, "colliedesk_kb_review_approve", map[string]any{"review_id": first})
	e.ok(send, "colliedesk_kb_article_update", map[string]any{"article_id": articleID, "updated_at": updatedAt(), "body": "Within ten days."})
	if message := e.fails(send, "colliedesk_kb_article_draft_publish", map[string]any{"article_id": articleID, "updated_at": updatedAt()}); !strings.Contains(message, "colliedesk_kb_article_send_for_review") {
		t.Errorf("publish after an edit to the approved text: %q, want a refusal that names colliedesk_kb_article_send_for_review", message)
	}
	listed := e.ok(send, "colliedesk_kb_reviews_list", map[string]any{"article_id": articleID})
	e.requireMatchesOutputSchema(send, "colliedesk_kb_reviews_list", listed)
	if at(t, listed, "reviews", 0, "id") != first || at(t, listed, "reviews", 0, "current") != false {
		t.Fatalf("article reviews = %v, want the approval no longer current", listed)
	}

	second := request("Within ten days.")
	changes := e.ok(bot, "colliedesk_kb_review_request_changes", map[string]any{"review_id": second, "note": "Say business days"})
	e.requireMatchesOutputSchema(bot, "colliedesk_kb_review_request_changes", changes)
	if at(t, changes, "review", "state") != "changes_requested" || at(t, changes, "review", "team_text", "decision_note") != "Say business days" {
		t.Fatalf("request changes = %v", changes)
	}
	third := request("Within ten business days.")
	withdrawn := e.ok(send, "colliedesk_kb_review_withdraw", map[string]any{"review_id": third})
	e.requireMatchesOutputSchema(send, "colliedesk_kb_review_withdraw", withdrawn)
	if at(t, withdrawn, "review", "state") != "withdrawn" {
		t.Fatalf("withdraw = %v", withdrawn)
	}
	fourth := request("Within ten business days, counted from the order.")
	bulk := e.ok(bot, "colliedesk_kb_review_approve", map[string]any{"review_ids": []string{fourth, first}})
	if at(t, bulk, "results", 0, "review", "state") != "approved" || at(t, bulk, "results", 1, "error", "code") != "review_state" {
		t.Fatalf("bulk approve = %v, want the open one approved and the closed one refused", bulk)
	}
	published := e.ok(send, "colliedesk_kb_article_draft_publish", map[string]any{"article_id": articleID, "updated_at": updatedAt()})
	if at(t, published, "article", "status") != "published" {
		t.Fatalf("draft publish after approval = %v", published)
	}
}
