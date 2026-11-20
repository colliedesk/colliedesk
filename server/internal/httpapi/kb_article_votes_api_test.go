package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"testing"
)

type kbVotesView struct {
	ArticleID  string `json:"article_id"`
	Helpful    int    `json:"helpful"`
	NotHelpful int    `json:"not_helpful"`
	Imported   struct {
		Helpful    int `json:"helpful"`
		NotHelpful int `json:"not_helpful"`
	} `json:"imported"`
	Votes []struct {
		Helpful bool    `json:"helpful"`
		Missing *string `json:"missing"`
		Contact *struct {
			Name string `json:"name"`
		} `json:"contact"`
	} `json:"votes"`
	Resets []struct {
		Helpful    int `json:"helpful"`
		NotHelpful int `json:"not_helpful"`
		ResetBy    *struct {
			Name string `json:"name"`
		} `json:"reset_by"`
		Token *struct {
			Label string `json:"label"`
		} `json:"token"`
	} `json:"resets"`
}

func TestTheTeamReadsAnArticlesVotesAndAResetKeepsTheEarlierCounts(t *testing.T) {
	api, service, pool, _ := testAPIWithStorage(t)
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Vote Read", "admin@vote-read.example.com")
	slug := workspaceSlug(t, api, admin)
	enableHelpCenter(t, api, admin, "Vote help", true)
	collection := kbCollection(t, api, admin, map[string]any{"name": "Guides"})
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Change a plan", "status": "published", "body_md": "Open Billing."})
	if _, err := pool.Exec(context.Background(), `UPDATE kb_articles SET source_meta='{"freshdesk_id": 7, "thumbs_up": 12, "thumbs_down": 3}' WHERE id=$1`, article.ID); err != nil {
		t.Fatal(err)
	}
	page := fmt.Sprintf("/help/%s/a/%d-%s", slug, article.Number, article.Slug)
	no := postVote(site, page+"/vote", url.Values{"helpful": {"no"}, "missing": {"Where is the annual plan? Mail me at sam@example.org"}}, testOrigin, "192.0.2.70")
	wantSeeOther(t, no, page+"?voted=sent#helpful")
	reader := chatCookie(t, no)
	wantSeeOther(t, postVote(site, page+"/vote", url.Values{"helpful": {"yes"}}, testOrigin, "192.0.2.71"), page+"?voted=yes#helpful")
	path := "/api/v1/kb/articles/" + article.ID + "/votes"

	var votes kbVotesView
	kbCall(t, api, http.MethodGet, path, nil, admin, http.StatusOK, &votes)
	if votes.ArticleID != article.ID || votes.Helpful != 13 || votes.NotHelpful != 4 || votes.Imported.Helpful != 12 || votes.Imported.NotHelpful != 3 {
		t.Fatalf("counts = %+v, want 13 helpful, 4 not, 12/3 imported", votes)
	}
	if len(votes.Votes) != 2 || !votes.Votes[0].Helpful || votes.Votes[0].Missing != nil || votes.Votes[1].Helpful ||
		votes.Votes[1].Missing == nil || *votes.Votes[1].Missing != "Where is the annual plan? Mail me at [email removed]" ||
		votes.Votes[0].Contact != nil || votes.Votes[1].Contact != nil || len(votes.Resets) != 0 {
		t.Fatalf("votes = %+v, want the Yes, then the anonymous No with its masked line, no reset", votes)
	}

	read := issueToken(t, api, admin, "Reader", "read").Token
	write := issueToken(t, api, admin, "Writer", "send").Token
	kbCall(t, api, http.MethodGet, path, nil, read, http.StatusOK, nil)
	refused(t, api, http.MethodPost, path+"/reset", nil, read, "kb_articles:update")

	var reset kbVotesView
	kbCall(t, api, http.MethodPost, path+"/reset", nil, write, http.StatusOK, &reset)
	if reset.Helpful != 0 || reset.NotHelpful != 0 || reset.Imported.Helpful != 0 || reset.Imported.NotHelpful != 0 || len(reset.Votes) != 0 {
		t.Fatalf("after reset = %+v, want every count 0 and no current vote", reset)
	}
	if len(reset.Resets) != 1 || reset.Resets[0].Helpful != 13 || reset.Resets[0].NotHelpful != 4 || reset.Resets[0].Token == nil || reset.Resets[0].Token.Label != "Writer" {
		t.Fatalf("resets = %+v, want one keeping 13 and 4, by the Writer token", reset.Resets)
	}
	// A repeat of the press finds nothing counted and stores no second reset.
	kbCall(t, api, http.MethodPost, path+"/reset", nil, write, http.StatusOK, &reset)
	if len(reset.Resets) != 1 {
		t.Fatalf("resets after a repeat = %+v, want still the one", reset.Resets)
	}
	wantPage(t, visit(site, page, "192.0.2.70"), http.StatusOK, []string{"Was this article helpful?"}, []string{"found this helpful"})
	// The reader who voted before votes again; the imported votes stay out.
	wantSeeOther(t, postVote(site, page+"/vote", url.Values{"helpful": {"yes"}}, testOrigin, "192.0.2.70", reader), page+"?voted=yes#helpful")
	wantPage(t, visit(site, page, "192.0.2.70"), http.StatusOK, []string{"1 out of 1 found this helpful"}, nil)

	kbCall(t, api, http.MethodPost, path+"/reset", nil, admin, http.StatusOK, &reset)
	if len(reset.Resets) != 2 || reset.Resets[0].Helpful != 1 || reset.Resets[0].NotHelpful != 0 || reset.Resets[0].ResetBy == nil || reset.Resets[0].Token != nil || reset.Resets[1].Helpful != 13 {
		t.Fatalf("resets = %+v, want the admin's (1, 0) first, then the token's (13, 4)", reset.Resets)
	}
}

func TestAnArticlesVotesStayInItsWorkspace(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "Vote Own", "admin@vote-own.example.com")
	other := createSession(t, api, service, "Vote Other", "admin@vote-other.example.com")
	collection := kbCollection(t, api, admin, map[string]any{"name": "Guides"})
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Own guide", "status": "published", "body_md": "a"})
	path := "/api/v1/kb/articles/" + article.ID + "/votes"

	kbCall(t, api, http.MethodGet, path, nil, other, http.StatusNotFound, nil)
	kbCall(t, api, http.MethodPost, path+"/reset", nil, other, http.StatusNotFound, nil)
	var votes kbVotesView
	kbCall(t, api, http.MethodGet, path, nil, admin, http.StatusOK, &votes)
	if len(votes.Resets) != 0 {
		t.Fatalf("another workspace's reset reached this article: %+v", votes.Resets)
	}
	for _, missing := range []string{"/api/v1/kb/articles/00000000-0000-4000-8000-000000000000/votes", "/api/v1/kb/articles/not-an-id/votes"} {
		kbCall(t, api, http.MethodGet, missing, nil, admin, http.StatusNotFound, nil)
	}
	// An article in the trash keeps its votes to read, and is not reset.
	kbCall(t, api, http.MethodDelete, "/api/v1/kb/articles/"+article.ID+"?updated_at="+url.QueryEscape(article.UpdatedAt), nil, admin, http.StatusNoContent, nil)
	kbCall(t, api, http.MethodGet, path, nil, admin, http.StatusOK, nil)
	kbCall(t, api, http.MethodPost, path+"/reset", nil, admin, http.StatusNotFound, nil)
}

func TestARepeatedResetPressStoresOneReset(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Vote Press", "admin@vote-press.example.com")
	slug := workspaceSlug(t, api, admin)
	enableHelpCenter(t, api, admin, "Press help", true)
	collection := kbCollection(t, api, admin, map[string]any{"name": "Guides"})
	first := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "First guide", "status": "published", "body_md": "a"})
	second := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Second guide", "status": "published", "body_md": "b"})
	page := fmt.Sprintf("/help/%s/a/%d-%s", slug, first.Number, first.Slug)
	wantSeeOther(t, postVote(site, page+"/vote", url.Values{"helpful": {"yes"}}, testOrigin, "192.0.2.80"), page+"?voted=yes#helpful")

	path := "/api/v1/kb/articles/" + first.ID + "/votes/reset"
	for range 2 {
		response := postWithKey(t, api, admin, path, "reset-press-1", nil)
		if response.Code != http.StatusOK {
			t.Fatalf("reset status=%d body=%s, want 200", response.Code, response.Body.String())
		}
		var votes kbVotesView
		decode(t, response, &votes)
		if len(votes.Resets) != 1 || votes.Resets[0].Helpful != 1 {
			t.Fatalf("resets = %+v, want the one reset keeping 1 helpful", votes.Resets)
		}
	}
	reused := postWithKey(t, api, admin, "/api/v1/kb/articles/"+second.ID+"/votes/reset", "reset-press-1", nil)
	if reused.Code != http.StatusUnprocessableEntity || errorCode(t, reused) != "idempotency_key_reused" {
		t.Fatalf("the key on another article: status=%d body=%s, want 422 idempotency_key_reused", reused.Code, reused.Body.String())
	}
}
