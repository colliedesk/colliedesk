package migrationcheck

import (
	"strings"
	"testing"
)

func TestCheckRejectsALineOverOneHundredCharacters(t *testing.T) {
	long := "-- " + strings.Repeat("x", 98)
	repo := migrationRepo(t, map[string]string{
		"20261201000000-notes.up.sql":   "-- Notes.\nCREATE TABLE notes (id int);\n" + long + "\n",
		"20261201000000-notes.down.sql": "DROP TABLE notes;\n",
	})
	err := Check(repo, Options{})
	if err == nil || !strings.Contains(err.Error(), "20261201000000-notes.up.sql:3") || !strings.Contains(err.Error(), "100") {
		t.Fatalf("want the long line named with its number, got %v", err)
	}
}

func TestCheckAcceptsLongLinesInAFunctionBodyAnInstallAlreadyStores(t *testing.T) {
	body := "CREATE FUNCTION f() RETURNS int LANGUAGE sql AS $$\n    SELECT " + strings.Repeat("1 + ", 30) + "1\n$$;\n"
	repo := migrationRepo(t, map[string]string{
		"20261020000000-f.up.sql":   "-- f.\n" + body,
		"20261020000000-f.down.sql": "DROP FUNCTION f();\n",
	})
	if err := Check(repo, Options{}); err != nil {
		t.Fatalf("Check: %v", err)
	}
	repo = migrationRepo(t, map[string]string{
		"20261201000000-f.up.sql":   "-- f.\n" + body,
		"20261201000000-f.down.sql": "DROP FUNCTION f();\n",
	})
	if err := Check(repo, Options{}); err == nil || !strings.Contains(err.Error(), "100") {
		t.Fatalf("want a new migration's long function line refused, got %v", err)
	}
}

func TestCheckRejectsAnUpMigrationWithoutAnOpeningComment(t *testing.T) {
	repo := migrationRepo(t, map[string]string{
		"20261201000000-notes.up.sql":   "CREATE TABLE notes (id int);\n",
		"20261201000000-notes.down.sql": "DROP TABLE notes;\n",
	})
	err := Check(repo, Options{})
	if err == nil || !strings.Contains(err.Error(), "comment") {
		t.Fatalf("want a missing opening comment refused, got %v", err)
	}
}

func TestCheckRejectsHandWrittenRowSecurityInAnOlderUpMigration(t *testing.T) {
	repo := migrationRepo(t, map[string]string{
		"20261020000000-notes.up.sql":   "-- Notes.\nCREATE TABLE notes (workspace_id uuid);\nALTER TABLE notes ENABLE ROW LEVEL SECURITY;\n",
		"20261020000000-notes.down.sql": "DROP TABLE notes;\n",
	})
	err := Check(repo, Options{})
	if err == nil || !strings.Contains(err.Error(), "secure_workspace_table") {
		t.Fatalf("want the helper named, got %v", err)
	}
}
