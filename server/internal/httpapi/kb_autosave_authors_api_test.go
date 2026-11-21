package httpapi_test

import (
	"net/http"
	"testing"
)

// A teammate who wrote part of the text only through autosaves is one of its authors: they are never
// named to review it.
func TestATeammateWhoOnlyAutosavedTheTextNeverReviewsIt(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "KB Autosave Author", "admin@kb-autosave-author.example.com")
	writer := agentSession(t, api, admin, "writer@kb-autosave-author.example.com")
	writerID := agentID(t, api, writer)
	collection := kbApprovalCollection(t, api, admin)
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Returns", "body_md": "By post."})

	kbAutosaveBody(t, api, writer, article.ID, "By post, within a month.")
	response := kbCall(t, api, http.MethodPost, "/api/v1/kb/articles/"+article.ID+"/reviews", map[string]any{"reviewer_ids": []string{writerID}}, admin, http.StatusBadRequest, nil)
	if code := errorCode(t, response); code != "invalid_request" {
		t.Fatalf("naming the teammate who autosaved the text: code=%s", code)
	}
}

func TestATeammateWhoOnlyAutosavedAPublishedDraftNeverReviewsIt(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "KB Autosave Author Live", "admin@kb-autosave-author-live.example.com")
	writer := agentSession(t, api, admin, "writer@kb-autosave-author-live.example.com")
	writerID := agentID(t, api, writer)
	collection := kbCollection(t, api, admin, map[string]any{"name": "Docs"})
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Returns", "body_md": "By post.", "status": "published"})
	kbAutosaveBody(t, api, writer, article.ID, "By post, within a month.")
	response := kbCall(t, api, http.MethodPost, "/api/v1/kb/articles/"+article.ID+"/reviews", map[string]any{"reviewer_ids": []string{writerID}}, admin, http.StatusBadRequest, nil)
	if code := errorCode(t, response); code != "invalid_request" {
		t.Fatalf("naming the teammate who autosaved the draft: code=%s", code)
	}
}

// An article written in part through autosaves is approved by another teammate and published, as any
// other: the autosaving writer is an author, not a reason to refuse.
func TestATextWithAnAutosavingWriterPublishesOnceAnotherTeammateApproves(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "KB Autosave Publish", "admin@kb-autosave-publish.example.com")
	writer := agentSession(t, api, admin, "writer@kb-autosave-publish.example.com")
	reviewer := agentSession(t, api, admin, "reviewer@kb-autosave-publish.example.com")
	collection := kbApprovalCollection(t, api, admin)
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Returns", "body_md": "By post."})
	kbAutosaveBody(t, api, writer, article.ID, "By post, within a month.")

	kbDecide(t, api, reviewer, kbRequestReview(t, api, writer, article.ID, agentID(t, api, reviewer)).ID, "approve", map[string]any{}, http.StatusOK)
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+article.ID, map[string]any{"updated_at": kbGetArticle(t, api, admin, article.ID).UpdatedAt, "status": "published"},
		writer, http.StatusOK, nil)
	if got := kbGetArticle(t, api, admin, article.ID); got.Status != "published" || got.BodyMD != "By post, within a month." {
		t.Fatalf("article after the approval = %s %q", got.Status, got.BodyMD)
	}
}

// Who wrote the text counts until it is published: once a teammate's words are on the help center,
// they may review the next change someone else makes.
func TestAnAutosavingWriterReviewsTheNextChangeAfterAPublish(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "KB Autosave Next", "admin@kb-autosave-next.example.com")
	writer := agentSession(t, api, admin, "writer@kb-autosave-next.example.com")
	reviewer := agentSession(t, api, admin, "reviewer@kb-autosave-next.example.com")
	collection := kbApprovalCollection(t, api, admin)
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Returns", "body_md": "By post."})
	kbAutosaveBody(t, api, writer, article.ID, "By post, within a month.")
	kbDecide(t, api, reviewer, kbRequestReview(t, api, admin, article.ID, agentID(t, api, reviewer)).ID, "approve", map[string]any{}, http.StatusOK)
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+article.ID, map[string]any{"updated_at": kbGetArticle(t, api, admin, article.ID).UpdatedAt, "status": "published"},
		admin, http.StatusOK, nil)

	kbEditBody(t, api, reviewer, article.ID, "By post or courier, within a month.")
	if review := kbRequestReview(t, api, reviewer, article.ID, agentID(t, api, writer)); len(review.Reviewers) != 1 {
		t.Fatalf("review of the next change = %+v", review)
	}
}
