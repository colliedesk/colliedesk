package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/colliedesk/colliedesk/server/internal/polar"
	"github.com/colliedesk/colliedesk/server/internal/retention"
	"github.com/jackc/pgx/v5/pgxpool"
)

const retentionUsage = "usage: collie retention plan | collie retention run --yes"

// defaultMaxDeletionsPerDay is the cap when RETENTION_MAX_DELETIONS_PER_DAY is not set.
const defaultMaxDeletionsPerDay = 5

// retentionSettings are the fail-safes of the job that deletes workspaces. Deletion is off until
// RETENTION_DELETE=on; the cap bounds the damage of a wrong decision.
// maxDeletionsPerDayCeiling bounds the daily cap itself: a typo cannot let a wrong decision delete hundreds.
const maxDeletionsPerDayCeiling = 20

type retentionSettings struct {
	DeleteEnabled      bool
	MaxDeletionsPerDay int
}

func retentionSettingsFromEnv(getenv func(string) string) (retentionSettings, error) {
	settings := retentionSettings{MaxDeletionsPerDay: defaultMaxDeletionsPerDay}
	switch getenv("RETENTION_DELETE") {
	case "", "off":
	case "on":
		settings.DeleteEnabled = true
	default:
		return retentionSettings{}, errors.New("RETENTION_DELETE must be on or off")
	}
	if value := getenv("RETENTION_MAX_DELETIONS_PER_DAY"); value != "" {
		max, err := strconv.Atoi(value)
		if err != nil || max < 0 || max > maxDeletionsPerDayCeiling {
			return retentionSettings{}, fmt.Errorf("RETENTION_MAX_DELETIONS_PER_DAY must be a whole number from 0 to %d", maxDeletionsPerDayCeiling)
		}
		settings.MaxDeletionsPerDay = max
	}
	return settings, nil
}

// retentionSettingsForServe never stops the server: a bad value is logged (variable name, never the
// value) and the job runs with deletion off. Notices still go out.
func retentionSettingsForServe(getenv func(string) string, logger *slog.Logger) retentionSettings {
	settings, err := retentionSettingsFromEnv(getenv)
	if err != nil {
		logger.Error("retention settings are invalid; workspace deletion stays off", "error", err)
		return retentionSettings{}
	}
	return settings
}

// retentionCommand is the ops view of the job that deletes workspaces locked for 12 months: plan
// lists what the next run would do and changes nothing, run does one pass (and needs --yes). Both
// refuse on an instance without billing, because self-hosted workspaces are never locked and
// never deleted.
func retentionCommand(ctx context.Context, pool *pgxpool.Pool, origin string, args []string, output io.Writer) error {
	apply := false
	switch {
	case len(args) == 1 && args[0] == "plan":
	case len(args) == 2 && args[0] == "run" && args[1] == "--yes":
		apply = true
	case len(args) == 1 && args[0] == "run":
		return errors.New("retention run can delete workspaces for good: read `collie retention plan` first, then pass --yes")
	default:
		return errors.New(retentionUsage)
	}
	if _, err := useServerConfig(ctx, pool); err != nil {
		return err
	}
	billing, err := billingConfig()
	if err != nil {
		return err
	}
	billing, _ = billingAtStart(billing, lockedScreenShipped)
	if !billing.Enabled() {
		return errors.New("retention applies to the hosted cloud only: billing is not configured on this instance, so no workspace is ever locked or deleted")
	}
	retentionSet, err := retentionSettingsFromEnv(settings)
	if err != nil {
		return err
	}
	service, err := newRetentionService(database.New(pool), origin, billing, retentionSet)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(output, "deletion %s, at most %d a day\n", map[bool]string{true: "on", false: "off (retention.delete)"}[retentionSet.DeleteEnabled], retentionSet.MaxDeletionsPerDay); err != nil {
		return err
	}
	return runRetention(ctx, service, apply, output)
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
	if _, werr := fmt.Fprintf(output, "noticed %d, deleted %d, reset %d, held back %d\n", result.Noticed, result.Deleted, result.Reset, result.Held); werr != nil && err == nil {
		err = werr
	}
	return err
}

// newRetentionService opens the attachment store, the Polar client and, when SMTP and
// MAIL_FROM_DOMAIN are set, the platform mailer. Without a mailer notices fail with a clear error
// and nothing is deleted.
func newRetentionService(db *database.DB, origin string, billing polar.Config, retentionSet retentionSettings) (*retention.Service, error) {
	storage, err := attachmentStorage()
	if err != nil {
		return nil, err
	}
	emailConfig, err := email.ConfigFromEnv(settings)
	if err != nil {
		return nil, err
	}
	config := retention.Config{DB: db, Storage: storage, Origin: origin, Polar: polar.NewClient(billing),
		DeleteEnabled: retentionSet.DeleteEnabled, MaxDeletionsPerDay: retentionSet.MaxDeletionsPerDay}
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
			slog.Info("workspace retention", "noticed", result.Noticed, "deleted", result.Deleted, "reset", result.Reset, "held", result.Held)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
