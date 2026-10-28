package mcpserver_test

import (
	"strings"
	"testing"
)

const (
	consoleGoogle = "Ab3dEf6hIj9lMn2pQr5tUv8xYz1-Ab3dEf6hIj9lMn2"
	consoleBing   = "0A1B2C3D4E5F60718293A4B5C6D7E8F9"
)

func TestHelpCenterSearchConsoleToolsReadSaveAndPropose(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("Console Tools Desk", "admin@console-tools.example.com")
	read, send, admin := e.connect(w.read, ""), e.connect(w.send, ""), e.connect(w.adminTier, "")

	if empty := e.ok(read, "colliedesk_help_center_search_console_get", nil); at(t, empty, "search_console", "google_site_verification") != nil {
		t.Fatalf("a new workspace has a token: %v", empty)
	}
	if proposed := e.ok(send, "colliedesk_help_center_search_console_update", map[string]any{"google_site_verification": consoleGoogle}); proposed["agent_request"] == nil {
		t.Fatalf("a send token's save was not filed as a request: %v", proposed)
	}
	if message := e.fails(admin, "colliedesk_help_center_search_console_update", map[string]any{"google_site_verification": `"><script>`}); !strings.Contains(message, "google_site_verification") {
		t.Fatalf("a bad token was not refused by field: %q", message)
	}
	saved := e.ok(admin, "colliedesk_help_center_search_console_update", map[string]any{"google_site_verification": consoleGoogle, "bing_site_verification": consoleBing})
	if at(t, saved, "search_console", "google_site_verification") != consoleGoogle || at(t, saved, "search_console", "bing_site_verification") != consoleBing {
		t.Fatalf("saved = %v", saved)
	}
	kept := e.ok(admin, "colliedesk_help_center_search_console_update", map[string]any{"bing_site_verification": nil})
	if at(t, kept, "search_console", "google_site_verification") != consoleGoogle || at(t, kept, "search_console", "bing_site_verification") != nil {
		t.Fatalf("after clearing Bing = %v", kept)
	}
	if got := e.ok(read, "colliedesk_help_center_search_console_get", nil); at(t, got, "search_console", "google_site_verification") != consoleGoogle {
		t.Fatalf("read = %v", got)
	}
}
