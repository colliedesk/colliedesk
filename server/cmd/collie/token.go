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

const tokenUsage = "usage: collie token create --workspace <slug> --email <agent email> (--scope <read|draft|send|admin> | --permission <resource:action>...) --label <text>"

// tokenCommand lets an operator give an agent a scoped token without a browser sign-in.
func tokenCommand(ctx context.Context, service *auth.Service, args []string, output io.Writer) error {
	if len(args) == 0 || args[0] != "create" {
		return errors.New(tokenUsage)
	}
	flags := flag.NewFlagSet("token create", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	workspace := flags.String("workspace", "", "workspace slug")
	email := flags.String("email", "", "email of the agent the token acts as")
	scope := flags.String("scope", "", "tier: "+strings.Join(auth.ScopeNames, ", "))
	label := flags.String("label", "", "name shown in Settings → Sessions")
	var permissions permissionFlags
	flags.Var(&permissions, "permission", "a permission (resource:action) instead of a tier; repeat for more")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 || *workspace == "" || *email == "" {
		return errors.New(tokenUsage)
	}
	created, err := service.IssueAgentToken(ctx, *workspace, *email, auth.TokenRequest{Label: *label, Scope: *scope, Permissions: permissions})
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(output, created.Token); err != nil {
		return err
	}
	held := *scope
	if permissions != nil {
		held = strings.Join(created.Session.Permissions, ", ")
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
