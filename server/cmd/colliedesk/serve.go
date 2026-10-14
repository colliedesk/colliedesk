package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/hclabsco/colliedesk/server/internal/httpapi"
	"github.com/hclabsco/colliedesk/server/internal/webui"
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
	server := &http.Server{
		Addr:              *address,
		Handler:           webui.Handler(httpapi.New(pool, httpapi.Config{Origin: origin})),
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
