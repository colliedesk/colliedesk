package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/freshdesk"
	"github.com/jackc/pgx/v5/pgxpool"
)

const cliTestKey = "cli-secret-key-NEVER-PRINT"

func TestImportFreshdeskRejectsBadArgumentsWithoutPrintingTheKey(t *testing.T) {
	databaseURL := cliDatabase(t)
	t.Setenv("FRESHDESK_API_KEY", cliTestKey)
	cases := map[string][]string{
		"freshdesk.com domain": {"import", "freshdesk", "--workspace", "cli-desk", "--domain", "evil.example.com"},
		"missing key env":      {"import", "freshdesk", "--workspace", "cli-desk", "--domain", "acme.freshdesk.com", "--api-key-env", "NO_SUCH_KEY_VAR"},
		"since format":         {"import", "freshdesk", "--workspace", "cli-desk", "--domain", "acme.freshdesk.com", "--since", "14/10/2026"},
		"key on command line":  {"import", "freshdesk", "--workspace", "cli-desk", "--domain", "acme.freshdesk.com", "--api-key=" + cliTestKey},
		"unknown source":       {"import", "zendesk", "--workspace", "cli-desk"},
		"missing workspace":    {"import", "freshdesk", "--domain", "acme.freshdesk.com"},
		"bad last ticket id":   {"import", "freshdesk", "--workspace", "cli-desk", "--domain", "acme.freshdesk.com", "--last-ticket-id", "-3"},
	}
	for name, args := range cases {
		var output bytes.Buffer
		err := run(context.Background(), args, &output, databaseURL, "https://desk.example.com")
		if err == nil {
			t.Errorf("%s: accepted", name)
			continue
		}
		if strings.Contains(err.Error()+output.String(), cliTestKey) {
			t.Errorf("%s: printed the API key: %v", name, err)
		}
	}
}

func TestImportFreshdeskWritesReportsAndExitsNonZeroUnlessOK(t *testing.T) {
	databaseURL := cliDatabase(t)
	t.Setenv("FRESHDESK_API_KEY", cliTestKey)
	t.Setenv("ATTACHMENTS_DIR", t.TempDir())
	var created bytes.Buffer
	if err := run(context.Background(), []string{"create-workspace", "--name", "Import Desk", "--admin-email", "admin@import.example.com"}, &created, databaseURL, "https://desk.example.com"); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{freshdesk.StatusFailed, freshdesk.StatusUnverified, freshdesk.StatusOK} {
		var received freshdesk.Options
		previous := importRun
		importRun = func(_ context.Context, _ *pgxpool.Pool, options freshdesk.Options) (*freshdesk.Report, error) {
			received = options
			return &freshdesk.Report{Status: status, Workspace: options.WorkspaceSlug, StartedAt: time.Date(2026, 10, 14, 1, 2, 3, 0, time.UTC),
				Failures: []freshdesk.Failure{{Entity: "attachment", FreshdeskID: "5", TicketID: "1", Error: "size mismatch"}}}, nil
		}
		reports := t.TempDir()
		var output bytes.Buffer
		err := run(context.Background(), []string{"import", "freshdesk", "--workspace", "import-desk", "--domain", "Acme.freshdesk.com",
			"--last-ticket-id", "4200", "--report-dir", reports}, &output, databaseURL, "https://desk.example.com")
		importRun = previous
		if status == freshdesk.StatusOK {
			if err != nil {
				t.Fatalf("OK import returned %v", err)
			}
		} else if err == nil || !strings.Contains(err.Error(), status) {
			t.Fatalf("%s import error = %v", status, err)
		}
		if received.BaseURL != "https://acme.freshdesk.com" || received.LastTicketID != 4200 || received.APIKey != cliTestKey || received.Storage == nil {
			t.Fatalf("importer options: base=%q last=%d storage=%v", received.BaseURL, received.LastTicketID, received.Storage != nil)
		}
		if !strings.Contains(output.String(), "Status: "+status) || strings.Contains(output.String(), cliTestKey) {
			t.Fatalf("output:\n%s", output.String())
		}
		files, _ := filepath.Glob(filepath.Join(reports, "freshdesk-import-import-desk-*"))
		if len(files) != 2 {
			t.Fatalf("report files = %v", files)
		}
		for _, file := range files {
			data, readErr := os.ReadFile(file)
			if readErr != nil || strings.Contains(string(data), cliTestKey) {
				t.Fatalf("report %s: %v", file, readErr)
			}
			if strings.HasSuffix(file, ".json") {
				var report struct {
					Status string `json:"status"`
				}
				if err := json.Unmarshal(data, &report); err != nil || report.Status != status {
					t.Fatalf("json report status=%q err=%v", report.Status, err)
				}
			}
		}
	}
}

func cliDatabase(t *testing.T) string {
	t.Helper()
	baseURL := os.Getenv("TEST_DATABASE_URL")
	if baseURL == "" {
		t.Skip("TEST_DATABASE_URL is unset; skipping real PostgreSQL CLI test")
	}
	return isolatedDatabaseURL(t, baseURL)
}
