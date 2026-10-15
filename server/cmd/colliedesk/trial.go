package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

const trialUsage = "usage: colliedesk trial set --workspace <slug> --ends <RFC3339 time, for example 2026-12-01T00:00:00Z>"

// trialCommand lets an operator set when a workspace's trial ends.
func trialCommand(ctx context.Context, pool *pgxpool.Pool, args []string, output io.Writer) error {
	if len(args) == 0 || args[0] != "set" {
		return errors.New(trialUsage)
	}
	flags := flag.NewFlagSet("trial set", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	slug := flags.String("workspace", "", "workspace slug")
	endsText := flags.String("ends", "", "new trial end, RFC3339")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 || *slug == "" || *endsText == "" {
		return errors.New(trialUsage)
	}
	ends, err := time.Parse(time.RFC3339, *endsText)
	if err != nil {
		return fmt.Errorf("--ends must be an RFC3339 time with a zone, for example 2026-12-01T00:00:00Z: %w", err)
	}
	workspaceID, err := database.New(pool).WorkspaceIDBySlug(ctx, *slug)
	if err != nil {
		return err
	}
	previous, err := store.New(pool).SetTrialEnd(ctx, workspaceID, ends)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(output, "Trial end of %s: was %s, now %s\n", *slug, previous.Format(time.RFC3339), ends.UTC().Format(time.RFC3339))
	return err
}
