package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/jackc/pgx/v5/pgxpool"
)

// stderr receives hints meant for the person at the terminal; stdout stays machine-readable.
var stderr io.Writer = os.Stderr

// version is stamped at image build time with -ldflags "-X main.version=..."; see server/Dockerfile.
var version = "dev"

func run(ctx context.Context, args []string, output io.Writer, databaseURL, origin string) error {
	if len(args) == 0 {
		return fmt.Errorf("expected subcommand: serve, migrate, create-workspace, pair, import, healthcheck, or version")
	}
	if args[0] == "--help" || args[0] == "help" {
		_, err := fmt.Fprintln(output, "Usage: colliedesk <serve|migrate|create-workspace|pair|import|healthcheck|version>")
		return err
	}
	if args[0] == "version" {
		_, err := fmt.Fprintln(output, version)
		return err
	}
	if args[0] == "healthcheck" {
		return healthcheck(ctx, args[1:], databaseURL)
	}
	if databaseURL == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	pool, err := openPool(ctx, databaseURL)
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
		if requestURL := os.Getenv("DATABASE_REQUEST_URL"); requestURL != "" {
			if err := database.EnsureRequestLogin(ctx, pool, requestURL); err != nil {
				return err
			}
		}
		_, err := fmt.Fprintln(output, "migrations applied")
		return err
	case "create-workspace":
		return createWorkspace(ctx, pool, origin, args[1:], output)
	case "pair":
		return pairAgent(ctx, pool, origin, args[1:], output)
	case "serve":
		return serve(ctx, pool, origin, args[1:])
	case "import":
		return importCommand(ctx, pool, args[1:], output)
	default:
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
}

func healthcheck(ctx context.Context, args []string, databaseURL string) error {
	flags := flag.NewFlagSet("healthcheck", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	endpoint := flags.String("url", "http://127.0.0.1:8080/", "server URL")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("healthcheck accepts no positional arguments")
	}
	if databaseURL != "" {
		pool, err := pgxpool.New(ctx, databaseURL)
		if err != nil {
			return fmt.Errorf("open healthcheck database: %w", err)
		}
		defer pool.Close()
		pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		if err := pool.Ping(pingCtx); err != nil {
			return fmt.Errorf("ping healthcheck database: %w", err)
		}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, *endpoint, nil)
	if err != nil {
		return fmt.Errorf("create healthcheck request: %w", err)
	}
	response, err := (&http.Client{Timeout: 3 * time.Second}).Do(request)
	if err != nil {
		return fmt.Errorf("server is not healthy: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 400 {
		return fmt.Errorf("server is not healthy: HTTP %d", response.StatusCode)
	}
	return nil
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
	workspace, link, err := auth.New(pool, origin).CreateWorkspace(ctx, *name, *email)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(output, link); err != nil {
		return err
	}
	_, err = fmt.Fprintf(stderr, "Workspace %q created (slug %s). The link works once and expires in 15 minutes; print a new one with:\n  colliedesk pair --workspace %s --email %s\n",
		workspace.Name, workspace.Slug, workspace.Slug, strings.ToLower(*email))
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
