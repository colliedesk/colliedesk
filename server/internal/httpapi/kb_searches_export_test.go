package httpapi_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"
)

// The archive keeps the help center searches: the masked words, the result count and the article
// opened from them, and nothing about the reader.
func TestTheExportKeepsTheHelpCenterSearches(t *testing.T) {
	api, service, pool, _ := testAPIWithStorage(t)
	d := newSearchSite(t, api, pool, createSession(t, api, service, "Search Export", "admin@search-export.example.com"))
	link := d.searchLinkFor(t, "password", "192.0.2.90")
	wantPage(t, visit(d.site, link, "192.0.2.90", "User-Agent", browserAgent), http.StatusFound, nil, nil)
	wantPage(t, visit(d.site, "/help/"+d.slug+"/search?q=ada@example.com", "192.0.2.90", "User-Agent", browserAgent), http.StatusOK, nil, nil)

	exported := request(t, api, http.MethodGet, "/api/v1/export", nil, d.admin, nil)
	if exported.Code != http.StatusOK {
		t.Fatalf("export status=%d body=%s", exported.Code, exported.Body.String())
	}
	entries := unzip(t, exported.Body.Bytes())
	searches := ndjsonLines(t, entries["kb-searches.ndjson"])
	if len(searches) != 2 {
		t.Fatalf("searches = %+v, want 2", searches)
	}
	first, second := searches[0], searches[1]
	if first["query"] != "password" || first["results"] != float64(1) || first["opened_article_id"] != d.article.ID || len(first) != 4 {
		t.Fatalf("first search = %+v, want query, results, opened_article_id and created_at only", first)
	}
	if second["query"] != "[email removed]" || second["results"] != float64(0) || second["opened_article_id"] != nil {
		t.Fatalf("second search = %+v, want the masked address, no result, nothing opened", second)
	}
	if at, err := time.Parse(time.RFC3339Nano, fmt.Sprint(first["created_at"])); err != nil || at.Location() != time.UTC {
		t.Fatalf("created_at = %v, want a UTC timestamp", first["created_at"])
	}
	var manifest struct {
		Counts map[string]int `json:"counts"`
	}
	if err := json.Unmarshal(entries["manifest.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Counts["kb_searches"] != 2 {
		t.Fatalf("manifest counts = %+v, want 2 searches", manifest.Counts)
	}
}
