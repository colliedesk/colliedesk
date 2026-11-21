package httpapi_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/jackc/pgx/v5/pgxpool"
)

type helpCenterWeek struct {
	From     string   `json:"from"`
	To       string   `json:"to"`
	FirstDay string   `json:"first_day"`
	LastDay  string   `json:"last_day"`
	Views    int64    `json:"views"`
	Requests int64    `json:"requests"`
	Score    *float64 `json:"score"`
}

type helpCenterWindow struct {
	From     string           `json:"from"`
	To       string           `json:"to"`
	Views    int64            `json:"views"`
	Requests int64            `json:"requests"`
	Score    *float64         `json:"score"`
	Weeks    []helpCenterWeek `json:"weeks"`
	Days     []struct {
		Day      string `json:"day"`
		Views    int64  `json:"views"`
		Requests int64  `json:"requests"`
	} `json:"days"`
	Articles []struct {
		ID             string `json:"id"`
		Number         int64  `json:"number"`
		Title          string `json:"title"`
		Views          int64  `json:"views"`
		Helpful        int64  `json:"helpful"`
		Votes          int64  `json:"votes"`
		HelpfulPercent *int   `json:"helpful_percent"`
		Uses           int64  `json:"uses"`
	} `json:"articles"`
	ArticlesTotal int `json:"articles_total"`
}

type helpCenterBlock struct {
	HelpCenter *struct {
		Timezone string                      `json:"timezone"`
		Windows  map[string]helpCenterWindow `json:"windows"`
	} `json:"help_center"`
}

func seedSQL(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatal(err)
	}
}

// openConversation adds a conversation created at the instant given and returns its id.
func openConversationAt(t *testing.T, pool *pgxpool.Pool, workspace, contact, at, status string, imported bool) string {
	t.Helper()
	origin := (*string)(nil)
	if imported {
		value := `{"source":"freshdesk"}`
		origin = &value
	}
	return queryString(t, pool, `INSERT INTO conversations(workspace_id,contact_id,subject,channel,status,origin,created_at,updated_at)
		VALUES($1,$2,'Help','email',$3,$4::jsonb,$5,$5) RETURNING id::text`, workspace, contact, status, origin, at)
}

func scoreText(value *float64) string {
	if value == nil {
		return "none"
	}
	return fmt.Sprintf("%.1f", *value)
}

// The help center block reads calendar days in the workspace's timezone (New York, across the end of
// summer time): 300 views and 30 conversations opened in one week score 10.0, a week with views and
// no conversation has no score, and the requests opened are exactly the conversations the inbox lists
// for the period.
func TestResultsHelpCenterScoresViewsPerConversationOpenedEachWeek(t *testing.T) {
	now := time.Date(2026, 11, 19, 15, 0, 0, 0, time.UTC) // Thursday 10:00 in New York
	api, service, pool, _ := testAPIWithConfig(t, func(config *httpapi.Config) { config.Now = func() time.Time { return now } })
	admin := createSession(t, api, service, "Score Desk", "admin@score.example.com")
	other := createSession(t, api, service, "Other Score Desk", "admin@other-score.example.com")
	workspace, otherWorkspace := workspaceID(t, api, admin), workspaceID(t, api, other)
	seedSQL(t, pool, `UPDATE workspaces SET timezone='America/New_York' WHERE id=$1`, workspace)

	collection := kbCollection(t, api, admin, map[string]any{"name": "Guides"})
	printers := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Clear a paper jam", "status": "published", "body_md": "Open tray two."})
	wifi := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Join the wifi", "status": "published", "body_md": "Pick the network."})
	for _, row := range []struct {
		article, day string
		views        int
	}{{printers.ID, "2026-11-16", 100}, {printers.ID, "2026-11-17", 100}, {printers.ID, "2026-11-18", 100}, {wifi.ID, "2026-10-28", 40}, {wifi.ID, "2026-09-30", 7}} {
		seedSQL(t, pool, `INSERT INTO kb_article_views(workspace_id,kb_article_id,locale,day,views) VALUES($1,$2,'en',$3,$4)`, workspace, row.article, row.day, row.views)
	}
	otherCollection := kbCollection(t, api, other, map[string]any{"name": "Guides"})
	otherArticle := kbArticle(t, api, other, map[string]any{"collection_id": otherCollection.ID, "title": "Elsewhere", "status": "published", "body_md": "Not here."})
	seedSQL(t, pool, `INSERT INTO kb_article_views(workspace_id,kb_article_id,locale,day,views) VALUES($1,$2,'en','2026-11-17',900)`, otherWorkspace, otherArticle.ID)

	contact := queryString(t, pool, `INSERT INTO contacts(workspace_id,name,email) VALUES($1,'Casey','casey@customer.example') RETURNING id::text`, workspace)
	// Twenty-nine on Monday and Tuesday, and one on Thursday 23:30 New York, already Friday in UTC.
	for index := range 29 {
		openConversationAt(t, pool, workspace, contact, time.Date(2026, 11, 16, 14, index, 0, 0, time.UTC).Format(time.RFC3339), "open", false)
	}
	late := openConversationAt(t, pool, workspace, contact, "2026-11-20T04:30:00Z", "resolved", false)
	// Sunday 22:00 in New York is the week before, though it is Monday in UTC.
	openConversationAt(t, pool, workspace, contact, "2026-11-16T03:00:00Z", "open", false)
	// Spam, an imported conversation and one from tomorrow are not requests opened.
	openConversationAt(t, pool, workspace, contact, "2026-11-17T12:00:00Z", "spam", false)
	openConversationAt(t, pool, workspace, contact, "2026-11-17T12:00:00Z", "open", true)
	openConversationAt(t, pool, workspace, contact, "2026-11-20T05:00:00Z", "open", false)

	// One reply both linked the article and was an AI draft quoting it: one use.
	reply := queryString(t, pool, `INSERT INTO messages(workspace_id,conversation_id,kind,body,created_at) VALUES($1,$2,'outgoing','See the guide','2026-11-19T12:00:00Z') RETURNING id::text`, workspace, late)
	for _, kind := range []string{"link", "ai_source"} {
		seedSQL(t, pool, `INSERT INTO kb_article_uses(workspace_id,kb_article_id,conversation_id,message_id,kind,created_at) VALUES($1,$2,$3,$4,$5,'2026-11-19T12:00:00Z')`, workspace, printers.ID, late, reply, kind)
	}
	for index, helpful := range []bool{true, true, true, false} {
		seedSQL(t, pool, `INSERT INTO kb_article_votes(workspace_id,article_id,voter,address,helpful) VALUES($1,$2,$3,$3,$4)`, workspace, printers.ID, bytes.Repeat([]byte{byte(index + 1)}, 32), helpful)
	}

	var page helpCenterBlock
	decode(t, request(t, api, http.MethodGet, "/api/v1/results", nil, admin, nil), &page)
	if page.HelpCenter == nil || page.HelpCenter.Timezone != "America/New_York" {
		t.Fatalf("help center = %+v", page.HelpCenter)
	}
	month, week := page.HelpCenter.Windows["30d"], page.HelpCenter.Windows["7d"]
	if month.From != "2026-10-21T04:00:00Z" || month.To != "2026-11-20T05:00:00Z" || month.Views != 340 || month.Requests != 31 || len(month.Days) != 30 {
		t.Fatalf("30 days = %s..%s views %d requests %d days %d, want 2026-10-21T04:00:00Z..2026-11-20T05:00:00Z 340 31 30", month.From, month.To, month.Views, month.Requests, len(month.Days))
	}
	if month.Days[0].Day != "2026-10-21" || month.Days[26].Day != "2026-11-16" || month.Days[26].Views != 100 || month.Days[26].Requests != 29 || month.Days[29].Requests != 1 {
		t.Fatalf("days = %+v", month.Days)
	}
	if len(month.Weeks) != 5 {
		t.Fatalf("weeks = %+v, want 5", month.Weeks)
	}
	first, quiet, last, before := month.Weeks[0], month.Weeks[1], month.Weeks[4], month.Weeks[3]
	if first.From != "2026-10-21T04:00:00Z" || first.FirstDay != "2026-10-21" || first.LastDay != "2026-10-25" || first.Score != nil {
		t.Fatalf("first week, clipped to the period = %+v", first)
	}
	if quiet.FirstDay != "2026-10-26" || quiet.Views != 40 || quiet.Requests != 0 || quiet.Score != nil {
		t.Fatalf("week with views and no requests = %+v (score %s), want 40 views, 0 requests, no score", quiet, scoreText(quiet.Score))
	}
	if before.FirstDay != "2026-11-09" || before.Requests != 1 || before.Views != 0 || before.Score == nil || *before.Score != 0 {
		t.Fatalf("the week of the Sunday-evening conversation = %+v", before)
	}
	if last.From != "2026-11-16T05:00:00Z" || last.To != "2026-11-20T05:00:00Z" || last.LastDay != "2026-11-19" || last.Views != 300 || last.Requests != 30 || last.Score == nil || *last.Score != 10.0 {
		t.Fatalf("this week = %+v (score %s), want 300 views, 30 requests, score 10.0", last, scoreText(last.Score))
	}
	if week.From != "2026-11-13T05:00:00Z" || week.Views != 300 || week.Requests != 31 || len(week.Weeks) != 2 || week.Weeks[1].Score == nil || *week.Weeks[1].Score != 10.0 {
		t.Fatalf("7 days = %+v", week)
	}

	if month.ArticlesTotal != 2 || len(month.Articles) != 2 {
		t.Fatalf("articles = %+v (total %d)", month.Articles, month.ArticlesTotal)
	}
	top, second := month.Articles[0], month.Articles[1]
	if top.ID != printers.ID || top.Title != "Clear a paper jam" || top.Views != 300 || top.Helpful != 3 || top.Votes != 4 || top.HelpfulPercent == nil || *top.HelpfulPercent != 75 || top.Uses != 1 {
		t.Fatalf("top article = %+v, want 300 views, 3 of 4 helpful (75%%), 1 use", top)
	}
	if second.ID != wifi.ID || second.Views != 40 || second.Votes != 0 || second.HelpfulPercent != nil || second.Uses != 0 {
		t.Fatalf("second article = %+v", second)
	}

	// Requests opened open exactly the conversations they count.
	query := url.Values{"source": {"native"}, "created_after": {month.From}, "created_before": {month.To}, "page_size": {"100"}}
	if code, listed := listedIDs(t, api, admin, query.Encode()); code != http.StatusOK || len(listed) != int(month.Requests) {
		t.Fatalf("inbox for the period = %d, %d conversations, want %d", code, len(listed), month.Requests)
	}

	// The other workspace sees only its own views.
	var elsewhere helpCenterBlock
	decode(t, request(t, api, http.MethodGet, "/api/v1/results", nil, other, nil), &elsewhere)
	if got := elsewhere.HelpCenter.Windows["30d"]; got.Views != 900 || got.Requests != 0 || len(got.Articles) != 1 || got.Articles[0].ID != otherArticle.ID {
		t.Fatalf("other workspace = %+v", got)
	}
}
