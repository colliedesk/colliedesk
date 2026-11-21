package migrationcheck

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// maxLineLength keeps a migration readable in a diff and a terminal without wrapping.
const maxLineLength = 100

// storedFunctionBodies is the newest migration whose function bodies installs already store: PostgreSQL
// keeps a function's text as written, so those bodies keep their old long lines. Every migration after
// it wraps function bodies too.
const storedFunctionBodies = "20261121154636-skip-retrieval-queue-for-deleted-workspaces"

var (
	createFunction = regexp.MustCompile(`(?i)\bcreate\s+(or\s+replace\s+)?(function|procedure)\b`)
	dollarTag      = regexp.MustCompile(`\$[A-Za-z_]*\$`)
)

// checkStyle enforces the layout docs/internals/migrations.md describes: an up migration opens with a
// comment saying what it changes and why, and no line is longer than maxLineLength characters.
func checkStyle(dir, stem string, files map[string]string) error {
	for direction, name := range files {
		contents, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return fmt.Errorf("read migration %q: %w", name, err)
		}
		sql := string(contents)
		if direction == "up" && !opensWithComment(sql) {
			return fmt.Errorf("migration %q must open with a -- comment that says what it changes and why "+
				"(docs/internals/migrations.md)", name)
		}
		exempt := map[int]bool{}
		if stem <= storedFunctionBodies {
			exempt = functionBodyLines(sql)
		}
		var long []string
		for i, line := range strings.Split(sql, "\n") {
			if length := len([]rune(line)); length > maxLineLength && !exempt[i] {
				long = append(long, fmt.Sprintf("%d (%d characters)", i+1, length))
			}
		}
		if len(long) > 0 {
			return fmt.Errorf("%s:%s: wrap these lines at %d characters (docs/internals/migrations.md)",
				name, strings.Join(long, ", "), maxLineLength)
		}
	}
	return nil
}

func opensWithComment(sql string) bool {
	for _, line := range strings.Split(sql, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		return strings.HasPrefix(line, "--") || strings.HasPrefix(line, "/*")
	}
	return false
}

// functionBodyLines are the 0-based numbers of the lines inside each CREATE FUNCTION or CREATE
// PROCEDURE body, from the line that opens its dollar quote to the line that closes it.
func functionBodyLines(sql string) map[int]bool {
	lines := map[int]bool{}
	for _, match := range createFunction.FindAllStringIndex(sql, -1) {
		open := dollarTag.FindStringIndex(sql[match[1]:])
		if open == nil {
			continue
		}
		start := match[1] + open[0]
		tag := sql[start : match[1]+open[1]]
		end := strings.Index(sql[start+len(tag):], tag)
		if end < 0 {
			continue
		}
		end += start + len(tag)
		first := strings.Count(sql[:start], "\n")
		last := strings.Count(sql[:end], "\n")
		for line := first; line <= last; line++ {
			lines[line] = true
		}
	}
	return lines
}
