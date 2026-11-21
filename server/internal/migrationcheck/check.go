package migrationcheck

import (
	"bytes"
	"context"
	"errors"
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
	stems := make([]string, 0, len(pairs))
	for stem := range pairs {
		stems = append(stems, stem)
	}
	sort.Strings(stems)
	// Every migration is checked, so one run lists everything to fix.
	var problems []error
	paired := true
	for _, stem := range stems {
		directions := pairs[stem]
		if len(directions) != 2 {
			problems = append(problems, fmt.Errorf("migration %q must have matching up and down SQL files", stem))
			paired = false
			continue
		}
		if err := checkTableSecurity(dir, stem, directions); err != nil {
			problems = append(problems, err)
		}
		if err := checkStyle(dir, stem, directions); err != nil {
			problems = append(problems, err)
		}
	}
	// A style problem does not stop the comparison with the base: both are worth knowing in one run.
	if options.Base != "" && paired {
		if err := checkBase(repo, dir, options); err != nil {
			problems = append(problems, err)
		}
	}
	return errors.Join(problems...)
}

// checkBase refuses a deleted or renamed applied migration. An edited one, up or down, is accepted
// only when Equivalent shows the chain builds and rolls back to the same schemas as the base. The
// server records applied migrations by name only, so an edit never runs again on a database that
// applied the file; it changes what a new install runs.
func checkBase(repo, dir string, options Options) error {
	command := exec.Command("git", "-C", repo, "ls-tree", "-r", "--name-only", options.Base, "--", "server/migrations")
	output, err := command.Output()
	if err != nil {
		return fmt.Errorf("read base %q: %w", options.Base, err)
	}
	names := strings.Fields(string(output))
	sort.Strings(names)
	var edited []string
	baseChain := make(map[string]*Migration)
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
		up := strings.TrimSuffix(strings.TrimSuffix(name, ".down.sql"), ".up.sql") + ".up.sql"
		if baseChain[up] == nil {
			baseChain[up] = &Migration{Name: up}
		}
		if strings.HasSuffix(name, ".up.sql") {
			baseChain[up].SQL = string(baseContents)
		} else {
			baseChain[up].Down = string(baseContents)
		}
		if !bytes.Equal(current, baseContents) {
			edited = append(edited, path)
		}
	}
	if len(edited) == 0 {
		return nil
	}
	if options.DatabaseURL == "" {
		return fmt.Errorf("base migration %q was modified: an applied migration may change only when it builds the "+
			"same schema; prove it with migration-check --base %s --database <PostgreSQL URL>", edited[0], options.Base)
	}
	before := make([]Migration, 0, len(baseChain))
	for _, migration := range baseChain {
		before = append(before, *migration)
	}
	after, err := chainMigrations(dir)
	if err != nil {
		return err
	}
	if err := Equivalent(context.Background(), options.DatabaseURL, before, after); err != nil {
		return fmt.Errorf("edited applied migrations (%s) build a different schema: %w", strings.Join(edited, ", "), err)
	}
	return nil
}

func chainMigrations(dir string) ([]Migration, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read migrations: %w", err)
	}
	var chain []Migration
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".up.sql") {
			continue
		}
		up, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		down, err := os.ReadFile(filepath.Join(dir, strings.TrimSuffix(entry.Name(), ".up.sql")+".down.sql"))
		if err != nil {
			return nil, err
		}
		chain = append(chain, Migration{Name: entry.Name(), SQL: string(up), Down: string(down)})
	}
	return chain, nil
}
