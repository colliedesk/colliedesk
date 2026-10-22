package mcpserver_test

import (
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
