package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/colliedesk/colliedesk/server/internal/polar"
	"github.com/colliedesk/colliedesk/server/internal/retention"
	"github.com/jackc/pgx/v5/pgxpool"
)

const retentionUsage = "usage: colliedesk retention <plan|run>"

// retentionCommand is the ops view of the job that deletes workspaces locked for 12 months: plan
// lists what the next run would do and changes nothing, run does it. Both refuse on an instance
// without billing, because self-hosted workspaces are never locked and never deleted.
func retentionCommand(ctx context.Context, pool *pgxpool.Pool, origin string, args []string, output io.Writer) error {
	if len(args) != 1 || (args[0] != "plan" && args[0] != "run") {
		return errors.New(retentionUsage)
	}
	billing, err := polar.ConfigFromEnv(os.Getenv)
	if err != nil {
		return err
	}
	if !billing.Enabled() {
		return errors.New("retention applies to the hosted cloud only: billing is not configured on this instance, so no workspace is ever locked or deleted")
	}
	service, err := newRetentionService(database.New(pool), origin, clock)
	if err != nil {
		return err
	}
	return runRetention(ctx, service, args[0] == "run", output)
}

// runRetention prints the plan, or runs the policy and prints what it did.
func runRetention(ctx context.Context, service *retention.Service, apply bool, output io.Writer) error {
	if !apply {
		items, err := service.Plan(ctx)
		if err != nil {
			return err
		}
		for _, item := range items {
			if _, err := fmt.Fprintf(output, "%s\t%s\t%s\tlocked_since=%s\tnotice_at=%s\tdelete_at=%s\n", item.Action, item.Slug, item.WorkspaceID,
				item.LockedSince.Format(time.RFC3339), item.NoticeAt.Format(time.RFC3339), item.DeleteAt.Format(time.RFC3339)); err != nil {
				return err
			}
		}
		_, err = fmt.Fprintf(output, "%d workspace(s) to act on\n", len(items))
		return err
	}
	result, err := service.Run(ctx)
	if _, werr := fmt.Fprintf(output, "noticed %d, deleted %d, reset %d\n", result.Noticed, result.Deleted, result.Reset); werr != nil && err == nil {
		err = werr
	}
	return err
}

// newRetentionService opens the attachment store and, when SMTP and MAIL_FROM_DOMAIN are set, the
// platform mailer. Without them notices fail with a clear error and nothing is deleted.
func newRetentionService(db *database.DB, origin string, now func() time.Time) (*retention.Service, error) {
	storage, err := attachmentStorage()
	if err != nil {
		return nil, err
	}
	emailConfig, err := email.ConfigFromEnv(os.Getenv)
	if err != nil {
		return nil, err
	}
	config := retention.Config{DB: db, Storage: storage, Origin: origin, Now: now}
	if emailConfig.SMTP.Configured() && emailConfig.MailFromDomain != "" {
		config.Mailer = email.SMTPSender{Config: emailConfig.SMTP}
		config.From = "noreply@" + emailConfig.MailFromDomain
	}
	return retention.New(config), nil
}

// sweepRetention runs the policy at start and then every hour until ctx ends.
func sweepRetention(ctx context.Context, service *retention.Service) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		if result, err := service.Run(ctx); err != nil && ctx.Err() == nil {
			slog.Error("workspace retention", "error", err)
		} else if result != (retention.Result{}) {
			slog.Info("workspace retention", "noticed", result.Noticed, "deleted", result.Deleted, "reset", result.Reset)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
