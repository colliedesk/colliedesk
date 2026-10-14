package migrationcheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckAcceptsPairedDomainNamedMigrations(t *testing.T) {
	repo := migrationRepo(t, map[string]string{
		"20261014060000-core-helpdesk.up.sql":   "SELECT 1;\n",
		"20261014060000-core-helpdesk.down.sql": "SELECT 1;\n",
	})
	if err := Check(repo, ""); err != nil {
		t.Fatalf("Check: %v", err)
	}
}

func TestCheckRejectsMigrationWithoutDownPair(t *testing.T) {
	repo := migrationRepo(t, map[string]string{
		"20261014060000-core-helpdesk.up.sql": "SELECT 1;\n",
	})
	err := Check(repo, "")
	if err == nil || !strings.Contains(err.Error(), "matching up and down") {
		t.Fatalf("expected missing pair error, got %v", err)
	}
}

func migrationRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	repo := t.TempDir()
	dir := filepath.Join(repo, "server", "migrations")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, contents := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return repo
}
