package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/helpcenter"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/colliedesk/colliedesk/server/internal/workspacehost"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	trialOfferUsage       = "collie trial offer --workspace <slug> [--valid <duration, at most 30d; default 30d>] [--by <name>]"
	trialOffersUsage      = "collie trial offers --workspace <slug>"
	trialOfferRevokeUsage = "collie trial offer revoke <offer id> [--by <name>]"
)

// parseOfferValidity reads --valid: a number of days ("7d") or a Go duration ("36h").
func parseOfferValidity(value string) (time.Duration, error) {
	if days, found := strings.CutSuffix(value, "d"); found {
		count, err := strconv.Atoi(days)
		if err != nil {
			return 0, fmt.Errorf("--valid %q: write days as 7d, or a duration like 36h", value)
		}
		return time.Duration(count) * 24 * time.Hour, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("--valid %q: write days as 7d, or a duration like 36h", value)
	}
	return duration, nil
}

// trialOfferCommand makes a one-time link that lets an admin of the workspace continue its trial for 14
// days from the moment they accept it, or revokes one. Colliedesk sends no email: the operator does.
func trialOfferCommand(ctx context.Context, pool *pgxpool.Pool, origin string, args []string, output io.Writer) error {
	if len(args) > 0 && args[0] == "revoke" {
		return trialOfferRevoke(ctx, pool, args[1:], output)
	}
	flags := flag.NewFlagSet("trial offer", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	slug := flags.String("workspace", "", "workspace slug")
	validText := flags.String("valid", "30d", "how long the link works, at most 30 days")
	by := flags.String("by", "", "who makes the offer (default: the operating system user)")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || *slug == "" {
		return errors.New("usage: " + trialOfferUsage)
	}
	valid, err := parseOfferValidity(*validText)
	if err != nil {
		return err
	}
	if valid <= 0 || valid > store.TrialOfferMaxValidity {
		return fmt.Errorf("--valid %s: an offer link is valid for more than 0 and at most 30 days", *validText)
	}
	operator, err := operatorName(*by)
	if err != nil {
		return err
	}
	if _, err := useServerConfig(ctx, pool); err != nil {
		return err
	}
	domain, err := helpcenter.DomainFromEnv(settings)
	if err != nil {
		return err
	}
	workspaceID, err := database.New(pool).WorkspaceIDBySlug(ctx, *slug)
	if err != nil {
		return err
	}
	offer, token, err := store.New(pool).CreateTrialOffer(ctx, workspaceID, operator, clock().UTC(), valid)
	if err != nil {
		return err
	}
	link := workspacehost.Origin(origin, domain, *slug) + "/trial-offer?token=" + url.QueryEscape(token)
	_, err = fmt.Fprintf(output, "Trial offer %s for %s, valid until %s (by %s).\n"+
		"Send this link in your own email. An admin of %s who opens it and presses Continue trial gets 14 days from that moment:\n%s\n",
		offer.ID, *slug, offer.ExpiresAt.Format(time.RFC3339), operator, *slug, link)
	return err
}

// trialOffersCommand lists the workspace's offers with their state.
func trialOffersCommand(ctx context.Context, pool *pgxpool.Pool, args []string, output io.Writer) error {
	flags := flag.NewFlagSet("trial offers", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	slug := flags.String("workspace", "", "workspace slug")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || *slug == "" {
		return errors.New("usage: " + trialOffersUsage)
	}
	workspaceID, err := database.New(pool).WorkspaceIDBySlug(ctx, *slug)
	if err != nil {
		return err
	}
	offers, err := store.New(pool).TrialOffers(ctx, workspaceID, clock().UTC())
	if err != nil {
		return err
	}
	if len(offers) == 0 {
		_, err = fmt.Fprintf(output, "No trial offers for %s.\n", *slug)
		return err
	}
	for _, offer := range offers {
		line := fmt.Sprintf("%s  %s  made by %s at %s", offer.ID, offer.State, offer.CreatedBy, offer.CreatedAt.Format(time.RFC3339))
		switch offer.State {
		case store.TrialOfferAccepted:
			line += fmt.Sprintf(", accepted by %s at %s", deref(offer.AcceptedBy), offer.AcceptedAt.Format(time.RFC3339))
		case store.TrialOfferRevoked:
			line += fmt.Sprintf(", revoked by %s at %s", deref(offer.RevokedBy), offer.RevokedAt.Format(time.RFC3339))
		case store.TrialOfferExpired:
			line += ", expired at " + offer.ExpiresAt.Format(time.RFC3339)
		default:
			line += ", valid until " + offer.ExpiresAt.Format(time.RFC3339)
		}
		if _, err := fmt.Fprintln(output, line); err != nil {
			return err
		}
	}
	return nil
}

func deref(value *string) string {
	if value == nil {
		return "someone"
	}
	return *value
}

// trialOfferRevoke revokes an open offer by its id; its link then says it was withdrawn.
func trialOfferRevoke(ctx context.Context, pool *pgxpool.Pool, args []string, output io.Writer) error {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return errors.New("usage: " + trialOfferRevokeUsage)
	}
	id := args[0]
	flags := flag.NewFlagSet("trial offer revoke", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	by := flags.String("by", "", "who revokes the offer (default: the operating system user)")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 {
		return errors.New("usage: " + trialOfferRevokeUsage)
	}
	operator, err := operatorName(*by)
	if err != nil {
		return err
	}
	offer, err := store.New(pool).RevokeTrialOffer(ctx, id, operator, clock().UTC())
	var refused *store.TrialOfferRefusedError
	switch {
	case errors.As(err, &refused):
		return fmt.Errorf("trial offer %s is %s; only an open offer can be revoked (collie trial offers lists them)", id, refused.Reason)
	case errors.Is(err, store.ErrTrialOfferNotFound):
		return fmt.Errorf("no trial offer has the id %s (collie trial offers --workspace <slug> lists them)", id)
	case err != nil:
		return err
	}
	_, err = fmt.Fprintf(output, "Revoked trial offer %s (by %s at %s). Its link now says it was withdrawn.\n", offer.ID, operator, offer.RevokedAt.Format(time.RFC3339))
	return err
}
