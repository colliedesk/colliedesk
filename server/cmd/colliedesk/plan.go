package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/user"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

const planUsage = "usage: colliedesk plan grant --workspace <slug> --plan team --until <RFC3339 time|forever> [--by <name>]\n" +
	"       colliedesk plan revoke --workspace <slug> [--by <name>]"

// planCommand grants or revokes a complimentary paid plan: a workspace that HC Labs keeps free of
// charge and without a trial clock. Every change records who made it and when.
func planCommand(ctx context.Context, pool *pgxpool.Pool, args []string, output io.Writer) error {
	if len(args) == 0 || (args[0] != "grant" && args[0] != "revoke") {
		return errors.New(planUsage)
	}
	verb := args[0]
	flags := flag.NewFlagSet("plan "+verb, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	slug := flags.String("workspace", "", "workspace slug")
	by := flags.String("by", "", "who makes the change (default: the operating system user)")
	plan := flags.String("plan", "", "plan to grant: team")
	until := flags.String("until", "", "end of the grant: an RFC3339 time or forever")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 || *slug == "" {
		return errors.New(planUsage)
	}
	if verb == "revoke" && (*plan != "" || *until != "") {
		return errors.New("plan revoke takes only --workspace and --by\n" + planUsage)
	}
	operator, err := operatorName(*by)
	if err != nil {
		return err
	}
	workspaceID, err := database.New(pool).WorkspaceIDBySlug(ctx, *slug)
	if err != nil {
		return err
	}
	now := clock().UTC()
	data := store.New(pool)
	if verb == "revoke" {
		if err := data.RevokeCompPlan(ctx, workspaceID, operator, now); err != nil {
			return fmt.Errorf("%s: %w", *slug, err)
		}
		_, err := fmt.Fprintf(output, "Revoked the complimentary plan of %s by %s at %s\n", *slug, operator, now.Format(time.RFC3339))
		return err
	}
	if *plan != domain.PlanTeam {
		return fmt.Errorf("--plan must be %s", domain.PlanTeam)
	}
	var end *time.Time
	endText := "forever"
	switch *until {
	case "":
		return errors.New(planUsage)
	case "forever":
	default:
		parsed, err := time.Parse(time.RFC3339, *until)
		if err != nil {
			return fmt.Errorf("--until must be forever or an RFC3339 time with a zone, for example 2027-01-01T00:00:00Z: %w", err)
		}
		parsed = parsed.UTC()
		end, endText = &parsed, parsed.Format(time.RFC3339)
	}
	previous, err := data.GrantCompPlan(ctx, workspaceID, *plan, operator, end, now)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(output, "Granted the complimentary %s plan to %s until %s by %s at %s\n", *plan, *slug, endText, operator, now.Format(time.RFC3339)); err != nil {
		return err
	}
	return describePrevious(output, previous)
}

// operatorName is the person recorded in the audit trail: --by, else the operating system user.
func operatorName(flagValue string) (string, error) {
	if flagValue != "" {
		return flagValue, nil
	}
	if name := os.Getenv("USER"); name != "" {
		return name, nil
	}
	current, err := user.Current()
	if err != nil || current.Username == "" {
		return "", errors.New("cannot tell who you are; pass --by <name>")
	}
	return current.Username, nil
}

// describePrevious tells the operator what happened to the grant that was in force.
func describePrevious(output io.Writer, previous *store.PreviousGrant) error {
	if previous == nil {
		return nil
	}
	until := "forever"
	if previous.Until != nil {
		until = previous.Until.UTC().Format(time.RFC3339)
	}
	if previous.Expired {
		_, err := fmt.Fprintf(output, "The previous grant (by %s) had expired on %s.\n", previous.GrantedBy, until)
		return err
	}
	_, err := fmt.Fprintf(output, "Replaced the grant by %s that ran until %s.\n", previous.GrantedBy, until)
	return err
}
