package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/freshdesk"
	"github.com/colliedesk/colliedesk/server/internal/helpcenter"
	"github.com/jackc/pgx/v5/pgxpool"
)

// freshdeskBaseURL turns a validated account domain into the API root.
// Tests point it at a local fake; there is no user-facing override.
var freshdeskBaseURL = func(domain string) string { return "https://" + domain }

// importRun is the importer entry point; tests replace it to check the command alone.
var importRun = freshdesk.Run

// progressOutput receives progress lines (stderr), so stdout keeps its stable summary format.
var progressOutput io.Writer = os.Stderr

const importUsage = "usage: collie import freshdesk --workspace <slug> --domain <account>.freshdesk.com [--api-key-env FRESHDESK_API_KEY] [--since YYYY-MM-DD] [--last-ticket-id N] [--include-personal-canned-responses] [--help-center-portal ID] [--old-help-center-domain HOST] [--dry-run] [--report-dir DIR]"

func importCommand(ctx context.Context, pool *pgxpool.Pool, args []string, output io.Writer) error {
	if len(args) > 0 && args[0] == "undo" {
		return undoCommand(ctx, pool, args[1:], output)
	}
	if len(args) > 0 && args[0] == "helpscout" {
		return importHelpScout(ctx, pool, args[1:], output)
	}
	if len(args) > 0 && args[0] == "bundle" {
		return importBundle(ctx, pool, args[1:], output)
	}
	if len(args) == 0 || args[0] != "freshdesk" {
		return errors.New(importUsage + "\n" + helpscoutUsage + "\n" + bundleUsage + "\n" + undoUsage)
	}
	options, reportDir, err := parseImportFlags(args[1:])
	if err != nil {
		return err
	}
	if err := database.Migrate(ctx, pool); err != nil {
		return err
	}
	if _, err := useServerConfig(ctx, pool); err != nil {
		return err
	}
	// Old Freshdesk help center links are pointed at the help center's address, built as the server
	// builds it. The origin is APP_ORIGIN as set, never the local default other commands fall back to:
	// without it the import rewrites no link and says so, instead of writing a local address for good.
	if options.HelpCenterDomain, err = helpcenter.DomainFromEnv(settings); err != nil {
		return err
	}
	options.Origin = strings.TrimSpace(os.Getenv("APP_ORIGIN"))
	options.Progress = progressOutput
	if !options.DryRun {
		storage, err := attachmentStorage()
		if err != nil {
			return err
		}
		options.Storage = storage
	}
	report, err := importRun(ctx, pool, options)
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
	if kb := report.KnowledgeBase; !kb.NotAvailable && kb.Articles.Source > 0 {
		if _, err := fmt.Fprintf(output, "Knowledge base: articles %d/%d, %d published (imported/in Freshdesk)\n", kb.Articles.Imported, kb.Articles.Source, len(kb.Published)); err != nil {
			return err
		}
	}
	if err := printUnresolved(output, report.Tickets.UnresolvedIDs); err != nil {
		return err
	}
	if report.TrialExtendedTo != nil {
		if _, err := fmt.Fprintf(output, "trial: extended to %s\n", report.TrialExtendedTo.UTC().Format(time.RFC3339)); err != nil {
			return err
		}
	}
	switch report.Status {
	case freshdesk.StatusFailed:
		return fmt.Errorf("import FAILED: %d failure(s); see %s, fix the cause and run the same command again", len(report.Failures), markdownPath)
	case freshdesk.StatusUnverified:
		return fmt.Errorf("import UNVERIFIED: the Freshdesk totals could not confirm that everything was imported; see the verification section of %s", markdownPath)
	}
	return nil
}

// printUnresolved tells the operator which ticket ids to check in Freshdesk when the archive
// endpoint was refused. It prints nothing when every id was resolved.
func printUnresolved(output io.Writer, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	const shown = 20
	list := strings.Join(ids[:min(len(ids), shown)], ", ")
	if len(ids) > shown {
		list += fmt.Sprintf(", … %d more in the report", len(ids)-shown)
	}
	_, err := fmt.Fprintf(output, "Unresolved ticket ids (archive unavailable, check in Freshdesk): %d: %s\n", len(ids), list)
	return err
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
	lastTicket := flags.Int64("last-ticket-id", 0, "highest ticket number shown in Freshdesk; the id check covers at least this far")
	portal := flags.Int64("help-center-portal", 0, "on a Freshdesk account with several portals, the portal whose published knowledge base articles stay published")
	oldHelpCenter := flags.String("old-help-center-domain", "", "the old Freshdesk help center's own custom domain, for example support.acme.com; its links are pointed at the new help center too")
	includePersonal := flags.Bool("include-personal-canned-responses", false, "also import canned responses from personal folders; they become shared with the whole workspace")
	if err := flags.Parse(args); err != nil {
		return freshdesk.Options{}, "", fmt.Errorf("%w\n%s", err, importUsage)
	}
	if flags.NArg() != 0 || *workspace == "" || *lastTicket < 0 || *portal < 0 {
		return freshdesk.Options{}, "", errors.New(importUsage)
	}
	host, err := freshdesk.AccountHost(*domain)
	if err != nil {
		return freshdesk.Options{}, "", fmt.Errorf("--domain must be your <account>.freshdesk.com address, got %q", *domain)
	}
	oldHelpCenterHost := ""
	if *oldHelpCenter != "" {
		if oldHelpCenterHost, err = freshdesk.OldHelpCenterHost(*oldHelpCenter); err != nil {
			return freshdesk.Options{}, "", fmt.Errorf("--old-help-center-domain must be a host name such as support.acme.com, got %q", *oldHelpCenter)
		}
	}
	apiKey := strings.TrimSpace(os.Getenv(*keyEnv))
	if apiKey == "" {
		return freshdesk.Options{}, "", fmt.Errorf("environment variable %s is empty; export your Freshdesk API key there", *keyEnv)
	}
	options := freshdesk.Options{WorkspaceSlug: *workspace, BaseURL: freshdeskBaseURL(host), APIKey: apiKey, DryRun: *dryRun, LastTicketID: *lastTicket, Cloud: billingEnabled(), PlansOnSale: plansOnSale(), IncludePersonalCannedResponses: *includePersonal, HelpCenterPortal: *portal,
		OldHelpCenterDomain: oldHelpCenterHost}
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

// billingEnabled is true where the trial exists: the instance has the hosted billing configured and
// this build serves it (the same decision serve makes at start).
// plansOnSale lists the plans the configured Polar products sell, for import refusals to name an upgrade.
func plansOnSale() []string {
	config, err := billingConfig()
	if err != nil || !config.Enabled() {
		return nil
	}
	return config.PlansOnSale()
}

func billingEnabled() bool {
	config, err := billingConfig()
	if err != nil {
		return false
	}
	config, _ = billingAtStart(config, lockedScreenShipped)
	return config.Enabled()
}
