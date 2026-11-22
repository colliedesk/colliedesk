package httpapi_test

import (
	"net/http"
	"net/url"
	"testing"
)

type kbSearchTermView struct {
	Query          string `json:"query"`
	Searches       int64  `json:"searches"`
	Opened         int64  `json:"opened"`
	Results        int    `json:"results"`
	LastSearchedAt string `json:"last_searched_at"`
}

type kbSearchReportView struct {
	From          string             `json:"from"`
	To            string             `json:"to"`
	Searches      int64              `json:"searches"`
	Top           []kbSearchTermView `json:"top"`
	NoResults     []kbSearchTermView `json:"no_results"`
	NothingOpened []kbSearchTermView `json:"nothing_opened"`
}

// A search that found nothing is in the report as soon as the reader made it, with the email address
// and the long number masked; another workspace's report does not show it.
func TestANoResultSearchIsInTheSearchReportAtOnceAndOnlyInItsWorkspace(t *testing.T) {
	api, service, pool, _ := testAPIWithStorage(t)
	a := newSearchSite(t, api, pool, createSession(t, api, service, "Report A", "admin@report-a.example.com"))
	b := newSearchSite(t, api, pool, createSession(t, api, service, "Report B", "admin@report-b.example.com"))
	for _, query := range []string{"refund for order 1234567 to casey@customer.example", "password"} {
		wantPage(t, visit(a.site, "/help/"+a.slug+"/search?q="+url.QueryEscape(query), "192.0.2.90", "User-Agent", browserAgent), http.StatusOK, nil, nil)
	}

	read := issueToken(t, api, a.admin, "Reader", "read").Token
	var report kbSearchReportView
	kbCall(t, api, http.MethodGet, "/api/v1/kb/searches/report", nil, read, http.StatusOK, &report)
	if report.Searches != 2 || len(report.Top) != 2 {
		t.Fatalf("report = %+v, want 2 searches in the top list", report)
	}
	if len(report.NoResults) != 1 || report.NoResults[0].Query != "refund for order [number removed] to [email removed]" ||
		report.NoResults[0].Searches != 1 || report.NoResults[0].Results != 0 || report.NoResults[0].LastSearchedAt == "" {
		t.Fatalf("no results = %+v, want the masked refund search once", report.NoResults)
	}
	if len(report.NothingOpened) != 1 || report.NothingOpened[0].Query != "password" || report.NothingOpened[0].Results != 1 || report.NothingOpened[0].Opened != 0 {
		t.Fatalf("nothing opened = %+v, want password with 1 result", report.NothingOpened)
	}

	var other kbSearchReportView
	kbCall(t, api, http.MethodGet, "/api/v1/kb/searches/report", nil, b.admin, http.StatusOK, &other)
	if other.Searches != 0 || len(other.Top) != 0 || len(other.NoResults) != 0 || len(other.NothingOpened) != 0 {
		t.Fatalf("other workspace's report = %+v, want empty", other)
	}
}
