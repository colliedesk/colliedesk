package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/ai"
	"github.com/colliedesk/colliedesk/server/internal/aiprovider"
	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/autofix/githubsync"
	"github.com/colliedesk/colliedesk/server/internal/autofixworker"
	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/channels/channelemail"
	"github.com/colliedesk/colliedesk/server/internal/channels/helpchat"
	"github.com/colliedesk/colliedesk/server/internal/channels/outbox"
	"github.com/colliedesk/colliedesk/server/internal/channels/pipeline"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/colliedesk/colliedesk/server/internal/emailoutbox"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/importjobs"
	"github.com/colliedesk/colliedesk/server/internal/mcpserver"
	"github.com/colliedesk/colliedesk/server/internal/providerdomain"
	"github.com/colliedesk/colliedesk/server/internal/resend"
	"github.com/colliedesk/colliedesk/server/internal/results"
	"github.com/colliedesk/colliedesk/server/internal/serverconfig"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/colliedesk/colliedesk/server/internal/telemetry"
	"github.com/colliedesk/colliedesk/server/internal/webui"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// process is what `collie serve` sets up once and keeps while its configuration changes: the pools, the
// attachment store, the import keys (only in memory without a key ring), one Cursor client (one call
// budget) and the API state a new configuration must not reset (httpapi.Carry).
type process struct {
	pool, requestPool *pgxpool.Pool
	db                *database.DB
	origin            string
	storage           *attachments.LocalDisk
	importKeys        *importjobs.Keys
	cursor            *autofixworker.Cursor
	carry             *httpapi.Carry
	// operatorOrigin is OPERATOR_ORIGIN when valid; operators signs operators in for every generation.
	operatorOrigin string
	operators      *auth.OperatorService
	configStore    *serverconfig.Store
	wait           sync.WaitGroup
}

// Go runs a loop that does not depend on the configuration for the life of the process.
func (p *process) Go(ctx context.Context, run func(context.Context)) {
	p.wait.Add(1)
	go func() {
		defer p.wait.Done()
		run(ctx)
	}()
}

// generation is the API and the workers built from one version of the configuration. A new version
// builds a new generation; the old one's workers stop before the new one's start, and the handler swaps,
// so the port never closes (docs/internals/operator-console.md › Re-serve).
type generation struct {
	handler  http.Handler
	api      *httpapi.Server
	imports  *importjobs.Worker
	workers  []func(context.Context)
	cancel   context.CancelFunc
	wait     sync.WaitGroup
	stopOnce sync.Once
}

// Go registers a worker; Start runs every one.
func (g *generation) Go(run func(context.Context)) { g.workers = append(g.workers, run) }

// Handler serves this generation's requests.
func (g *generation) Handler() http.Handler { return g.handler }

// Start runs the workers until Stop or ctx ends.
func (g *generation) Start(ctx context.Context) {
	ctx, g.cancel = context.WithCancel(ctx)
	for _, run := range g.workers {
		g.wait.Add(1)
		go func() {
			defer g.wait.Done()
			run(ctx)
		}()
	}
}

// Stop ends the workers and waits up to timeout for them; false means some were still running.
func (g *generation) Stop(timeout time.Duration) bool {
	stopped := true
	g.stopOnce.Do(func() {
		if g.cancel != nil {
			g.cancel()
		}
		done := make(chan struct{})
		go func() { g.wait.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(timeout):
			stopped = false
		}
	})
	return stopped
}

// Busy says what keeps the generation from being replaced now: an in-app import it runs.
func (g *generation) Busy() string {
	if g.imports != nil && g.imports.Running() > 0 {
		return "an import running in the app"
	}
	return ""
}

// hotSettings are the settings the API reads at each use.
func hotSettings(config builtConfig) httpapi.HotSettings {
	return httpapi.HotSettings{AlertEmail: config.BillingAlertEmail, InternalSignups: config.InternalSignups,
		ProblemReportWorkspace: config.ProblemReportWorkspace, KBVideoMaxBytes: config.KBVideoMaxBytes, MailImageCacheBytes: config.MailImageCacheBytes,
		RequireTOTP: config.RequireTOTP}
}

// newGeneration builds the API and the workers of config, read from loaded. It starts nothing.
func newGeneration(p *process, loaded serverconfig.Loaded, config builtConfig, logger *slog.Logger) *generation {
	g := &generation{}
	db, origin, storage := p.db, p.origin, p.storage
	billing, billingWarning := billingAtStart(config.Billing, lockedScreenShipped)
	if billingWarning != "" {
		logger.Error(billingWarning)
	}
	trustedProxies, internalSignups, signupKey, cloudAuth := config.TrustedProxies, config.InternalSignups, config.SignupHashKey, config.CloudAuth
	signupWarning := signupProblem(cloudAuth.Enabled(), billing.Enabled(), trustedProxies, signupKey)
	if signupWarning != "" {
		logger.Error(signupWarning)
	}
	if cloudAuth.Enabled() && billing.Enabled() && signupWarning == "" {
		service := auth.NewWithDB(db, origin)
		g.Go(func(ctx context.Context) { sweepSignupAddresses(ctx, service) })
	}
	// Microsoft 365 seals its grants with the same ring as Gmail (MAILBOX_TOKEN_KEYS).
	emailConfig, googleConfig, microsoftConfig, helpCenterDomain, aiServer := config.Email, config.Google, config.Microsoft, config.HelpCenterDomain, config.AIServer
	if aiServer != nil && aiServer.Allowance != nil && aiServer.Kind != aiprovider.KindAnthropic {
		logger.Warn("ai.server.monthly_allowance holds only while the OpenAI-compatible server reports token usage; calls without usage cost nothing against it")
	}
	googleConfig.Base = googleBaseURL
	microsoftConfig.Base = microsoftBaseURL
	githubConfig := config.GitHub
	githubConfig.Base = githubBaseURL
	// What channels receive and send goes through their types (docs/internals/channels.md): the outbox
	// worker sends every channel's replies, email through SMTP, Gmail or Microsoft 365; without any,
	// replies that need them fail with the reason. The journal worker processes what Async channels received.
	channelConfig := httpapi.Config{Email: emailConfig, Google: googleConfig, Microsoft: microsoftConfig, Billing: billing}
	channelStore := store.NewWithDB(db).WithObjects(storage).WithChannels(httpapi.Channels(channelConfig, httpapi.Outbound(channelConfig), helpchat.Config{})).
		WithSealKeys(googleConfig.Keys).WithPlanLimits(billing.Enabled())
	var sender emailoutbox.Sender
	if emailConfig.SMTP.Configured() {
		sender = email.SMTPSender{Config: emailConfig.SMTP}
	}
	if emailConfig.SMTP.Configured() || googleConfig.Enabled() || microsoftConfig.Enabled() {
		// A server with a mailbox provider and no relay still sends email; replies that need SMTP fail with the reason.
		worker := emailoutbox.New(channelStore, sender,
			emailoutbox.Options{FromPolicy: emailConfig.FromPolicy, MailFromDomain: emailConfig.MailFromDomain, Storage: storage, Google: &googleConfig, Microsoft: &microsoftConfig})
		g.Go(func(ctx context.Context) { worker.Run(ctx, 5*time.Second) })
		// A quiet Microsoft 365 mailbox would lose its grant after 90 days unused; this refreshes it weekly
		// and shows a revoked one before a customer waits for a reply.
		if microsoftConfig.Enabled() {
			g.Go(func(ctx context.Context) { worker.RunKeepAlive(ctx, time.Hour) })
		}
	} else {
		// Without either, email waits for a server that can send it; every other channel is sent here.
		worker := outbox.New(channelStore, outbox.Options{Skip: []channels.Type{channelemail.Type}})
		g.Go(func(ctx context.Context) { worker.Run(ctx, 5*time.Second) })
	}
	journal := pipeline.New(channelStore, nil)
	g.Go(func(ctx context.Context) { journal.Run(ctx, 2*time.Second) })
	if emailConfig.ResendAPIKey != "" {
		// Provider domains a workspace stopped using, or left behind by being deleted, free their slots here.
		client := &resend.Client{APIKey: emailConfig.ResendAPIKey, Base: emailConfig.ResendAPIBase}
		sweep := providerdomain.New(store.NewWithDB(db), client, providerdomain.Options{})
		g.Go(func(ctx context.Context) { sweep.Run(ctx, time.Minute) })
	}
	// In-app imports run here, on the owner pool like the CLI. Their keys are sealed with the Gmail ring
	// (MAILBOX_TOKEN_KEYS); without one they stay in this process's memory (p.importKeys).
	var importPlans []string
	if billing.Enabled() {
		importPlans = billing.PlansOnSale()
	}
	importOptions := importjobs.WorkerOptions{Pool: p.pool, DB: db, Keys: p.importKeys, Storage: storage,
		Cloud: billing.Enabled(), PlansOnSale: importPlans, BaseURL: freshdeskBaseURL, Configure: importJobNetwork, Origin: origin, HelpCenterDomain: helpCenterDomain}
	// The admins hear when an import ends, from the platform address, as for retention notices.
	if emailConfig.SMTP.Configured() && emailConfig.MailFromDomain != "" {
		importOptions.Mailer, importOptions.MailFrom = email.SMTPSender{Config: emailConfig.SMTP}, "noreply@"+emailConfig.MailFromDomain
	}
	g.imports = importjobs.NewWorker(importOptions)
	g.Go(g.imports.Run)
	warnWithoutTrustedProxies(logger, trustedProxies)
	// The hosted cloud is cloud sign-in with billing; a self-hosted server may use sign-in alone.
	hostedCloud := cloudAuth.Enabled() && billing.Enabled()
	telemetryConfig := telemetry.ConfigFromEnv(loaded.Getenv, origin, hostedCloud, version)
	// The snapshot answers DISABLE_TELEMETRY for the setting too: say which turned it off.
	if telemetryConfig.OffReason == "DISABLE_TELEMETRY is set" && os.Getenv("DISABLE_TELEMETRY") == "" {
		telemetryConfig.OffReason = "telemetry.enabled is false"
	}
	usage := telemetry.New(db, telemetryConfig, nil)
	g.Go(func(ctx context.Context) { usage.Run(ctx, logger) })
	// The results page only reads numbers this worker computes (docs/internals/results.md). It also sends
	// the weekly email to admins, from the platform address, when the server can send mail.
	resultsWorker := results.NewWorker(db)
	if emailConfig.SMTP.Configured() && emailConfig.MailFromDomain != "" {
		resultsWorker.WithMail(results.Mail{Mailer: email.SMTPSender{Config: emailConfig.SMTP}, From: "noreply@" + emailConfig.MailFromDomain, Origin: origin, HelpCenterDomain: helpCenterDomain,
			Locked: func(ctx context.Context, tx pgx.Tx, workspaceID string, now time.Time) (bool, error) {
				if !billing.Enabled() {
					return false, nil
				}
				access, err := store.WorkspaceAccess(ctx, tx, workspaceID, now)
				return access.Locked, err
			}})
	}
	g.Go(resultsWorker.Run)
	if billing.Enabled() {
		retentionService, err := newRetentionService(db, origin, billing, config.Retention)
		if err != nil {
			logger.Error("workspace retention is not running", "error", err)
		} else {
			g.Go(func(ctx context.Context) { sweepRetention(ctx, retentionService) })
		}
	}
	// Workspace AI keys are sealed with the same ring as Gmail and imports (MAILBOX_TOKEN_KEYS).
	aiConfig := ai.Config{Server: aiServer, Keys: googleConfig.Keys, AllowPrivateBaseURLs: config.AllowPrivateBaseURLs, PlanLimits: billing.Enabled()}
	apiConfig := httpapi.Config{Cursor: p.cursor, AI: aiConfig, ImportKeys: p.importKeys, Origin: origin, HelpCenterDomain: helpCenterDomain, Version: version, CloudAuth: cloudAuth,
		Email: emailConfig, Google: googleConfig, Microsoft: microsoftConfig, GitHub: githubConfig, Attachments: storage, MailImageCacheBytes: config.MailImageCacheBytes,
		KBVideoMaxBytes: config.KBVideoMaxBytes, RequestPool: p.requestPool, Billing: billing, TrustedProxies: trustedProxies, SignupHashKey: signupKey,
		SignupDisabled: signupWarning != "", InternalSignups: internalSignups, Funnel: telemetry.NewFunnel(telemetryConfig, nil),
		ProblemReportWorkspace: config.ProblemReportWorkspace, Carry: p.carry, OperatorOrigin: p.operatorOrigin, Operators: p.operators,
		ServerConfig: p.configStore, CheckConfig: func(snap serverconfig.Snapshot) error { _, err := checkSnapshot(snap); return err }, Environ: os.Getenv}
	// The web app reports how it is used, on the cloud and self-hosted alike; the same switch as the
	// usage report (DISABLE_TELEMETRY / DO_NOT_TRACK) turns it off.
	if telemetryConfig.Enabled() {
		apiConfig.ProductAnalytics = &httpapi.ProductAnalytics{Key: telemetryConfig.ProjectKey, Host: telemetryConfig.Endpoint, Realm: telemetryConfig.Realm}
	}
	if emailConfig.SMTP.Configured() {
		apiConfig.AlertEmail = config.BillingAlertEmail
		apiConfig.AlertMailer = email.SMTPSender{Config: emailConfig.SMTP}
	}
	apiConfig.LookupCNAME, apiConfig.LookupTXT = lookupCNAME, lookupTXT
	api := httpapi.New(p.pool, apiConfig)
	g.api = api
	g.Go(func(ctx context.Context) { recheckHelpCenterDomainsDaily(ctx, api) })
	// AI tasks run in this process with the API's service, so a queued task wakes the worker at once.
	aiWorker := ai.NewWorker(api.AI(), db, ai.WorkerOptions{})
	aiWorker.Handle(ai.SlotDraft, api.AI().DraftHandler())
	aiWorker.Handle(ai.SlotTriage, api.AI().TriageHandler())
	g.Go(aiWorker.Run)
	// Autofix follows GitHub every ten minutes too, for a server the App's webhook cannot reach.
	if sync := api.GitHubSync(); sync != nil {
		g.Go(func(ctx context.Context) { sync.Run(ctx, githubsync.ReconcileEvery) })
		// Fixes sent to Cursor: the worker reads each repository on GitHub before a fix leaves for it.
		fixes := &autofixworker.Worker{Cursor: p.cursor, Repos: sync}
		g.Go(func(ctx context.Context) { fixes.Run(ctx, 5*time.Second) })
	}
	if billing.Enabled() {
		g.Go(func(ctx context.Context) { refreshFoundingAtStart(ctx, api) })
	}
	// The console's sign-in page offers cloud sign-in when it is configured (POST /api/v1/operator/auth/cloud).
	var consoleSignIn webui.ConsoleSignIn
	if cloudAuth.Enabled() {
		consoleSignIn.PublishableKey = cloudAuth.PublishableKey
	}
	// The help center is outermost: a request to a help center host never reaches the app, the API or MCP.
	inner := mcpserver.Mount(mcpserver.Config{API: api, Auth: auth.NewWithDB(db, origin), Version: version, Operators: p.operators}, webui.Handler(api, consoleSignIn))
	g.handler = operatorHost(origin, p.operatorOrigin, api.WrapHelpCenter(inner), inner, webui.OperatorHandler("", consoleSignIn))
	return g
}
