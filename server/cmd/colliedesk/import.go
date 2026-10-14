package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/freshdesk"
	"github.com/jackc/pgx/v5/pgxpool"
)

// freshdeskBaseURL turns a validated account domain into the API root.
// Tests point it at a local fake; there is no user-facing override.
var freshdeskBaseURL = func(domain string) string { return "https://" + domain }

var freshdeskDomain = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?\.freshdesk\.com$`)

const importUsage = "usage: colliedesk import freshdesk --workspace <slug> --domain <account>.freshdesk.com [--api-key-env FRESHDESK_API_KEY] [--since YYYY-MM-DD] [--dry-run] [--report-dir DIR]"

func importCommand(ctx context.Context, pool *pgxpool.Pool, args []string, output io.Writer) error {
	if len(args) == 0 || args[0] != "freshdesk" {
		return errors.New(importUsage)
	}
	options, reportDir, err := parseImportFlags(args[1:])
	if err != nil {
		return err
	}
	if err := database.Migrate(ctx, pool); err != nil {
		return err
	}
	if !options.DryRun {
		storage, err := attachmentStorage()
		if err != nil {
			return err
		}
		options.Storage = storage
	}
	report, err := freshdesk.Run(ctx, pool, options)
	if err != nil {
		return fmt.Errorf("import stopped: %w (completed items are kept; run the same command again to resume)", err)
	}
	jsonPath, markdownPath, err := report.WriteFiles(reportDir)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(output, "Status: %s\nContacts %d/%d, tickets %d/%d, messages %d/%d, attachments %d/%d, time entries %d/%d (imported/in Freshdesk)\nReport: %s\nReport: %s\n",
		report.Status, report.Contacts.Imported, report.Contacts.Source, report.Tickets.Imported, report.Tickets.Source,
		report.Messages.Imported, report.Messages.Source, report.Attachments.Imported, report.Attachments.Source,
		report.TimeEntries.Imported, report.TimeEntries.Source, markdownPath, jsonPath); err != nil {
		return err
	}
	if report.Status == freshdesk.StatusFailed {
		return fmt.Errorf("import FAILED: %d failure(s); see %s, fix the cause and run the same command again", len(report.Failures), markdownPath)
	}
	return nil
}

func parseImportFlags(args []string) (freshdesk.Options, string, error) {
	flags := flag.NewFlagSet("import freshdesk", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	workspace := flags.String("workspace", "", "workspace slug")
	domain := flags.String("domain", "", "Freshdesk account domain, for example acme.freshdesk.com")
	keyEnv := flags.String("api-key-env", "FRESHDESK_API_KEY", "name of the environment variable that holds the API key")
	since := flags.String("since", "", "only tickets updated at or after this UTC date (YYYY-MM-DD) or RFC 3339 time")
	dryRun := flags.Bool("dry-run", false, "count what would be imported without writing anything")
	reportDir := flags.String("report-dir", ".", "directory for the reconcile report")
	if err := flags.Parse(args); err != nil {
		return freshdesk.Options{}, "", fmt.Errorf("%w\n%s", err, importUsage)
	}
	if flags.NArg() != 0 || *workspace == "" {
		return freshdesk.Options{}, "", errors.New(importUsage)
	}
	host := strings.ToLower(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(*domain), "https://"), "/"))
	if !freshdeskDomain.MatchString(host) {
		return freshdesk.Options{}, "", fmt.Errorf("--domain must be your <account>.freshdesk.com address, got %q", *domain)
	}
	apiKey := strings.TrimSpace(os.Getenv(*keyEnv))
	if apiKey == "" {
		return freshdesk.Options{}, "", fmt.Errorf("environment variable %s is empty; export your Freshdesk API key there", *keyEnv)
	}
	options := freshdesk.Options{WorkspaceSlug: *workspace, BaseURL: freshdeskBaseURL(host), APIKey: apiKey, DryRun: *dryRun}
	if *since != "" {
		parsed, err := parseSince(*since)
		if err != nil {
			return freshdesk.Options{}, "", err
		}
		options.Since = &parsed
	}
	return options, *reportDir, nil
}

func parseSince(value string) (time.Time, error) {
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed.UTC(), nil
	}
	if parsed, err := time.ParseInLocation(time.DateOnly, value, time.UTC); err == nil {
		return parsed, nil
	}
	return time.Time{}, fmt.Errorf("--since must be YYYY-MM-DD (UTC) or an RFC 3339 time, got %q", value)
}
