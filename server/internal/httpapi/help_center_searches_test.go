package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// searchSite is one help center with a published article, on an API that other workspaces share.
type searchSite struct {
	site    http.Handler
	admin   string
	slug    string
	id      string
	article kbArticleView
	page    string
}

func newSearchSite(t *testing.T, api http.Handler, pool *pgxpool.Pool, admin string) searchSite {
	t.Helper()
	d := searchSite{site: helpCenterSite(api), admin: admin, slug: workspaceSlug(t, api, admin)}
	if err := pool.QueryRow(context.Background(), `SELECT id FROM workspaces WHERE slug=$1`, d.slug).Scan(&d.id); err != nil {
		t.Fatal(err)
	}
	enableHelpCenter(t, api, admin, d.slug+" help", true)
	collection := kbCollection(t, api, admin, map[string]any{"name": "Guides"})
	d.article = kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Reset a password", "status": "published", "body_md": "Open Settings."})
	d.page = fmt.Sprintf("/help/%s/a/%d-%s", d.slug, d.article.Number, d.article.Slug)
	return d
}

var searchLink = regexp.MustCompile(`href="(/help/[^"]+/a/[^"?]+\?s=[^"]+)"`)

func (d searchSite) searchLinkFor(t *testing.T, query, client string) string {
	t.Helper()
	response := visit(d.site, "/help/"+d.slug+"/search?q="+query, client, "User-Agent", browserAgent)
	wantPage(t, response, http.StatusOK, nil, nil)
	match := searchLink.FindStringSubmatch(response.Body.String())
	if match == nil {
		t.Fatalf("no result link with a search in %s", response.Body.String())
	}
	return match[1]
}

func searchesOf(t *testing.T, pool *pgxpool.Pool, workspace string) (count int, opened []string) {
	t.Helper()
	rows, err := pool.Query(context.Background(), `SELECT COALESCE(opened_article_id::text, '') FROM kb_searches WHERE workspace_id=$1 ORDER BY id`, workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		count++
		if id != "" {
			opened = append(opened, id)
		}
	}
	return count, opened
}

// A reader's search is kept with its result count; its result links carry the search, and opening one
// records the article and moves to the article's canonical address, which carries no search.
func TestAReaderOpeningAnArticleFromASearchIsRecordedAndLandsOnTheCanonicalAddress(t *testing.T) {
	api, service, pool, _ := testAPIWithStorage(t)
	d := newSearchSite(t, api, pool, createSession(t, api, service, "Search Open", "admin@search-open.example.com"))
	search := visit(d.site, "/help/"+d.slug+"/search?q=password", "192.0.2.80", "User-Agent", browserAgent)
	wantPage(t, search, http.StatusOK, []string{d.page + "?s="}, nil)
	if got := search.Header().Get("Cache-Control"); got != "private, max-age=300" || search.Header().Get("ETag") != "" {
		t.Fatalf("logged search page Cache-Control=%q ETag=%q, want private, max-age=300 and no ETag", got, search.Header().Get("ETag"))
	}
	var query string
	var results int
	if err := pool.QueryRow(context.Background(), `SELECT query, results FROM kb_searches WHERE workspace_id=$1`, d.id).Scan(&query, &results); err != nil || query != "password" || results != 1 {
		t.Fatalf("kept search = %q %d %v, want password with 1 result", query, results, err)
	}
	link := searchLink.FindStringSubmatch(search.Body.String())[1]
	opened := visit(d.site, link, "192.0.2.80", "User-Agent", browserAgent)
	if opened.Code != http.StatusFound || opened.Header().Get("Location") != d.page || opened.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("open from search = %d to %q (%q), want 302 to %s with no-store", opened.Code, opened.Header().Get("Location"), opened.Header().Get("Cache-Control"), d.page)
	}
	if _, got := searchesOf(t, pool, d.id); len(got) != 1 || got[0] != d.article.ID {
		t.Fatalf("opened = %v, want the article %s", got, d.article.ID)
	}
	wantPage(t, visit(d.site, d.page, "192.0.2.80", "User-Agent", browserAgent), http.StatusOK, []string{`rel="canonical"`}, []string{"?s="})
}

// A bot, a prefetch and a signed-in team member neither keep a search nor open one.
func TestSearchesAndOpensThatAreNotAReadersAreNotKept(t *testing.T) {
	api, service, pool, _ := testAPIWithStorage(t)
	d := newSearchSite(t, api, pool, createSession(t, api, service, "Search Bots", "admin@search-bots.example.com"))
	for _, header := range [][]string{
		{"User-Agent", "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)"},
		{"User-Agent", browserAgent, "Sec-Purpose", "prefetch"},
		{"User-Agent", browserAgent, "Cookie", "colliedesk_session=" + d.admin},
	} {
		response := visit(d.site, "/help/"+d.slug+"/search?q=password", "192.0.2.81", header...)
		wantPage(t, response, http.StatusOK, []string{d.page}, []string{"?s="})
	}
	link := d.searchLinkFor(t, "password", "192.0.2.82")
	if prefetched := visit(d.site, link, "192.0.2.82", "User-Agent", browserAgent, "Sec-Purpose", "prefetch"); prefetched.Code != http.StatusFound {
		t.Fatalf("prefetched open = %d, want 302", prefetched.Code)
	}
	if count, opened := searchesOf(t, pool, d.id); count != 1 || len(opened) != 0 {
		t.Fatalf("searches = %d opened %v, want only the reader's search and nothing opened", count, opened)
	}
}

// A search link of one workspace, or a forged one, opens nothing anywhere.
func TestASearchLinkOfAnotherWorkspaceOrAForgedOneOpensNothing(t *testing.T) {
	api, service, pool, _ := testAPIWithStorage(t)
	a := newSearchSite(t, api, pool, createSession(t, api, service, "Search A", "admin@search-a.example.com"))
	b := newSearchSite(t, api, pool, createSession(t, api, service, "Search B", "admin@search-b.example.com"))
	link := a.searchLinkFor(t, "password", "192.0.2.83")
	token := link[len(a.page+"?s="):]
	forged := regexp.MustCompile(`\.[^.]+$`).ReplaceAllString(token, ".AAAAAAAAAAAAAAAA")
	for _, address := range []string{b.page + "?s=" + token, a.page + "?s=" + forged} {
		response := visit(a.site, address, "192.0.2.83", "User-Agent", browserAgent)
		if response.Code != http.StatusFound {
			t.Fatalf("%s = %d, want 302 to the canonical address", address, response.Code)
		}
	}
	for _, d := range []searchSite{a, b} {
		if _, opened := searchesOf(t, pool, d.id); len(opened) != 0 {
			t.Fatalf("workspace %s opened %v, want nothing", d.slug, opened)
		}
	}
}

// Past the searches a workspace keeps in a minute the page is the same, its links without a search.
func TestASearchPastTheWorkspaceCapIsAnsweredWithoutASearchInItsLinks(t *testing.T) {
	api, service, pool, _ := testAPIWithStorage(t)
	d := newSearchSite(t, api, pool, createSession(t, api, service, "Search Cap", "admin@search-cap.example.com"))
	if _, err := pool.Exec(context.Background(), `INSERT INTO kb_searches (workspace_id, query, query_key, results)
		SELECT $1, 'word', 'word', 0 FROM generate_series(1, 120)`, d.id); err != nil {
		t.Fatal(err)
	}
	wantPage(t, visit(d.site, "/help/"+d.slug+"/search?q=password", "192.0.2.84", "User-Agent", browserAgent), http.StatusOK,
		[]string{d.page, "Reset a password"}, []string{"?s="})
	if count, _ := searchesOf(t, pool, d.id); count != 120 {
		t.Fatalf("searches = %d, want the 120 kept before", count)
	}
}

// A search keeps the help center that served it: one on the second help center's domain records the
// second help center, one on the primary's domain records the primary.
func TestASearchOnASecondHelpCenterRecordsThatHelpCenter(t *testing.T) {
	f := newTwoHelpCenters(t)
	var primary string
	if err := f.pool.QueryRow(context.Background(), `SELECT primary_help_center_id($1)::text`, f.workspace).Scan(&primary); err != nil {
		t.Fatal(err)
	}
	for index, search := range []struct{ host, path, recorded string }{
		{hostHardware, "/search?q=copperline", f.hardware},
		{hostMain, "/search?q=bramble", primary},
	} {
		page := visit(f.site, search.path, fmt.Sprintf("192.0.2.%d", 120+index), "Host", search.host, "User-Agent", browserAgent)
		wantPage(t, page, http.StatusOK, nil, nil)
		var recorded string
		if err := f.pool.QueryRow(context.Background(), `SELECT help_center_id::text FROM kb_searches
			WHERE workspace_id=$1 ORDER BY id DESC LIMIT 1`, f.workspace).Scan(&recorded); err != nil {
			t.Fatalf("search %s%s: %v", search.host, search.path, err)
		}
		if recorded != search.recorded {
			t.Fatalf("search %s%s recorded help center %s, want %s", search.host, search.path, recorded, search.recorded)
		}
	}
}
