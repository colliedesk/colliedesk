package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/jackc/pgx/v5/pgxpool"
)

const operatorUsage = `usage: collie operator <command>
  pair --email E [--reset-totp] [--by B]   print a one-time sign-in link for the operator console (15 minutes)
  add --email E [--by B]                   make E an operator without a link: with cloud sign-in they sign in with that email
  list                                     list the operators
  revoke EMAIL [--by B]                    end an operator's access: every session and token stops
  sessions [--revoke ID] [--by B]          list the operator sessions and tokens, or end one
  token create --email E --label L [--permission P]... [--by B]
                                           make an operator token for an AI agent (printed once)`

// operatorService opens the operator sign-in with the bootstrap of the environment.
func operatorService(pool *pgxpool.Pool, origin string) (*auth.OperatorService, error) {
	store, err := openConfigStore(database.New(pool))
	if err != nil {
		return nil, err
	}
	return auth.NewOperatorService(database.New(pool), store.Keys(), consoleBase(origin), nil), nil
}

func operatorCommand(ctx context.Context, pool *pgxpool.Pool, origin string, args []string, output io.Writer) error {
	if len(args) == 0 {
		return errors.New(operatorUsage)
	}
	service, err := operatorService(pool, origin)
	if err != nil {
		return err
	}
	switch args[0] {
	case "pair":
		flags, by := configFlags("pair")
		email := flags.String("email", "", "operator email")
		reset := flags.Bool("reset-totp", false, "replace the operator's authenticator")
		if rest, err := parseFlagsAnywhere(flags, args[1:]); err != nil || len(rest) != 0 || *email == "" {
			return errors.New("usage: collie operator pair --email EMAIL [--reset-totp] [--by NAME]")
		}
		actor, err := cliActor(*by)
		if err != nil {
			return err
		}
		link, err := service.Pair(ctx, *email, *reset, actor.Name)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(output, link)
		return err
	case "add":
		flags, by := configFlags("add")
		email := flags.String("email", "", "operator email")
		if rest, err := parseFlagsAnywhere(flags, args[1:]); err != nil || len(rest) != 0 || *email == "" {
			return errors.New("usage: collie operator add --email EMAIL [--by NAME]")
		}
		actor, err := cliActor(*by)
		if err != nil {
			return err
		}
		added, err := service.Add(ctx, *email, "cli", actor.Name)
		if err != nil {
			return err
		}
		normalized := strings.ToLower(strings.TrimSpace(*email))
		if !added {
			_, err = fmt.Fprintf(output, "%s is already an operator\n", normalized)
			return err
		}
		_, err = fmt.Fprintf(output, "added %s: with cloud sign-in they sign in with this email at %s/sign-in; otherwise collie operator pair --email %s prints their link. collie operator revoke %s takes them out\n",
			normalized, service.ConsoleBase(), normalized, normalized)
		return err
	case "list":
		operators, err := service.Operators(ctx)
		if err != nil {
			return err
		}
		for _, operator := range operators {
			state := "active"
			if operator.RevokedAt != nil {
				state = "revoked " + operator.RevokedAt.UTC().Format(time.RFC3339)
			}
			totp := "no authenticator"
			if operator.TOTPEnabledAt != nil {
				totp = "authenticator on"
			}
			if _, err := fmt.Fprintf(output, "%s\t%s\t%s\tadded %s by %s\n", operator.Email, state, totp, operator.CreatedAt.UTC().Format(time.RFC3339), operator.CreatedBy); err != nil {
				return err
			}
		}
		return nil
	case "revoke":
		flags, by := configFlags("revoke")
		rest, err := parseFlagsAnywhere(flags, args[1:])
		if err != nil || len(rest) != 1 {
			return errors.New("usage: collie operator revoke EMAIL [--by NAME]")
		}
		actor, err := cliActor(*by)
		if err != nil {
			return err
		}
		found, err := service.RevokeOperator(ctx, rest[0], "cli", actor.Name)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("no active operator %s", rest[0])
		}
		_, err = fmt.Fprintf(output, "revoked %s; collie operator pair --email %s lets them in again\n", rest[0], rest[0])
		return err
	case "sessions":
		flags, by := configFlags("sessions")
		revoke := flags.String("revoke", "", "end this session or token")
		if rest, err := parseFlagsAnywhere(flags, args[1:]); err != nil || len(rest) != 0 {
			return errors.New("usage: collie operator sessions [--revoke ID] [--by NAME]")
		}
		if *revoke != "" {
			actor, err := cliActor(*by)
			if err != nil {
				return err
			}
			found, err := service.RevokeSession(ctx, *revoke, "cli", actor.Name)
			if err != nil {
				return err
			}
			if !found {
				return fmt.Errorf("no active session %s", *revoke)
			}
			_, err = fmt.Fprintf(output, "ended %s\n", *revoke)
			return err
		}
		sessions, err := service.Sessions(ctx)
		if err != nil {
			return err
		}
		for _, session := range sessions {
			if _, err := fmt.Fprintf(output, "%s\t%s\t%s\t%s\t%s\texpires %s\n", session.ID, session.Email, session.Kind, session.Label,
				strings.Join(session.Permissions, ","), session.ExpiresAt.UTC().Format(time.RFC3339)); err != nil {
				return err
			}
		}
		return nil
	case "token":
		if len(args) < 2 || args[1] != "create" {
			return errors.New(operatorUsage)
		}
		flags, by := configFlags("token create")
		email := flags.String("email", "", "the operator the token acts as")
		label := flags.String("label", "", "what the token is for")
		var permissions stringList
		flags.Var(&permissions, "permission", "a permission besides reading: operator_config:write, operator:manage, operator_requests:cancel")
		if rest, err := parseFlagsAnywhere(flags, args[2:]); err != nil || len(rest) != 0 || *email == "" || *label == "" {
			return errors.New("usage: collie operator token create --email EMAIL --label LABEL [--permission P]... [--by NAME]")
		}
		if _, err := cliActor(*by); err != nil {
			return err
		}
		principal, err := service.CLIPrincipal(ctx, *email)
		if err != nil {
			return err
		}
		raw, session, err := service.CreateToken(ctx, principal, *label, permissions)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(stderr, "operator token %s for %s, expires %s; it is printed once\n", session.ID, session.Email, session.ExpiresAt.UTC().Format(time.RFC3339))
		_, err = fmt.Fprintln(output, raw)
		return err
	}
	return errors.New(operatorUsage)
}

type stringList []string

func (l *stringList) String() string     { return strings.Join(*l, ",") }
func (l *stringList) Set(v string) error { *l = append(*l, v); return nil }

var _ flag.Value = (*stringList)(nil)
