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

func TestImportSendsProgressToStderrNotStdout(t *testing.T) {
	databaseURL := cliDatabase(t)
	t.Setenv("FRESHDESK_API_KEY", cliTestKey)
	t.Setenv("ATTACHMENTS_DIR", t.TempDir())
	createImportDesk(t, databaseURL)
	var stderr bytes.Buffer
	previous := progressOutput
	progressOutput = &stderr
	defer func() { progressOutput = previous }()
	stdout, err := runImportWithReport(t, databaseURL, &freshdesk.Report{Status: freshdesk.StatusOK}, func(options freshdesk.Options) {
		if options.Progress == nil {
			t.Error("importer received no progress writer")
			return
		}
		_, _ = options.Progress.Write([]byte("progress: importing tickets 1/2, elapsed 0s\n"))
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr.String(), "progress: importing tickets 1/2") || strings.Contains(stdout, "progress:") {
		t.Fatalf("progress must go to stderr only.\nstderr:\n%s\nstdout:\n%s", stderr.String(), stdout)
	}
}

// The import points old Freshdesk help center links at the help center's address, which it builds
// from the server's APP_ORIGIN and HELP_CENTER_DOMAIN, as the server does.
func TestImportGivesTheImporterTheHelpCenterAddress(t *testing.T) {
	databaseURL := cliDatabase(t)
	t.Setenv("FRESHDESK_API_KEY", cliTestKey)
	t.Setenv("ATTACHMENTS_DIR", t.TempDir())
	t.Setenv("APP_ORIGIN", " https://support-desk.example.org ")
	t.Setenv("HELP_CENTER_DOMAIN", "Help.Example.NET")
	createImportDesk(t, databaseURL)
	var origin, domain string
	if _, err := runImportWithReport(t, databaseURL, &freshdesk.Report{Status: freshdesk.StatusOK}, func(options freshdesk.Options) {
		origin, domain = options.Origin, options.HelpCenterDomain
	}); err != nil {
		t.Fatal(err)
	}
	if origin != "https://support-desk.example.org" || domain != "help.example.net" {
		t.Fatalf("importer got origin %q and help center domain %q", origin, domain)
	}
}

// Without APP_ORIGIN the import does not know the server's address: it must not point links at the
// local default the other commands fall back to, so the importer gets no origin at all.
func TestImportWithoutAppOriginGivesTheImporterNoOrigin(t *testing.T) {
	databaseURL := cliDatabase(t)
	t.Setenv("FRESHDESK_API_KEY", cliTestKey)
	t.Setenv("ATTACHMENTS_DIR", t.TempDir())
	t.Setenv("APP_ORIGIN", "")
	createImportDesk(t, databaseURL)
	origin := "unset"
	if _, err := runImportWithReport(t, databaseURL, &freshdesk.Report{Status: freshdesk.StatusOK}, func(options freshdesk.Options) {
		origin = options.Origin
	}); err != nil {
		t.Fatal(err)
	}
	if origin != "" {
		t.Fatalf("importer got origin %q without APP_ORIGIN", origin)
	}
}
