package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// postVote posts a vote form to the help center the way a browser does, from origin ("" sends none).
func postVote(site http.Handler, path string, form url.Values, origin, client string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.RemoteAddr, req.Host = client+":4242", testOriginHost
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	return serve(site, req)
}

func wantSeeOther(t *testing.T, response *httptest.ResponseRecorder, location string) {
	t.Helper()
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != location {
		t.Fatalf("vote: %d to %q, want 303 to %q\n%s", response.Code, response.Header().Get("Location"), location, response.Body.String())
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("vote Cache-Control = %q, want no-store", got)
	}
}

func TestAReaderVotesWithoutScriptAndASecondVoteChangesTheFirst(t *testing.T) {
	api, service, pool, _ := testAPIWithStorage(t)
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Vote Post", "admin@vote-post.example.com")
	slug := workspaceSlug(t, api, admin)
	enableHelpCenter(t, api, admin, "Vote help", true)
	collection := kbCollection(t, api, admin, map[string]any{"name": "Guides"})
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Reset a password", "status": "published", "body_md": "Open Settings."})
	page := fmt.Sprintf("/help/%s/a/%d-%s", slug, article.Number, article.Slug)
	votes := func() (rows int, helpful bool, missing string) {
		t.Helper()
		if err := pool.QueryRow(context.Background(), `SELECT count(*) OVER (), helpful, missing FROM kb_article_votes WHERE article_id=$1`, article.ID).Scan(&rows, &helpful, &missing); err != nil && !strings.Contains(err.Error(), "no rows") {
			t.Fatal(err)
		}
		return rows, helpful, missing
	}

	before := visit(site, page, "192.0.2.61")
	wantPage(t, before, http.StatusOK, []string{`<form class="helpful-form" method="post" action="` + page + `/vote">`, `name="helpful" value="yes"`, `name="helpful" value="no"`}, nil)
	policy, tag := before.Header().Get("Content-Security-Policy"), before.Header().Get("ETag")

	yes := postVote(site, page+"/vote", url.Values{"helpful": {"yes"}}, testOrigin, "192.0.2.61")
	wantSeeOther(t, yes, page+"?voted=yes#helpful")
	if got := yes.Header().Get("Content-Security-Policy"); got != policy {
		t.Fatalf("the vote's CSP = %q, want the page's %q", got, policy)
	}
	reader := chatCookie(t, yes)
	if !strings.HasPrefix(reader.Name, "__Secure-") || reader.Path != "/help/"+slug || !reader.HttpOnly || !reader.Secure || reader.SameSite != http.SameSiteLaxMode {
		t.Fatalf("reader cookie = %+v, want __Secure-, Path /help/%s, HttpOnly, Secure, SameSite=Lax", reader, slug)
	}
	wantPage(t, visit(site, page+"?voted=yes", "192.0.2.61"), http.StatusOK, []string{"Thanks for letting us know.", "1 out of 1 found this helpful"}, []string{`class="helpful-form"`})
	// The count changed and no content did: the page cached before the vote is stale.
	wantPage(t, visit(site, page, "192.0.2.61", "If-None-Match", tag), http.StatusOK, []string{"1 out of 1 found this helpful"}, nil)

	wantSeeOther(t, postVote(site, page+"/vote", url.Values{"helpful": {"no"}}, testOrigin, "192.0.2.61", reader), page+"?voted=no#helpful")
	wantPage(t, visit(site, page+"?voted=no", "192.0.2.61"), http.StatusOK,
		[]string{"What was missing?", `<textarea`, `name="missing"`, `maxlength="500"`, "0 out of 1 found this helpful"}, nil)
	line := url.Values{"helpful": {"no"}, "missing": {"Step 2 is wrong, write to jane.doe@example.com or call 0912 345 678"}}
	wantSeeOther(t, postVote(site, page+"/vote", line, testOrigin, "192.0.2.61", reader), page+"?voted=sent#helpful")
	wantPage(t, visit(site, page+"?voted=sent", "192.0.2.61"), http.StatusOK, []string{"Thanks, we will use it to improve this article."}, nil)
	if rows, helpful, missing := votes(); rows != 1 || helpful || missing != "Step 2 is wrong, write to [email removed] or call [number removed]" {
		t.Fatalf("votes = %d rows, helpful %v, missing %q; want one No with the line masked", rows, helpful, missing)
	}

	// Another reader is another vote.
	wantSeeOther(t, postVote(site, page+"/vote", url.Values{"helpful": {"yes"}}, testOrigin, "192.0.2.62"), page+"?voted=yes#helpful")
	wantPage(t, visit(site, page, "192.0.2.62"), http.StatusOK, []string{"1 out of 2 found this helpful"}, nil)
	// Any other query still moves to the article's address.
	if response := visit(site, page+"?voted=maybe", "192.0.2.62"); response.Code != http.StatusMovedPermanently {
		t.Fatalf("?voted=maybe: %d, want 301", response.Code)
	}
}

func TestAVoteIsRefusedFromAnotherSiteAndOnlyTheVoteAddressTakesAPost(t *testing.T) {
	api, service, pool, _ := testAPIWithStorage(t)
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Vote Refused", "admin@vote-refused.example.com")
	slug := workspaceSlug(t, api, admin)
	enableHelpCenter(t, api, admin, "Vote help", true)
	collection := kbCollection(t, api, admin, map[string]any{"name": "Guides"})
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Export data", "status": "published", "body_md": "a"})
	draft := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Not yet", "status": "draft", "body_md": "b"})
	page := fmt.Sprintf("/help/%s/a/%d-%s", slug, article.Number, article.Slug)
	yes := url.Values{"helpful": {"yes"}}

	for name, origin := range map[string]string{"no Origin": "", "another site": "https://attacker.example", "another scheme": "http://" + testOriginHost} {
		if response := postVote(site, page+"/vote", yes, origin, "192.0.2.63"); response.Code != http.StatusForbidden {
			t.Errorf("%s: %d, want 403", name, response.Code)
		}
	}
	if response := postVote(site, page+"/vote", url.Values{"helpful": {"maybe"}}, testOrigin, "192.0.2.63"); response.Code != http.StatusBadRequest {
		t.Errorf("helpful=maybe: %d, want 400", response.Code)
	}
	if response := postVote(site, fmt.Sprintf("/help/%s/a/%d-%s/vote", slug, draft.Number, draft.Slug), yes, testOrigin, "192.0.2.63"); response.Code != http.StatusNotFound {
		t.Errorf("a draft: %d, want 404", response.Code)
	}
	for _, path := range []string{page, "/help/" + slug + "/search"} {
		if response := postVote(site, path, yes, testOrigin, "192.0.2.63"); response.Code != http.StatusMethodNotAllowed {
			t.Errorf("POST %s: %d, want 405", path, response.Code)
		}
	}
	var rows int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM kb_article_votes`).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("votes stored = %d (%v), want none", rows, err)
	}
}
