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

const tokenUsage = "usage: collie token create --workspace <slug> --email <agent email> --scope <read|draft|send|admin> --label <text>"

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
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 || *workspace == "" || *email == "" {
		return errors.New(tokenUsage)
	}
	created, err := service.IssueAgentToken(ctx, *workspace, *email, *label, *scope)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(output, created.Token); err != nil {
		return err
	}
	_, err = fmt.Fprintf(stderr, "Token %q (%s) created for %s. It is shown once above; Colliedesk keeps only its hash.\nRevoke it under Settings → Sessions (session %s).\n",
		created.Session.Label, *scope, created.Session.AgentEmail, created.Session.ID)
	return err
}
