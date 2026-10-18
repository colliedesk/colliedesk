package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

const emailUsage = "usage: collie email resume <workspace slug> | collie email release-domain [--by <name>] <domain>"

// emailCommand is the operator's side of the email controls. Admins cannot lift a sending pause
// themselves, because the pause exists when their own mail drew complaints, nor take a domain away from
// the workspace that holds it verified.
func emailCommand(ctx context.Context, pool *pgxpool.Pool, args []string, output io.Writer) error {
	if len(args) == 0 {
		return errors.New(emailUsage)
	}
	switch args[0] {
	case "resume":
		if len(args) != 2 {
			return errors.New(emailUsage)
		}
		return resumeEmail(ctx, pool, args[1], output)
	case "release-domain":
		return releaseDomain(ctx, pool, args[1:], output)
	}
	return errors.New(emailUsage)
}

func resumeEmail(ctx context.Context, pool *pgxpool.Pool, slug string, output io.Writer) error {
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

// releaseDomain lets a different workspace verify a domain name that an abandoned workspace holds.
func releaseDomain(ctx context.Context, pool *pgxpool.Pool, args []string, output io.Writer) error {
	flags := flag.NewFlagSet("email release-domain", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	by := flags.String("by", "", "who makes the change (default: the operating system user)")
	if err := flags.Parse(args); err != nil || flags.NArg() != 1 {
		return errors.New(emailUsage)
	}
	name := strings.ToLower(strings.TrimSpace(flags.Arg(0)))
	if name == "" || !strings.Contains(name, ".") || strings.ContainsAny(name, "@ /") {
		return errors.New("release-domain takes a domain name such as example.com, not an address")
	}
	operator, err := operatorName(*by)
	if err != nil {
		return err
	}
	released, err := store.New(pool).ReleaseVerifiedDomain(ctx, name, clock().UTC())
	if err != nil {
		return err
	}
	if released == nil {
		_, err = fmt.Fprintf(output, "No workspace holds %s verified through the email provider; nothing changed.\n", name)
		return err
	}
	slog.Warn("verified email domain released", "domain", name, "workspace", released.WorkspaceID, "slug", released.Slug, "provider_domain", released.ProviderDomainID, "by", operator)
	_, err = fmt.Fprintf(output, "Released %s from workspace %s (by %s). Its provider domain is queued for deletion and that workspace sends from the platform address now. The owner can verify the domain.\n",
		name, released.Slug, operator)
	return err
}
