package httpapi_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

// The help center block of Results reads one help center when help_center_id names it: the views of
// the articles it shows, the requests that came in through it and its articles only. Without it the
// block reads the whole workspace, an email request included; another workspace's help center is not
// found.
func TestResultsHelpCenterFilterCountsOneHelpCenter(t *testing.T) {
	now := time.Date(2026, 11, 19, 15, 0, 0, 0, time.UTC)
	api, service, pool, _ := testAPIWithConfig(t, func(config *httpapi.Config) { config.Now = func() time.Time { return now } })
	admin := createSession(t, api, service, "Shelf Results", "admin@shelf-results.example.com")
	other := createSession(t, api, service, "Other Shelf Results", "admin@other-shelf-results.example.com")
	workspace := workspaceID(t, api, admin)
	enableHelpCenter(t, api, admin, "Main answers", true)
	hardware := createHelpCenter(t, api, admin, "Hardware", "hardware")
	elsewhere := createHelpCenter(t, api, other, "Elsewhere", "elsewhere")
	primary := queryString(t, pool, `SELECT primary_help_center_id($1)::text`, workspace)

	guides := kbCollection(t, api, admin, map[string]any{"name": "Guides"})
	jam := kbArticle(t, api, admin, map[string]any{"collection_id": guides.ID, "title": "Clear a paper jam", "status": "published", "body_md": "Open tray two."})
	var cables placedCollectionView
	kbCall(t, api, http.MethodPost, "/api/v1/kb/collections", map[string]any{"name": "Cables", "help_center_ids": []string{hardware.ID}},
		admin, http.StatusCreated, &cables)
	swap := kbArticle(t, api, admin, map[string]any{"collection_id": cables.ID, "title": "Swap a cable", "status": "published", "body_md": "Unplug it."})
	seedSQL(t, pool, `INSERT INTO kb_article_views(workspace_id,kb_article_id,locale,day,views) VALUES($1,$2,'en','2026-11-17',100),($1,$3,'en','2026-11-17',30)`,
		workspace, jam.ID, swap.ID)

	contact := queryString(t, pool, `INSERT INTO contacts(workspace_id,name,email) VALUES($1,'Casey','casey@customer.example') RETURNING id::text`, workspace)
	for _, helpCenter := range []any{primary, primary, primary, hardware.ID, hardware.ID, nil} {
		seedSQL(t, pool, `INSERT INTO conversations(workspace_id,contact_id,subject,channel,status,created_at,updated_at,help_center_id)
			VALUES($1,$2,'Help','email','open','2026-11-17T12:00:00Z','2026-11-17T12:00:00Z',$3::uuid)`, workspace, contact, helpCenter)
	}

	for _, check := range []struct {
		name, query     string
		views, requests int64
		articles        []string
	}{
		{"the workspace", "", 130, 6, []string{"Clear a paper jam", "Swap a cable"}},
		{"Hardware", "?help_center_id=" + hardware.ID, 30, 2, []string{"Swap a cable"}},
		{"the primary", "?help_center_id=" + primary, 100, 3, []string{"Clear a paper jam"}},
	} {
		var page helpCenterBlock
		kbCall(t, api, http.MethodGet, "/api/v1/results"+check.query, nil, admin, http.StatusOK, &page)
		window := page.HelpCenter.Windows["30d"]
		var titles []string
		for _, article := range window.Articles {
			titles = append(titles, article.Title)
		}
		if window.Views != check.views || window.Requests != check.requests || len(titles) != len(check.articles) {
			t.Fatalf("%s: views=%d requests=%d articles=%v, want %d, %d, %v", check.name, window.Views, window.Requests, titles, check.views, check.requests, check.articles)
		}
		for i := range titles {
			if titles[i] != check.articles[i] {
				t.Fatalf("%s: articles=%v, want %v", check.name, titles, check.articles)
			}
		}
	}

	kbCall(t, api, http.MethodGet, "/api/v1/results?help_center_id="+elsewhere.ID, nil, admin, http.StatusNotFound, nil)
	kbCall(t, api, http.MethodGet, "/api/v1/results?help_center_id=not-a-help-center", nil, admin, http.StatusBadRequest, nil)
}
