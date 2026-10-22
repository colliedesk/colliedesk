package mcpserver_test

import (
	"net/http"
	"strings"
	"testing"
)

// An agent starting an import names the old help center's own domain as a person does in the app.
func TestImportPreviewToolTakesTheOldHelpCenterAddress(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("Import Tools Desk", "admin@import-tools.example.com")
	admin := e.connect(w.adminTier, "")
	job := e.ok(admin, "colliedesk_import_preview", map[string]any{"domain": "acme.freshdesk.com", "api_key": "fd-key",
		"old_help_center_domain": "https://Support.Acme.com/"})
	if got := at(t, job, "import", "options", "old_help_center_domain"); got != "support.acme.com" {
		t.Fatalf("import preview options = %v", job)
	}
	_, listed := e.rest(http.MethodGet, "/api/v1/imports", nil, w.admin)
	if !strings.Contains(stringify(listed), `"old_help_center_domain":"support.acme.com"`) {
		t.Fatalf("imports = %v", listed)
	}
}
