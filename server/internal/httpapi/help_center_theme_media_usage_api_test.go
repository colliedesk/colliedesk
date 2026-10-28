package httpapi_test

import (
	"net/http"
	"slices"
	"testing"
)

// helpCenterUsageView is a library item with only what says the help center uses it.
type helpCenterUsageView struct {
	ID      string  `json:"id"`
	Kept    bool    `json:"kept"`
	PurgeAt *string `json:"purge_at"`
	Updated string  `json:"updated_at"`
	Usage   *struct {
		HelpCenter []string `json:"help_center"`
	} `json:"usage"`
}

func helpCenterUsage(t *testing.T, api http.Handler, token, path string) map[string]helpCenterUsageView {
	t.Helper()
	var page struct {
		Items []helpCenterUsageView `json:"items"`
	}
	kbCall(t, api, http.MethodGet, path, nil, token, http.StatusOK, &page)
	byID := map[string]helpCenterUsageView{}
	for _, item := range page.Items {
		byID[item.ID] = item
	}
	return byID
}

func usedByHelpCenter(item helpCenterUsageView) []string {
	if item.Usage == nil {
		return nil
	}
	return item.Usage.HelpCenter
}

// The library says which items the help center shows and as what, in the list, the details and the
// trash; a trashed logo has no purge date, since the purge keeps it.
func TestMediaLibraryShowsWhatTheHelpCenterUses(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "Theme Usage", "admin@theme-usage.example.com")
	logo := uploadKBImage(t, api, admin, kbPNG, http.StatusCreated)
	icon := uploadKBImage(t, api, admin, pngImage(13), http.StatusCreated)
	loose := uploadKBImage(t, api, admin, pngImage(14), http.StatusCreated)
	kbCall(t, api, http.MethodPut, themePath, map[string]any{"logo_media_id": logo.ID, "favicon_media_id": icon.ID}, admin, http.StatusOK, nil)

	inUse := helpCenterUsage(t, api, admin, "/api/v1/kb/media?usage=in_use")
	if got := usedByHelpCenter(inUse[logo.ID]); !slices.Equal(got, []string{"logo"}) {
		t.Fatalf("logo usage.help_center = %v, want [logo]", got)
	}
	if got := usedByHelpCenter(inUse[icon.ID]); !slices.Equal(got, []string{"favicon"}) {
		t.Fatalf("favicon usage.help_center = %v, want [favicon]", got)
	}
	if _, listed := inUse[loose.ID]; listed {
		t.Fatal("usage=in_use lists an item nobody uses")
	}
	all := helpCenterUsage(t, api, admin, "/api/v1/kb/media")
	if got := usedByHelpCenter(all[loose.ID]); got == nil || len(got) != 0 {
		t.Fatalf("unused item usage.help_center = %v, want []", got)
	}
	var details helpCenterUsageView
	kbCall(t, api, http.MethodGet, "/api/v1/kb/media/"+logo.ID+"/details", nil, admin, http.StatusOK, &details)
	if got := usedByHelpCenter(details); !slices.Equal(got, []string{"logo"}) || details.PurgeAt != nil {
		t.Fatalf("logo details = %+v", details)
	}

	kbCall(t, api, http.MethodDelete, "/api/v1/kb/media/"+logo.ID, nil, admin, http.StatusOK, nil)
	kbCall(t, api, http.MethodDelete, "/api/v1/kb/media/"+loose.ID, nil, admin, http.StatusOK, nil)
	trash := helpCenterUsage(t, api, admin, "/api/v1/kb/media/trash")
	if item := trash[logo.ID]; item.PurgeAt != nil || !slices.Equal(usedByHelpCenter(item), []string{"logo"}) {
		t.Fatalf("trashed logo = %+v, want no purge date and help_center [logo]", item)
	}
	if trash[loose.ID].PurgeAt == nil {
		t.Fatal("a trashed item nobody uses has no purge date")
	}
}

// The help center keeps its logo and favicon only while it shows them: an image it lets go of is no
// longer kept and "Remove unused" takes it, unless the other field still shows it.
func TestImageTheHelpCenterLetsGoOfCanBeRemovedAsUnused(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "Theme Let Go", "admin@theme-let-go.example.com")
	first := uploadKBImage(t, api, admin, kbPNG, http.StatusCreated)
	icon := uploadKBImage(t, api, admin, pngImage(15), http.StatusCreated)
	second := uploadKBImage(t, api, admin, pngImage(16), http.StatusCreated)
	for _, id := range []string{first.ID, icon.ID, second.ID} {
		kbCall(t, api, http.MethodPatch, "/api/v1/kb/media/"+id, map[string]any{"kept": false, "updated_at": itemUpdatedAt(t, api, admin, id)}, admin, http.StatusOK, nil)
	}
	kbCall(t, api, http.MethodPut, themePath, map[string]any{"logo_media_id": first.ID, "favicon_media_id": first.ID}, admin, http.StatusOK, nil)
	marked := itemUpdatedAt(t, api, admin, first.ID)
	// The logo moves to another image, but the favicon still shows the first one: it stays kept.
	kbCall(t, api, http.MethodPut, themePath, map[string]any{"logo_media_id": second.ID, "favicon_media_id": first.ID}, admin, http.StatusOK, nil)
	if !itemKept(t, api, admin, first.ID) {
		t.Fatal("an image the favicon still shows lost its kept mark")
	}
	kbCall(t, api, http.MethodPut, themePath, map[string]any{"logo_media_id": second.ID, "favicon_media_id": icon.ID}, admin, http.StatusOK, nil)
	kbCall(t, api, http.MethodPut, themePath, map[string]any{"logo_media_id": second.ID}, admin, http.StatusOK, nil)
	if itemKept(t, api, admin, first.ID) || itemKept(t, api, admin, icon.ID) || !itemKept(t, api, admin, second.ID) {
		t.Fatalf("kept: first %v, icon %v, second %v; want only the logo in use", itemKept(t, api, admin, first.ID), itemKept(t, api, admin, icon.ID), itemKept(t, api, admin, second.ID))
	}
	if itemUpdatedAt(t, api, admin, first.ID) == marked {
		t.Fatal("dropping the kept mark left updated_at as it was")
	}
	unused := helpCenterUsage(t, api, admin, "/api/v1/kb/media?usage=unused")
	if _, ok := unused[first.ID]; !ok {
		t.Fatal("the former logo is not listed as unused")
	}
	var swept struct {
		Trashed int `json:"trashed"`
	}
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media/trash-unused", map[string]any{"before": "2100-01-01T00:00:00Z"}, admin, http.StatusOK, &swept)
	trash := helpCenterUsage(t, api, admin, "/api/v1/kb/media/trash")
	_, firstGone := trash[first.ID]
	_, iconGone := trash[icon.ID]
	_, logoGone := trash[second.ID]
	if swept.Trashed != 2 || !firstGone || !iconGone || logoGone {
		t.Fatalf("remove unused moved %d: first %v, icon %v, current logo %v", swept.Trashed, firstGone, iconGone, logoGone)
	}
}

func itemKept(t *testing.T, api http.Handler, token, id string) bool {
	t.Helper()
	var item helpCenterUsageView
	kbCall(t, api, http.MethodGet, "/api/v1/kb/media/"+id+"/details", nil, token, http.StatusOK, &item)
	return item.Kept
}

func itemUpdatedAt(t *testing.T, api http.Handler, token, id string) string {
	t.Helper()
	var item helpCenterUsageView
	kbCall(t, api, http.MethodGet, "/api/v1/kb/media/"+id+"/details", nil, token, http.StatusOK, &item)
	return item.Updated
}
