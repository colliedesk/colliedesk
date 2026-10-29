package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/billingreconcile"
	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/colliedesk/colliedesk/server/internal/polar"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

const billingUsage = "usage: collie billing reconcile [--dry-run] [--require-alert]"

// alertMailer is the mail boundary of the operator summary.
type alertMailer interface {
	Send(ctx context.Context, from string, to []string, message []byte) error
}

// reconcileMailer builds the mailer of the summary; tests replace it.
var reconcileMailer = func(config email.SMTPConfig) alertMailer { return email.SMTPSender{Config: config} }

// reconcileData is the database side of the command; tests wrap it.
var reconcileData = func(pool *pgxpool.Pool) billingreconcile.Data { return store.New(pool) }

const reconcileMailTimeout = 15 * time.Second

func billingCommand(ctx context.Context, pool *pgxpool.Pool, args []string, output io.Writer) error {
	if len(args) == 0 || args[0] != "reconcile" {
		return errors.New(billingUsage)
	}
	flags := flag.NewFlagSet("billing reconcile", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	dryRun := flags.Bool("dry-run", false, "print what would change; apply and send nothing")
	requireAlert := flags.Bool("require-alert", false, "fail when no operator alert can be sent, even with nothing to report")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 {
		return errors.New(billingUsage)
	}
	if _, err := useServerConfig(ctx, pool); err != nil {
		return err
	}
	billing, err := billingConfig()
	if err != nil {
		return err
	}
	if !billing.Enabled() {
		return errors.New("billing is not configured on this instance (polar.* settings, `collie config list --group billing`), so there is nothing to reconcile")
	}
	emailConfig, err := email.ConfigFromEnv(settings)
	if err != nil {
		return err
	}
	now := clock().UTC()
	alertTo := strings.TrimSpace(settings("BILLING_ALERT_EMAIL"))
	alertReady := alertTo != "" && emailConfig.SMTP.Configured() && emailConfig.MailFromDomain != ""
	report, runErr := billingreconcile.Run(ctx, polar.NewClient(billing), reconcileData(pool), billing.Products, now, *dryRun)
	if runErr != nil && len(report.Changes) == 0 {
		return fmt.Errorf("billing reconcile: %w", runErr)
	}
	// A failure after some changes were written still prints and mails what was written.
	var failures []error
	if runErr != nil {
		failures = append(failures, fmt.Errorf("billing reconcile: %w", runErr))
	}
	if err := report.Print(output); err != nil {
		return errors.Join(append(failures, err)...)
	}
	if !*dryRun && report.NeedsAttention() {
		if err := mailReconcileReport(ctx, report, emailConfig, alertTo, alertReady, now); err != nil {
			failures = append(failures, err)
		}
	}
	if *requireAlert && !alertReady {
		failures = append(failures, errors.New("--require-alert: no alert can be sent (set billing.alert_email, smtp.host and mail_from.domain)"))
	}
	return errors.Join(failures...)
}

// mailReconcileReport sends the one summary mail. The findings are already applied and printed.
// Without an alert configuration it only warns, like the webhook alert; a send that fails fails the
// command, so a timer does not look healthy while nobody hears.
func mailReconcileReport(ctx context.Context, report billingreconcile.Report, config email.Config, to string, ready bool, now time.Time) error {
	if !ready {
		_, err := fmt.Fprintln(stderr, "billing reconcile found items but no alert is configured (set billing.alert_email, smtp.host and mail_from.domain); the list above is the only report")
		return err
	}
	from := "noreply@" + config.MailFromDomain
	message, err := report.Mail(from, to, now)
	if err != nil {
		return fmt.Errorf("compose reconcile alert: %w", err)
	}
	sendCtx, cancel := context.WithTimeout(ctx, reconcileMailTimeout)
	defer cancel()
	if err := reconcileMailer(config.SMTP).Send(sendCtx, from, []string{to}, message); err != nil {
		return fmt.Errorf("send reconcile alert: %w", err)
	}
	return nil
}
