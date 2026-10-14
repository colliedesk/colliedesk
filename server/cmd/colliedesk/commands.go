package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/hclabsco/colliedesk/server/internal/auth"
	"github.com/hclabsco/colliedesk/server/internal/database"
	"github.com/jackc/pgx/v5/pgxpool"
)

func run(ctx context.Context, args []string, output io.Writer, databaseURL, origin string) error {
	if len(args) == 0 {
		return fmt.Errorf("expected subcommand: serve, migrate, create-workspace, or pair")
	}
	if databaseURL == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer pool.Close()

	switch args[0] {
	case "migrate":
		if len(args) != 1 {
			return fmt.Errorf("migrate accepts no arguments")
		}
		if err := database.Migrate(ctx, pool); err != nil {
			return err
		}
		_, err := fmt.Fprintln(output, "migrations applied")
		return err
	case "create-workspace":
		return createWorkspace(ctx, pool, origin, args[1:], output)
	case "pair":
		return pairAgent(ctx, pool, origin, args[1:], output)
	case "serve":
		return serve(ctx, pool, origin, args[1:])
	default:
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
}

func createWorkspace(ctx context.Context, pool *pgxpool.Pool, origin string, args []string, output io.Writer) error {
	flags := flag.NewFlagSet("create-workspace", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	name := flags.String("name", "", "workspace name")
	email := flags.String("admin-email", "", "first admin email")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if err := database.Migrate(ctx, pool); err != nil {
		return err
	}
	_, link, err := auth.New(pool, origin).CreateWorkspace(ctx, *name, *email)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(output, link)
	return err
}

func pairAgent(ctx context.Context, pool *pgxpool.Pool, origin string, args []string, output io.Writer) error {
	flags := flag.NewFlagSet("pair", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	workspace := flags.String("workspace", "", "workspace slug")
	email := flags.String("email", "", "agent email")
	if err := flags.Parse(args); err != nil {
		return err
	}
	link, err := auth.New(pool, origin).Pair(ctx, *workspace, *email)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(output, link)
	return err
}
