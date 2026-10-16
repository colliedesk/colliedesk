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

	"github.com/colliedesk/colliedesk/server/internal/billingreconcile"
	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/colliedesk/colliedesk/server/internal/polar"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

const billingUsage = "usage: collie billing reconcile [--dry-run]"

// alertMailer is the mail boundary of the operator summary.
type alertMailer interface {
	Send(ctx context.Context, from string, to []string, message []byte) error
}

// reconcileMailer builds the mailer of the summary; tests replace it.
var reconcileMailer = func(config email.SMTPConfig) alertMailer { return email.SMTPSender{Config: config} }

const reconcileMailTimeout = 15 * time.Second

func billingCommand(ctx context.Context, pool *pgxpool.Pool, args []string, output io.Writer) error {
	if len(args) == 0 || args[0] != "reconcile" {
		return errors.New(billingUsage)
	}
	flags := flag.NewFlagSet("billing reconcile", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	dryRun := flags.Bool("dry-run", false, "print what would change; apply and send nothing")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 {
		return errors.New(billingUsage)
	}
	billing, err := polar.ConfigFromEnv(os.Getenv)
	if err != nil {
		return err
	}
	if !billing.Enabled() {
		return errors.New("billing is not configured on this instance (POLAR_* variables), so there is nothing to reconcile")
	}
	emailConfig, err := email.ConfigFromEnv(os.Getenv)
	if err != nil {
		return err
	}
	now := clock().UTC()
	report, err := billingreconcile.Run(ctx, polar.NewClient(billing), store.New(pool), billing.Products, now, *dryRun)
	if err != nil {
		return fmt.Errorf("billing reconcile: %w", err)
	}
	if err := report.Print(output, *dryRun); err != nil {
		return err
	}
	if *dryRun || !report.NeedsAttention() {
		return nil
	}
	return mailReconcileReport(ctx, report, emailConfig, strings.TrimSpace(os.Getenv("BILLING_ALERT_EMAIL")), now)
}

// mailReconcileReport sends the one summary mail. The findings are already applied and printed.
// Without an alert configuration it only warns, like the webhook alert; a send that fails fails the
// command, so a timer does not look healthy while nobody hears.
func mailReconcileReport(ctx context.Context, report billingreconcile.Report, config email.Config, to string, now time.Time) error {
	if to == "" || !config.SMTP.Configured() || config.MailFromDomain == "" {
		_, err := fmt.Fprintln(stderr, "billing reconcile found items but no alert is configured (set BILLING_ALERT_EMAIL, SMTP_HOST and MAIL_FROM_DOMAIN); the list above is the only report")
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
