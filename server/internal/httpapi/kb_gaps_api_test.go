package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/store"
)

type kbGapNamed struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Name  string `json:"name"`
}

type kbGapView struct {
	ID                 string      `json:"id"`
	Title              string      `json:"title"`
	Status             string      `json:"status"`
	Kinds              []string    `json:"kinds"`
	Askers             int         `json:"askers"`
	RecentAskers       int         `json:"recent_askers"`
	FirstSeen          string      `json:"first_seen"`
	LastSeen           string      `json:"last_seen"`
	FixedByArticle     *kbGapNamed `json:"fixed_by_article"`
	FixedAutomatically bool        `json:"fixed_automatically"`
	StatusChangedBy    *kbGapNamed `json:"status_changed_by"`
	StatusChangedAt    *string     `json:"status_changed_at"`
	SignalCount        int         `json:"signal_count"`
	Signals            []struct {
		Kind      string `json:"kind"`
		Text      string `json:"text"`
		CreatedAt string `json:"created_at"`
	} `json:"signals"`
}

type kbGapsView struct {
	Items []kbGapView `json:"items"`
}

func gapTitles(gaps kbGapsView) []string {
	titles := []string{}
	for _, g := range gaps.Items {
		titles = append(titles, g.Title)
	}
	return titles
}

func wantGapTitles(t *testing.T, gaps kbGapsView, want ...string) {
	t.Helper()
	if got := gapTitles(gaps); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("gaps = %q, want %q", got, want)
	}
}

func TestTheGapsListRanksByRecentAskersAndFiltersByStatusAndKind(t *testing.T) {
	api, service, pool, _ := testAPIWithStorage(t)
	a := newSearchSite(t, api, pool, createSession(t, api, service, "Gaps A", "admin@gaps-a.example.com"))
	b := newSearchSite(t, api, pool, createSession(t, api, service, "Gaps B", "admin@gaps-b.example.com"))
	for index, query := range []string{"refund policy", "refund policy", "refund policy", "shipping to canada"} {
		client := fmt.Sprintf("192.0.2.%d", 80+index)
		wantPage(t, visit(a.site, "/help/"+a.slug+"/search?q="+url.QueryEscape(query), client, "User-Agent", browserAgent), http.StatusOK, nil, nil)
	}
	no := postVote(a.site, a.page+"/vote", url.Values{"helpful": {"no"}}, testOrigin, "192.0.2.99")
	line := url.Values{"helpful": {"no"}, "missing": {"Where do I download my invoices"}}
	wantSeeOther(t, postVote(a.site, a.page+"/vote", line, testOrigin, "192.0.2.99", chatCookie(t, no)), a.page+"?voted=sent#helpful")
	if _, err := store.New(pool).BuildKBGaps(context.Background()); err != nil {
		t.Fatal(err)
	}

	read := issueToken(t, api, a.admin, "Gap reader", "read").Token
	var open kbGapsView
	kbCall(t, api, http.MethodGet, "/api/v1/kb/gaps", nil, read, http.StatusOK, &open)
	// Most askers in the last 30 days first; a tie goes to the gap seen last.
	wantGapTitles(t, open, "refund policy", "Where do I download my invoices", "shipping to canada")
	refund := open.Items[0]
	if refund.Status != "open" || refund.Askers != 3 || refund.RecentAskers != 3 || strings.Join(refund.Kinds, ",") != "search_no_result" ||
		refund.FirstSeen == "" || refund.LastSeen == "" || refund.FixedByArticle != nil || refund.StatusChangedBy != nil {
		t.Fatalf("refund gap = %+v", refund)
	}
	var votes kbGapsView
	kbCall(t, api, http.MethodGet, "/api/v1/kb/gaps?kind=vote_no", nil, read, http.StatusOK, &votes)
	wantGapTitles(t, votes, "Where do I download my invoices")

	// The detail lists the signals newest first, and never how askers are told apart.
	var detail kbGapView
	raw := kbCall(t, api, http.MethodGet, "/api/v1/kb/gaps/"+refund.ID, nil, read, http.StatusOK, &detail)
	if detail.ID != refund.ID || detail.SignalCount != 3 || len(detail.Signals) != 3 {
		t.Fatalf("detail = %s", raw.Body.String())
	}
	if strings.Contains(raw.Body.String(), "asker_key") {
		t.Fatalf("detail shows asker keys: %s", raw.Body.String())
	}
	for index, s := range detail.Signals {
		if s.Kind != "search_no_result" || s.Text != "refund policy" || index > 0 && s.CreatedAt > detail.Signals[index-1].CreatedAt {
			t.Fatalf("signals = %+v, want the three searches newest first", detail.Signals)
		}
	}

	// Reading is not changing: a read token is refused and told what it lacks.
	refused := serve(api, newRequest(t, http.MethodPatch, "/api/v1/kb/gaps/"+refund.ID, map[string]any{"status": "dismissed"}, read, nil))
	if refused.Code != http.StatusForbidden || errorCode(t, refused) != "permission_missing" {
		t.Fatalf("read token PATCH status=%d body=%s, want 403 permission_missing", refused.Code, refused.Body.String())
	}

	change := customToken(t, api, a.admin, "Gap editor", "kb_articles:read", "kb_articles:update")
	var linked kbGapView
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/gaps/"+refund.ID, map[string]any{"fixed_by_article_id": a.article.ID}, change, http.StatusOK, &linked)
	if linked.Status != "fixed" || linked.FixedByArticle == nil || linked.FixedByArticle.ID != a.article.ID || linked.FixedByArticle.Title != "Reset a password" ||
		linked.FixedAutomatically || linked.StatusChangedBy == nil || linked.StatusChangedBy.Name == "" || linked.StatusChangedAt == nil {
		t.Fatalf("linked gap = %+v", linked)
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/gaps", nil, read, http.StatusOK, &open)
	wantGapTitles(t, open, "Where do I download my invoices", "shipping to canada")
	var fixed kbGapsView
	kbCall(t, api, http.MethodGet, "/api/v1/kb/gaps?status=fixed", nil, read, http.StatusOK, &fixed)
	wantGapTitles(t, fixed, "refund policy")

	shipping := open.Items[1]
	var dismissed kbGapView
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/gaps/"+shipping.ID, map[string]any{"status": "dismissed"}, change, http.StatusOK, &dismissed)
	if dismissed.Status != "dismissed" || dismissed.FixedByArticle != nil {
		t.Fatalf("dismissed gap = %+v", dismissed)
	}
	// Every way in has a way out: open again clears the link.
	var reopened kbGapView
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/gaps/"+refund.ID, map[string]any{"status": "open"}, change, http.StatusOK, &reopened)
	if reopened.Status != "open" || reopened.FixedByArticle != nil {
		t.Fatalf("reopened gap = %+v", reopened)
	}

	// Another workspace's article cannot fix this gap, and the other workspace sees none of it.
	other := serve(api, newRequest(t, http.MethodPatch, "/api/v1/kb/gaps/"+refund.ID, map[string]any{"fixed_by_article_id": b.article.ID}, change, nil))
	if other.Code != http.StatusNotFound {
		t.Fatalf("link to another workspace's article status=%d body=%s, want 404", other.Code, other.Body.String())
	}
	invalid := serve(api, newRequest(t, http.MethodPatch, "/api/v1/kb/gaps/"+refund.ID, map[string]any{"status": "solved"}, change, nil))
	if invalid.Code != http.StatusBadRequest || errorCode(t, invalid) != "invalid_request" {
		t.Fatalf("unknown status status=%d body=%s, want 400 invalid_request", invalid.Code, invalid.Body.String())
	}
	var none kbGapsView
	kbCall(t, api, http.MethodGet, "/api/v1/kb/gaps?status=open", nil, b.admin, http.StatusOK, &none)
	wantGapTitles(t, none)
	for method, body := range map[string]any{http.MethodGet: nil, http.MethodPatch: map[string]any{"status": "dismissed"}} {
		response := serve(api, newRequest(t, method, "/api/v1/kb/gaps/"+refund.ID, body, b.admin, nil))
		if response.Code != http.StatusNotFound {
			t.Fatalf("another workspace %s status=%d body=%s, want 404", method, response.Code, response.Body.String())
		}
	}
}
