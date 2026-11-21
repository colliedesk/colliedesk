package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
)

// kbApprovalTranslationFixture is a published article in a collection that then starts to need
// approval, with French listed on the help center.
func kbApprovalTranslationFixture(t *testing.T, api http.Handler, admin string) kbArticleView {
	t.Helper()
	collection := kbCollection(t, api, admin, map[string]any{"name": "Docs"})
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Refunds", "body_md": "Within 14 days.", "status": "published"})
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/collections/"+collection.ID, map[string]any{"updated_at": collection.UpdatedAt, "require_approval": "on"}, admin, http.StatusOK, nil)
	kbCall(t, api, http.MethodPut, "/api/v1/help-center/languages", map[string]any{"languages": []map[string]any{{"locale": "fr", "visible": true}}}, admin, http.StatusOK, nil)
	return article
}

// A translation in a collection that needs approval goes on the help center only with the text a
// teammate approved: created or switched to published without it, it is refused; once approved it
// publishes; a later edit of the live translation needs a new approval.
func TestATranslationThatNeedsApprovalPublishesOnlyTheApprovedText(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "KB Review Translations", "admin@kb-review-translations.example.com")
	reviewer := agentSession(t, api, admin, "reviewer@kb-review-translations.example.com")
	article := kbApprovalTranslationFixture(t, api, admin)

	response := kbCall(t, api, http.MethodPost, "/api/v1/kb/articles/"+article.ID+"/translations",
		map[string]any{"locale": "fr", "title": "Remboursements", "body_md": "Sous 14 jours.", "status": "published"}, admin, http.StatusConflict, nil)
	if code := errorCode(t, response); code != "approval_required" {
		t.Fatalf("creating a published translation: code=%s", code)
	}
	var draft kbTranslationView
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles/"+article.ID+"/translations", map[string]any{"locale": "fr", "title": "Remboursements", "body_md": "Sous 14 jours."},
		admin, http.StatusCreated, &draft)
	response = kbCall(t, api, http.MethodPatch, kbTranslationPath(article.ID, "fr"), map[string]any{"updated_at": draft.UpdatedAt, "status": "published"}, admin, http.StatusConflict, nil)
	if code := errorCode(t, response); code != "approval_required" {
		t.Fatalf("publishing the unapproved translation: code=%s", code)
	}

	var sent kbReviewView
	kbCall(t, api, http.MethodPost, kbTranslationPath(article.ID, "fr", "/reviews"), map[string]any{"reviewer_ids": []string{agentID(t, api, reviewer)}, "note": "Check the French"},
		admin, http.StatusCreated, &sent)
	if sent.Locale == nil || *sent.Locale != "fr" || sent.State != "open" {
		t.Fatalf("translation review = %+v", sent)
	}
	var listed struct {
		Reviews []kbReviewView `json:"reviews"`
	}
	kbCall(t, api, http.MethodGet, kbTranslationPath(article.ID, "fr", "/reviews"), nil, admin, http.StatusOK, &listed)
	if len(listed.Reviews) != 1 || listed.Reviews[0].ID != sent.ID || len(kbArticleReviews(t, api, admin, article.ID)) != 0 {
		t.Fatalf("translation reviews = %+v", listed.Reviews)
	}
	var detail kbReviewDetailView
	kbCall(t, api, http.MethodGet, "/api/v1/kb/reviews/"+sent.ID, nil, reviewer, http.StatusOK, &detail)
	if detail.Proposed.Title != "Remboursements" || detail.Proposed.BodyMD != "Sous 14 jours." || detail.Live != nil {
		t.Fatalf("translation review detail = %+v", detail)
	}
	kbDecide(t, api, admin, sent.ID, "approve", map[string]any{}, http.StatusForbidden)
	kbDecide(t, api, reviewer, sent.ID, "approve", map[string]any{"note": "Bon"}, http.StatusOK)

	var published kbTranslationView
	kbCall(t, api, http.MethodGet, kbTranslationPath(article.ID, "fr"), nil, admin, http.StatusOK, &published)
	kbCall(t, api, http.MethodPatch, kbTranslationPath(article.ID, "fr"), map[string]any{"updated_at": published.UpdatedAt, "status": "published"}, admin, http.StatusOK, &published)
	if published.Status != "published" {
		t.Fatalf("approved translation = %+v", published)
	}
	response = kbCall(t, api, http.MethodPatch, kbTranslationPath(article.ID, "fr"), map[string]any{"updated_at": published.UpdatedAt, "body_md": "Sous 30 jours."}, admin, http.StatusConflict, nil)
	if code := errorCode(t, response); code != "approval_required" {
		t.Fatalf("editing the live translation: code=%s", code)
	}
	// A translation has no draft beside its live text: the refusal says how to get a change reviewed.
	if body := response.Body.String(); !strings.Contains(body, "status draft") || !strings.Contains(body, "/translations/{locale}/reviews") {
		t.Fatalf("refusal of a live translation edit does not say how to review it: %s", body)
	}
	kbCall(t, api, http.MethodPatch, kbTranslationPath(article.ID, "fr"), map[string]any{"updated_at": published.UpdatedAt, "outdated": true}, admin, http.StatusOK, nil)

	var versions struct {
		Items []struct {
			Kind string `json:"kind"`
		} `json:"items"`
	}
	kbCall(t, api, http.MethodGet, kbTranslationPath(article.ID, "fr", "/versions"), nil, admin, http.StatusOK, &versions)
	var kinds []string
	for _, v := range versions.Items {
		kinds = append(kinds, v.Kind)
	}
	if got := strings.Join(kinds, ","); got != "mark_outdated,edit,review_approve,review_request,create" {
		t.Fatalf("translation history = %s", got)
	}
}

// Another workspace never reaches a translation's reviews.
func TestAnotherWorkspaceCannotReachATranslationsReviews(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "KB Review Translations Scope", "admin@kb-review-translations-scope.example.com")
	article := kbApprovalTranslationFixture(t, api, admin)
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles/"+article.ID+"/translations", map[string]any{"locale": "fr", "title": "Remboursements", "body_md": "Sous 14 jours."},
		admin, http.StatusCreated, nil)
	other := createSession(t, api, service, "KB Review Translations Other", "admin@kb-review-translations-other.example.com")
	otherReviewer := agentID(t, api, agentSession(t, api, other, "reviewer@kb-review-translations-other.example.com"))
	kbCall(t, api, http.MethodGet, kbTranslationPath(article.ID, "fr", "/reviews"), nil, other, http.StatusNotFound, nil)
	kbCall(t, api, http.MethodPost, kbTranslationPath(article.ID, "fr", "/reviews"), map[string]any{"reviewer_ids": []string{otherReviewer}}, other, http.StatusNotFound, nil)
}
