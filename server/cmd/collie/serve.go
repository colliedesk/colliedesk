package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/cloudauth"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/colliedesk/colliedesk/server/internal/emailoutbox"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/mcpserver"
	"github.com/colliedesk/colliedesk/server/internal/polar"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/colliedesk/colliedesk/server/internal/telemetry"
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
	billing, billingWarning := billingAtStart(billing, lockedScreenShipped)
	if billingWarning != "" {
		slog.Error(billingWarning)
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
	trustedProxies, err := httpapi.ParseTrustedProxies(os.Getenv("TRUSTED_PROXIES"))
	if err != nil {
		return err
	}
	signupKey := os.Getenv("SIGNUP_HASH_KEY")
	cloudAuth := cloudAuthConfig()
	signupWarning := signupProblem(cloudAuth.Enabled(), billing.Enabled(), trustedProxies, signupKey)
	if signupWarning != "" {
		slog.Error(signupWarning)
	}
	if cloudAuth.Enabled() && billing.Enabled() && signupWarning == "" {
		go sweepSignupAddresses(ctx, auth.NewWithDB(db, origin))
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
	warnWithoutTrustedProxies(slog.Default(), trustedProxies)
	// The hosted cloud is cloud sign-in with billing; a self-hosted server may use sign-in alone.
	hostedCloud := cloudAuth.Enabled() && billing.Enabled()
	go telemetry.New(db, telemetry.ConfigFromEnv(os.Getenv, origin, hostedCloud, version), nil).Run(ctx, slog.Default())
	if billing.Enabled() {
		retentionService, err := newRetentionService(db, origin, billing, retentionSettingsForServe(os.Getenv, slog.Default()))
		if err != nil {
			slog.Error("workspace retention is not running", "error", err)
		} else {
			go sweepRetention(ctx, retentionService)
		}
	}
	api := httpapi.New(pool, httpapi.Config{Origin: origin, CloudAuth: cloudAuth, Email: emailConfig, Attachments: storage, RequestPool: requestPool, Billing: billing, TrustedProxies: trustedProxies, SignupHashKey: signupKey, SignupDisabled: signupWarning != ""})
	server := &http.Server{
		Addr:              *address,
		Handler:           mcpserver.Mount(mcpserver.Config{API: api, Auth: auth.NewWithDB(db, origin), Version: version}, webui.Handler(api)),
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

// minSignupKeyBytes is the shortest SIGNUP_HASH_KEY accepted.
const minSignupKeyBytes = 32

// signupProblem explains why public sign-up must stay off on the hosted cloud (cloud sign-in and
// billing both on, which is when sign-up exists): it needs the key of the sign-up record hashes and
// the trusted proxies that reveal the client address. Without the second, every client shares the
// proxy's address and the fourth sign-up of the day is refused for everyone. A missing setting turns
// sign-up off with a logged reason instead of stopping the server, so the agents already working in
// their inboxes are never taken down by it. The text names variables, never values.
func signupProblem(cloudSignIn, billing bool, trustedProxies []netip.Prefix, key string) string {
	if !cloudSignIn || !billing {
		return ""
	}
	if len(key) < minSignupKeyBytes {
		return fmt.Sprintf("cloud sign-up is turned off: set SIGNUP_HASH_KEY to at least %d random characters (keeps the trial records pseudonymous; changing it later forgets who had a trial)", minSignupKeyBytes)
	}
	if len(trustedProxies) == 0 {
		return "cloud sign-up is turned off: set TRUSTED_PROXIES to the address of the reverse proxy as the server sees it (for example the Docker gateway, 172.18.0.1/32); without it every sign-up shares one client address"
	}
	return ""
}

// sweepSignupAddresses clears old client-address hashes at start and then every hour until ctx ends.
func sweepSignupAddresses(ctx context.Context, service *auth.Service) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		if _, err := service.ClearSignupAddresses(ctx); err != nil && ctx.Err() == nil {
			slog.Error("clear old sign-up addresses", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
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
// of a broken inbox. Billing locks workspaces, so serve keeps it off (and says so) until this is true.
const lockedScreenShipped = true

// billingAtStart keeps billing off while the web app cannot show the lock, and says why. The server
// still starts: an image older than its configuration must never take the inboxes down.
func billingAtStart(billing polar.Config, shipped bool) (polar.Config, string) {
	if billing.Enabled() && !shipped {
		return polar.Config{}, "billing is configured but turned off: this build has no locked screen yet, so workspaces would lock with no explanation in the web app"
	}
	return billing, ""
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
