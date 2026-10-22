package httpapi_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/store"
)

// A logo or favicon is library media no article links: the purge and the unused filter must still
// leave it alone while the help center names it, in the trash or not.
func TestHelpCenterLogoIsNeverPurgedOrCalledUnused(t *testing.T) {
	api, service, pool, storage := testAPIWithStorage(t)
	admin := createSession(t, api, service, "Theme Purge", "admin@theme-purge.example.com")
	logo := uploadKBImage(t, api, admin, kbPNG, http.StatusCreated)
	icon := uploadKBImage(t, api, admin, pngImage(7), http.StatusCreated)
	kbCall(t, api, http.MethodPut, themePath, map[string]any{"logo_media_id": logo.ID, "favicon_media_id": icon.ID}, admin, http.StatusOK, nil)
	var details kbMediaView
	kbCall(t, api, http.MethodGet, "/api/v1/kb/media/"+logo.ID+"/details", nil, admin, http.StatusOK, &details)
	if !details.Kept {
		t.Fatal("the logo was not marked kept")
	}
	ctx := context.Background()
	// Even unkept, a month old and in no article, the help center's media stays.
	if _, err := pool.Exec(ctx, `UPDATE kb_media SET kept=false, created_at=now()-interval '31 days', last_uploaded_at=now()-interval '31 days'
		WHERE id IN ($1,$2)`, logo.ID, icon.ID); err != nil {
		t.Fatal(err)
	}
	for _, item := range kbMediaList(t, api, admin, "usage=unused").Items {
		if item.ID == logo.ID || item.ID == icon.ID {
			t.Fatalf("usage=unused lists the help center's %s", item.ID)
		}
	}
	var swept struct {
		Trashed int `json:"trashed"`
	}
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media/trash-unused", map[string]any{"before": "2100-01-01T00:00:00Z", "include_kept": true}, admin, http.StatusOK, &swept)
	if swept.Trashed != 0 {
		t.Fatalf("trash-unused moved %d of the help center's items", swept.Trashed)
	}
	purge := store.New(pool).WithObjects(storage)
	if _, err := purge.PurgeKBTrash(ctx); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{logo.ID, icon.ID} {
		kbCall(t, api, http.MethodGet, "/api/v1/kb/media/"+id+"/details", nil, admin, http.StatusOK, nil)
	}
	// A trashed logo or favicon waits there: neither the daily purge nor an admin's purge deletes it,
	// and a restore brings it back.
	for _, id := range []string{logo.ID, icon.ID} {
		kbCall(t, api, http.MethodDelete, "/api/v1/kb/media/"+id, nil, admin, http.StatusOK, nil)
	}
	if _, err := pool.Exec(ctx, `UPDATE kb_media SET deleted_at=now()-interval '31 days' WHERE id IN ($1,$2)`, logo.ID, icon.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := purge.PurgeKBTrash(ctx); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{logo.ID, icon.ID} {
		kbCall(t, api, http.MethodGet, "/api/v1/kb/media/"+id+"/details", nil, admin, http.StatusOK, nil)
	}
	var purged struct {
		Results []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"results"`
	}
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media/purge", map[string]any{"ids": []string{logo.ID, icon.ID}}, admin, http.StatusOK, &purged)
	if len(purged.Results) != 2 || purged.Results[0].Status != "in_use" || purged.Results[1].Status != "in_use" {
		t.Fatalf("admin purge of the trashed logo and favicon = %+v, want in_use for both", purged.Results)
	}
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media/"+icon.ID+"/restore", nil, admin, http.StatusOK, nil)
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media/"+logo.ID+"/restore", nil, admin, http.StatusOK, nil)
	var theme themeView
	kbCall(t, api, http.MethodGet, themePath, nil, admin, http.StatusOK, &theme)
	if theme.LogoMediaID == nil || *theme.LogoMediaID != logo.ID || theme.FaviconMediaID == nil || *theme.FaviconMediaID != icon.ID {
		t.Fatalf("theme after restore = %+v", theme)
	}
}

func TestExportCarriesTheHelpCenterTheme(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "Theme Export", "admin@theme-export.example.com")
	logo := uploadKBImage(t, api, admin, kbPNG, http.StatusCreated)
	kbCall(t, api, http.MethodPut, themePath, fullTheme(logo.ID), admin, http.StatusOK, nil)
	response := request(t, api, http.MethodGet, "/api/v1/export", nil, admin, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("export status=%d body=%s", response.Code, response.Body.String())
	}
	lines := ndjsonLines(t, unzip(t, response.Body.Bytes())["help-center.ndjson"])
	if len(lines) != 1 {
		t.Fatalf("help-center.ndjson = %v", lines)
	}
	center := lines[0]
	for key, want := range map[string]any{
		"logo_media_id": logo.ID, "favicon_media_id": logo.ID, "logo_alt": "Acme", "linkback_url": "https://acme.example/",
		"contact_url": "mailto:help@acme.example", "contact_phone": "+1 (555) 010-2030", "color_primary": "#0b5cad",
		"color_header": "#ffffff", "color_header_text": "#16201c", "color_background": "#ffffff", "color_text": "#1f2328",
		"color_link": "#0b5cad", "color_footer": "#f6f8fa", "body_font": "humanist-sans", "heading_font": "transitional-serif",
	} {
		if center[key] != want {
			t.Errorf("help-center.ndjson %s = %v, want %v", key, center[key], want)
		}
	}
}
