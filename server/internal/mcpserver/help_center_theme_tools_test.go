package mcpserver_test

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestHelpCenterThemeToolsCheckSaveAndPropose(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("Theme Tools Desk", "admin@theme-tools.example.com")
	low := map[string]any{"colors": map[string]any{"text": "#999999", "background": "#ffffff"}}

	read := e.connect(w.read, "")
	checked := e.ok(read, "colliedesk_help_center_theme_check", low)
	if at(t, checked, "ok") != false || !strings.HasPrefix(at(t, checked, "fields", "colors.text").(string), "Text on the background is 2.85:1") {
		t.Fatalf("check = %v", checked)
	}
	if theme := e.ok(read, "colliedesk_help_center_theme_get", nil); at(t, theme, "theme", "colors", "text") != nil {
		t.Fatalf("default theme = %v", theme)
	}

	send := e.connect(w.send, "")
	if proposed := e.ok(send, "colliedesk_help_center_theme_update", map[string]any{"colors": map[string]any{"primary": "#0b5cad"}}); proposed["agent_request"] == nil {
		t.Fatalf("send token update = %v, want an agent request", proposed)
	}

	admin := e.connect(w.adminTier, "")
	if message := e.fails(admin, "colliedesk_help_center_theme_update", low); !strings.Contains(message, "colors.text") || !strings.Contains(message, "4.5:1") {
		t.Fatalf("low contrast update: %q", message)
	}
	saved := e.ok(admin, "colliedesk_help_center_theme_update", map[string]any{"colors": map[string]any{"primary": "#0B5CAD"}, "body_font": "humanist-sans"})
	if at(t, saved, "theme", "colors", "primary") != "#0b5cad" || at(t, saved, "theme", "body_font") != "humanist-sans" {
		t.Fatalf("saved = %v", saved)
	}
	if theme := e.ok(read, "colliedesk_help_center_theme_get", nil); at(t, theme, "theme", "colors", "primary") != "#0b5cad" {
		t.Fatalf("theme after save = %v", theme)
	}
}

// An agent changes one color the documented way, get then update with the whole theme, after the
// logo was moved to the library's trash: the logo the theme already has does not block the save.
func TestHelpCenterThemeUpdateKeepsATrashedLogoItAlreadyHas(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("Theme Trashed Logo Tools", "admin@theme-trashed-logo-tools.example.com")
	admin := e.connect(w.adminTier, "")
	logo := at(t, e.ok(admin, "colliedesk_kb_image_upload", map[string]any{"data_base64": base64.StdEncoding.EncodeToString(kbPNG), "name": "Logo"}), "image", "id").(string)
	e.ok(admin, "colliedesk_help_center_theme_update", map[string]any{"logo_media_id": logo, "logo_alt": "Acme"})
	listed := e.ok(admin, "colliedesk_kb_media_list", map[string]any{"usage": "in_use"})
	if at(t, listed, "media", 0, "id") != logo || at(t, listed, "media", 0, "usage", "help_center", 0) != "logo" {
		t.Fatalf("in use = %v, want the logo with usage.help_center [logo]", listed)
	}
	if trashed := e.ok(admin, "colliedesk_kb_media_trash", map[string]any{"media_ids": []string{logo}}); at(t, trashed, "results", 0, "status") != "trashed" {
		t.Fatalf("trash = %v", trashed)
	}

	theme := at(t, e.ok(admin, "colliedesk_help_center_theme_get", nil), "theme").(map[string]any)
	update := map[string]any{"colors": map[string]any{"primary": "#7a3e00"}}
	for _, field := range []string{"logo_media_id", "favicon_media_id", "logo_alt", "linkback_url", "contact_url", "contact_phone", "body_font", "heading_font"} {
		if value := theme[field]; value != nil {
			update[field] = value
		}
	}
	saved := e.ok(admin, "colliedesk_help_center_theme_update", update)
	if at(t, saved, "theme", "logo_media_id") != logo || at(t, saved, "theme", "colors", "primary") != "#7a3e00" {
		t.Fatalf("saved = %v", saved)
	}
}
