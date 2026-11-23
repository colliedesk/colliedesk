package migrationcheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckAcceptsPairedDomainNamedMigrations(t *testing.T) {
	repo := migrationRepo(t, map[string]string{
		"20261014060000-core-helpdesk.up.sql":   "-- Core.\nSELECT 1;\n",
		"20261014060000-core-helpdesk.down.sql": "SELECT 1;\n",
	})
	if err := Check(repo, Options{}); err != nil {
		t.Fatalf("Check: %v", err)
	}
}

func TestCheckRejectsMigrationWithoutDownPair(t *testing.T) {
	repo := migrationRepo(t, map[string]string{
		"20261014060000-core-helpdesk.up.sql": "SELECT 1;\n",
	})
	err := Check(repo, Options{})
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
			"20261201000000-notes.up.sql":   "-- Notes.\nCREATE TABLE notes (workspace_id uuid);\n" + statement + "\n",
			"20261201000000-notes.down.sql": "DROP TABLE notes;\n",
		})
		err := Check(repo, Options{})
		if err == nil || !strings.Contains(err.Error(), "secure_workspace_table") {
			t.Errorf("%s: want an error naming the helper, got %v", statement, err)
		}
	}
}

func TestCheckAcceptsHandWrittenRowSecurityBeforeTheHelpers(t *testing.T) {
	repo := migrationRepo(t, map[string]string{
		"20261014135950-workspace-row-level-security.up.sql":   "-- Bootstrap.\nALTER TABLE notes ENABLE ROW LEVEL SECURITY;\nSELECT 1 FROM pg_policies;\n",
		"20261014135950-workspace-row-level-security.down.sql": "SELECT set_config('app.privileged', 'on', true);\n",
	})
	if err := Check(repo, Options{}); err != nil {
		t.Fatalf("Check: %v", err)
	}
}

func TestCheckAcceptsHandWrittenRowSecurityInTheHelpCenterHelpers(t *testing.T) {
	repo := migrationRepo(t, map[string]string{
		"20261122081247-help-center-row-security.up.sql":   "-- Help center reader.\nCREATE POLICY help_center_isolation ON notes USING (true);\nSELECT 1 FROM pg_policies;\n",
		"20261122081247-help-center-row-security.down.sql": "DROP POLICY help_center_isolation ON notes;\n",
	})
	if err := Check(repo, Options{}); err != nil {
		t.Fatalf("Check: %v", err)
	}
}

// The migration that defines release_help_center_table spells out what it undoes; a later down
// migration calls it instead.
func TestCheckAcceptsHandWrittenRowSecurityInTheHelpCenterReleaseHelper(t *testing.T) {
	repo := migrationRepo(t, map[string]string{
		"20261122220740-help-center-table-release.up.sql":   "-- Undo of secure_help_center_table.\nDROP POLICY help_center_isolation ON notes;\n",
		"20261122220740-help-center-table-release.down.sql": "SELECT 1;\n",
		"20261201000000-notes.up.sql":                       "-- Notes.\nSELECT secure_help_center_table('notes', 'true', 'SELECT');\n",
		"20261201000000-notes.down.sql":                     "SELECT release_help_center_table('notes');\n",
	})
	if err := Check(repo, Options{}); err != nil {
		t.Fatalf("Check: %v", err)
	}
}

func TestCheckAcceptsTheHelpCenterHelperCall(t *testing.T) {
	repo := migrationRepo(t, map[string]string{
		"20261201000000-notes.up.sql": "-- Notes.\nCREATE TABLE notes (workspace_id uuid);\nSELECT secure_workspace_table('notes');\n" +
			"SELECT secure_help_center_table('notes', 'true', 'SELECT');\n",
		"20261201000000-notes.down.sql": "DROP TABLE notes;\n",
	})
	if err := Check(repo, Options{}); err != nil {
		t.Fatalf("Check: %v", err)
	}
}

func TestCheckAcceptsTheHelperCalls(t *testing.T) {
	repo := migrationRepo(t, map[string]string{
		"20261201000000-notes.up.sql": "-- Notes.\nCREATE TABLE notes (workspace_id uuid);\nSELECT secure_workspace_table('notes');\n" +
			"SELECT limit_app_role('notes', 'SELECT', 'INSERT');\n",
		"20261201000000-notes.down.sql": "DROP TABLE notes;\n",
	})
	if err := Check(repo, Options{}); err != nil {
		t.Fatalf("Check: %v", err)
	}
}

func TestCheckIgnoresHelperWordsInComments(t *testing.T) {
	repo := migrationRepo(t, map[string]string{
		"20261201000000-notes.up.sql":   "/* no CREATE POLICY here,\n   secure_workspace_table writes it */\nCREATE TABLE notes (workspace_id uuid);\n-- row level security comes from the helper\nSELECT secure_workspace_table('notes');\n",
		"20261201000000-notes.down.sql": "DROP TABLE notes;\n",
	})
	if err := Check(repo, Options{}); err != nil {
		t.Fatal(err)
	}
}
