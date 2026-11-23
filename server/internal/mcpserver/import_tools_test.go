package mcpserver_test

import (
	"context"
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

// An agent reads, without a second call, which help center each Freshdesk portal becomes and became.
func TestImportToolsNameTheHelpCenterOfEachFreshdeskPortal(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("Import Portals Desk", "admin@import-portals.example.com")
	admin := e.connect(w.adminTier, "")
	job := e.ok(admin, "colliedesk_import_preview", map[string]any{"domain": "acme.freshdesk.com", "api_key": "fd-key"})
	id, _ := at(t, job, "import", "id").(string)
	// The worker is not running here: the job is written as it leaves a finished import.
	if _, err := e.pool.Exec(context.Background(), `UPDATE import_jobs SET state='finished', phase='full', status='OK', started_at=now(), finished_at=now(),
		preview='{"knowledge_base":"readable","help_centers":[{"portal_id":6,"portal_name":"Help desk","categories":3,"primary":true,"status":"primary"},
			{"portal_id":5,"portal_name":"Hardware store","categories":2,"primary":false,"status":"planned"}]}',
		report='{"status":"OK","knowledge_base":{"help_centers":[{"portal_id":6,"portal_name":"Help desk","help_center_name":"Import Portals Desk","primary":true,"status":"primary"},
			{"portal_id":5,"portal_name":"Hardware store","help_center_name":"Hardware store","handle":"hardware-store","primary":false,"status":"created"}]}}',
		report_markdown='# Freshdesk import: OK' WHERE workspace_id=$1 AND id=$2`, w.id, id); err != nil {
		t.Fatal(err)
	}
	got := stringify(e.ok(admin, "colliedesk_import_get", map[string]any{"import_id": id}))
	if !strings.Contains(got, `"portal_name":"Hardware store"`) || !strings.Contains(got, `"status":"planned"`) {
		t.Fatalf("import get = %s", got)
	}
	report := stringify(e.ok(admin, "colliedesk_import_report", map[string]any{"import_id": id}))
	if !strings.Contains(report, `"handle":"hardware-store"`) || !strings.Contains(report, `"help_center_name":"Hardware store"`) {
		t.Fatalf("import report = %s", report)
	}
}
