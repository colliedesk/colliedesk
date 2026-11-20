package httpapi_test

import (
	"net/http"
	"net/url"
	"testing"
)

// kbListedUpdatedAt is the updated_at the article list shows for id.
func kbListedUpdatedAt(t *testing.T, api http.Handler, token, id string) string {
	t.Helper()
	var page struct {
		Items []kbArticleView `json:"items"`
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles?q=Refunds", nil, token, http.StatusOK, &page)
	for _, item := range page.Items {
		if item.ID == id {
			return item.UpdatedAt
		}
	}
	t.Fatalf("article %s not in the list %+v", id, page.Items)
	return ""
}

// While an article has unpublished changes, the updated_at the list, the article and its draft
// version show is one token, and it works for the next write whatever that write is: deleting,
// taking out of the trash, saving, publishing or discarding.
func TestEveryReadOfAnArticleWithADraftShowsTheTokenItsNextWriteNeeds(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "KB Token", "admin@kb-token.example.com")
	live, _ := kbDraftedFixture(t, api, admin)

	listed := kbListedUpdatedAt(t, api, admin, live.ID)
	kbCall(t, api, http.MethodDelete, "/api/v1/kb/articles/"+live.ID+"?updated_at="+url.QueryEscape(listed), nil, admin, http.StatusNoContent, nil)

	trashed := kbGetArticle(t, api, admin, live.ID)
	var back kbDraftedArticle
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles/"+live.ID+"/restore", map[string]any{"updated_at": trashed.UpdatedAt}, admin, http.StatusOK, &back)
	if !back.HasDraft || back.Draft == nil {
		t.Fatalf("back from the trash without its draft: %+v", back.kbArticleView)
	}

	got := kbGetArticle(t, api, admin, live.ID)
	var saved kbDraftedArticle
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+live.ID, map[string]any{"updated_at": got.UpdatedAt, "body_md": "Within 45 days."}, admin, http.StatusOK, &saved)
	if saved.BodyMD != "Within 14 days." || saved.Draft == nil || saved.Draft.BodyMD != "Within 45 days." {
		t.Fatalf("saved = %+v draft=%+v", saved.kbArticleView, saved.Draft)
	}

	var drafted kbArticleView
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles/"+live.ID+"?version=draft", nil, admin, http.StatusOK, &drafted)
	if drafted.UpdatedAt != saved.UpdatedAt {
		t.Errorf("draft version updated_at = %s, article's = %s; want one token", drafted.UpdatedAt, saved.UpdatedAt)
	}
	var published kbDraftedArticle
	kbPublishDraft(t, api, admin, live.ID, drafted.UpdatedAt, &published)
	if published.BodyMD != "Within 45 days." || published.HasDraft {
		t.Fatalf("published = %+v", published)
	}

	// A draft saved, then the article trashed and taken out: the draft version shows the token the
	// discard needs.
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+live.ID, map[string]any{"updated_at": published.UpdatedAt, "body_md": "Within 60 days."}, admin, http.StatusOK, &saved)
	kbCall(t, api, http.MethodDelete, "/api/v1/kb/articles/"+live.ID+"?updated_at="+url.QueryEscape(saved.UpdatedAt), nil, admin, http.StatusNoContent, nil)
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles/"+live.ID+"/restore", map[string]any{"updated_at": kbGetArticle(t, api, admin, live.ID).UpdatedAt}, admin, http.StatusOK, nil)
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles/"+live.ID+"?version=draft", nil, admin, http.StatusOK, &drafted)
	var discarded kbDraftedArticle
	kbCall(t, api, http.MethodDelete, "/api/v1/kb/articles/"+live.ID+"/draft?updated_at="+url.QueryEscape(drafted.UpdatedAt), nil, admin, http.StatusOK, &discarded)
	if discarded.BodyMD != "Within 45 days." || discarded.HasDraft {
		t.Fatalf("discarded = %+v", discarded)
	}
}
