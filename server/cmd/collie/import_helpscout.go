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
	"github.com/colliedesk/colliedesk/server/internal/helpscout"
	"github.com/jackc/pgx/v5/pgxpool"
)

// helpscoutRun is the importer entry point; tests replace it to check the command alone.
var helpscoutRun = helpscout.Run

// helpscoutBaseURL is the API root. End-to-end builds point it at a local fake; there is no
// user-facing override.
var helpscoutBaseURL = helpscout.DefaultBaseURL

const helpscoutUsage = "usage: collie import helpscout --workspace <slug> [--mailbox <inbox id>] [--app-id-env HELPSCOUT_APP_ID] [--app-secret-env HELPSCOUT_APP_SECRET] [--since YYYY-MM-DD] [--dry-run] [--report-dir DIR]"

func importHelpScout(ctx context.Context, pool *pgxpool.Pool, args []string, output io.Writer) error {
	options, reportDir, err := parseHelpScoutFlags(args)
	if err != nil {
		return err
	}
	if err := database.Migrate(ctx, pool); err != nil {
		return err
	}
	options.Progress, options.Cloud = progressOutput, billingEnabled()
	if !options.DryRun {
		storage, err := attachmentStorage()
		if err != nil {
			return err
		}
		options.Storage = storage
	}
	report, err := helpscoutRun(ctx, pool, options)
	if err != nil {
		return fmt.Errorf("import stopped: %w (completed items are kept; run the same command again to resume)", err)
	}
	jsonPath, markdownPath, err := report.WriteFiles(reportDir)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(output, "Status: %s\nInbox %d (%s): conversations %d/%d, contacts %d/%d, messages %d/%d, attachments %d/%d, inline images %d/%d, tags %d/%d (imported/in Help Scout)\nReport: %s\nReport: %s\n",
		report.Status, report.Mailbox.ID, report.Mailbox.Name, report.Conversations.Imported, report.Conversations.Source, report.Contacts.Imported, report.Contacts.Source,
		report.Messages.Imported, report.Messages.Source, report.Attachments.Imported, report.Attachments.Source,
		report.InlineImages.Imported, report.InlineImages.Source, report.Tags.Imported, report.Tags.Source, markdownPath, jsonPath); err != nil {
		return err
	}
	if report.TrialExtendedTo != nil {
		if _, err := fmt.Fprintf(output, "trial: extended to %s\n", report.TrialExtendedTo.UTC().Format(time.RFC3339)); err != nil {
			return err
		}
	}
	switch report.Status {
	case helpscout.StatusFailed:
		return fmt.Errorf("import FAILED: %d failure(s); see %s, fix the cause and run the same command again", len(report.Failures), markdownPath)
	case helpscout.StatusUnverified:
		return fmt.Errorf("import UNVERIFIED: the Help Scout totals could not confirm that everything was imported; see the verification section of %s", markdownPath)
	}
	return nil
}

func parseHelpScoutFlags(args []string) (helpscout.Options, string, error) {
	flags := flag.NewFlagSet("import helpscout", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	workspace := flags.String("workspace", "", "workspace slug")
	mailbox := flags.Int64("mailbox", 0, "Help Scout inbox id; required when the account has several inboxes")
	idEnv := flags.String("app-id-env", "HELPSCOUT_APP_ID", "name of the environment variable that holds the App ID")
	secretEnv := flags.String("app-secret-env", "HELPSCOUT_APP_SECRET", "name of the environment variable that holds the App Secret")
	since := flags.String("since", "", "only conversations modified after this UTC date (YYYY-MM-DD) or RFC 3339 time")
	dryRun := flags.Bool("dry-run", false, "count what would be imported without writing anything")
	reportDir := flags.String("report-dir", ".", "directory for the reconcile report")
	if err := flags.Parse(args); err != nil {
		return helpscout.Options{}, "", fmt.Errorf("%w\n%s", err, helpscoutUsage)
	}
	if flags.NArg() != 0 || *workspace == "" || *mailbox < 0 {
		return helpscout.Options{}, "", errors.New(helpscoutUsage)
	}
	appID, appSecret := strings.TrimSpace(os.Getenv(*idEnv)), strings.TrimSpace(os.Getenv(*secretEnv))
	if appID == "" || appSecret == "" {
		return helpscout.Options{}, "", fmt.Errorf("environment variables %s and %s must hold the App ID and App Secret of your Help Scout app", *idEnv, *secretEnv)
	}
	options := helpscout.Options{WorkspaceSlug: *workspace, BaseURL: helpscoutBaseURL, AppID: appID, AppSecret: appSecret, MailboxID: *mailbox, DryRun: *dryRun}
	if *since != "" {
		parsed, err := parseSince(*since)
		if err != nil {
			return helpscout.Options{}, "", err
		}
		options.Since = &parsed
	}
	return options, *reportDir, nil
}
