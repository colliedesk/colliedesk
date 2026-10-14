package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/colliedesk/colliedesk/server/internal/migrationcheck"
)

func main() {
	base := flag.String("base", "", "optional Git base commit or ref")
	repo := flag.String("repo", "", "repository path (defaults to current Git repository)")
	flag.Parse()
	root, err := migrationcheck.RepositoryRoot(*repo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "migration-check: %v\n", err)
		os.Exit(1)
	}
	if err := migrationcheck.Check(root, *base); err != nil {
		fmt.Fprintf(os.Stderr, "migration-check: %v\n", err)
		os.Exit(1)
	}
}
