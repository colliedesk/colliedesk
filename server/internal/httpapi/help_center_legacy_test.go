package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ledgerRow records in the import ledger that a Freshdesk item of source became target in the
// workspace with slug, last written at updated (an SQL timestamp).
func ledgerRow(t *testing.T, pool *pgxpool.Pool, slug, source, entity, freshdeskID, target, status, updated string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `INSERT INTO import_items(workspace_id,source,entity,source_id,target_id,status,updated_at)
		SELECT id,$2,$3,$4,$5,$6,$7::timestamptz FROM workspaces WHERE slug=$1`, slug, source, entity, freshdeskID, target, status, updated); err != nil {
		t.Fatal(err)
	}
}

// wantMove checks that path answers status with Location want, and is never cached.
func wantMove(t *testing.T, site http.Handler, path string, status int, want string, header ...string) {
	t.Helper()
	response := visit(site, path, "198.51.100.71", header...)
	if response.Code != status || response.Header().Get("Location") != want {
		t.Errorf("GET %s = %d %q, want %d %q", path, response.Code, response.Header().Get("Location"), status, want)
	}
	if response.Code == status && response.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("GET %s Cache-Control=%q, want no-store", path, response.Header().Get("Cache-Control"))
	}
}

// Every old Freshdesk help center address answers: an imported item that is public now moves to its
// page for good (301); a draft, an item in the trash, a collection with nothing published and an id
// nobody imported go to the search with the words of the old address (302, so a later publish takes
// over); the old home and search move to the new ones; any other old page goes home.
func TestOldFreshdeskAddressesMoveToTheirNewPages(t *testing.T) {
	api, service, pool, _ := testAPIWithStorage(t)
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Legacy Links", "admin@legacy-links.example.com")
	slug := workspaceSlug(t, api, admin)
	enableHelpCenter(t, api, admin, "Legacy help", true)
	billing := kbCollection(t, api, admin, map[string]any{"name": "Billing"})
	cards := kbCollection(t, api, admin, map[string]any{"name": "Cards", "parent_id": billing.ID})
	empty := kbCollection(t, api, admin, map[string]any{"name": "Later"})
	paid := kbArticle(t, api, admin, map[string]any{"collection_id": cards.ID, "title": "Pay by card", "status": "published", "body_md": "Any card."})
	draft := kbArticle(t, api, admin, map[string]any{"collection_id": empty.ID, "title": "Coming soon", "body_md": "Soon."})
	trashed := kbArticle(t, api, admin, map[string]any{"collection_id": cards.ID, "title": "Old fees", "status": "published", "body_md": "Fees."})
	kbCall(t, api, http.MethodDelete, "/api/v1/kb/articles/"+trashed.ID+"?updated_at="+url.QueryEscape(trashed.UpdatedAt), nil, admin, http.StatusNoContent, nil)
	older := kbArticle(t, api, admin, map[string]any{"collection_id": cards.ID, "title": "Refunds", "status": "published", "body_md": "Refunds."})
	const source, second = "freshdesk:account:1", "freshdesk:account:2"
	for _, row := range [][3]string{{"kb_category", "11", billing.ID}, {"kb_folder", "21", cards.ID}, {"kb_folder", "22", empty.ID},
		{"kb_article", "7201", paid.ID}, {"kb_article", "7202", draft.ID}, {"kb_article", "7203", trashed.ID}} {
		ledgerRow(t, pool, slug, source, row[0], row[1], row[2], "done", "2026-10-20 10:00:00+00")
	}
	// A failed row is not an imported item yet.
	ledgerRow(t, pool, slug, source, "kb_article", "7204", paid.ID, "failed", "2026-10-20 10:00:00+00")
	// Two Freshdesk accounts imported into one workspace with the same article id: the newest row of a
	// public page wins.
	ledgerRow(t, pool, slug, source, "kb_article", "7210", older.ID, "done", "2026-10-20 10:00:00+00")
	ledgerRow(t, pool, slug, second, "kb_article", "7210", paid.ID, "done", "2026-10-21 10:00:00+00")
	// The same id twice, the newer row a draft: the public page wins over the newer row.
	ledgerRow(t, pool, slug, source, "kb_article", "7220", older.ID, "done", "2026-10-20 10:00:00+00")
	ledgerRow(t, pool, slug, second, "kb_article", "7220", draft.ID, "done", "2026-10-21 10:00:00+00")

	home := testOrigin + "/help/" + slug
	article := home + fmt.Sprintf("/a/%d-pay-by-card", paid.Number)
	base := "/help/" + slug
	moves := []struct {
		path   string
		status int
		want   string
	}{
		{"/support/solutions/articles/7201-pay-by-card", http.StatusMovedPermanently, article},
		{"/de/support/solutions/articles/7201", http.StatusMovedPermanently, article},
		{"/support/solutions/articles/7201-old-title?utm_source=mail", http.StatusMovedPermanently, article},
		{"/support/solutions/articles/7210-refunds", http.StatusMovedPermanently, article},
		{"/support/solutions/articles/7220-refunds", http.StatusMovedPermanently, home + fmt.Sprintf("/a/%d-refunds", older.Number)},
		{"/Support/Solutions/Articles/7201-Pay-By-Card", http.StatusMovedPermanently, article},
		{"/support/solutions/11", http.StatusMovedPermanently, home + "/c/billing"},
		{"/support/solutions/folders/21-cards", http.StatusMovedPermanently, home + "/c/billing/cards"},
		{"/support/home", http.StatusMovedPermanently, home},
		{"/en/support/solutions", http.StatusMovedPermanently, home},
		{"/support", http.StatusMovedPermanently, home},
		{"/support/solutions/articles/7202-coming-soon", http.StatusFound, home + "/search?q=coming+soon"},
		{"/support/solutions/articles/7203-old-fees", http.StatusFound, home + "/search?q=old+fees"},
		{"/support/solutions/articles/7204-pay-by-card", http.StatusFound, home + "/search?q=pay+by+card"},
		{"/support/solutions/folders/22-later", http.StatusFound, home + "/search?q=later"},
		{"/support/solutions/articles/9999-reset-your-password", http.StatusFound, home + "/search?q=reset+your+password"},
		{"/support/solutions/articles/9999", http.StatusFound, home},
		{"/support/search/solutions?term=card%20fees&page=2", http.StatusFound, home + "/search?q=card+fees"},
		{"/support/search", http.StatusFound, home},
		{"/support/tickets/new", http.StatusFound, home},
		{"/support/solutions/folders/x", http.StatusFound, home},
	}
	for _, move := range moves {
		wantMove(t, site, base+move.path, move.status, move.want)
	}
	// The page a 301 names is the article itself.
	if response := visit(site, base+fmt.Sprintf("/a/%d-pay-by-card", paid.Number), "198.51.100.72"); response.Code != http.StatusOK {
		t.Fatalf("new article page status=%d", response.Code)
	}
	// A host the request names is never echoed: the target is the help center's own address.
	wantMove(t, site, base+"/support/solutions/articles/7201", http.StatusMovedPermanently, article, "X-Forwarded-Host", "evil.example")

	// The app's own host keeps its paths: /support/... outside /help/ is never the help center.
	if response := visit(site, "/support/solutions/articles/7201", "198.51.100.73"); response.Header().Get("Location") != "" {
		t.Errorf("app host /support redirected to %q", response.Header().Get("Location"))
	}
	// A help center turned off answers its one 404, old addresses included.
	enableHelpCenter(t, api, admin, "Legacy help", false)
	if response := visit(site, base+"/support/solutions/articles/7201", "198.51.100.74"); response.Code != http.StatusNotFound {
		t.Errorf("off help center old address status=%d, want 404", response.Code)
	}
}

// Freshdesk ids are not unique across Freshdesk accounts, and every workspace imports its own: an id
// another workspace imported never leads to that workspace's page.
func TestOldFreshdeskAddressesStayInTheirWorkspace(t *testing.T) {
	api, service, pool, _ := testAPIWithStorage(t)
	site := helpCenterSite(api)
	adminA := createSession(t, api, service, "Legacy Alpha", "admin@legacy-alpha.example.com")
	adminB := createSession(t, api, service, "Legacy Bravo", "admin@legacy-bravo.example.com")
	slugA, slugB := workspaceSlug(t, api, adminA), workspaceSlug(t, api, adminB)
	enableHelpCenter(t, api, adminA, "Alpha help", true)
	enableHelpCenter(t, api, adminB, "Bravo help", true)
	shelfA := kbCollection(t, api, adminA, map[string]any{"name": "Alpha shelf"})
	shelfB := kbCollection(t, api, adminB, map[string]any{"name": "Bravo shelf"})
	alpha := kbArticle(t, api, adminA, map[string]any{"collection_id": shelfA.ID, "title": "Alpha secret", "status": "published", "body_md": "A."})
	alphaOnly := kbArticle(t, api, adminA, map[string]any{"collection_id": shelfA.ID, "title": "Alpha only", "status": "published", "body_md": "A."})
	bravo := kbArticle(t, api, adminB, map[string]any{"collection_id": shelfB.ID, "title": "Bravo answer", "status": "published", "body_md": "B."})
	// The same Freshdesk account id and article id in both workspaces, and one id only A imported.
	ledgerRow(t, pool, slugA, "freshdesk:account:1", "kb_article", "7201", alpha.ID, "done", "2026-10-21 10:00:00+00")
	ledgerRow(t, pool, slugB, "freshdesk:account:1", "kb_article", "7201", bravo.ID, "done", "2026-10-20 10:00:00+00")
	ledgerRow(t, pool, slugA, "freshdesk:account:1", "kb_article", "7205", alphaOnly.ID, "done", "2026-10-21 10:00:00+00")
	ledgerRow(t, pool, slugA, "freshdesk:account:1", "kb_folder", "21", shelfA.ID, "done", "2026-10-21 10:00:00+00")

	homeB := testOrigin + "/help/" + slugB
	wantMove(t, site, "/help/"+slugB+"/support/solutions/articles/7201", http.StatusMovedPermanently, homeB+fmt.Sprintf("/a/%d-bravo-answer", bravo.Number))
	wantMove(t, site, "/help/"+slugB+"/support/solutions/articles/7205-alpha-only", http.StatusFound, homeB+"/search?q=alpha+only")
	wantMove(t, site, "/help/"+slugB+"/support/solutions/folders/21", http.StatusFound, homeB)
	wantMove(t, site, "/help/"+slugA+"/support/solutions/articles/7201", http.StatusMovedPermanently,
		testOrigin+"/help/"+slugA+fmt.Sprintf("/a/%d-alpha-secret", alpha.Number))
}

// The redirect serves on every address of a help center: its own host name below the help center
// domain, an active custom domain, and the path address, which first moves to the help center's
// address with the old path kept.
func TestOldFreshdeskAddressesAnswerOnEveryHelpCenterAddress(t *testing.T) {
	api, service, pool, _ := testAPIWithConfig(t, func(config *httpapi.Config) { config.HelpCenterDomain = "help.example.net" })
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Legacy Hosts", "admin@legacy-hosts.example.com")
	slug := workspaceSlug(t, api, admin)
	enableHelpCenter(t, api, admin, "Hosts help", true)
	shelf := kbCollection(t, api, admin, map[string]any{"name": "Shelf"})
	article := kbArticle(t, api, admin, map[string]any{"collection_id": shelf.ID, "title": "Reset password", "status": "published", "body_md": "R."})
	ledgerRow(t, pool, slug, "freshdesk:account:1", "kb_article", "7201", article.ID, "done", "2026-10-21 10:00:00+00")
	path := "/support/solutions/articles/7201-reset-password"
	slugHost := slug + ".help.example.net"
	page := fmt.Sprintf("/a/%d-reset-password", article.Number)

	wantMove(t, site, "/help/"+slug+path, http.StatusMovedPermanently, "https://"+slugHost+"/help"+path)
	wantMove(t, site, path, http.StatusMovedPermanently, "https://"+slugHost+"/help"+page, "Host", slugHost)
	wantMove(t, site, "/help"+path, http.StatusMovedPermanently, "https://"+slugHost+"/help"+page, "Host", slugHost)
	wantMove(t, site, "/de/support/solutions/articles/9999-gone", http.StatusFound, "https://"+slugHost+"/help/search?q=gone", "Host", slugHost)

	const custom = "help.legacy-hosts.example"
	if _, err := pool.Exec(context.Background(), `INSERT INTO help_center_domains (workspace_id, hostname, claim, token, status, verified_at)
		SELECT id, $2, left(md5('legacy-claim'), 24), md5('legacy-domain'), 'active', now() FROM workspaces WHERE slug=$1`, slug, custom); err != nil {
		t.Fatal(err)
	}
	wantMove(t, site, path, http.StatusMovedPermanently, "https://"+custom+page, "Host", custom)
	wantMove(t, site, "/support/home", http.StatusMovedPermanently, "https://"+custom, "Host", custom)
	if response := visit(site, page, "198.51.100.75", "Host", custom); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Reset password") {
		t.Fatalf("custom domain article status=%d", response.Code)
	}
}
