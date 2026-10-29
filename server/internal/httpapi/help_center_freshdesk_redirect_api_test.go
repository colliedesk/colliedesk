package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

const freshdeskRedirectPath = "/api/v1/help-center/freshdesk-redirect"

type freshdeskRedirectView struct {
	State         string  `json:"state"`
	FreshdeskHost *string `json:"freshdesk_domain"`
	Address       string  `json:"address"`
	KeepWeeks     int     `json:"keep_weeks"`
	Snippet       *string `json:"snippet"`
	Mapped        *struct {
		Articles   int  `json:"articles"`
		Folders    int  `json:"folders"`
		Categories int  `json:"categories"`
		Embedded   bool `json:"embedded"`
	} `json:"mapped"`
}

// finishedFreshdeskImport writes a finished import of a Freshdesk account the way the worker leaves it.
func finishedFreshdeskImport(t *testing.T, pool *pgxpool.Pool, slug, domain, options string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `INSERT INTO import_jobs(workspace_id,kind,source,domain,account_source,state,phase,options,status,started_at,finished_at)
		SELECT id,'import','freshdesk',$2,'freshdesk:account:777','finished','full',$3::jsonb,'OK',now(),now() FROM workspaces WHERE slug=$1`, slug, domain, options); err != nil {
		t.Fatal(err)
	}
}

func redirectFor(t *testing.T, api http.Handler, token, plan string) freshdeskRedirectView {
	t.Helper()
	path := freshdeskRedirectPath
	if plan != "" {
		path += "?plan=" + plan
	}
	var view freshdeskRedirectView
	kbCall(t, api, http.MethodGet, path, nil, token, http.StatusOK, &view)
	return view
}

// A team on <name>.freshdesk.com with no custom domain gets a snippet built from its own help center
// address and what the import made public, only after it says which Freshdesk plan it has: Growth cannot
// paste code into the portal, so it gets no snippet at all.
func TestFreshdeskRedirectSnippetFollowsThePlanAndTheImport(t *testing.T) {
	api, service, pool, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "Redirect Alpha", "admin@redirect-alpha.example.com")
	other := createSession(t, api, service, "Redirect Bravo", "admin@redirect-bravo.example.com")
	slug, otherSlug := workspaceSlug(t, api, admin), workspaceSlug(t, api, other)
	enableHelpCenter(t, api, admin, "Redirect help", true)
	enableHelpCenter(t, api, other, "Bravo help", true)
	home := testOrigin + "/help/" + slug

	if view := redirectFor(t, api, admin, "pro"); view.State != "no_import" || view.Snippet != nil || view.FreshdeskHost != nil || view.Address != home {
		t.Fatalf("before an import: %+v", view)
	}

	billing := kbCollection(t, api, admin, map[string]any{"name": "Billing"})
	cards := kbCollection(t, api, admin, map[string]any{"name": "Cards", "parent_id": billing.ID})
	empty := kbCollection(t, api, admin, map[string]any{"name": "Later"})
	paid := kbArticle(t, api, admin, map[string]any{"collection_id": cards.ID, "title": "Pay by card", "status": "published", "body_md": "Any card."})
	draft := kbArticle(t, api, admin, map[string]any{"collection_id": empty.ID, "title": "Coming soon", "body_md": "Soon."})
	const source = "freshdesk:account:777"
	for _, row := range [][3]string{{"kb_category", "11", billing.ID}, {"kb_folder", "21", cards.ID}, {"kb_folder", "22", empty.ID},
		{"kb_article", "7201", paid.ID}, {"kb_article", "7202", draft.ID}} {
		ledgerRow(t, pool, slug, source, row[0], row[1], row[2], "done", "2026-10-20 10:00:00+00")
	}
	// Another Freshdesk account of the same workspace, and another workspace, never reach this snippet.
	ledgerRow(t, pool, slug, "freshdesk:account:888", "kb_article", "7299", paid.ID, "done", "2026-10-20 10:00:00+00")
	ledgerRow(t, pool, otherSlug, source, "kb_article", "7298", paid.ID, "done", "2026-10-20 10:00:00+00")
	finishedFreshdeskImport(t, pool, slug, "acme.freshdesk.com", `{}`)

	asked := redirectFor(t, api, admin, "")
	if asked.State != "ask_plan" || asked.Snippet != nil || asked.FreshdeskHost == nil || *asked.FreshdeskHost != "acme.freshdesk.com" || asked.KeepWeeks != 4 {
		t.Fatalf("without a plan: %+v", asked)
	}
	if growth := redirectFor(t, api, admin, "growth"); growth.State != "growth" || growth.Snippet != nil || growth.Mapped != nil {
		t.Fatalf("growth: %+v", growth)
	}
	for _, plan := range []string{"pro", "enterprise"} {
		view := redirectFor(t, api, admin, plan)
		if view.State != "ready" || view.Snippet == nil || view.Mapped == nil || !view.Mapped.Embedded || view.Mapped.Articles != 1 || view.Mapped.Folders != 1 || view.Mapped.Categories != 1 {
			t.Fatalf("%s: %+v", plan, view)
		}
		snippet := *view.Snippet
		for _, want := range []string{
			fmt.Sprintf(`"7201":"/a/%d-pay-by-card"`, paid.Number), `"21":"/c/billing/cards"`, `"11":"/c/billing"`, `"` + home + `"`,
		} {
			if !strings.Contains(snippet, want) {
				t.Errorf("%s snippet lacks %s:\n%s", plan, want, snippet)
			}
		}
		for _, unwanted := range []string{`"7202"`, `"22"`, `"7299"`, `"7298"`, "coming-soon"} {
			if strings.Contains(snippet, unwanted) {
				t.Errorf("%s snippet holds %s:\n%s", plan, unwanted, snippet)
			}
		}
	}
	if response := request(t, api, http.MethodGet, freshdeskRedirectPath+"?plan=gold", nil, admin, nil); response.Code != http.StatusBadRequest {
		t.Fatalf("unknown plan status=%d", response.Code)
	}
	// The other workspace has no import of its own.
	if view := redirectFor(t, api, other, "pro"); view.State != "no_import" || view.Snippet != nil {
		t.Fatalf("another workspace: %+v", view)
	}
}

// A team with its own domain on the old help center redirects at DNS, and a help center that is off has
// nowhere to send visitors: neither gets a snippet.
func TestFreshdeskRedirectSnippetIsNotOfferedWithAnOwnDomainOrAnOffHelpCenter(t *testing.T) {
	api, service, pool, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "Redirect Own", "admin@redirect-own.example.com")
	slug := workspaceSlug(t, api, admin)
	enableHelpCenter(t, api, admin, "Own help", true)
	finishedFreshdeskImport(t, pool, slug, "acme.freshdesk.com", `{"old_help_center_domain":"support.acme.example"}`)
	if view := redirectFor(t, api, admin, "pro"); view.State != "own_domain" || view.Snippet != nil {
		t.Fatalf("own domain: %+v", view)
	}
	if _, err := pool.Exec(context.Background(), `UPDATE import_jobs SET options='{}'`); err != nil {
		t.Fatal(err)
	}
	enableHelpCenter(t, api, admin, "Own help", false)
	if view := redirectFor(t, api, admin, "pro"); view.State != "help_center_off" || view.Snippet != nil {
		t.Fatalf("help center off: %+v", view)
	}
}
