package httpapi_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

func TestMoveGuideFollowsRealStateAndKeepsCompletedSteps(t *testing.T) {
	api, service, pool, dns := testAPIWithConfig(t, func(c *httpapi.Config) { c.HelpCenterDomain = "help.example.net" })
	admin := createSession(t, api, service, "Move Desk", "admin@move.example")
	slug := workspaceSlug(t, api, admin)
	if _, err := pool.Exec(t.Context(), `INSERT INTO workspace_onboarding(workspace_id,goal) SELECT id,'leave_helpdesk' FROM workspaces WHERE slug=$1`, slug); err != nil {
		t.Fatal(err)
	}
	read := func() map[string]any {
		t.Helper()
		req := newRequest(t, http.MethodGet, "/api/v1/workspace/setup", nil, admin, nil)
		response := serve(api, req)
		validateContractResponse(t, req, response)
		var v map[string]any
		decode(t, response, &v)
		return v
	}
	v := read()
	var keys []string
	for _, s := range v["steps"].([]any) {
		keys = append(keys, s.(map[string]any)["key"].(string))
	}
	if fmt.Sprint(keys) != "[import custom_domain plan cname search_console]" {
		t.Fatalf("move steps=%v", keys)
	}
	if v["next_url"] != "/settings/import-freshdesk" {
		t.Fatalf("next=%v", v["next_url"])
	}
	update := func(step, state string, want int) {
		t.Helper()
		req := newRequest(t, http.MethodPut, "/api/v1/workspace/setup/move", map[string]any{"step": step, "state": state}, admin, nil)
		response := serve(api, req)
		if response.Code != want {
			t.Fatalf("%s %s: %d %s", step, state, response.Code, response.Body.String())
		}
		validateContractResponse(t, req, response)
	}
	update("preview", "done", 422)
	insertImport(t, pool, slug, "finished")
	v = read()
	if v["recommended"] != "custom_domain" || v["next_url"] != "/settings#help-center-heading" {
		t.Fatalf("enable prerequisites=%v", v)
	}
	if center := v["help_center"].(map[string]any); center["action"] != "enable" || center["url"] != "/settings#help-center-heading" {
		t.Fatalf("empty imported help center=%v", center)
	}
	// An old preview confirmation must not create an extra step or block the move.
	if _, err := pool.Exec(t.Context(), `UPDATE workspace_onboarding SET move_steps='{"preview":"done"}' WHERE workspace_id=(SELECT id FROM workspaces WHERE slug=$1)`, slug); err != nil {
		t.Fatal(err)
	}
	collection := kbCollection(t, api, admin, map[string]any{"name": "Guides"})
	kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "First article", "body_md": "Hello.", "status": "published"})
	kbCall(t, api, http.MethodPut, "/api/v1/help-center", map[string]any{"enabled": true, "title": "Move Desk", "locale": "en"}, admin, 200, nil)
	v = read()
	if v["next_url"] != "/settings#help-center-domain-heading" {
		t.Fatalf("domain link=%v", v["next_url"])
	}
	if center := v["help_center"].(map[string]any); center["action"] != "view" || center["url"] != "https://"+slug+".help.example.net/help" {
		t.Fatalf("optional help center=%v", center)
	}
	update("preview", "done", 422)
	domain := setDomain(t, api, admin, "help.move.example", 200)
	dns.set("_help-center-verify.help.move.example", domain.token())
	checkDomain(t, api, admin, 200)
	if got := readSetup(t, api, admin).state(t, "custom_domain"); got != "done" {
		t.Fatalf("TXT state=%s", got)
	}
	// A real successful address check completes cutover (the transport serves this API's help center).
	dns.setCNAME("help.move.example", domain.cnameTarget()+".")
	checkDomain(t, api, admin, 200)
	if got := readSetup(t, api, admin).state(t, "cname"); got != "done" {
		t.Fatalf("cutover state=%s", got)
	}
	kbCall(t, api, http.MethodPut, "/api/v1/help-center/search-console", map[string]any{"google_site_verification": "abcdefghijklmnop123456"}, admin, 200, nil)
	v = read()
	last := v["steps"].([]any)[4].(map[string]any)
	if last["detail"] != "verification_present" || last["state"] != "none" {
		t.Fatalf("Search Console should show existing verification without claiming sitemap submission: %v", last)
	}
	update("search_console", "done", 200)
	kbCall(t, api, http.MethodDelete, domainPath, nil, admin, 200, nil)
	kbCall(t, api, http.MethodPut, "/api/v1/help-center", map[string]any{"enabled": false, "title": "Move Desk", "locale": "en"}, admin, 200, nil)
	for _, key := range []string{"custom_domain", "cname", "search_console"} {
		if got := readSetup(t, api, admin).state(t, key); got != "done" {
			t.Fatalf("completed %s regressed to %s", key, got)
		}
	}
}

func TestMoveGuideSkipIsReversibleAndWorkspaceScoped(t *testing.T) {
	polar := newFakePolarAPI(t)
	api, service, pool, _ := testAPIWithConfig(t, func(c *httpapi.Config) { c.Billing = polar.config() })
	mine := createSession(t, api, service, "Move Mine", "admin@move-mine.example")
	theirs := createSession(t, api, service, "Move Theirs", "admin@move-theirs.example")
	if _, err := pool.Exec(t.Context(), `INSERT INTO workspace_onboarding(workspace_id,goal) SELECT id,'leave_helpdesk' FROM workspaces`); err != nil {
		t.Fatal(err)
	}
	put := func(step, state, token string, want int) {
		t.Helper()
		response := request(t, api, http.MethodPut, "/api/v1/workspace/setup/move", map[string]any{"step": step, "state": state}, token, nil)
		if response.Code != want {
			t.Fatalf("%s %s: %d %s", step, state, response.Code, response.Body.String())
		}
	}
	put("custom_domain", "skipped", mine, 200)
	put("custom_domain", "skipped", mine, 200)
	for _, key := range []string{"custom_domain", "plan", "cname", "search_console"} {
		if got := readSetup(t, api, mine).state(t, key); got != "skipped" {
			t.Fatalf("%s=%s", key, got)
		}
	}
	if got := readSetup(t, api, theirs).state(t, "custom_domain"); got != "none" {
		t.Fatalf("other workspace changed: %s", got)
	}
	put("custom_domain", "none", mine, 200)
	if got := readSetup(t, api, mine).state(t, "custom_domain"); got != "none" {
		t.Fatalf("restore=%s", got)
	}
	for _, value := range [][2]string{{"import", "done"}, {"custom_domain", "done"}, {"preview", "skipped"}, {"search_console", "done"}, {"nonsense", "done"}} {
		put(value[0], value[1], mine, 422)
	}
	put("custom_domain", "skipped", "", 401)
}

func TestMoveGuidePaymentComesFromTheBillingWebhook(t *testing.T) {
	f := newLimitsFixture(t, true)
	if _, err := f.pool.Exec(t.Context(), `INSERT INTO workspace_onboarding(workspace_id,goal) VALUES($1,'leave_helpdesk'),($2,'leave_helpdesk')`, f.workspaceA, f.workspaceB); err != nil {
		t.Fatal(err)
	}
	if got := readSetup(t, f.api, f.adminA).state(t, "plan"); got != "none" {
		t.Fatalf("trial plan=%s", got)
	}
	sub := activeTeam(f.workspaceA, "sub_move", f.clock.Now())
	f.billing.mustDeliver(t, "move-active", sub.event("subscription.active"))
	if got := readSetup(t, f.api, f.adminA).state(t, "plan"); got != "done" {
		t.Fatalf("paid plan=%s", got)
	}
	if got := readSetup(t, f.api, f.adminB).state(t, "plan"); got != "none" {
		t.Fatalf("other workspace plan=%s", got)
	}
	sub.Status = "canceled"
	sub.ModifiedAt = f.clock.Now().Add(time.Minute)
	f.billing.mustDeliver(t, "move-canceled", sub.event("subscription.canceled"))
	if got := readSetup(t, f.api, f.adminA).state(t, "plan"); got != "done" {
		t.Fatalf("completed plan regressed=%s", got)
	}
}

func TestVerifiedDomainAfterImportNeedsEnablingInsteadOfAnotherImport(t *testing.T) {
	api, service, pool, dns := testAPIWithConfig(t, nil)
	admin := createSession(t, api, service, "Imported Domain", "admin@imported-domain.example")
	slug := workspaceSlug(t, api, admin)
	domain := setDomain(t, api, admin, "help.imported-domain.example", 200)
	dns.set("_help-center-verify.help.imported-domain.example", domain.token())
	checkDomain(t, api, admin, 200)
	var view map[string]any
	kbCall(t, api, http.MethodGet, domainPath, nil, admin, 200, &view)
	if next := view["next_step"].(map[string]any); next["key"] != "import" {
		t.Fatalf("before import: %v", next)
	}
	insertImport(t, pool, slug, "finished")
	kbCall(t, api, http.MethodGet, domainPath, nil, admin, 200, &view)
	if next := view["next_step"].(map[string]any); next["key"] != "enable" || next["url"] != "/settings#help-center-heading" {
		t.Fatalf("after import: %v", next)
	}
}
