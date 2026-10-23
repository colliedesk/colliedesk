// Command collie-client is the collie a person installs on their own computer: it only has
// `collie mcp`, which connects an AI host to a Colliedesk server, and version and help. Releases
// ship it as `collie`; the server's collie, with the web app and the database commands, runs in
// the Docker image. It must not import the server (TestTheClientBuildLeavesTheServerOut).
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/colliedesk/colliedesk/server/internal/mcpbridge"
)

// version is stamped by release.yml with -ldflags "-X main.version=...".
var version = "dev"

const usage = `Usage: collie <mcp|version|help>

This collie is the client build: it connects an AI host to Colliedesk with collie mcp.
The server runs in the Docker image; see https://github.com/colliedesk/colliedesk/tree/main/docs/self-host

` + mcpbridge.Usage

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Getenv, os.Stdin, os.Stdout, os.Stderr))
}

// run returns the exit code: 2 for a command or setting to fix, 1 when the bridge stops on an error.
func run(ctx context.Context, args []string, getenv func(string) string, stdin io.Reader, stdout, stderr io.Writer) int {
	command := "help"
	if len(args) > 0 {
		command = args[0]
	}
	switch command {
	case "version":
		fmt.Fprintln(stdout, version)
		return 0
	case "help", "--help", "-h":
		fmt.Fprintln(stdout, usage)
		return 0
	case "mcp":
		cfg, err := mcpbridge.Parse(args[1:], getenv)
		if err != nil {
			fmt.Fprintf(stderr, "collie: %v\n", err)
			return 2
		}
		cfg.Version, cfg.Stderr = version, stderr
		if err := mcpbridge.Run(ctx, cfg, stdin, stdout); err != nil {
			fmt.Fprintf(stderr, "collie: %v\n", err)
			return 1
		}
		return 0
	default:
		fmt.Fprintf(stderr, "collie: %q is not in this collie. %s\n", command, usage)
		return 2
	}
}
