package httpapi_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestTheExportKeepsTheViewsOfAnArticleAndTheRepliesThatUsedIt(t *testing.T) {
	d := newViewsDesk(t, "views-export", nil)
	wantPage(t, visit(d.site, d.page, "192.0.2.30", "User-Agent", browserAgent), http.StatusOK, nil, nil)
	wantPage(t, visit(d.site, d.page, "192.0.2.31", "User-Agent", browserAgent), http.StatusOK, nil, nil)
	d.flush(t)
	contact := create(t, d.api, d.admin, "/api/v1/contacts", map[string]any{"name": "Ada Lovelace", "email": "ada@views-export.example"})
	conversation := create(t, d.api, d.admin, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Password"})
	reply(t, d.api, d.admin, conversation, fmt.Sprintf("Hi Ada, see https://%s%s", testOriginHost, d.page))

	exported := request(t, d.api, http.MethodGet, "/api/v1/export", nil, d.admin, nil)
	if exported.Code != http.StatusOK {
		t.Fatalf("export status=%d body=%s", exported.Code, exported.Body.String())
	}
	entries := unzip(t, exported.Body.Bytes())
	views := ndjsonLines(t, entries["kb-article-views.ndjson"])
	if len(views) != 1 || views[0]["article_id"] != d.article.ID || views[0]["locale"] != "en" || views[0]["views"] != float64(2) {
		t.Fatalf("views = %+v, want one day of this article in English with 2 views", views)
	}
	if _, err := time.Parse("2006-01-02", fmt.Sprint(views[0]["day"])); err != nil || len(views[0]) != 4 {
		t.Fatalf("views = %+v, want article_id, locale, a day written YYYY-MM-DD and views, nothing about the readers", views)
	}
	uses := ndjsonLines(t, entries["kb-article-uses.ndjson"])
	if len(uses) != 1 || uses[0]["article_id"] != d.article.ID || uses[0]["conversation_id"] != conversation || uses[0]["kind"] != "link" || uses[0]["message_id"] == nil {
		t.Fatalf("uses = %+v, want the reply that linked this article", uses)
	}
	if at, err := time.Parse(time.RFC3339Nano, fmt.Sprint(uses[0]["created_at"])); err != nil || at.Location() != time.UTC {
		t.Fatalf("use created_at = %v, want a UTC timestamp", uses[0]["created_at"])
	}
	var manifest struct {
		Counts map[string]int `json:"counts"`
	}
	if err := json.Unmarshal(entries["manifest.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Counts["kb_article_views"] != 1 || manifest.Counts["kb_article_uses"] != 1 {
		t.Fatalf("manifest counts = %+v, want 1 day of views and 1 use", manifest.Counts)
	}
}
