package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/webui"
	"github.com/jackc/pgx/v5/pgxpool"
)

func serve(ctx context.Context, pool *pgxpool.Pool, origin string, args []string) error {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	defaultAddress := os.Getenv("LISTEN_ADDR")
	if defaultAddress == "" {
		defaultAddress = ":8080"
	}
	address := flags.String("listen", defaultAddress, "listen address")
	if err := flags.Parse(args); err != nil {
		return err
	}
	storage, err := attachmentStorage()
	if err != nil {
		return err
	}
	server := &http.Server{
		Addr:              *address,
		Handler:           webui.Handler(httpapi.New(pool, httpapi.Config{Origin: origin, Attachments: storage})),
		ReadHeaderTimeout: 5 * time.Second,
	}
	done := make(chan error, 1)
	go func() { done <- server.ListenAndServe() }()
	select {
	case err := <-done:
		if err != http.ErrServerClosed {
			return fmt.Errorf("serve HTTP: %w", err)
		}
		return nil
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			return fmt.Errorf("shutdown HTTP server: %w", err)
		}
		return nil
	}
}

// attachmentStorage opens the local-disk attachment store at ATTACHMENTS_DIR
// (default ./data/attachments). serve and import must point at the same place.
func attachmentStorage() (*attachments.LocalDisk, error) {
	dir := os.Getenv("ATTACHMENTS_DIR")
	if dir == "" {
		dir = "data/attachments"
	}
	return attachments.NewLocalDisk(dir)
}
