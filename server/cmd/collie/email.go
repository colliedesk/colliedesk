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
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

const emailUsage = "usage: collie email resume <workspace slug> | collie email release-domain [--by <name>] <domain>" +
	" | collie email sender-name [--by <name>] <workspace slug> <name>"

// emailCommand is the operator's side of the email controls. Admins cannot lift a sending pause
// themselves, because the pause exists when their own mail drew complaints, nor take a domain away from
// the workspace that holds it verified. The operator also sets a sender name the hosted cloud refuses
// through the API because it names a brand, for the brand's genuine owner.
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
	case "sender-name":
		return setSenderName(ctx, pool, args[1:], output)
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

// setSenderName sets the name customers see in the From line of the workspace's replies. It checks the
// name as the API does but not against the brand denylist: the operator has confirmed the workspace owns
// the brand.
func setSenderName(ctx context.Context, pool *pgxpool.Pool, args []string, output io.Writer) error {
	flags := flag.NewFlagSet("email sender-name", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	by := flags.String("by", "", "who makes the change (default: the operating system user)")
	if err := flags.Parse(args); err != nil || flags.NArg() != 2 {
		return errors.New(emailUsage)
	}
	slug := flags.Arg(0)
	name, ok := domain.NormalizeHeaderName(flags.Arg(1))
	if !ok {
		return errors.New("the sender name must be 1 to 100 characters without line breaks or other control characters")
	}
	operator, err := operatorName(*by)
	if err != nil {
		return err
	}
	workspaceID, err := database.New(pool).WorkspaceIDBySlug(ctx, slug)
	if err != nil {
		return err
	}
	data := store.New(pool)
	before, err := data.GetEmailChannel(ctx, workspaceID)
	if err != nil {
		return err
	}
	noChannel := fmt.Errorf("workspace %s has no email channel; an admin saves a support address first", slug)
	if before == nil {
		return noChannel
	}
	channel, err := data.SetEmailSenderName(ctx, workspaceID, name)
	if errors.Is(err, store.ErrEmailChannelMissing) {
		return noChannel
	}
	if err != nil {
		return err
	}
	slog.Warn("email sender name set by the operator", "workspace", workspaceID, "slug", slug, "from", before.SenderName, "to", channel.SenderName, "by", operator)
	_, err = fmt.Fprintf(output, "Sender name of %s changed from %q to %q (by %s). The next reply uses it.\n",
		slug, before.SenderName, channel.SenderName, operator)
	return err
}
