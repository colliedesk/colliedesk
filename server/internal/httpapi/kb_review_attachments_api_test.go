package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
)

type kbReviewWithFiles struct {
	kbReviewView
	Proposed struct {
		BodyMD      string             `json:"body_md"`
		Attachments []kbAttachmentView `json:"attachments"`
	} `json:"proposed"`
}

// The files an article lists are part of what a reviewer approves: the review shows them, a change of
// the list while a review is open opens a new one, and a list changed after the approval is refused at
// publish, so the help center keeps the files it had.
func TestAFileListChangedAfterTheApprovalNeedsANewApproval(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "KB Review Files", "admin@kb-review-files.example.com")
	reviewer := agentSession(t, api, admin, "reviewer@kb-review-files.example.com")
	reviewerID := agentID(t, api, reviewer)
	form := uploadKBFile(t, api, admin, "Warranty form.pdf", []byte("%PDF-1.4\n"+strings.Repeat("x", 100)))
	sheet := uploadKBFile(t, api, admin, "parts.csv", []byte("sku,name\nA-1,Kettle\n"))
	article := kbApprovalTranslationFixture(t, api, admin)
	path := "/api/v1/kb/articles/" + article.ID

	var saved kbDraftedArticle
	kbCall(t, api, http.MethodPatch, path, map[string]any{"updated_at": article.UpdatedAt, "body_md": "Within 30 days."}, admin, http.StatusOK, &saved)
	putKBAttachments(t, api, admin, path, []string{form.ID}, http.StatusOK)
	first := kbRequestReview(t, api, admin, article.ID, reviewerID)
	var detail kbReviewWithFiles
	kbCall(t, api, http.MethodGet, "/api/v1/kb/reviews/"+first.ID, nil, reviewer, http.StatusOK, &detail)
	if got := attachmentNames(detail.Proposed.Attachments); got != "Warranty form.pdf" {
		t.Fatalf("review shows files %q, want Warranty form.pdf", got)
	}

	putKBAttachments(t, api, admin, path, []string{form.ID, sheet.ID}, http.StatusOK)
	var states []string
	for _, review := range kbArticleReviews(t, api, admin, article.ID) {
		states = append(states, review.State)
	}
	if got := strings.Join(states, ","); got != "open,superseded" {
		t.Fatalf("reviews after the list changed = %s, want open,superseded", got)
	}
	second := kbArticleReviews(t, api, admin, article.ID)[0]
	kbDecide(t, api, reviewer, second.ID, "approve", map[string]any{}, http.StatusOK)

	withDraft := putKBAttachments(t, api, admin, path, []string{sheet.ID}, http.StatusOK)
	response := kbCall(t, api, http.MethodPost, path+"/draft/publish", map[string]any{"updated_at": withDraft.UpdatedAt}, admin, http.StatusConflict, nil)
	if code := errorCode(t, response); code != "approval_required" {
		t.Fatalf("publishing a file list changed after its approval: code=%s", code)
	}
	var live kbArticleWithFiles
	kbCall(t, api, http.MethodGet, path, nil, admin, http.StatusOK, &live)
	if len(live.Attachments) != 0 {
		t.Fatalf("help center files after the refusal = %q, want none", attachmentNames(live.Attachments))
	}
}

// A published translation's file list changes the help center at once, so in a collection that needs
// approval it is refused like an edit of its text.
func TestAPublishedTranslationsFileListNeedsAnApproval(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "KB Review Translation Files", "admin@kb-review-translation-files.example.com")
	reviewer := agentSession(t, api, admin, "reviewer@kb-review-translation-files.example.com")
	form := uploadKBFile(t, api, admin, "Formulaire.pdf", []byte("%PDF-1.4\n"+strings.Repeat("x", 100)))
	article := kbApprovalTranslationFixture(t, api, admin)

	var draft kbTranslationView
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles/"+article.ID+"/translations", map[string]any{"locale": "fr", "title": "Remboursements", "body_md": "Sous 14 jours."},
		admin, http.StatusCreated, &draft)
	var sent kbReviewView
	kbCall(t, api, http.MethodPost, kbTranslationPath(article.ID, "fr", "/reviews"), map[string]any{"reviewer_ids": []string{agentID(t, api, reviewer)}},
		admin, http.StatusCreated, &sent)
	kbDecide(t, api, reviewer, sent.ID, "approve", map[string]any{}, http.StatusOK)
	var published kbTranslationView
	kbCall(t, api, http.MethodGet, kbTranslationPath(article.ID, "fr"), nil, admin, http.StatusOK, &published)
	kbCall(t, api, http.MethodPatch, kbTranslationPath(article.ID, "fr"), map[string]any{"updated_at": published.UpdatedAt, "status": "published"}, admin, http.StatusOK, nil)

	response := kbCall(t, api, http.MethodPut, kbTranslationPath(article.ID, "fr", "/attachments"), map[string]any{"media_ids": []string{form.ID}}, admin, http.StatusConflict, nil)
	if code := errorCode(t, response); code != "approval_required" {
		t.Fatalf("listing a file under the live translation: code=%s", code)
	}
	var live struct {
		Attachments []kbAttachmentView `json:"attachments"`
	}
	kbCall(t, api, http.MethodGet, kbTranslationPath(article.ID, "fr"), nil, admin, http.StatusOK, &live)
	if len(live.Attachments) != 0 {
		t.Fatalf("translation files after the refusal = %q, want none", attachmentNames(live.Attachments))
	}
}

// A teammate who changed the files of a text wrote part of it, so they never review it.
func TestATeammateWhoChangedTheFilesDoesNotReviewThem(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "KB Review File Authors", "admin@kb-review-file-authors.example.com")
	reviewer := agentSession(t, api, admin, "reviewer@kb-review-file-authors.example.com")
	form := uploadKBFile(t, api, admin, "Warranty form.pdf", []byte("%PDF-1.4\n"+strings.Repeat("x", 100)))
	collection := kbApprovalCollection(t, api, admin)
	draft := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Refunds", "body_md": "Within 14 days."})

	putKBAttachments(t, api, reviewer, "/api/v1/kb/articles/"+draft.ID, []string{form.ID}, http.StatusOK)
	response := kbCall(t, api, http.MethodPost, "/api/v1/kb/articles/"+draft.ID+"/reviews", map[string]any{"reviewer_ids": []string{agentID(t, api, reviewer)}},
		admin, http.StatusBadRequest, nil)
	if code := errorCode(t, response); code != "invalid_request" {
		t.Fatalf("naming the teammate who listed the files: code=%s", code)
	}
}
