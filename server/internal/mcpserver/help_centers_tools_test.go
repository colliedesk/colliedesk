package mcpserver_test

import (
	"strings"
	"testing"
)

// An agent lists the help centers, adds one, works on its settings by help_center_id while the primary
// stays as it is, and deletes it; a read-only token cannot add one and another workspace cannot reach it.
func TestHelpCenterToolsAddOneWorkOnItByIDAndDeleteIt(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("Help Centers Tools", "admin@help-centers-tools.example.com")
	other := e.newWorkspace("Help Centers Elsewhere", "admin@help-centers-elsewhere.example.com")
	admin := e.connect(w.adminTier, "")

	listed := e.ok(admin, "colliedesk_help_centers_list", nil)
	if centers, _ := listed["help_centers"].([]any); len(centers) != 1 || at(t, centers[0], "primary") != true || listed["limit"] != float64(20) {
		t.Fatalf("list of a new workspace = %v, want the primary alone and a limit of 20", listed)
	}
	e.refused(e.connect(w.read, ""), "colliedesk_help_center_create", map[string]any{"name": "Hardware", "handle": "hardware", "locale": "en"})

	created := e.ok(admin, "colliedesk_help_center_create", map[string]any{"name": "Hardware", "handle": "hardware", "locale": "en", "title": "Hardware answers"})
	hardware, _ := at(t, created, "help_center", "id").(string)
	if at(t, created, "help_center", "name") != "Hardware" || at(t, created, "help_center", "primary") != false ||
		!strings.HasSuffix(at(t, created, "help_center", "url").(string), "/hardware") {
		t.Fatalf("create = %v, want Hardware, not primary, served below the primary at /hardware", created)
	}

	renamed := e.ok(admin, "colliedesk_help_center_update", map[string]any{"help_center_id": hardware, "enabled": true, "title": "Device answers", "locale": "en", "name": "Devices"})
	if at(t, renamed, "help_center", "name") != "Devices" || at(t, renamed, "help_center", "enabled") != true {
		t.Fatalf("update by id = %v", renamed)
	}
	if got := e.ok(admin, "colliedesk_help_center_get", map[string]any{"help_center_id": hardware}); at(t, got, "help_center", "title") != "Device answers" {
		t.Fatalf("get by id = %v", got)
	}
	if got := e.ok(admin, "colliedesk_help_center_get", nil); at(t, got, "help_center", "title") == "Device answers" {
		t.Fatalf("the primary took the second help center's title: %v", got)
	}
	if message := e.fails(admin, "colliedesk_help_center_update", map[string]any{"help_center_id": hardware, "enabled": true, "title": "x", "locale": "en", "require_approval": true}); !strings.Contains(message, "require_approval") {
		t.Errorf("require_approval on one help center: %q", message)
	}
	if message := e.fails(admin, "colliedesk_help_center_update", map[string]any{"enabled": true, "title": "x", "locale": "en", "handle": "other"}); !strings.Contains(message, "help_center_id") {
		t.Errorf("handle without help_center_id: %q", message)
	}

	const token = "hardware-site-token-0123"
	e.ok(admin, "colliedesk_help_center_search_console_update", map[string]any{"help_center_id": hardware, "google_site_verification": token})
	if got := e.ok(admin, "colliedesk_help_center_search_console_get", map[string]any{"help_center_id": hardware}); at(t, got, "search_console", "google_site_verification") != token {
		t.Fatalf("search console of the second help center = %v", got)
	}
	if got := e.ok(admin, "colliedesk_help_center_search_console_get", nil); at(t, got, "search_console", "google_site_verification") != nil {
		t.Fatalf("the primary's search console = %v, want untouched", got)
	}
	for _, tool := range []string{"colliedesk_help_center_theme_get", "colliedesk_help_center_domain_get", "colliedesk_help_center_languages_get"} {
		e.ok(admin, tool, map[string]any{"help_center_id": hardware})
	}
	previewed := e.ok(admin, "colliedesk_help_center_theme_check", map[string]any{"help_center_id": hardware, "preview": "home"})
	if html, _ := previewed["html"].(string); !strings.Contains(html, "Device answers") {
		t.Errorf("a preview of the second help center does not show its title: %v", previewed)
	}
	if html, _ := e.ok(admin, "colliedesk_help_center_theme_check", map[string]any{"preview": "home"})["html"].(string); strings.Contains(html, "Device answers") {
		t.Errorf("the primary's preview shows the second help center's title")
	}

	stranger := e.connect(other.adminTier, "")
	for _, tool := range []string{"colliedesk_help_center_domain_get", "colliedesk_help_center_get", "colliedesk_help_center_delete"} {
		if message := e.fails(stranger, tool, map[string]any{"help_center_id": hardware}); !strings.Contains(message, "colliedesk_help_centers_list") {
			t.Errorf("%s with another workspace's help center: %q", tool, message)
		}
	}

	if deleted := e.ok(admin, "colliedesk_help_center_delete", map[string]any{"help_center_id": hardware}); deleted["deleted"] != true || deleted["help_center_id"] != hardware {
		t.Fatalf("delete = %v", deleted)
	}
	if centers, _ := e.ok(admin, "colliedesk_help_centers_list", nil)["help_centers"].([]any); len(centers) != 1 {
		t.Fatalf("after delete the list holds %d help centers, want the primary alone", len(centers))
	}
	primary, _ := at(t, listed["help_centers"], 0, "id").(string)
	if message := e.fails(admin, "colliedesk_help_center_delete", map[string]any{"help_center_id": primary}); !strings.Contains(message, "primary") {
		t.Errorf("delete the primary: %q", message)
	}
}

// A top-level collection is shown on the help centers it names, and each help center's home keeps an
// order of its own.
func TestCollectionToolsChooseHelpCentersAndOrderEachHome(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("Shown On Tools", "admin@shown-on-tools.example.com")
	admin := e.connect(w.adminTier, "")
	hardware, _ := at(t, e.ok(admin, "colliedesk_help_center_create", map[string]any{"name": "Hardware", "handle": "hardware", "locale": "en"}), "help_center", "id").(string)

	routers := e.ok(admin, "colliedesk_kb_collection_create", map[string]any{"name": "Routers", "help_center_ids": []string{hardware}})
	if ids, _ := at(t, routers, "collection", "help_center_ids").([]any); len(ids) != 1 || ids[0] != hardware {
		t.Fatalf("a collection created for the second help center = %v", routers)
	}
	switches := e.ok(admin, "colliedesk_kb_collection_create", map[string]any{"name": "Switches", "help_center_ids": []string{hardware}})
	billing := e.ok(admin, "colliedesk_kb_collection_create", map[string]any{"name": "Billing"})
	if ids, _ := at(t, billing, "collection", "help_center_ids").([]any); len(ids) != 1 || ids[0] == hardware {
		t.Fatalf("a collection created without help_center_ids = %v, want the primary", billing)
	}
	routersID, switchesID := at(t, routers, "collection", "id"), at(t, switches, "collection", "id")

	ordered := e.ok(admin, "colliedesk_kb_order_set", map[string]any{"help_center_id": hardware, "collection_ids": []any{switchesID, routersID}})
	if at(t, ordered, "help_center_id") != hardware || at(t, ordered, "collection_ids", 0) != switchesID {
		t.Fatalf("order of the second help center's home = %v", ordered)
	}
	if message := e.fails(admin, "colliedesk_kb_order_set", map[string]any{"help_center_id": hardware, "collection_ids": []any{routersID}}); !strings.Contains(message, "Nothing changed") {
		t.Errorf("an order missing a collection shown there: %q", message)
	}

	both := []any{at(t, billing, "collection", "help_center_ids", 0), hardware}
	moved := e.ok(admin, "colliedesk_kb_collection_update", map[string]any{"collection_id": at(t, billing, "collection", "id"),
		"updated_at": at(t, billing, "collection", "updated_at"), "help_center_ids": both})
	if ids, _ := at(t, moved, "collection", "help_center_ids").([]any); len(ids) != 2 {
		t.Fatalf("billing shown on both = %v", moved)
	}
}
