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
// publishes; a later edit waits in its draft, and publishing that draft needs an approval of its text.
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
	// An edit of the published translation waits in its draft; publishing the draft needs an approval of its text.
	var saved kbDraftedTranslation
	kbCall(t, api, http.MethodPatch, kbTranslationPath(article.ID, "fr"), map[string]any{"updated_at": published.UpdatedAt, "body_md": "Sous 30 jours."}, admin, http.StatusOK, &saved)
	if saved.BodyMD != "Sous 14 jours." || saved.Draft == nil || saved.Draft.BodyMD != "Sous 30 jours." {
		t.Fatalf("edit of the published translation = %+v draft=%+v", saved.kbTranslationView, saved.Draft)
	}
	publishDraft := func(want int) *kbDraftedTranslation {
		t.Helper()
		var answer kbDraftedTranslation
		response := kbCall(t, api, http.MethodPost, kbTranslationPath(article.ID, "fr", "/draft/publish"), map[string]any{"updated_at": saved.UpdatedAt}, admin, want, &answer)
		if want == http.StatusConflict {
			if body := response.Body.String(); errorCode(t, response) != "approval_required" || !strings.Contains(body, "/translations/{locale}/draft/publish") ||
				!strings.Contains(body, "/translations/{locale}/reviews") {
				t.Fatalf("refusal of an unapproved translation draft does not say how to review it: %s", body)
			}
		}
		return &answer
	}
	publishDraft(http.StatusConflict)

	var second kbReviewView
	kbCall(t, api, http.MethodPost, kbTranslationPath(article.ID, "fr", "/reviews"), map[string]any{"reviewer_ids": []string{agentID(t, api, reviewer)}}, admin, http.StatusCreated, &second)
	kbCall(t, api, http.MethodGet, "/api/v1/kb/reviews/"+second.ID, nil, reviewer, http.StatusOK, &detail)
	if detail.Proposed.BodyMD != "Sous 30 jours." || detail.Live == nil || detail.Live.BodyMD != "Sous 14 jours." {
		t.Fatalf("review of the translation draft = %+v live=%+v", detail.Proposed, detail.Live)
	}
	// A further edit supersedes the open review and opens one of the new text for the same reviewer.
	kbCall(t, api, http.MethodPatch, kbTranslationPath(article.ID, "fr"), map[string]any{"updated_at": saved.UpdatedAt, "body_md": "Sous 45 jours."}, admin, http.StatusOK, &saved)
	kbCall(t, api, http.MethodGet, "/api/v1/kb/reviews/"+second.ID, nil, admin, http.StatusOK, &detail)
	if detail.State != "superseded" {
		t.Fatalf("review after an edit of the draft = %s, want superseded", detail.State)
	}
	kbCall(t, api, http.MethodGet, kbTranslationPath(article.ID, "fr", "/reviews"), nil, admin, http.StatusOK, &listed)
	if len(listed.Reviews) != 3 || listed.Reviews[0].State != "open" {
		t.Fatalf("translation reviews after the edit = %+v", listed.Reviews)
	}
	kbDecide(t, api, reviewer, listed.Reviews[0].ID, "approve", map[string]any{}, http.StatusOK)

	// An edit after the approval is not approved; the approved text publishes.
	kbCall(t, api, http.MethodPatch, kbTranslationPath(article.ID, "fr"), map[string]any{"updated_at": saved.UpdatedAt, "body_md": "Sous 60 jours."}, admin, http.StatusOK, &saved)
	publishDraft(http.StatusConflict)
	kbCall(t, api, http.MethodPatch, kbTranslationPath(article.ID, "fr"), map[string]any{"updated_at": saved.UpdatedAt, "body_md": "Sous 45 jours."}, admin, http.StatusOK, &saved)
	if live := publishDraft(http.StatusOK); live.BodyMD != "Sous 45 jours." || live.HasDraft {
		t.Fatalf("published approved draft = %+v", live.kbTranslationView)
	} else {
		published = live.kbTranslationView
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
	if got := strings.Join(kinds, ","); got != "mark_outdated,edit,draft,draft,review_approve,draft,review_request,draft,edit,review_approve,review_request,create" {
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
