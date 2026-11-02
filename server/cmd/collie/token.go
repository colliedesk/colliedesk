package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/auth"
)

const tokenUsage = "usage: collie token create --workspace <slug> --email <agent email> (--role <role name> | --permission <resource:action>...) --label <text>"

// tokenCommand lets an operator give an agent a scoped token without a browser sign-in.
func tokenCommand(ctx context.Context, service *auth.Service, args []string, output io.Writer) error {
	if len(args) == 0 || args[0] != "create" {
		return errors.New(tokenUsage)
	}
	flags := flag.NewFlagSet("token create", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	workspace := flags.String("workspace", "", "workspace slug")
	email := flags.String("email", "", "email of the agent the token acts as")
	role := flags.String("role", "", "one of the person's roles, by name; the token follows it")
	scope := flags.String("scope", "", "deprecated: read, draft or send become that tier's old list of permissions, admin the Admin role")
	label := flags.String("label", "", "name shown in Settings → Sessions")
	var permissions permissionFlags
	flags.Var(&permissions, "permission", "a permission (resource:action) instead of a tier; repeat for more")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 || *workspace == "" || *email == "" {
		return errors.New(tokenUsage)
	}
	if *scope != "" {
		if _, err := fmt.Fprintln(stderr, "--scope is deprecated: tiers were replaced by roles. Use --role <name> or --permission <resource:action>."); err != nil {
			return err
		}
	}
	created, err := service.IssueAgentToken(ctx, *workspace, *email, auth.TokenRequest{Label: *label, RoleName: *role, Permissions: permissions, LegacyScope: *scope})
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(output, created.Token); err != nil {
		return err
	}
	held := strings.Join(created.Session.Permissions, ", ")
	if grant := created.Session.Token; grant != nil && grant.Role != nil {
		held = "follows the role " + grant.Role.Name
	}
	_, err = fmt.Fprintf(stderr, "Token %q (%s) created for %s. It is shown once above; Colliedesk keeps only its hash.\nRevoke it under Settings → Sessions (session %s).\n",
		created.Session.Label, held, created.Session.AgentEmail, created.Session.ID)
	return err
}

// permissionFlags collects repeated --permission flags.
type permissionFlags []string

func (p *permissionFlags) String() string { return strings.Join(*p, ",") }

func (p *permissionFlags) Set(value string) error {
	*p = append(*p, value)
	return nil
}
