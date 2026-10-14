package migrationcheck

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var migrationPattern = regexp.MustCompile(`^(\d{14})-([a-z0-9]+(?:-[a-z0-9]+)*)\.(up|down)\.sql$`)

// Check validates migration pairs and, when base is set, immutable history.
func Check(repo, base string) error {
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
	}
	if base == "" {
		return nil
	}
	return checkBase(repo, base, dir)
}

func checkBase(repo, base, dir string) error {
	command := exec.Command("git", "-C", repo, "ls-tree", "-r", "--name-only", base, "--", "server/migrations")
	output, err := command.Output()
	if err != nil {
		return fmt.Errorf("read base %q: %w", base, err)
	}
	names := strings.Fields(string(output))
	sort.Strings(names)
	for _, path := range names {
		if !strings.HasSuffix(path, ".sql") {
			continue
		}
		current, err := os.ReadFile(filepath.Join(dir, filepath.Base(path)))
		if err != nil {
			return fmt.Errorf("base migration %q was deleted or renamed", path)
		}
		show := exec.Command("git", "-C", repo, "show", base+":"+path)
		baseContents, err := show.Output()
		if err != nil {
			return fmt.Errorf("read base migration %q: %w", path, err)
		}
		if !bytes.Equal(current, baseContents) {
			return fmt.Errorf("base migration %q was modified", path)
		}
	}
	return nil
}
