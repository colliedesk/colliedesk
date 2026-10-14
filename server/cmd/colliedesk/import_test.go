package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestImportFreshdeskWritesReportsAndFailsOnPartialImport(t *testing.T) {
	databaseURL := cliDatabase(t)
	t.Setenv("FRESHDESK_API_KEY", cliTestKey)
	t.Setenv("ATTACHMENTS_DIR", t.TempDir())
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, _, _ := r.BasicAuth()
		if user != cliTestKey && r.URL.Path != "/files/1" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		body := map[string]string{
			"/api/v2/agents": `[]`, "/api/v2/companies": `[]`, "/api/v2/contacts": `[]`,
			"/api/v2/tickets":                 `[{"id":1,"subject":"Hi","status":2,"priority":1,"requester_id":9,"updated_at":"2025-05-01T00:00:00Z","created_at":"2025-05-01T00:00:00Z"}]`,
			"/api/v2/tickets/1":               `{"id":1,"subject":"Hi","status":2,"priority":1,"requester_id":9,"description_text":"Hello","tags":[],"attachments":[{"id":5,"name":"a.txt","content_type":"text/plain","size":999,"attachment_url":"` + "http://" + r.Host + `/files/1"}],"updated_at":"2025-05-01T00:00:00Z","created_at":"2025-05-01T00:00:00Z"}`,
			"/api/v2/tickets/1/conversations": `[]`,
			"/api/v2/tickets/1/time_entries":  `[]`,
			"/api/v2/contacts/9":              `{"id":9,"name":"Nine","email":"nine@example.com","created_at":"2025-05-01T00:00:00Z","updated_at":"2025-05-01T00:00:00Z"}`,
			"/files/1":                        `abc`,
		}[r.URL.Path]
		if body == "" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	defer fake.Close()
	previous := freshdeskBaseURL
	freshdeskBaseURL = func(string) string { return fake.URL }
	t.Cleanup(func() { freshdeskBaseURL = previous })

	var created bytes.Buffer
	if err := run(context.Background(), []string{"create-workspace", "--name", "Import Desk", "--admin-email", "admin@import.example.com"}, &created, databaseURL, "https://desk.example.com"); err != nil {
		t.Fatal(err)
	}
	reports := t.TempDir()
	var output bytes.Buffer
	err := run(context.Background(), []string{"import", "freshdesk", "--workspace", "import-desk", "--domain", "acme.freshdesk.com", "--report-dir", reports}, &output, databaseURL, "https://desk.example.com")
	if err == nil || !strings.Contains(err.Error(), "FAILED") {
		t.Fatalf("partial import error = %v, output:\n%s", err, output.String())
	}
	if !strings.Contains(output.String(), "Status: FAILED") || strings.Contains(output.String(), cliTestKey) {
		t.Fatalf("output:\n%s", output.String())
	}
	files, _ := filepath.Glob(filepath.Join(reports, "freshdesk-import-import-desk-*"))
	if len(files) != 2 {
		t.Fatalf("report files = %v", files)
	}
	for _, file := range files {
		data, readErr := os.ReadFile(file)
		if readErr != nil || strings.Contains(string(data), cliTestKey) || !strings.Contains(string(data), "size mismatch") {
			t.Fatalf("report %s: %v\n%s", file, readErr, data)
		}
		if strings.HasSuffix(file, ".json") {
			var report struct {
				Status string `json:"status"`
			}
			if err := json.Unmarshal(data, &report); err != nil || report.Status != "FAILED" {
				t.Fatalf("json report status=%q err=%v", report.Status, err)
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
