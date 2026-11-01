package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// collie import bundle reads an archive, checks it with --dry-run, imports it, and collie import undo
// removes it by its source.
func TestImportBundleFromTheCommandLine(t *testing.T) {
	databaseURL := cliDatabase(t)
	t.Setenv("ATTACHMENTS_DIR", t.TempDir())
	var created bytes.Buffer
	if err := run(context.Background(), []string{"create-workspace", "--name", "Bundle Desk", "--admin-email", "admin@bundle-cli.example.com"}, &created, databaseURL, "https://desk.example.com"); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)
	manifest, _ := json.Marshal(map[string]any{"format": "colliedesk-bundle/1", "source": "cli",
		"contacts": []any{map[string]any{"id": "c1", "name": "Ada", "email": "ada@bundle-cli.example.com"}},
		"conversations": []any{map[string]any{"id": "t1", "contact": "c1", "subject": "Hello", "status": "open", "priority": "normal", "created_at": at, "updated_at": at,
			"messages": []any{map[string]any{"id": "m1", "kind": "incoming", "body": "Hello", "created_at": at}}}},
		"counts": map[string]any{"contacts": 1, "conversations": 1, "incoming": 1}})
	path := filepath.Join(t.TempDir(), "bundle.zip")
	var archive bytes.Buffer
	w := zip.NewWriter(&archive)
	f, _ := w.Create("bundle.json")
	_, _ = f.Write(manifest)
	_ = w.Close()
	if err := os.WriteFile(path, archive.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	reports := t.TempDir()
	for _, args := range [][]string{{"--dry-run"}, {}} {
		var output bytes.Buffer
		command := append([]string{"import", "bundle", "--workspace", "bundle-desk", "--report-dir", reports}, args...)
		if err := run(context.Background(), append(command, path), &output, databaseURL, "https://desk.example.com"); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, output.String())
		}
		if !strings.Contains(output.String(), "Status: OK") || !strings.Contains(output.String(), "conversations 1/1/1") {
			t.Fatalf("%v output:\n%s", args, output.String())
		}
	}
	var undone bytes.Buffer
	if err := run(context.Background(), []string{"import", "undo", "--workspace", "bundle-desk", "--source", "bundle:cli"}, &undone, databaseURL, "https://desk.example.com"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(undone.String(), "Removed from bundle:cli: 1 conversations, 1 contacts") {
		t.Fatalf("undo output:\n%s", undone.String())
	}
	// A report that cannot be written never hides what was imported: the outcome is printed first.
	var unwritten bytes.Buffer
	err := run(context.Background(), []string{"import", "bundle", "--workspace", "bundle-desk", "--report-dir", filepath.Join(path, "not-a-directory"), path}, &unwritten, databaseURL, "https://desk.example.com")
	if err == nil || !strings.Contains(err.Error(), "imported") || !strings.Contains(unwritten.String(), "Status: OK") || !strings.Contains(unwritten.String(), "conversations 1/1/1") {
		t.Fatalf("with a report it cannot write: %v\n%s", err, unwritten.String())
	}
}
