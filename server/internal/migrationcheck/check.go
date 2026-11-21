package migrationcheck

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var migrationPattern = regexp.MustCompile(`^(\d{14})-([a-z0-9]+(?:-[a-z0-9]+)*)\.(up|down)\.sql$`)

// Options says what Check compares the migrations with.
type Options struct {
	// Base is a Git commit or ref whose migrations are applied history.
	Base string
	// DatabaseURL is a PostgreSQL database where Check proves that edited applied migrations still
	// build the same schema (Equivalent). Without it an edited applied migration is refused.
	DatabaseURL string
}

// Check validates migration names, pairs and style and, when options.Base is set, applied history.
func Check(repo string, options Options) error {
	dir := filepath.Join(repo, "server", "migrations")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}
	pairs := make(map[string]map[string]string)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		match := migrationPattern.FindStringSubmatch(entry.Name())
		if match == nil {
			return fmt.Errorf("invalid migration filename %q", entry.Name())
		}
		stem := match[1] + "-" + match[2]
		if pairs[stem] == nil {
			pairs[stem] = make(map[string]string)
		}
		pairs[stem][match[3]] = entry.Name()
	}
	for stem, directions := range pairs {
		if len(directions) != 2 {
			return fmt.Errorf("migration %q must have matching up and down SQL files", stem)
		}
		if err := checkTableSecurity(dir, stem, directions); err != nil {
			return err
		}
	}
	if options.Base == "" {
		return nil
	}
	return checkBase(repo, dir, options)
}

// checkBase refuses a deleted or renamed applied migration. An edited one is accepted only when the
// schema stays the same: an up migration through Equivalent, a down migration when only its layout
// and comments changed. The server records applied migrations by name only, so an edit never runs
// again on a database that applied the file; it changes what a new install runs.
func checkBase(repo, dir string, options Options) error {
	command := exec.Command("git", "-C", repo, "ls-tree", "-r", "--name-only", options.Base, "--", "server/migrations")
	output, err := command.Output()
	if err != nil {
		return fmt.Errorf("read base %q: %w", options.Base, err)
	}
	names := strings.Fields(string(output))
	sort.Strings(names)
	var editedUp []string
	baseUp := make(map[string]string)
	for _, path := range names {
		if !strings.HasSuffix(path, ".sql") {
			continue
		}
		name := filepath.Base(path)
		current, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return fmt.Errorf("base migration %q was deleted or renamed", path)
		}
		show := exec.Command("git", "-C", repo, "show", options.Base+":"+path)
		baseContents, err := show.Output()
		if err != nil {
			return fmt.Errorf("read base migration %q: %w", path, err)
		}
		if strings.HasSuffix(name, ".up.sql") {
			baseUp[name] = string(baseContents)
		}
		if bytes.Equal(current, baseContents) {
			continue
		}
		if strings.HasSuffix(name, ".down.sql") {
			if sqlLayout(string(current)) != sqlLayout(string(baseContents)) {
				return fmt.Errorf("base migration %q was modified beyond its layout and comments", path)
			}
			continue
		}
		editedUp = append(editedUp, path)
	}
	if len(editedUp) == 0 {
		return nil
	}
	if options.DatabaseURL == "" {
		return fmt.Errorf("base migration %q was modified: an applied migration may change only when it builds the "+
			"same schema; prove it with migration-check --base %s --database <PostgreSQL URL>", editedUp[0], options.Base)
	}
	before := make([]Migration, 0, len(baseUp))
	for name, sql := range baseUp {
		before = append(before, Migration{Name: name, SQL: sql})
	}
	after, err := upMigrations(dir)
	if err != nil {
		return err
	}
	if err := Equivalent(context.Background(), options.DatabaseURL, before, after); err != nil {
		return fmt.Errorf("edited applied migrations (%s) build a different schema: %w", strings.Join(editedUp, ", "), err)
	}
	return nil
}

func upMigrations(dir string) ([]Migration, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read migrations: %w", err)
	}
	var chain []Migration
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".up.sql") {
			continue
		}
		contents, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		chain = append(chain, Migration{Name: entry.Name(), SQL: string(contents)})
	}
	return chain, nil
}

// sqlLayout is the SQL without comments and with every run of spaces and line breaks outside
// quoted strings made one space, so two files with the same sqlLayout differ only in layout.
func sqlLayout(sql string) string {
	code := withoutComments(sql)
	var layout strings.Builder
	quoted := false
	space := false
	for _, r := range code {
		if r == '\'' {
			quoted = !quoted
		}
		if !quoted && (r == ' ' || r == '\t' || r == '\n' || r == '\r') {
			space = true
			continue
		}
		if space && layout.Len() > 0 {
			layout.WriteByte(' ')
		}
		space = false
		layout.WriteRune(r)
	}
	return layout.String()
}
