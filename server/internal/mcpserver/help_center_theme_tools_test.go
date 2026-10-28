package mcpserver_test

import (
	"encoding/base64"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/mcpserver"
)

func TestHelpCenterThemeToolsCheckSaveAndPropose(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("Theme Tools Desk", "admin@theme-tools.example.com")
	low := map[string]any{"colors": map[string]any{"text": "#999999", "background": "#ffffff"}, "custom_css": ""}

	read := e.connect(w.read, "")
	checked := e.ok(read, "colliedesk_help_center_theme_check", low)
	if at(t, checked, "ok") != false || !strings.HasPrefix(at(t, checked, "fields", "colors.text").(string), "Text on the background is 2.85:1") {
		t.Fatalf("check = %v", checked)
	}
	// With preview, the check also renders the page the way the settings card shows it, and saves nothing.
	previewed := e.ok(read, "colliedesk_help_center_theme_check", map[string]any{"colors": map[string]any{"text": "#999999"}, "preview": "article"})
	page, _ := previewed["html"].(string)
	if at(t, previewed, "ok") != false || !strings.Contains(page, "<h1>An example article</h1>") || !strings.Contains(page, "--ink:#999999;") ||
		!strings.Contains(page, "script-src 'none'") || strings.Contains(page, "<script") {
		t.Fatalf("check with preview = %v", previewed)
	}
	if plain := e.ok(read, "colliedesk_help_center_theme_check", low); plain["html"] != nil {
		t.Fatalf("check without preview carries html: %v", plain)
	}
	if theme := e.ok(read, "colliedesk_help_center_theme_get", nil); at(t, theme, "theme", "colors", "text") != nil {
		t.Fatalf("default theme = %v", theme)
	}

	send := e.connect(w.send, "")
	if proposed := e.ok(send, "colliedesk_help_center_theme_update", map[string]any{"colors": map[string]any{"primary": "#0b5cad"}, "custom_css": nil}); proposed["agent_request"] == nil {
		t.Fatalf("send token update = %v, want an agent request", proposed)
	}

	admin := e.connect(w.adminTier, "")
	if message := e.fails(admin, "colliedesk_help_center_theme_update", low); !strings.Contains(message, "colors.text") || !strings.Contains(message, "4.5:1") {
		t.Fatalf("low contrast update: %q", message)
	}
	saved := e.ok(admin, "colliedesk_help_center_theme_update", map[string]any{"colors": map[string]any{"primary": "#0B5CAD"}, "body_font": "humanist-sans", "custom_css": ""})
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
	e.ok(admin, "colliedesk_help_center_theme_update", map[string]any{"logo_media_id": logo, "logo_alt": "Acme", "custom_css": ""})
	listed := e.ok(admin, "colliedesk_kb_media_list", map[string]any{"usage": "in_use"})
	if at(t, listed, "media", 0, "id") != logo || at(t, listed, "media", 0, "usage", "help_center", 0) != "logo" {
		t.Fatalf("in use = %v, want the logo with usage.help_center [logo]", listed)
	}
	if trashed := e.ok(admin, "colliedesk_kb_media_trash", map[string]any{"media_ids": []string{logo}}); at(t, trashed, "results", 0, "status") != "trashed" {
		t.Fatalf("trash = %v", trashed)
	}

	theme := at(t, e.ok(admin, "colliedesk_help_center_theme_get", nil), "theme").(map[string]any)
	update := map[string]any{"colors": map[string]any{"primary": "#7a3e00"}}
	for _, field := range []string{"logo_media_id", "favicon_media_id", "logo_alt", "linkback_url", "contact_url", "contact_phone", "body_font", "heading_font", "custom_css"} {
		if value := theme[field]; value != nil {
			update[field] = value
		}
	}
	saved := e.ok(admin, "colliedesk_help_center_theme_update", update)
	if at(t, saved, "theme", "logo_media_id") != logo || at(t, saved, "theme", "colors", "primary") != "#7a3e00" {
		t.Fatalf("saved = %v", saved)
	}
}

// Custom CSS goes through the same tools: checked and previewed with a read token, saved with an admin
// token, refused at its line and column, and cleared by sending it empty.
func TestHelpCenterThemeToolsTakeCustomCSS(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("Theme CSS Tools", "admin@theme-css-tools.example.com")
	css := ".masthead{border-bottom:2px solid #0b5cad}"

	read := e.connect(w.read, "")
	previewed := e.ok(read, "colliedesk_help_center_theme_check", map[string]any{"custom_css": css, "preview": "home"})
	if page, _ := previewed["html"].(string); !strings.Contains(page, css) {
		t.Fatalf("check with preview = %v", previewed)
	}

	admin := e.connect(w.adminTier, "")
	if message := e.fails(admin, "colliedesk_help_center_theme_update", map[string]any{"custom_css": "a{background:url(x)}"}); !strings.Contains(message, "line 1, column 14: url() is not allowed") {
		t.Fatalf("refused custom CSS: %q", message)
	}
	if saved := e.ok(admin, "colliedesk_help_center_theme_update", map[string]any{"custom_css": css}); at(t, saved, "theme", "custom_css") != css {
		t.Fatalf("saved = %v", saved)
	}
	// The update replaces the whole theme, so it must say what custom CSS to keep: one without it is
	// refused before it reaches the server and the CSS stays.
	if message := e.fails(admin, "colliedesk_help_center_theme_update", map[string]any{"body_font": "mono"}); !strings.Contains(message, "custom_css") {
		t.Fatalf("update without custom_css: %q", message)
	}
	if theme := e.ok(read, "colliedesk_help_center_theme_get", nil); at(t, theme, "theme", "custom_css") != css {
		t.Fatalf("theme after save = %v", theme)
	}
	if cleared := e.ok(admin, "colliedesk_help_center_theme_update", map[string]any{"custom_css": ""}); at(t, cleared, "theme", "custom_css") != "" {
		t.Fatalf("cleared = %v", cleared)
	}
}

// An agent builds its call from the tool's schema: the update, which replaces the whole theme, names
// custom_css as required (null or "" for none); the check, which saves nothing, does not.
func TestHelpCenterThemeUpdateToolRequiresCustomCSS(t *testing.T) {
	seen := 0
	for _, spec := range mcpserver.Catalog() {
		if spec.Name != "colliedesk_help_center_theme_update" && spec.Name != "colliedesk_help_center_theme_check" {
			continue
		}
		seen++
		var schema struct {
			Required   []string                   `json:"required"`
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if err := json.Unmarshal([]byte(spec.Input), &schema); err != nil {
			t.Fatalf("%s input schema: %v", spec.Name, err)
		}
		if want := spec.Name == "colliedesk_help_center_theme_update"; slices.Contains(schema.Required, "custom_css") != want {
			t.Errorf("%s required = %v, want custom_css required %v", spec.Name, schema.Required, want)
		}
		// The limit is in bytes, which a JSON schema cannot count: no maxLength that counts characters.
		if strings.Contains(string(schema.Properties["custom_css"]), "maxLength") || !strings.Contains(string(schema.Properties["custom_css"]), "32,768 bytes") {
			t.Errorf("%s custom_css schema = %s", spec.Name, schema.Properties["custom_css"])
		}
	}
	if seen != 2 {
		t.Fatalf("found %d of the two theme tools", seen)
	}
}
