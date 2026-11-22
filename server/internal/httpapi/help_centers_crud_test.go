package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

type helpCenterView struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Handle  string `json:"handle"`
	Primary bool   `json:"primary"`
	Enabled bool   `json:"enabled"`
	Title   string `json:"title"`
	Locale  string `json:"locale"`
	URL     string `json:"url"`
}

type helpCenterList struct {
	HelpCenters []helpCenterView `json:"help_centers"`
	Limit       int              `json:"limit"`
}

func createHelpCenter(t *testing.T, api http.Handler, token, name, handle string) helpCenterView {
	t.Helper()
	var created helpCenterView
	kbCall(t, api, http.MethodPost, "/api/v1/help-centers", map[string]any{"name": name, "handle": handle,
		"title": name + " answers", "locale": "en", "enabled": true}, token, http.StatusCreated, &created)
	return created
}

func listHelpCenters(t *testing.T, api http.Handler, token string) helpCenterList {
	t.Helper()
	var list helpCenterList
	kbCall(t, api, http.MethodGet, "/api/v1/help-centers", nil, token, http.StatusOK, &list)
	return list
}

// A help center made through the API is listed after the primary, is read and changed by its id, and
// is served below its handle with its own title; a handle already taken is refused.
func TestAHelpCenterIsCreatedListedChangedAndServedBelowItsHandle(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "Crud Desk", "admin@crud-desk.example.com")
	slug := workspaceSlug(t, api, admin)
	enableHelpCenter(t, api, admin, "Main answers", true)

	hardware := createHelpCenter(t, api, admin, "Hardware", "hardware")
	if hardware.ID == "" || hardware.Primary || hardware.Handle != "hardware" || hardware.Name != "Hardware" || !hardware.Enabled {
		t.Fatalf("created = %+v", hardware)
	}
	if !strings.HasSuffix(hardware.URL, "/help/"+slug+"/hardware") {
		t.Errorf("url = %q, want the primary's address below /hardware", hardware.URL)
	}
	list := listHelpCenters(t, api, admin)
	if len(list.HelpCenters) != 2 || !list.HelpCenters[0].Primary || list.HelpCenters[1].ID != hardware.ID || list.Limit != 20 {
		t.Fatalf("list = %+v", list)
	}
	if list.HelpCenters[0].Title != "Main answers" {
		t.Errorf("primary = %+v", list.HelpCenters[0])
	}

	path := "/api/v1/help-centers/" + hardware.ID
	var changed helpCenterView
	kbCall(t, api, http.MethodPatch, path, map[string]any{"title": "Hardware help", "name": "Devices"}, admin, http.StatusOK, &changed)
	if changed.Title != "Hardware help" || changed.Name != "Devices" || changed.Handle != "hardware" || !changed.Enabled {
		t.Fatalf("changed = %+v", changed)
	}
	var read helpCenterView
	kbCall(t, api, http.MethodGet, path, nil, admin, http.StatusOK, &read)
	if read != changed {
		t.Errorf("read = %+v, want %+v", read, changed)
	}
	site := helpCenterSite(api)
	wantPage(t, visit(site, "/help/"+slug+"/hardware", "198.51.100.40"), http.StatusOK, []string{"Hardware help"}, []string{"Main answers"})

	taken := kbCall(t, api, http.MethodPost, "/api/v1/help-centers", map[string]any{"name": "Again", "handle": "hardware", "locale": "en"}, admin, http.StatusConflict, nil)
	if code := errorCode(t, taken); code != "duplicate_handle" {
		t.Errorf("taken handle code = %q", code)
	}
	route := kbCall(t, api, http.MethodPost, "/api/v1/help-centers", map[string]any{"name": "Search", "handle": "search", "locale": "en"}, admin, http.StatusBadRequest, nil)
	if code := errorCode(t, route); code != "invalid_request" {
		t.Errorf("route handle code = %q", code)
	}
	kbCall(t, api, http.MethodPatch, "/api/v1/help-centers/"+list.HelpCenters[0].ID, map[string]any{"handle": "main"}, admin, http.StatusBadRequest, nil)
}

// The primary help center cannot be deleted. Deleting another one takes its pages off line and
// releases its custom domain at once: the host name no longer serves it, even from a warm cache, and
// another help center can claim it.
func TestDeletingAHelpCenterRefusesThePrimaryAndReleasesItsDomain(t *testing.T) {
	f := newTwoHelpCenters(t)
	list := listHelpCenters(t, f.api, f.admin)
	primary := list.HelpCenters[0]
	refused := kbCall(t, f.api, http.MethodDelete, "/api/v1/help-centers/"+primary.ID, nil, f.admin, http.StatusConflict, nil)
	if code := errorCode(t, refused); code != "primary_help_center" {
		t.Errorf("primary delete code = %q", code)
	}
	wantPage(t, f.at(hostHardware, "/"), http.StatusOK, []string{"Hardware answers"}, nil)

	kbCall(t, f.api, http.MethodDelete, "/api/v1/help-centers/"+f.hardware, nil, f.admin, http.StatusNoContent, nil)
	wantPage(t, f.at(hostHardware, "/"), http.StatusNotFound, nil, []string{"Hardware answers"})
	wantPage(t, f.at(hostMain, "/hardware"), http.StatusNotFound, nil, []string{"Hardware answers"})
	wantPage(t, f.at(hostMain, articlePath(f.main)), http.StatusOK, []string{"bramble"}, nil)
	kbCall(t, f.api, http.MethodGet, "/api/v1/help-centers/"+f.hardware, nil, f.admin, http.StatusNotFound, nil)
	kbCall(t, f.api, http.MethodDelete, "/api/v1/help-centers/"+f.hardware, nil, f.admin, http.StatusNotFound, nil)
	if got := listHelpCenters(t, f.api, f.admin); len(got.HelpCenters) != 1 {
		t.Fatalf("after delete = %+v", got)
	}

	again := createHelpCenter(t, f.api, f.admin, "Hardware", "hardware")
	kbCall(t, f.api, http.MethodPut, "/api/v1/help-center/domain?help_center_id="+again.ID, map[string]any{"hostname": hostHardware},
		f.admin, http.StatusOK, nil)
}

// A workspace has at most 20 help centers, the primary included; the 21st is refused and says why.
// A deleted one frees its place.
func TestTheTwentyFirstHelpCenterIsRefused(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "Many Desk", "admin@many-desk.example.com")
	enableHelpCenter(t, api, admin, "Main answers", true)
	var last helpCenterView
	for index := 2; index <= 20; index++ {
		last = createHelpCenter(t, api, admin, fmt.Sprintf("Shelf %d", index), fmt.Sprintf("shelf-%d", index))
	}
	refused := kbCall(t, api, http.MethodPost, "/api/v1/help-centers", map[string]any{"name": "One more", "handle": "extra-shelf", "locale": "en"},
		admin, http.StatusConflict, nil)
	if code := errorCode(t, refused); code != "help_center_limit" || !strings.Contains(refused.Body.String(), "20 help centers") {
		t.Fatalf("21st = %s", refused.Body.String())
	}
	kbCall(t, api, http.MethodDelete, "/api/v1/help-centers/"+last.ID, nil, admin, http.StatusNoContent, nil)
	createHelpCenter(t, api, admin, "One more", "extra-shelf")
}

// Another workspace's help center is not found by any route that names it, and nothing of it changes.
func TestAnotherWorkspacesHelpCenterIsNotFound(t *testing.T) {
	f := newTwoHelpCenters(t)
	other := createSession(t, f.api, f.service, "Other Desk", "admin@other-desk.example.com")
	path := "/api/v1/help-centers/" + f.hardware
	kbCall(t, f.api, http.MethodGet, path, nil, other, http.StatusNotFound, nil)
	kbCall(t, f.api, http.MethodPatch, path, map[string]any{"title": "Taken"}, other, http.StatusNotFound, nil)
	kbCall(t, f.api, http.MethodDelete, path, nil, other, http.StatusNotFound, nil)
	kbCall(t, f.api, http.MethodGet, "/api/v1/help-center/theme?help_center_id="+f.hardware, nil, other, http.StatusNotFound, nil)
	kbCall(t, f.api, http.MethodPut, "/api/v1/help-center/search-console?help_center_id="+f.hardware,
		map[string]any{"google_site_verification": "taken-token-0123456789"}, other, http.StatusNotFound, nil)
	kbCall(t, f.api, http.MethodDelete, "/api/v1/help-center/domain?help_center_id="+f.hardware, nil, other, http.StatusNotFound, nil)
	kbCall(t, f.api, http.MethodPost, "/api/v1/kb/collections", map[string]any{"name": "Stolen", "help_center_ids": []string{f.hardware}},
		other, http.StatusBadRequest, nil)
	kbCall(t, f.api, http.MethodPut, "/api/v1/kb/order", map[string]any{"parent_id": nil, "help_center_id": f.hardware,
		"collection_ids": []string{}, "article_ids": []string{}}, other, http.StatusNotFound, nil)
	if got := listHelpCenters(t, f.api, other); len(got.HelpCenters) != 1 || got.HelpCenters[0].ID == f.hardware {
		t.Fatalf("other's list = %+v", got)
	}
	var title string
	if err := f.pool.QueryRow(context.Background(), `SELECT title FROM help_centers WHERE id=$1`, f.hardware).Scan(&title); err != nil || title != "Hardware answers" {
		t.Fatalf("title = %q, %v", title, err)
	}
	wantPage(t, f.at(hostHardware, "/"), http.StatusOK, []string{"Hardware answers"}, nil)
	kbCall(t, f.api, http.MethodGet, "/api/v1/help-center/theme?help_center_id=not-a-uuid", nil, f.admin, http.StatusBadRequest, nil)
}
