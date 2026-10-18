package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

const emailUsage = "usage: collie email resume <workspace slug>"

// emailCommand is the operator's side of the email sending pause. Admins cannot lift it themselves: the
// pause exists because the workspace's own mail drew complaints.
func emailCommand(ctx context.Context, pool *pgxpool.Pool, args []string, output io.Writer) error {
	if len(args) != 2 || args[0] != "resume" {
		return errors.New(emailUsage)
	}
	slug := args[1]
	workspaceID, err := database.New(pool).WorkspaceIDBySlug(ctx, slug)
	if err != nil {
		return err
	}
	resumed, err := store.New(pool).ResumeEmailSending(ctx, workspaceID)
	if err != nil {
		return err
	}
	if resumed == nil {
		_, err = fmt.Fprintf(output, "Email sending of %s is not paused; nothing changed.\n", slug)
		return err
	}
	_, err = fmt.Fprintf(output, "Email sending of %s resumed. It was paused %s: %s\nReplies held back are sent now.\n",
		slug, resumed.PausedAt.Format(time.RFC3339), resumed.Reason)
	return err
}
