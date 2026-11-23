package mcpserver_test

import (
	"context"
	"net"
	"sync"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

// An agent sets the brand, the languages and the custom domain of a second help center by
// help_center_id: each read by that id shows the change and the primary keeps its own settings. A token
// that may only read the help centers is refused on every settings write that names a help center.
func TestHelpCenterSettingsToolsActOnTheHelpCenterTheyName(t *testing.T) {
	var mu sync.Mutex
	txt := map[string][]string{}
	lookupTXT := func(_ context.Context, name string) ([]string, error) {
		mu.Lock()
		defer mu.Unlock()
		if values, ok := txt[name]; ok {
			return values, nil
		}
		return nil, &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
	}
	lookupCNAME := func(_ context.Context, host string) (string, error) {
		return "", &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	e := newEnvBuilt(t, func(config *httpapi.Config) { config.LookupTXT, config.LookupCNAME = lookupTXT, lookupCNAME }, nil)
	w := e.newWorkspace("Settings By Center", "admin@settings-by-center.example.com")
	admin := e.connect(w.adminTier, "")
	created := e.ok(admin, "colliedesk_help_center_create", map[string]any{"name": "Devices", "handle": "devices", "locale": "en", "title": "Device answers"})
	devices, _ := at(t, created, "help_center", "id").(string)
	onDevices := func(arguments map[string]any) map[string]any {
		arguments["help_center_id"] = devices
		return arguments
	}

	saved := e.ok(admin, "colliedesk_help_center_theme_update", onDevices(map[string]any{"colors": map[string]any{"primary": "#7A1F5C"}, "custom_css": ""}))
	if at(t, saved, "theme", "colors", "primary") != "#7a1f5c" {
		t.Fatalf("theme update of the second help center = %v", saved)
	}
	if got := e.ok(admin, "colliedesk_help_center_theme_get", onDevices(map[string]any{})); at(t, got, "theme", "colors", "primary") != "#7a1f5c" {
		t.Errorf("theme of the second help center = %v, want its primary color", got)
	}
	if got := e.ok(admin, "colliedesk_help_center_theme_get", nil); at(t, got, "theme", "colors", "primary") != nil {
		t.Errorf("the primary's theme = %v, want its default", got)
	}

	e.ok(admin, "colliedesk_help_center_languages_set", onDevices(map[string]any{"languages": []any{map[string]any{"locale": "fr", "visible": true}}}))
	if got := e.ok(admin, "colliedesk_help_center_languages_get", onDevices(map[string]any{})); at(t, got, "languages", "languages", 0, "locale") != "fr" {
		t.Errorf("languages of the second help center = %v, want fr", got)
	}
	if got := e.ok(admin, "colliedesk_help_center_languages_get", nil); len(at(t, got, "languages", "languages").([]any)) != 0 {
		t.Errorf("the primary's languages = %v, want none added", got)
	}

	const host = "help.devices-by-id.example"
	set := e.ok(admin, "colliedesk_help_center_domain_set", onDevices(map[string]any{"hostname": host}))
	records, _ := at(t, set, "domain", "records").([]any)
	if at(t, set, "domain", "hostname") != host || len(records) != 2 {
		t.Fatalf("domain set on the second help center = %v", set)
	}
	if got := e.ok(admin, "colliedesk_help_center_domain_get", nil); at(t, got, "domain", "status") != "none" {
		t.Errorf("the primary's domain = %v, want none", got)
	}
	mu.Lock()
	txt["_help-center-verify."+host] = []string{records[1].(map[string]any)["value"].(string)}
	mu.Unlock()
	if checked := e.ok(admin, "colliedesk_help_center_domain_check", onDevices(map[string]any{})); at(t, checked, "domain", "status") != "active" {
		t.Errorf("check of the second help center's domain = %v, want active", checked)
	}
	if got := e.ok(admin, "colliedesk_help_center_domain_get", onDevices(map[string]any{})); at(t, got, "domain", "hostname") != host || at(t, got, "domain", "status") != "active" {
		t.Errorf("domain of the second help center = %v", got)
	}
	if removed := e.ok(admin, "colliedesk_help_center_domain_remove", onDevices(map[string]any{})); at(t, removed, "domain", "status") != "none" {
		t.Errorf("remove on the second help center = %v", removed)
	}

	read := e.connect(w.read, "")
	for tool, arguments := range map[string]map[string]any{
		"colliedesk_help_center_theme_update":          {"colors": map[string]any{"primary": "#0b5cad"}, "custom_css": ""},
		"colliedesk_help_center_languages_set":         {"languages": []any{map[string]any{"locale": "de", "visible": true}}},
		"colliedesk_help_center_domain_set":            {"hostname": "help.refused-by-id.example"},
		"colliedesk_help_center_domain_check":          {},
		"colliedesk_help_center_domain_remove":         {},
		"colliedesk_help_center_search_console_update": {"google_site_verification": "refused-token-0123"},
		"colliedesk_help_center_analytics_set":         {"provider": "plausible", "enabled": false},
		"colliedesk_help_center_update":                {"enabled": true, "title": "Refused", "locale": "en"},
		"colliedesk_kb_order_set":                      {"collection_ids": []any{}},
	} {
		e.refused(read, tool, onDevices(arguments))
	}
	if got := e.ok(read, "colliedesk_help_center_theme_get", onDevices(map[string]any{})); at(t, got, "theme", "colors", "primary") != "#7a1f5c" {
		t.Errorf("theme of the second help center after the refused writes = %v", got)
	}
}
