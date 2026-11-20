package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestOneAddressAddsAtMostTenReadersToAnArticleADayAndStillSeesThanks(t *testing.T) {
	api, service, pool, _ := testAPIWithStorage(t)
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Vote Cap", "admin@vote-cap.example.com")
	slug := workspaceSlug(t, api, admin)
	enableHelpCenter(t, api, admin, "Vote help", true)
	collection := kbCollection(t, api, admin, map[string]any{"name": "Guides"})
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Cap guide", "status": "published", "body_md": "a"})
	other := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Other guide", "status": "published", "body_md": "b"})
	page := fmt.Sprintf("/help/%s/a/%d-%s", slug, article.Number, article.Slug)
	otherPage := fmt.Sprintf("/help/%s/a/%d-%s", slug, other.Number, other.Slug)
	yes := url.Values{"helpful": {"yes"}}
	rows := func(articleID string) int {
		t.Helper()
		var n int
		if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM kb_article_votes WHERE article_id=$1`, articleID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	first := postVote(site, page+"/vote", yes, testOrigin, "192.0.2.80")
	wantSeeOther(t, first, page+"?voted=yes#helpful")
	reader := chatCookie(t, first)
	for i := 2; i <= 10; i++ {
		wantSeeOther(t, postVote(site, page+"/vote", yes, testOrigin, "192.0.2.80"), page+"?voted=yes#helpful")
	}
	// The eleventh new reader from the address gets the same thanks, and nothing is counted.
	over := postVote(site, page+"/vote", yes, testOrigin, "192.0.2.80")
	wantSeeOther(t, over, page+"?voted=yes#helpful")
	if over.Header().Get("Set-Cookie") == "" {
		t.Errorf("the vote over the cap set no reader cookie; a counted vote does")
	}
	if got := rows(article.ID); got != 10 {
		t.Fatalf("votes from one address = %d, want 10", got)
	}
	wantPage(t, visit(site, page, "192.0.2.80"), http.StatusOK, []string{"10 out of 10 found this helpful"}, nil)
	// A reader already counted still changes their vote.
	wantSeeOther(t, postVote(site, page+"/vote", url.Values{"helpful": {"no"}}, testOrigin, "192.0.2.80", reader), page+"?voted=no#helpful")
	wantPage(t, visit(site, page, "192.0.2.80"), http.StatusOK, []string{"9 out of 10 found this helpful"}, nil)
	// The cap is per article and per address.
	wantSeeOther(t, postVote(site, otherPage+"/vote", yes, testOrigin, "192.0.2.80"), otherPage+"?voted=yes#helpful")
	wantSeeOther(t, postVote(site, page+"/vote", yes, testOrigin, "192.0.2.81"), page+"?voted=yes#helpful")
	if got, others := rows(article.ID), rows(other.ID); got != 11 || others != 1 {
		t.Fatalf("votes = %d on the article and %d on the other, want 11 and 1", got, others)
	}
	// Addresses of one IPv6 /64 are one address.
	for i := 1; i <= 11; i++ {
		wantSeeOther(t, postVote(site, page+"/vote", yes, testOrigin, fmt.Sprintf("[2001:db8:1:2::%x]", i)), page+"?voted=yes#helpful")
	}
	if got := rows(article.ID); got != 21 {
		t.Fatalf("votes after eleven readers of one /64 = %d, want 21", got)
	}

	// Only a keyed hash of the address is kept, never the address itself.
	var hashed, plain int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FILTER (WHERE octet_length(address) = 32),
		count(*) FILTER (WHERE position(convert_to('192.0.2', 'UTF8') IN address) > 0 OR position(convert_to('2001:db8', 'UTF8') IN address) > 0)
		FROM kb_article_votes`).Scan(&hashed, &plain); err != nil {
		t.Fatal(err)
	}
	if hashed != 22 || plain != 0 {
		t.Fatalf("votes with a 32-byte address hash = %d (want 22), with the address in it = %d (want 0)", hashed, plain)
	}

	// A day later the address may add readers again.
	if _, err := pool.Exec(context.Background(), `UPDATE kb_article_votes SET created_at = created_at - interval '25 hours'`); err != nil {
		t.Fatal(err)
	}
	wantSeeOther(t, postVote(site, page+"/vote", yes, testOrigin, "192.0.2.80"), page+"?voted=yes#helpful")
	if got := rows(article.ID); got != 22 {
		t.Fatalf("votes after a day = %d, want 22", got)
	}
}

func TestVotesHaveTheirOwnLimitChargedOnlyOnceTheOriginPasses(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Vote Limit", "admin@vote-limit.example.com")
	slug := workspaceSlug(t, api, admin)
	enableHelpCenter(t, api, admin, "Vote help", true)
	collection := kbCollection(t, api, admin, map[string]any{"name": "Guides"})
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Limit guide", "status": "published", "body_md": "a"})
	page := fmt.Sprintf("/help/%s/a/%d-%s", slug, article.Number, article.Slug)
	yes := url.Values{"helpful": {"yes"}}

	// Searches spent to the last one leave the votes alone.
	for i := 0; i < 30; i++ {
		if response := visit(site, "/help/"+slug+"/search?q=guide", "192.0.2.90"); response.Code != http.StatusOK {
			t.Fatalf("search %d: %d", i+1, response.Code)
		}
	}
	if response := visit(site, "/help/"+slug+"/search?q=guide", "192.0.2.90"); response.Code != http.StatusTooManyRequests {
		t.Fatalf("31st search: %d, want 429", response.Code)
	}
	wantSeeOther(t, postVote(site, page+"/vote", yes, testOrigin, "192.0.2.90"), page+"?voted=yes#helpful")

	// Forms from another site are refused before they cost the address a vote.
	for i := 0; i < 25; i++ {
		if response := postVote(site, page+"/vote", yes, "https://attacker.example", "192.0.2.91"); response.Code != http.StatusForbidden {
			t.Fatalf("foreign vote %d: %d, want 403", i+1, response.Code)
		}
	}
	first := postVote(site, page+"/vote", yes, testOrigin, "192.0.2.91")
	wantSeeOther(t, first, page+"?voted=yes#helpful")
	reader := chatCookie(t, first)
	for i := 2; i <= 20; i++ {
		wantSeeOther(t, postVote(site, page+"/vote", yes, testOrigin, "192.0.2.91", reader), page+"?voted=yes#helpful")
	}
	busy := postVote(site, page+"/vote", yes, testOrigin, "192.0.2.91", reader)
	if busy.Code != http.StatusTooManyRequests || busy.Header().Get("Retry-After") == "" || !strings.Contains(busy.Header().Get("Cache-Control"), "no-store") {
		t.Fatalf("21st vote in a minute: %d %v, want 429 with Retry-After and no-store", busy.Code, busy.Header())
	}
}

func TestAVoteLeavesTheCachedPagesOfOtherArticlesFresh(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Vote Tags", "admin@vote-tags.example.com")
	slug := workspaceSlug(t, api, admin)
	enableHelpCenter(t, api, admin, "Vote help", true)
	collection := kbCollection(t, api, admin, map[string]any{"name": "Guides"})
	voted := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Voted guide", "status": "published", "body_md": "a"})
	quiet := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Quiet guide", "status": "published", "body_md": "b"})
	page := fmt.Sprintf("/help/%s/a/%d-%s", slug, voted.Number, voted.Slug)
	quietPage := fmt.Sprintf("/help/%s/a/%d-%s", slug, quiet.Number, quiet.Slug)
	votedTag := visit(site, page, "192.0.2.95").Header().Get("ETag")
	quietTag := visit(site, quietPage, "192.0.2.95").Header().Get("ETag")

	wantSeeOther(t, postVote(site, page+"/vote", url.Values{"helpful": {"yes"}}, testOrigin, "192.0.2.95"), page+"?voted=yes#helpful")

	if response := visit(site, quietPage, "192.0.2.95", "If-None-Match", quietTag); response.Code != http.StatusNotModified {
		t.Fatalf("the other article after a vote: %d, want 304", response.Code)
	}
	wantPage(t, visit(site, page, "192.0.2.95", "If-None-Match", votedTag), http.StatusOK, []string{"1 out of 1 found this helpful"}, nil)
}
