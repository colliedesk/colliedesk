package migrationcheck

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func equivalenceDatabase(t *testing.T) string {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is unset; skipping real PostgreSQL test")
	}
	return url
}

func TestEquivalentAcceptsAReflowedMigration(t *testing.T) {
	url := equivalenceDatabase(t)
	before := []Migration{{Name: "1-notes.up.sql", SQL: "CREATE TABLE notes (id int PRIMARY KEY, body text NOT NULL DEFAULT ''); INSERT INTO notes VALUES (1, 'a');"}}
	after := []Migration{{Name: "1-notes.up.sql", SQL: "-- Notes.\nCREATE TABLE notes (\n    id int PRIMARY KEY,\n    body text NOT NULL DEFAULT ''\n);\nINSERT INTO notes VALUES (1, 'a');\n"}}
	if err := Equivalent(context.Background(), url, before, after); err != nil {
		t.Fatalf("Equivalent: %v", err)
	}
}

func TestEquivalentNamesTheFirstMigrationThatChangesTheSchema(t *testing.T) {
	url := equivalenceDatabase(t)
	before := []Migration{
		{Name: "1-notes.up.sql", SQL: "CREATE TABLE notes (id int);"},
		{Name: "2-note-size.up.sql", SQL: "ALTER TABLE notes ADD COLUMN size int;"},
		{Name: "3-note-index.up.sql", SQL: "CREATE INDEX notes_size ON notes(size);"},
	}
	after := []Migration{
		before[0],
		{Name: "2-note-size.up.sql", SQL: "ALTER TABLE notes ADD COLUMN size bigint;"},
		before[2],
	}
	err := Equivalent(context.Background(), url, before, after)
	if err == nil || !strings.Contains(err.Error(), "2-note-size.up.sql") || !strings.Contains(err.Error(), "bigint") {
		t.Fatalf("want an error naming 2-note-size.up.sql and the bigint column, got %v", err)
	}
}

func TestEquivalentComparesRows(t *testing.T) {
	url := equivalenceDatabase(t)
	before := []Migration{{Name: "1-plans.up.sql", SQL: "CREATE TABLE plans (name text); INSERT INTO plans VALUES ('team');"}}
	after := []Migration{{Name: "1-plans.up.sql", SQL: "CREATE TABLE plans (name text); INSERT INTO plans VALUES ('teams');"}}
	err := Equivalent(context.Background(), url, before, after)
	if err == nil || !strings.Contains(err.Error(), "plans") {
		t.Fatalf("want an error naming the plans rows, got %v", err)
	}
}

func TestEquivalentComparesPoliciesAndGrants(t *testing.T) {
	url := equivalenceDatabase(t)
	table := "CREATE TABLE notes (workspace_id uuid); ALTER TABLE notes ENABLE ROW LEVEL SECURITY;"
	before := []Migration{{Name: "1-notes.up.sql", SQL: table + " CREATE POLICY own ON notes USING (workspace_id IS NOT NULL);"}}
	after := []Migration{{Name: "1-notes.up.sql", SQL: table + " CREATE POLICY own ON notes USING (true);"}}
	if err := Equivalent(context.Background(), url, before, after); err == nil || !strings.Contains(err.Error(), "policy") {
		t.Fatalf("want a policy difference, got %v", err)
	}
	before = []Migration{{Name: "1-notes.up.sql", SQL: "CREATE TABLE notes (id int); GRANT SELECT ON notes TO PUBLIC;"}}
	after = []Migration{{Name: "1-notes.up.sql", SQL: "CREATE TABLE notes (id int); GRANT SELECT, DELETE ON notes TO PUBLIC;"}}
	if err := Equivalent(context.Background(), url, before, after); err == nil || !strings.Contains(err.Error(), "notes") {
		t.Fatalf("want a grant difference, got %v", err)
	}
}

// A rewrite may define a function earlier than the old chain did, so older migrations can call it;
// the end of both chains must still be the same.
func TestEquivalentAllowsAFunctionDefinedEarlier(t *testing.T) {
	url := equivalenceDatabase(t)
	function := "CREATE OR REPLACE FUNCTION answer() RETURNS int LANGUAGE sql AS 'SELECT 42';"
	before := []Migration{
		{Name: "1-notes.up.sql", SQL: "CREATE TABLE notes (id int);"},
		{Name: "3-answer.up.sql", SQL: function},
	}
	after := []Migration{
		{Name: "1-notes.up.sql", SQL: "CREATE TABLE notes (id int);"},
		{Name: "2-answer-first.up.sql", SQL: function},
		{Name: "3-answer.up.sql", SQL: function},
	}
	if err := Equivalent(context.Background(), url, before, after); err != nil {
		t.Fatalf("Equivalent: %v", err)
	}
	after[2] = Migration{Name: "3-answer.up.sql", SQL: "SELECT 1;"}
	if err := Equivalent(context.Background(), url, before, after); err != nil {
		t.Fatalf("Equivalent with the function only defined earlier: %v", err)
	}
	after[1] = Migration{Name: "2-answer-first.up.sql", SQL: "CREATE FUNCTION answer() RETURNS int LANGUAGE sql AS 'SELECT 41';"}
	if err := Equivalent(context.Background(), url, before, after); err == nil || !strings.Contains(err.Error(), "answer") {
		t.Fatalf("want the different function at the end, got %v", err)
	}
}

func TestCheckRefusesAModifiedBaseMigrationWithoutADatabase(t *testing.T) {
	repo := gitMigrationRepo(t, map[string]string{
		"20261014060000-notes.up.sql":   "-- Notes.\nCREATE TABLE notes (id int);\n",
		"20261014060000-notes.down.sql": "DROP TABLE notes;\n",
	})
	writeMigration(t, repo, "20261014060000-notes.up.sql", "-- Notes.\nCREATE TABLE notes (\n    id int\n);\n")
	err := Check(repo, Options{Base: "HEAD"})
	if err == nil || !strings.Contains(err.Error(), "was modified") || !strings.Contains(err.Error(), "--database") {
		t.Fatalf("want a modified-migration error naming --database, got %v", err)
	}
}

func TestCheckAcceptsAModifiedBaseMigrationThatKeepsTheSchema(t *testing.T) {
	url := equivalenceDatabase(t)
	repo := gitMigrationRepo(t, map[string]string{
		"20261014060000-notes.up.sql":   "-- Notes.\nCREATE TABLE notes (id int);\n",
		"20261014060000-notes.down.sql": "DROP TABLE notes;\n",
	})
	writeMigration(t, repo, "20261014060000-notes.up.sql", "-- Notes.\nCREATE TABLE notes (\n    id int\n);\n")
	if err := Check(repo, Options{Base: "HEAD", DatabaseURL: url}); err != nil {
		t.Fatalf("Check: %v", err)
	}
	writeMigration(t, repo, "20261014060000-notes.up.sql", "-- Notes.\nCREATE TABLE notes (id bigint);\n")
	if err := Check(repo, Options{Base: "HEAD", DatabaseURL: url}); err == nil || !strings.Contains(err.Error(), "20261014060000-notes.up.sql") {
		t.Fatalf("want the changed schema refused, got %v", err)
	}
}

func gitMigrationRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	repo := migrationRepo(t, files)
	for _, args := range [][]string{
		{"init", "-q"},
		{"add", "."},
		{"-c", "user.name=test", "-c", "user.email=test@example.com", "commit", "-q", "-m", "base"},
	} {
		command := exec.Command("git", append([]string{"-C", repo}, args...)...)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	return repo
}

func writeMigration(t *testing.T, repo, name, contents string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, "server", "migrations", name), []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestEquivalentRollsBothChainsBack(t *testing.T) {
	url := equivalenceDatabase(t)
	before := []Migration{
		{Name: "1-notes.up.sql", SQL: "CREATE TABLE notes (id int);", Down: "DROP TABLE notes;"},
		{Name: "2-note-size.up.sql", SQL: "ALTER TABLE notes ADD COLUMN size int;", Down: "ALTER TABLE notes DROP COLUMN size;"},
	}
	after := []Migration{
		before[0],
		{Name: "2-note-size.up.sql", SQL: before[1].SQL, Down: "-- Removes the size.\nALTER TABLE notes\n    DROP COLUMN size;\n"},
	}
	if err := Equivalent(context.Background(), url, before, after); err != nil {
		t.Fatalf("Equivalent: %v", err)
	}
	after[1].Down = "SELECT 1;"
	err := Equivalent(context.Background(), url, before, after)
	if err == nil || !strings.Contains(err.Error(), "2-note-size") || !strings.Contains(err.Error(), "size") {
		t.Fatalf("want the down of 2-note-size refused, got %v", err)
	}
}

// A style problem in one migration must not hide that an edited applied migration changes the schema.
func TestCheckComparesTheSchemaEvenWhenAnotherMigrationHasAStyleProblem(t *testing.T) {
	url := equivalenceDatabase(t)
	repo := gitMigrationRepo(t, map[string]string{
		"20261014060000-notes.up.sql":   "-- Notes.\nCREATE TABLE notes (id int);\n",
		"20261014060000-notes.down.sql": "DROP TABLE notes;\n",
	})
	writeMigration(t, repo, "20261014060000-notes.up.sql", "-- Notes.\nCREATE TABLE notes (id bigint);\n")
	writeMigration(t, repo, "20261201000000-tags.up.sql", "CREATE TABLE tags (id int);\n")
	writeMigration(t, repo, "20261201000000-tags.down.sql", "DROP TABLE tags;\n")
	err := Check(repo, Options{Base: "HEAD", DatabaseURL: url})
	if err == nil || !strings.Contains(err.Error(), "open with") || !strings.Contains(err.Error(), "different schema") {
		t.Fatalf("want both the style problem and the schema difference, got %v", err)
	}
}
