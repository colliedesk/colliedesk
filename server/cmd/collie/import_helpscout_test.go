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

	"github.com/colliedesk/colliedesk/server/internal/helpscout"
	"github.com/jackc/pgx/v5/pgxpool"
)

const cliTestSecret = "hs-cli-secret-NEVER-PRINT"

func TestImportHelpScoutRejectsBadArgumentsWithoutPrintingTheSecret(t *testing.T) {
	databaseURL := cliDatabase(t)
	t.Setenv("HELPSCOUT_APP_ID", "app-id")
	t.Setenv("HELPSCOUT_APP_SECRET", cliTestSecret)
	cases := map[string][]string{
		"missing workspace":      {"import", "helpscout", "--mailbox", "501"},
		"missing secret env":     {"import", "helpscout", "--workspace", "cli-desk", "--app-secret-env", "NO_SUCH_SECRET_VAR"},
		"secret on command line": {"import", "helpscout", "--workspace", "cli-desk", "--app-secret=" + cliTestSecret},
		"negative mailbox":       {"import", "helpscout", "--workspace", "cli-desk", "--mailbox", "-3"},
		"since format":           {"import", "helpscout", "--workspace", "cli-desk", "--since", "14/10/2026"},
		"extra argument":         {"import", "helpscout", "--workspace", "cli-desk", "acme"},
	}
	for name, args := range cases {
		var output bytes.Buffer
		err := run(context.Background(), args, &output, databaseURL, "https://desk.example.com")
		if err == nil {
			t.Errorf("%s: accepted", name)
			continue
		}
		if strings.Contains(err.Error()+output.String(), cliTestSecret) {
			t.Errorf("%s: printed the App Secret: %v", name, err)
		}
	}
}

func TestImportHelpScoutWritesReportsAndExitsNonZeroUnlessOK(t *testing.T) {
	databaseURL := cliDatabase(t)
	t.Setenv("HELPSCOUT_APP_ID", "app-id")
	t.Setenv("HELPSCOUT_APP_SECRET", cliTestSecret)
	t.Setenv("ATTACHMENTS_DIR", t.TempDir())
	var created bytes.Buffer
	if err := run(context.Background(), []string{"create-workspace", "--name", "Import Desk", "--admin-email", "admin@import.example.com"}, &created, databaseURL, "https://desk.example.com"); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{helpscout.StatusFailed, helpscout.StatusUnverified, helpscout.StatusOK} {
		var received helpscout.Options
		previous := helpscoutRun
		helpscoutRun = func(_ context.Context, _ *pgxpool.Pool, options helpscout.Options) (*helpscout.Report, error) {
			received = options
			return &helpscout.Report{Status: status, Workspace: options.WorkspaceSlug, StartedAt: time.Date(2026, 10, 19, 1, 2, 3, 0, time.UTC),
				Conversations: helpscout.ConversationCount{EntityCount: helpscout.EntityCount{Source: 3, Imported: 2}},
				Failures:      []helpscout.Failure{{Entity: "attachment", HelpScoutID: "5", ConversationID: "1", Error: "size mismatch"}}}, nil
		}
		reports := t.TempDir()
		var output bytes.Buffer
		err := run(context.Background(), []string{"import", "helpscout", "--workspace", "import-desk", "--mailbox", "501",
			"--since", "2026-01-01", "--report-dir", reports}, &output, databaseURL, "https://desk.example.com")
		helpscoutRun = previous
		if status == helpscout.StatusOK {
			if err != nil {
				t.Fatalf("OK import returned %v", err)
			}
		} else if err == nil || !strings.Contains(err.Error(), status) {
			t.Fatalf("%s import error = %v", status, err)
		}
		if received.BaseURL != "https://api.helpscout.net" || received.MailboxID != 501 || received.AppID != "app-id" || received.AppSecret != cliTestSecret ||
			received.Storage == nil || received.Since == nil || !received.Since.Equal(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)) {
			t.Fatalf("importer options: base=%q mailbox=%d since=%v storage=%v", received.BaseURL, received.MailboxID, received.Since, received.Storage != nil)
		}
		if !strings.Contains(output.String(), "Status: "+status) || !strings.Contains(output.String(), "conversations 2/3") || strings.Contains(output.String(), cliTestSecret) {
			t.Fatalf("output:\n%s", output.String())
		}
		files, _ := filepath.Glob(filepath.Join(reports, "helpscout-import-import-desk-*"))
		if len(files) != 2 {
			t.Fatalf("report files = %v", files)
		}
		for _, file := range files {
			data, readErr := os.ReadFile(file)
			if readErr != nil || strings.Contains(string(data), cliTestSecret) {
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

func TestImportUndoNamesHelpScoutConversationIDs(t *testing.T) {
	databaseURL := cliDatabase(t)
	t.Setenv("ATTACHMENTS_DIR", t.TempDir())
	var created bytes.Buffer
	if err := run(context.Background(), []string{"create-workspace", "--name", "Scout Desk", "--admin-email", "admin@scout.example.com"}, &created, databaseURL, "https://desk.example.com"); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err := pool.Exec(context.Background(), `INSERT INTO import_items(workspace_id,source,entity,source_id,status,meta)
		SELECT id,'helpscout:mailbox:501','contact','9101','failed','{}' FROM workspaces WHERE slug='scout-desk'`); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := run(context.Background(), []string{"import", "undo", "--workspace", "scout-desk", "--dry-run"}, &output, databaseURL, "https://desk.example.com"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Would remove from helpscout:mailbox:501") || !strings.Contains(output.String(), "(Help Scout conversation ids): none") {
		t.Fatalf("undo output:\n%s", output.String())
	}
}
