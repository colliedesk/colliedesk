package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	databaseURL := os.Getenv("DATABASE_URL")
	origin := os.Getenv("APP_ORIGIN")
	if origin == "" {
		origin = "http://localhost:8080"
	}
	if err := run(ctx, os.Args[1:], os.Stdout, databaseURL, origin); err != nil {
		fmt.Fprintf(os.Stderr, "colliedesk: %v\n", err)
		os.Exit(1)
	}
}
