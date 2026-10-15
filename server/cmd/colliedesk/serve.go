package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/cloudauth"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/colliedesk/colliedesk/server/internal/emailoutbox"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/polar"
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
	billing, err := polar.ConfigFromEnv(os.Getenv)
	if err != nil {
		return err
	}
	if err := requireLockedScreen(billing, lockedScreenShipped); err != nil {
		return err
	}
	requestPool := pool
	if requestURL := os.Getenv("DATABASE_REQUEST_URL"); requestURL != "" {
		opened, err := openPool(ctx, requestURL)
		if err != nil {
			return fmt.Errorf("open DATABASE_REQUEST_URL: %w", err)
		}
		defer opened.Close()
		requestPool = opened
	}
	db := database.NewSplit(pool, requestPool)
	if err := db.Verify(ctx); err != nil {
		return err
	}
	storage, err := attachmentStorage()
	if err != nil {
		return err
	}
	emailConfig, err := email.ConfigFromEnv(os.Getenv)
	if err != nil {
		return err
	}
	if emailConfig.SMTP.Configured() {
		worker := emailoutbox.New(store.NewWithDB(db), email.SMTPSender{Config: emailConfig.SMTP},
			emailoutbox.Options{FromPolicy: emailConfig.FromPolicy, MailFromDomain: emailConfig.MailFromDomain})
		go worker.Run(ctx, 5*time.Second)
	}
	trustedProxies, err := trustedProxiesFromEnv(os.Getenv)
	if err != nil {
		return err
	}
	server := &http.Server{
		Addr:              *address,
		Handler:           webui.Handler(httpapi.New(pool, httpapi.Config{Origin: origin, CloudAuth: cloudAuthConfig(), Email: emailConfig, Attachments: storage, RequestPool: requestPool, Billing: billing, TrustedProxies: trustedProxies})),
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

func cloudAuthConfig() cloudauth.Config {
	return cloudauth.Config{
		Issuer:            os.Getenv("CLOUD_AUTH_ISSUER"),
		JWKSURL:           os.Getenv("CLOUD_AUTH_JWKS_URL"),
		AuthorizedParties: strings.Split(os.Getenv("CLOUD_AUTH_AUTHORIZED_PARTIES"), ","),
		PublishableKey:    os.Getenv("CLERK_PUBLISHABLE_KEY"),
		EmailClaim:        os.Getenv("CLOUD_AUTH_EMAIL_CLAIM"),
	}
}

// lockedScreenShipped says the web app shows a locked workspace (trial over, nothing paid) instead
// of a broken inbox. Billing locks workspaces, so it stays off in production until this is true.
const lockedScreenShipped = false

// requireLockedScreen refuses to start with billing on while the web app cannot show the lock.
func requireLockedScreen(billing polar.Config, shipped bool) error {
	if billing.Enabled() && !shipped {
		return errors.New("billing is configured but this build has no locked screen yet: workspaces would lock with no explanation in the web app; unset every POLAR_* variable")
	}
	return nil
}

// defaultPoolConns is the pool size when the database URL has no pool_max_conns. pgx would pick
// max(4, CPU count), which a handful of slow workspace exports could exhaust; this stays well above
// the number of exports the server allows at once.
const defaultPoolConns = 20

// openPool connects with an explicit pool size. Put pool_max_conns=N in the URL to change it.
func openPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, err
	}
	if parsed, err := url.Parse(databaseURL); err != nil || !parsed.Query().Has("pool_max_conns") {
		config.MaxConns = defaultPoolConns
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	return pool, nil
}
