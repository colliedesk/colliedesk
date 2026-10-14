package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/hclabsco/colliedesk/server/internal/migrationcheck"
)

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

func main() {
	if err := run(os.Args[1:], os.Stdout, time.Now); err != nil {
		fmt.Fprintf(os.Stderr, "migration-new: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, output io.Writer, now func() time.Time) error {
	flags := flag.NewFlagSet("migration-new", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	name := flags.String("name", "", "kebab-case migration domain name")
	repo := flags.String("repo", "", "repository path (defaults to current Git repository)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if !slugPattern.MatchString(*name) {
		return fmt.Errorf("invalid --name %q: use lowercase kebab-case", *name)
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", flags.Args())
	}
	root, err := migrationcheck.RepositoryRoot(*repo)
	if err != nil {
		return err
	}
	stem := now().UTC().Format("20060102150405") + "-" + *name
	dir := filepath.Join(root, "server", "migrations")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create migration directory: %w", err)
	}
	paths := []string{
		filepath.Join(dir, stem+".up.sql"),
		filepath.Join(dir, stem+".down.sql"),
	}
	for _, path := range paths {
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			return fmt.Errorf("create %s: %w", filepath.Base(path), err)
		}
		if err := file.Close(); err != nil {
			return fmt.Errorf("close %s: %w", filepath.Base(path), err)
		}
	}
	_, err = fmt.Fprintf(output, "created server/migrations/%s.{up,down}.sql\n", stem)
	return err
}
