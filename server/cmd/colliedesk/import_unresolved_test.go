package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/freshdesk"
	"github.com/jackc/pgx/v5/pgxpool"
)

func createImportDesk(t *testing.T, databaseURL string) {
	t.Helper()
	var created bytes.Buffer
	if err := run(context.Background(), []string{"create-workspace", "--name", "Import Desk", "--admin-email", "admin@import.example.com"}, &created, databaseURL, "https://desk.example.com"); err != nil {
		t.Fatal(err)
	}
}

func runImportWithReport(t *testing.T, databaseURL string, report *freshdesk.Report, during func(freshdesk.Options)) (string, error) {
	t.Helper()
	previous := importRun
	importRun = func(_ context.Context, _ *pgxpool.Pool, options freshdesk.Options) (*freshdesk.Report, error) {
		if during != nil {
			during(options)
		}
		report.Workspace, report.StartedAt = options.WorkspaceSlug, time.Date(2026, 10, 14, 1, 2, 3, 0, time.UTC)
		return report, nil
	}
	defer func() { importRun = previous }()
	var output bytes.Buffer
	err := run(context.Background(), []string{"import", "freshdesk", "--workspace", "import-desk", "--domain", "acme.freshdesk.com", "--report-dir", t.TempDir()},
		&output, databaseURL, "https://desk.example.com")
	return output.String(), err
}

func TestImportPrintsUnresolvedTicketIDsWhenTheArchiveWasRefused(t *testing.T) {
	databaseURL := cliDatabase(t)
	t.Setenv("FRESHDESK_API_KEY", cliTestKey)
	t.Setenv("ATTACHMENTS_DIR", t.TempDir())
	createImportDesk(t, databaseURL)
	report := &freshdesk.Report{Status: freshdesk.StatusUnverified, Tickets: freshdesk.TicketCount{UnresolvedIDs: []string{"32", "34", "62"}}}
	output, err := runImportWithReport(t, databaseURL, report, nil)
	if err == nil || !strings.Contains(err.Error(), "UNVERIFIED") {
		t.Fatalf("error = %v, want UNVERIFIED", err)
	}
	if !strings.Contains(output, "Unresolved ticket ids (archive unavailable, check in Freshdesk): 3: 32, 34, 62\n") {
		t.Fatalf("output has no unresolved id line:\n%s", output)
	}
	clean, _ := runImportWithReport(t, databaseURL, &freshdesk.Report{Status: freshdesk.StatusOK, Tickets: freshdesk.TicketCount{UnresolvedIDs: []string{}}}, nil)
	if strings.Contains(clean, "Unresolved ticket ids") {
		t.Fatalf("a clean report printed an unresolved line:\n%s", clean)
	}
}
