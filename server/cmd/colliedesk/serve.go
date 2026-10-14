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

	"github.com/colliedesk/colliedesk/server/internal/cloudauth"
	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/colliedesk/colliedesk/server/internal/emailoutbox"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/store"
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
	smtpConfig, err := email.SMTPConfigFromEnv(os.Getenv)
	if err != nil {
		return err
	}
	if smtpConfig.Configured() {
		worker := emailoutbox.New(store.New(pool), email.SMTPSender{Config: smtpConfig}, emailoutbox.Options{})
		go worker.Run(ctx, 5*time.Second)
	}
	server := &http.Server{
		Addr:              *address,
		Handler:           webui.Handler(httpapi.New(pool, httpapi.Config{Origin: origin, CloudAuth: cloudAuthConfig(), SMTP: smtpConfig})),
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

func cloudAuthConfig() cloudauth.Config {
	return cloudauth.Config{
		Issuer:            os.Getenv("CLOUD_AUTH_ISSUER"),
		JWKSURL:           os.Getenv("CLOUD_AUTH_JWKS_URL"),
		AuthorizedParties: strings.Split(os.Getenv("CLOUD_AUTH_AUTHORIZED_PARTIES"), ","),
		PublishableKey:    os.Getenv("CLERK_PUBLISHABLE_KEY"),
		EmailClaim:        os.Getenv("CLOUD_AUTH_EMAIL_CLAIM"),
	}
}
