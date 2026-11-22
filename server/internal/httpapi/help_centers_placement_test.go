package httpapi_test

import (
	"net/http"
	"slices"
	"strings"
	"testing"
)

type placedCollectionView struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	UpdatedAt     string   `json:"updated_at"`
	HelpCenterIDs []string `json:"help_center_ids"`
}

// A top-level collection is shown on the help centers help_center_ids names: a new one on the
// primary unless it says otherwise, a nested one on those of its top-level collection. Its readers see
// it there and nowhere else.
func TestATopLevelCollectionIsShownOnTheHelpCentersItNames(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "Shelf Desk", "admin@shelf-desk.example.com")
	slug := workspaceSlug(t, api, admin)
	enableHelpCenter(t, api, admin, "Main answers", true)
	hardware := createHelpCenter(t, api, admin, "Hardware", "hardware")
	primary := listHelpCenters(t, api, admin).HelpCenters[0]

	var cables, router placedCollectionView
	kbCall(t, api, http.MethodPost, "/api/v1/kb/collections", map[string]any{"name": "Cables", "help_center_ids": []string{hardware.ID}},
		admin, http.StatusCreated, &cables)
	kbCall(t, api, http.MethodPost, "/api/v1/kb/collections", map[string]any{"name": "Router"}, admin, http.StatusCreated, &router)
	if !slices.Equal(cables.HelpCenterIDs, []string{hardware.ID}) || !slices.Equal(router.HelpCenterIDs, []string{primary.ID}) {
		t.Fatalf("cables on %v, router on %v", cables.HelpCenterIDs, router.HelpCenterIDs)
	}
	kbArticle(t, api, admin, map[string]any{"collection_id": cables.ID, "title": "Swap a cable", "status": "published", "body_md": "copperline"})
	kbArticle(t, api, admin, map[string]any{"collection_id": router.ID, "title": "Reset the router", "status": "published", "body_md": "bramble"})

	site := helpCenterSite(api)
	home, below := "/help/"+slug, "/help/"+slug+"/hardware"
	wantPage(t, visit(site, below, "198.51.100.50"), http.StatusOK, []string{"Hardware answers", "Cables"}, []string{"Router"})
	wantPage(t, visit(site, home, "198.51.100.51"), http.StatusOK, []string{"Main answers", "Router"}, []string{"Cables"})

	var both placedCollectionView
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/collections/"+cables.ID, map[string]any{"help_center_ids": []string{primary.ID, hardware.ID},
		"updated_at": cables.UpdatedAt}, admin, http.StatusOK, &both)
	if len(both.HelpCenterIDs) != 2 {
		t.Fatalf("both = %v", both.HelpCenterIDs)
	}
	wantPage(t, visit(site, home, "198.51.100.52"), http.StatusOK, []string{"Router", "Cables"}, nil)

	var nested placedCollectionView
	kbCall(t, api, http.MethodPost, "/api/v1/kb/collections", map[string]any{"name": "Copper", "parent_id": cables.ID}, admin, http.StatusCreated, &nested)
	if len(nested.HelpCenterIDs) != 2 {
		t.Errorf("nested follows its top: %v", nested.HelpCenterIDs)
	}
	refused := kbCall(t, api, http.MethodPost, "/api/v1/kb/collections", map[string]any{"name": "Fiber", "parent_id": cables.ID,
		"help_center_ids": []string{hardware.ID}}, admin, http.StatusBadRequest, nil)
	if !strings.Contains(refused.Body.String(), "top-level") {
		t.Errorf("nested with help_center_ids = %s", refused.Body.String())
	}
	kbCall(t, api, http.MethodPost, "/api/v1/kb/collections", map[string]any{"name": "Ghost",
		"help_center_ids": []string{"00000000-0000-4000-8000-000000000001"}}, admin, http.StatusBadRequest, nil)
}

// The home order of each help center is its own: ordering one leaves the other's order, and the
// collections' own positions, as they were.
func TestEachHelpCenterKeepsItsOwnHomeOrder(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "Order Desk", "admin@order-desk.example.com")
	slug := workspaceSlug(t, api, admin)
	enableHelpCenter(t, api, admin, "Main answers", true)
	hardware := createHelpCenter(t, api, admin, "Hardware", "hardware")
	primary := listHelpCenters(t, api, admin).HelpCenters[0]
	both := []string{primary.ID, hardware.ID}
	var alpha, beta placedCollectionView
	kbCall(t, api, http.MethodPost, "/api/v1/kb/collections", map[string]any{"name": "Alpha shelf", "help_center_ids": both}, admin, http.StatusCreated, &alpha)
	kbCall(t, api, http.MethodPost, "/api/v1/kb/collections", map[string]any{"name": "Beta shelf", "help_center_ids": both}, admin, http.StatusCreated, &beta)
	for _, c := range []placedCollectionView{alpha, beta} {
		kbArticle(t, api, admin, map[string]any{"collection_id": c.ID, "title": "In " + c.Name, "status": "published", "body_md": "text"})
	}

	var order struct {
		HelpCenterID  *string  `json:"help_center_id"`
		CollectionIDs []string `json:"collection_ids"`
	}
	kbCall(t, api, http.MethodPut, "/api/v1/kb/order", map[string]any{"parent_id": nil, "help_center_id": hardware.ID,
		"collection_ids": []string{beta.ID, alpha.ID}, "article_ids": []string{}}, admin, http.StatusOK, &order)
	if order.HelpCenterID == nil || *order.HelpCenterID != hardware.ID || !slices.Equal(order.CollectionIDs, []string{beta.ID, alpha.ID}) {
		t.Fatalf("order = %+v", order)
	}
	site := helpCenterSite(api)
	wantOrder := func(path, client, first, second string) {
		t.Helper()
		body := visit(site, path, client).Body.String()
		if a, b := strings.Index(body, first), strings.Index(body, second); a < 0 || b < 0 || a > b {
			t.Errorf("%s: %q at %d, %q at %d", path, first, a, second, b)
		}
	}
	wantOrder("/help/"+slug+"/hardware", "198.51.100.60", "Beta shelf", "Alpha shelf")
	wantOrder("/help/"+slug, "198.51.100.61", "Alpha shelf", "Beta shelf")

	stale := kbCall(t, api, http.MethodPut, "/api/v1/kb/order", map[string]any{"parent_id": nil, "help_center_id": hardware.ID,
		"collection_ids": []string{beta.ID}, "article_ids": []string{}}, admin, http.StatusConflict, nil)
	if code := errorCode(t, stale); code != "order_stale" {
		t.Errorf("stale code = %q", code)
	}
	kbCall(t, api, http.MethodPut, "/api/v1/kb/order", map[string]any{"parent_id": alpha.ID, "help_center_id": hardware.ID,
		"collection_ids": []string{}, "article_ids": []string{}}, admin, http.StatusBadRequest, nil)
}

// The help center routes without help_center_id act on the primary help center, as they did before a
// workspace could have several; with it, on the one it names.
func TestHelpCenterSettingsWithoutAnIDActOnThePrimary(t *testing.T) {
	f := newTwoHelpCenters(t)
	type console struct {
		Google string `json:"google_site_verification"`
	}
	kbCall(t, f.api, http.MethodPut, "/api/v1/help-center/search-console", map[string]any{"google_site_verification": "primary-token-0123456789"}, f.admin, http.StatusOK, nil)
	kbCall(t, f.api, http.MethodPut, "/api/v1/help-center/search-console?help_center_id="+f.hardware,
		map[string]any{"google_site_verification": "hardware-token-0123456789"}, f.admin, http.StatusOK, nil)
	var primary, hardware console
	kbCall(t, f.api, http.MethodGet, "/api/v1/help-center/search-console", nil, f.admin, http.StatusOK, &primary)
	kbCall(t, f.api, http.MethodGet, "/api/v1/help-center/search-console?help_center_id="+f.hardware, nil, f.admin, http.StatusOK, &hardware)
	if primary.Google != "primary-token-0123456789" || hardware.Google != "hardware-token-0123456789" {
		t.Fatalf("primary %+v, hardware %+v", primary, hardware)
	}

	type theme struct {
		Theme struct {
			Colors struct {
				Primary *string `json:"primary"`
			} `json:"colors"`
		} `json:"theme"`
	}
	kbCall(t, f.api, http.MethodPut, "/api/v1/help-center/theme?help_center_id="+f.hardware,
		map[string]any{"colors": map[string]any{"primary": "#aa3300"}, "custom_css": nil}, f.admin, http.StatusOK, nil)
	var mainTheme, hardwareTheme theme
	kbCall(t, f.api, http.MethodGet, "/api/v1/help-center/theme", nil, f.admin, http.StatusOK, &mainTheme)
	kbCall(t, f.api, http.MethodGet, "/api/v1/help-center/theme?help_center_id="+f.hardware, nil, f.admin, http.StatusOK, &hardwareTheme)
	if mainTheme.Theme.Colors.Primary != nil || hardwareTheme.Theme.Colors.Primary == nil || *hardwareTheme.Theme.Colors.Primary != "#aa3300" {
		t.Fatalf("main %+v, hardware %+v", mainTheme, hardwareTheme)
	}
	wantPage(t, f.at(hostHardware, "/"), http.StatusOK, []string{"#aa3300"}, nil)
	wantPage(t, f.at(hostMain, "/"), http.StatusOK, nil, []string{"#aa3300"})

	type domainView struct {
		Hostname *string `json:"hostname"`
	}
	var mainDomain, hardwareDomain domainView
	kbCall(t, f.api, http.MethodGet, "/api/v1/help-center/domain", nil, f.admin, http.StatusOK, &mainDomain)
	kbCall(t, f.api, http.MethodGet, "/api/v1/help-center/domain?help_center_id="+f.hardware, nil, f.admin, http.StatusOK, &hardwareDomain)
	if mainDomain.Hostname == nil || *mainDomain.Hostname != hostMain || hardwareDomain.Hostname == nil || *hardwareDomain.Hostname != hostHardware {
		t.Fatalf("main domain %+v, hardware domain %+v", mainDomain, hardwareDomain)
	}

	type languages struct {
		Languages []struct {
			Locale string `json:"locale"`
		} `json:"languages"`
	}
	kbCall(t, f.api, http.MethodPut, "/api/v1/help-center/languages?help_center_id="+f.hardware, map[string]any{"languages": []map[string]any{}},
		f.admin, http.StatusOK, nil)
	var mainLanguages, hardwareLanguages languages
	kbCall(t, f.api, http.MethodGet, "/api/v1/help-center/languages", nil, f.admin, http.StatusOK, &mainLanguages)
	kbCall(t, f.api, http.MethodGet, "/api/v1/help-center/languages?help_center_id="+f.hardware, nil, f.admin, http.StatusOK, &hardwareLanguages)
	if len(mainLanguages.Languages) != 1 || len(hardwareLanguages.Languages) != 0 {
		t.Fatalf("main %+v, hardware %+v", mainLanguages, hardwareLanguages)
	}
}
