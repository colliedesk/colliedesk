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

	"github.com/colliedesk/colliedesk/server/internal/ai"
	"github.com/colliedesk/colliedesk/server/internal/aiprovider"
	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/cloudauth"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/colliedesk/colliedesk/server/internal/emailoutbox"
	"github.com/colliedesk/colliedesk/server/internal/freshdesk"
	"github.com/colliedesk/colliedesk/server/internal/google"
	"github.com/colliedesk/colliedesk/server/internal/helpcenter"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/importjobs"
	"github.com/colliedesk/colliedesk/server/internal/mcpserver"
	"github.com/colliedesk/colliedesk/server/internal/polar"
	"github.com/colliedesk/colliedesk/server/internal/providerdomain"
	"github.com/colliedesk/colliedesk/server/internal/resend"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/colliedesk/colliedesk/server/internal/telemetry"
	"github.com/colliedesk/colliedesk/server/internal/webui"
	"github.com/jackc/pgx/v5/pgxpool"
)

// importJobNetwork changes the importer options of in-app imports; only end-to-end builds set it,
// to reach a local fake Freshdesk.
var importJobNetwork func(*freshdesk.Options)

// googleBaseURL replaces Google's addresses with a local stand-in; only end-to-end builds set it.
var googleBaseURL = ""

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
	internalSignups, err := httpapi.ParseInternalSignups(os.Getenv("INTERNAL_SIGNUP_EMAILS"))
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
	googleConfig, err := google.ConfigFromEnv(os.Getenv)
	if err != nil {
		return err
	}
	helpCenterDomain, err := helpcenter.DomainFromEnv(os.Getenv)
	if err != nil {
		return err
	}
	aiServer, err := ai.ServerProviderFromEnv(os.Getenv)
	if err != nil {
		return err
	}
	if aiServer != nil && aiServer.Allowance != nil && aiServer.Kind != aiprovider.KindAnthropic {
		slog.Warn("AI_SERVER_MONTHLY_ALLOWANCE holds only while the OpenAI-compatible server reports token usage; calls without usage cost nothing against it")
	}
	googleConfig.Base = googleBaseURL
	if emailConfig.SMTP.Configured() || googleConfig.Enabled() {
		// A server with Gmail and no relay still runs the worker; replies that need SMTP fail with the reason.
		var sender emailoutbox.Sender
		if emailConfig.SMTP.Configured() {
			sender = email.SMTPSender{Config: emailConfig.SMTP}
		}
		worker := emailoutbox.New(store.NewWithDB(db), sender,
			emailoutbox.Options{FromPolicy: emailConfig.FromPolicy, MailFromDomain: emailConfig.MailFromDomain, Storage: storage, Google: &googleConfig})
		go worker.Run(ctx, 5*time.Second)
	}
	if emailConfig.ResendAPIKey != "" {
		// Provider domains a workspace stopped using, or left behind by being deleted, free their slots here.
		client := &resend.Client{APIKey: emailConfig.ResendAPIKey, Base: emailConfig.ResendAPIBase}
		go providerdomain.New(store.NewWithDB(db), client, providerdomain.Options{}).Run(ctx, time.Minute)
	}
	// In-app imports run here, on the owner pool like the CLI. Their keys are sealed with the Gmail ring
	// (MAILBOX_TOKEN_KEYS); without one they stay in this process's memory.
	importKeys := importjobs.NewKeys(googleConfig.Keys)
	var importPlans []string
	if billing.Enabled() {
		importPlans = billing.PlansOnSale()
	}
	importOptions := importjobs.WorkerOptions{Pool: pool, DB: db, Keys: importKeys, Storage: storage,
		Cloud: billing.Enabled(), PlansOnSale: importPlans, BaseURL: freshdeskBaseURL, Configure: importJobNetwork, Origin: origin}
	// The admins hear when an import ends, from the platform address, as for retention notices.
	if emailConfig.SMTP.Configured() && emailConfig.MailFromDomain != "" {
		importOptions.Mailer, importOptions.MailFrom = email.SMTPSender{Config: emailConfig.SMTP}, "noreply@"+emailConfig.MailFromDomain
	}
	go importjobs.NewWorker(importOptions).Run(ctx)
	warnWithoutTrustedProxies(slog.Default(), trustedProxies)
	// The hosted cloud is cloud sign-in with billing; a self-hosted server may use sign-in alone.
	hostedCloud := cloudAuth.Enabled() && billing.Enabled()
	telemetryConfig := telemetry.ConfigFromEnv(os.Getenv, origin, hostedCloud, version)
	go telemetry.New(db, telemetryConfig, nil).Run(ctx, slog.Default())
	go sweepDaily(ctx, store.NewWithDB(db).WithObjects(storage))
	mailImageCacheBytes, err := mailImageCacheBytesFromEnv(os.Getenv)
	if err != nil {
		return err
	}
	go sweepMailImageCache(ctx, store.NewWithDB(db).WithObjects(storage), mailImageCacheBytes)
	// What AI features may quote is built from a queue the database fills (docs/internals/ai-retrieval.md).
	go store.NewWithDB(db).RunAIRetrieval(ctx, 2*time.Second, func(err error) { slog.Error("build AI retrieval", "error", err) })
	kbVideoMaxBytes, err := kbVideoMaxBytesFromEnv(os.Getenv)
	if err != nil {
		return err
	}
	if billing.Enabled() {
		retentionService, err := newRetentionService(db, origin, billing, retentionSettingsForServe(os.Getenv, slog.Default()))
		if err != nil {
			slog.Error("workspace retention is not running", "error", err)
		} else {
			go sweepRetention(ctx, retentionService)
		}
	}
	// Workspace AI keys are sealed with the same ring as Gmail and imports (MAILBOX_TOKEN_KEYS).
	aiConfig := ai.Config{Server: aiServer, Keys: googleConfig.Keys, AllowPrivateBaseURLs: os.Getenv("AI_ALLOW_PRIVATE_BASE_URLS") == "1", PlanLimits: billing.Enabled()}
	apiConfig := httpapi.Config{AI: aiConfig, ImportKeys: importKeys, Origin: origin, HelpCenterDomain: helpCenterDomain, Version: version, CloudAuth: cloudAuth, Email: emailConfig, Google: googleConfig, Attachments: storage, MailImageCacheBytes: mailImageCacheBytes, KBVideoMaxBytes: kbVideoMaxBytes, RequestPool: requestPool, Billing: billing, TrustedProxies: trustedProxies, SignupHashKey: signupKey, SignupDisabled: signupWarning != "", InternalSignups: internalSignups, Funnel: telemetry.NewFunnel(telemetryConfig, nil),
		ProblemReportWorkspace: strings.TrimSpace(os.Getenv("PROBLEM_REPORT_WORKSPACE"))}
	// The web app reports how it is used, on the cloud and self-hosted alike; the same switch as the
	// usage report (DISABLE_TELEMETRY / DO_NOT_TRACK) turns it off.
	if telemetryConfig.Enabled() {
		apiConfig.ProductAnalytics = &httpapi.ProductAnalytics{Key: telemetryConfig.ProjectKey, Host: telemetryConfig.Endpoint, Realm: telemetryConfig.Realm}
	}
	if emailConfig.SMTP.Configured() {
		apiConfig.AlertEmail = strings.TrimSpace(os.Getenv("BILLING_ALERT_EMAIL"))
		apiConfig.AlertMailer = email.SMTPSender{Config: emailConfig.SMTP}
	}
	api := httpapi.New(pool, apiConfig)
	// AI tasks run in this process with the API's service, so a queued task wakes the worker at once.
	aiWorker := ai.NewWorker(api.AI(), db, ai.WorkerOptions{})
	aiWorker.Handle(ai.SlotDraft, api.AI().DraftHandler())
	aiWorker.Handle(ai.SlotTriage, api.AI().TriageHandler())
	go aiWorker.Run(ctx)
	if billing.Enabled() {
		go refreshFoundingAtStart(ctx, api)
	}
	server := &http.Server{
		Addr: *address,
		// The help center is outermost: a request to a help center host never reaches the app, the API or MCP.
		Handler:           api.WrapHelpCenter(mcpserver.Mount(mcpserver.Config{API: api, Auth: auth.NewWithDB(db, origin), Version: version}, webui.Handler(api))),
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

// refreshFoundingAtStart counts the Founding slots once so the state survives a restart. A failure
// leaves the state unknown, which keeps Founding on offer; the next Founding webhook counts again.
func refreshFoundingAtStart(ctx context.Context, api *httpapi.Server) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := api.RefreshFounding(ctx); err != nil {
		slog.Error("founding slot count at start failed", "error", err)
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

// sweepDaily runs the daily clean-up at start and then daily until ctx ends: it clears the call and
// answer of agent requests decided more than 90 days ago (the rows stay as the record of who asked and
// who decided), and deletes for good the saved replies and knowledge base articles and collections that
// have been in the trash for 30 days, and knowledge base images no article has linked for 30 days.
func sweepDaily(ctx context.Context, data *store.Store) {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		if pruned, err := data.PruneAgentRequests(ctx); err != nil && ctx.Err() == nil {
			slog.Error("prune old agent requests", "error", err)
		} else if pruned > 0 {
			slog.Info("pruned old agent requests", "count", pruned)
		}
		if purged, err := data.PurgeSavedReplyTrash(ctx); err != nil && ctx.Err() == nil {
			slog.Error("purge saved reply trash", "error", err)
		} else if purged > 0 {
			slog.Info("purged saved replies deleted more than 30 days ago", "count", purged)
		}
		if purged, err := data.PurgeKBTrash(ctx); err != nil && ctx.Err() == nil {
			slog.Error("purge knowledge base trash", "error", err)
		} else if purged != (store.KBPurge{}) {
			slog.Info("purged knowledge base articles and collections deleted more than 30 days ago, and unused images",
				"articles", purged.Articles, "collections", purged.Collections, "images", purged.Images)
		}
		backfillKBMedia(ctx, data)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// backfillKBMedia gives the library images stored before the server read their size and made a
// thumbnail at upload (and imported ones) both, a batch at a time until none is left.
func backfillKBMedia(ctx context.Context, data *store.Store) {
	total := 0
	for {
		done, err := data.BackfillKBMedia(ctx, store.KBBackfillBatch)
		if err != nil {
			if ctx.Err() == nil {
				slog.Error("backfill knowledge base images", "error", err)
			}
			return
		}
		total += done
		if done < store.KBBackfillBatch {
			break
		}
	}
	if total > 0 {
		slog.Info("read the size and made the thumbnail of older knowledge base images", "count", total)
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
