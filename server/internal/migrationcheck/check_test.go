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

func TestCheckRejectsHandWrittenRowSecurityAfterTheHelpers(t *testing.T) {
	for _, statement := range []string{
		"ALTER TABLE notes ENABLE ROW LEVEL SECURITY;",
		"CREATE POLICY workspace_isolation ON notes USING (true);",
		"SELECT roles[1] FROM pg_policies WHERE tablename = 'workspaces';",
		"SELECT set_config('app.privileged', 'on', true);",
		"ALTER POLICY workspace_isolation ON notes USING (true);",
		"DROP POLICY workspace_isolation ON notes;",
		"SELECT '--'; CREATE POLICY everyone ON notes USING (true);",
	} {
		repo := migrationRepo(t, map[string]string{
			"20261201000000-notes.up.sql":   "CREATE TABLE notes (workspace_id uuid);\n" + statement + "\n",
			"20261201000000-notes.down.sql": "DROP TABLE notes;\n",
		})
		err := Check(repo, "")
		if err == nil || !strings.Contains(err.Error(), "secure_workspace_table") {
			t.Errorf("%s: want an error naming the helper, got %v", statement, err)
		}
	}
}

func TestCheckAcceptsHandWrittenRowSecurityBeforeTheHelpers(t *testing.T) {
	repo := migrationRepo(t, map[string]string{
		"20261014135950-workspace-row-level-security.up.sql":   "ALTER TABLE notes ENABLE ROW LEVEL SECURITY;\nSELECT 1 FROM pg_policies;\n",
		"20261014135950-workspace-row-level-security.down.sql": "SELECT set_config('app.privileged', 'on', true);\n",
	})
	if err := Check(repo, ""); err != nil {
		t.Fatalf("Check: %v", err)
	}
}

func TestCheckAcceptsTheHelperCalls(t *testing.T) {
	repo := migrationRepo(t, map[string]string{
		"20261201000000-notes.up.sql": "CREATE TABLE notes (workspace_id uuid);\nSELECT secure_workspace_table('notes');\n" +
			"SELECT limit_app_role('notes', 'SELECT', 'INSERT');\n",
		"20261201000000-notes.down.sql": "DROP TABLE notes;\n",
	})
	if err := Check(repo, ""); err != nil {
		t.Fatalf("Check: %v", err)
	}
}

func TestCheckIgnoresHelperWordsInComments(t *testing.T) {
	repo := migrationRepo(t, map[string]string{
		"20261201000000-notes.up.sql":   "/* no CREATE POLICY here,\n   secure_workspace_table writes it */\nCREATE TABLE notes (workspace_id uuid);\n-- row level security comes from the helper\nSELECT secure_workspace_table('notes');\n",
		"20261201000000-notes.down.sql": "DROP TABLE notes;\n",
	})
	if err := Check(repo, ""); err != nil {
		t.Fatal(err)
	}
}
