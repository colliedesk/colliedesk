package httpapi_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

type kbArticleStatsView struct {
	ArticleID string `json:"article_id"`
	Timezone  string `json:"timezone"`
	From      string `json:"from"`
	To        string `json:"to"`
	FirstDay  string `json:"first_day"`
	LastDay   string `json:"last_day"`
	Views     int64  `json:"views"`
	Uses      int64  `json:"uses"`
	Days      []struct {
		Day   string `json:"day"`
		Views int64  `json:"views"`
		Uses  int64  `json:"uses"`
	} `json:"days"`
	Replies []struct {
		ConversationID string   `json:"conversation_id"`
		DisplayRef     string   `json:"display_ref"`
		Subject        string   `json:"subject"`
		MessageID      string   `json:"message_id"`
		Kinds          []string `json:"kinds"`
		At             string   `json:"at"`
	} `json:"replies"`
	RepliesTotal int `json:"replies_total"`
}

// An article's views and uses are read per calendar day in the workspace's timezone (Tokyo, where
// 15:00 UTC is already the next day), for the last 30 days unless a range is given, and only by the
// article's own workspace.
func TestAnArticlesStatsCountItsViewsAndTheRepliesThatUsedItPerDay(t *testing.T) {
	now := time.Date(2026, 11, 19, 15, 30, 0, 0, time.UTC) // 2026-11-20 00:30 in Tokyo
	api, service, pool, _ := testAPIWithConfig(t, func(config *httpapi.Config) { config.Now = func() time.Time { return now } })
	admin := createSession(t, api, service, "Stats Desk", "admin@stats.example.com")
	other := createSession(t, api, service, "Other Stats Desk", "admin@other-stats.example.com")
	workspace := workspaceID(t, api, admin)
	seedSQL(t, pool, `UPDATE workspaces SET timezone='Asia/Tokyo' WHERE id=$1`, workspace)
	collection := kbCollection(t, api, admin, map[string]any{"name": "Guides"})
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Clear a paper jam", "status": "published", "body_md": "Open tray two."})
	neighbour := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Join the wifi", "status": "published", "body_md": "Pick the network."})
	for _, row := range []struct {
		article, day string
		views        int
	}{{article.ID, "2026-11-20", 5}, {article.ID, "2026-10-22", 3}, {article.ID, "2026-10-21", 50}, {neighbour.ID, "2026-11-20", 7}} {
		seedSQL(t, pool, `INSERT INTO kb_article_views(workspace_id,kb_article_id,locale,day,views) VALUES($1,$2,'en',$3,$4)`, workspace, row.article, row.day, row.views)
	}
	contact := queryString(t, pool, `INSERT INTO contacts(workspace_id,name,email) VALUES($1,'Casey','casey@customer.example') RETURNING id::text`, workspace)
	conversation := openConversationAt(t, pool, workspace, contact, "2026-10-01T00:00:00Z", "open", false)
	ref := queryString(t, pool, `SELECT display_ref FROM conversations WHERE id=$1`, conversation)
	// This morning in Tokyo, one reply both linked the article and was an AI draft quoting it; the other
	// was sent on 21 October in Tokyo, a minute before it became the 22nd.
	for _, sent := range []string{"2026-11-19T15:10:00Z", "2026-10-21T14:59:00Z"} {
		message := queryString(t, pool, `INSERT INTO messages(workspace_id,conversation_id,kind,body,created_at) VALUES($1,$2,'outgoing','See the guide',$3) RETURNING id::text`, workspace, conversation, sent)
		kinds := []string{"link"}
		if sent[:10] == "2026-11-19" {
			kinds = []string{"link", "ai_source"}
		}
		for _, kind := range kinds {
			seedSQL(t, pool, `INSERT INTO kb_article_uses(workspace_id,kb_article_id,conversation_id,message_id,kind,created_at) VALUES($1,$2,$3,$4,$5,$6)`, workspace, article.ID, conversation, message, kind, sent)
		}
	}

	path := "/api/v1/kb/articles/" + article.ID + "/stats"
	read := issueToken(t, api, admin, "Reader", "read").Token
	var stats kbArticleStatsView
	kbCall(t, api, http.MethodGet, path, nil, read, http.StatusOK, &stats)
	if stats.ArticleID != article.ID || stats.Timezone != "Asia/Tokyo" || stats.From != "2026-10-21T15:00:00Z" || stats.To != "2026-11-20T15:00:00Z" || stats.FirstDay != "2026-10-22" || stats.LastDay != "2026-11-20" {
		t.Fatalf("range = %s %s..%s (%s..%s), want Asia/Tokyo 2026-10-21T15:00:00Z..2026-11-20T15:00:00Z (2026-10-22..2026-11-20)", stats.Timezone, stats.From, stats.To, stats.FirstDay, stats.LastDay)
	}
	if stats.Views != 8 || stats.Uses != 1 || len(stats.Days) != 30 {
		t.Fatalf("views %d uses %d days %d, want 8, 1, 30", stats.Views, stats.Uses, len(stats.Days))
	}
	if stats.Days[0].Day != "2026-10-22" || stats.Days[0].Views != 3 || stats.Days[29].Day != "2026-11-20" || stats.Days[29].Views != 5 || stats.Days[29].Uses != 1 || stats.Days[1].Views != 0 {
		t.Fatalf("days = %+v", stats.Days)
	}
	if stats.RepliesTotal != 1 || len(stats.Replies) != 1 {
		t.Fatalf("replies = %+v (total %d), want 1", stats.Replies, stats.RepliesTotal)
	}
	used := stats.Replies[0]
	if used.ConversationID != conversation || used.DisplayRef == "" || used.DisplayRef != ref || used.Subject != "Help" || used.At != "2026-11-19T15:10:00Z" || len(used.Kinds) != 2 || used.Kinds[0] != "ai_source" || used.Kinds[1] != "link" {
		t.Fatalf("reply = %+v, want conversation %s (%s) \"Help\" at 2026-11-19T15:10:00Z, kinds [ai_source link]", used, conversation, ref)
	}

	// A range is calendar days in Tokyo, the last one excluded.
	kbCall(t, api, http.MethodGet, path+"?from=2026-10-21&to=2026-10-22", nil, read, http.StatusOK, &stats)
	if stats.From != "2026-10-20T15:00:00Z" || stats.To != "2026-10-21T15:00:00Z" || stats.Views != 50 || stats.Uses != 1 || len(stats.Days) != 1 || stats.RepliesTotal != 1 {
		t.Fatalf("21 October = %+v, want 50 views and 1 use", stats)
	}
	for _, query := range []string{"?from=2026-11-01&to=2026-11-01", "?from=2026-11-01", "?to=2026-11-02", "?from=2024-01-01&to=2026-01-02"} {
		kbCall(t, api, http.MethodGet, path+query, nil, read, http.StatusBadRequest, nil)
	}

	// The article itself carries the last 30 days.
	var got struct {
		Views30d *int64 `json:"views_30d"`
		Uses30d  *int64 `json:"uses_30d"`
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles/"+article.ID, nil, admin, http.StatusOK, &got)
	if got.Views30d == nil || *got.Views30d != 8 || got.Uses30d == nil || *got.Uses30d != 1 {
		t.Fatalf("article views_30d %v uses_30d %v, want 8 and 1", got.Views30d, got.Uses30d)
	}

	// Another workspace's article is not found.
	kbCall(t, api, http.MethodGet, path, nil, other, http.StatusNotFound, nil)
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles/not-an-id/stats", nil, admin, http.StatusNotFound, nil)
}
