package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
)

// has_draft=true lists only the articles with unpublished changes, each marked has_draft.
func TestTheArticleListFiltersOnUnpublishedChanges(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "KB Draft List", "admin@kb-draft-list.example.com")
	live, _ := kbDraftedFixture(t, api, admin)
	kbArticle(t, api, admin, map[string]any{"collection_id": live.CollectionID, "title": "Shipping", "body_md": "Two days.", "status": "published"})

	var page struct {
		Items []kbDraftedArticle `json:"items"`
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles?has_draft=true", nil, admin, http.StatusOK, &page)
	if len(page.Items) != 1 || page.Items[0].ID != live.ID || !page.Items[0].HasDraft || page.Items[0].Title != "Refunds" {
		t.Fatalf("has_draft=true = %+v", page.Items)
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles", nil, admin, http.StatusOK, &page)
	if len(page.Items) != 2 {
		t.Fatalf("all articles = %+v", page.Items)
	}
	if bad := request(t, api, http.MethodGet, "/api/v1/kb/articles?has_draft=yes", nil, admin, nil); bad.Code != http.StatusBadRequest {
		t.Fatalf("has_draft=yes status=%d, want 400", bad.Code)
	}
}

// GET ?version=draft answers the article as publishing would make it; without a draft it is 404.
func TestVersionDraftReadsTheArticleAsItWillBePublished(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "KB Draft Read", "admin@kb-draft-read.example.com")
	live, saved := kbDraftedFixture(t, api, admin)

	var drafted kbDraftedArticle
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles/"+live.ID+"?version=draft", nil, admin, http.StatusOK, &drafted)
	if drafted.Title != "Refunds policy" || drafted.BodyMD != "Within 30 days." || drafted.Number != live.Number {
		t.Fatalf("version=draft = %+v", drafted.kbArticleView)
	}
	if bad := request(t, api, http.MethodGet, "/api/v1/kb/articles/"+live.ID+"?version=next", nil, admin, nil); bad.Code != http.StatusBadRequest {
		t.Fatalf("version=next status=%d, want 400", bad.Code)
	}
	kbCall(t, api, http.MethodDelete, "/api/v1/kb/articles/"+live.ID+"/draft?updated_at="+url.QueryEscape(saved.Draft.UpdatedAt), nil, admin, http.StatusOK, nil)
	if none := request(t, api, http.MethodGet, "/api/v1/kb/articles/"+live.ID+"?version=draft", nil, admin, nil); none.Code != http.StatusNotFound {
		t.Fatalf("version=draft without a draft status=%d, want 404", none.Code)
	}
}

// Another workspace can neither read, publish nor discard an article's draft.
func TestAnotherWorkspaceCannotReachADraft(t *testing.T) {
	api, service := testAPI(t)
	alpha := createSession(t, api, service, "KB Draft Alpha", "admin@kb-draft-alpha.example.com")
	beta := createSession(t, api, service, "KB Draft Beta", "admin@kb-draft-beta.example.com")
	live, saved := kbDraftedFixture(t, api, alpha)
	seen := saved.Draft.UpdatedAt

	for _, call := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/api/v1/kb/articles/" + live.ID + "?version=draft", nil},
		{http.MethodPost, "/api/v1/kb/articles/" + live.ID + "/draft/publish", map[string]any{"updated_at": seen}},
		{http.MethodDelete, "/api/v1/kb/articles/" + live.ID + "/draft?updated_at=" + url.QueryEscape(seen), nil},
	} {
		if response := request(t, api, call.method, call.path, call.body, beta, nil); response.Code != http.StatusNotFound {
			t.Errorf("beta %s %s status=%d body=%s, want 404", call.method, call.path, response.Code, response.Body.String())
		}
	}
	if got := kbGetArticle(t, api, alpha, live.ID); got.BodyMD != "Within 14 days." || got.Draft == nil || got.Draft.UpdatedAt != seen {
		t.Fatalf("alpha's article after beta's calls = %+v draft=%+v", got.kbArticleView, got.Draft)
	}
	var page struct {
		Items []kbDraftedArticle `json:"items"`
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles?has_draft=true", nil, beta, http.StatusOK, &page)
	if len(page.Items) != 0 {
		t.Fatalf("beta's has_draft list = %+v", page.Items)
	}
}

// Pressing Publish changes twice with one Idempotency-Key publishes once: the second answer is the first.
func TestARepeatedPublishWithTheSameKeyPublishesOnce(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "KB Draft Repeat", "admin@kb-draft-repeat.example.com")
	live, saved := kbDraftedFixture(t, api, admin)
	path := "/api/v1/kb/articles/" + live.ID + "/draft/publish"
	body := map[string]any{"updated_at": saved.Draft.UpdatedAt}

	first := postWithKey(t, api, admin, path, "publish-refunds", body)
	again := postWithKey(t, api, admin, path, "publish-refunds", body)
	if first.Code != http.StatusOK || again.Code != http.StatusOK {
		t.Fatalf("publish status=%d then %d: %s / %s", first.Code, again.Code, first.Body.String(), again.Body.String())
	}
	var one, two kbDraftedArticle
	if err := json.Unmarshal(first.Body.Bytes(), &one); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(again.Body.Bytes(), &two); err != nil {
		t.Fatal(err)
	}
	if one.BodyMD != "Within 30 days." || two.UpdatedAt != one.UpdatedAt || two.BodyMD != one.BodyMD {
		t.Fatalf("first = %+v, again = %+v", one.kbArticleView, two.kbArticleView)
	}
	if got := kbVersionKinds(t, api, admin, live.ID); got != "edit,draft,create" {
		t.Errorf("versions = %s, want edit, draft, create", got)
	}
}

// A translation shows the article it translates as the help center does: the live text, and the
// version of that text, not a later save of unpublished changes.
func TestATranslationShowsTheVersionOfTheLiveTextItTranslates(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "KB Draft Original", "admin@kb-draft-original.example.com")
	kbCall(t, api, http.MethodPut, "/api/v1/help-center/languages", map[string]any{"languages": []map[string]any{{"locale": "fr", "visible": false}}}, admin, http.StatusOK, nil)
	live, _ := kbDraftedFixture(t, api, admin)
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles/"+live.ID+"/translations", map[string]any{"locale": "fr", "title": "Remboursements", "body_md": "Sous 14 jours."}, admin, http.StatusCreated, nil)

	var translation kbTranslationView
	kbCall(t, api, http.MethodGet, kbTranslationPath(live.ID, "fr"), nil, admin, http.StatusOK, &translation)
	if translation.Original.BodyMD != "Within 14 days." || translation.Original.Version != 1 {
		t.Fatalf("original = %+v, want the live text at version 1", translation.Original)
	}
}
