package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/bundle"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/jackc/pgx/v5/pgxpool"
)

const bundleUsage = "usage: collie import bundle --workspace <slug> [--dry-run] [--report-dir DIR] <bundle.zip>"

// importBundle imports a bundle archive (docs/self-host/import-bundle.md) in one transaction; a report
// that is not OK leaves nothing.
func importBundle(ctx context.Context, pool *pgxpool.Pool, args []string, output io.Writer) error {
	flags := flag.NewFlagSet("import bundle", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	workspace := flags.String("workspace", "", "workspace slug")
	dryRun := flags.Bool("dry-run", false, "check and reconcile everything, write nothing")
	reportDir := flags.String("report-dir", ".", "where the report goes")
	if err := flags.Parse(args); err != nil || flags.NArg() != 1 || *workspace == "" {
		return errors.New(bundleUsage)
	}
	file, err := os.Open(flags.Arg(0))
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	archive, err := bundle.Read(file, info.Size())
	if err != nil {
		return err
	}
	if err := database.Migrate(ctx, pool); err != nil {
		return err
	}
	options := bundle.Options{WorkspaceSlug: *workspace, DryRun: *dryRun}
	if !*dryRun {
		storage, err := attachmentStorage()
		if err != nil {
			return err
		}
		options.Storage = storage
	}
	report, err := bundle.Run(ctx, pool, archive, options)
	if err != nil {
		return fmt.Errorf("import stopped, nothing was written: %w", err)
	}
	// The outcome is printed before the report files are written: an import that committed is never
	// hidden behind a report that cannot be written.
	lines := make([]string, 0, len(report.Lines))
	for _, line := range report.Lines {
		lines = append(lines, fmt.Sprintf("%s %d/%d/%d", line.What, line.Written, line.Bundle, line.Source))
	}
	mode := ""
	if *dryRun {
		mode = " (dry run: nothing was written)"
	}
	if _, err := fmt.Fprintf(output, "Status: %s%s\n%s (written/bundle/source)\n", report.Status, mode, strings.Join(lines, ", ")); err != nil {
		return err
	}
	jsonPath, markdownPath, err := report.WriteFiles(*reportDir)
	if err != nil {
		if report.Status == bundle.StatusOK && !*dryRun {
			return fmt.Errorf("%s is imported (Status: OK above), but its report could not be written: %w", report.Source, err)
		}
		return fmt.Errorf("the report could not be written: %w", err)
	}
	if _, err := fmt.Fprintf(output, "Report: %s\nReport: %s\n", jsonPath, markdownPath); err != nil {
		return err
	}
	if report.Status != bundle.StatusOK {
		return errors.New("the import did not reconcile and nothing was kept: " + strings.Join(report.Problems, "; "))
	}
	return nil
}
