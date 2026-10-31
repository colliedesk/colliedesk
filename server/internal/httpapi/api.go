package httpapi

import (
	"context"
	"errors"
	"github.com/colliedesk/colliedesk/server/internal/autofixworker"
	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/channels/channelemail"
	"github.com/colliedesk/colliedesk/server/internal/channels/helpchat"
	"github.com/colliedesk/colliedesk/server/internal/channels/pipeline"
	"github.com/colliedesk/colliedesk/server/internal/channels/registry"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/ai"
	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/autofix/githubsync"
	"github.com/colliedesk/colliedesk/server/internal/cloudauth"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/colliedesk/colliedesk/server/internal/github"
	"github.com/colliedesk/colliedesk/server/internal/google"
	"github.com/colliedesk/colliedesk/server/internal/helpcenter"
	"github.com/colliedesk/colliedesk/server/internal/importjobs"
	"github.com/colliedesk/colliedesk/server/internal/mailimage"
	"github.com/colliedesk/colliedesk/server/internal/microsoft"
	"github.com/colliedesk/colliedesk/server/internal/polar"
	"github.com/colliedesk/colliedesk/server/internal/resend"
	"github.com/colliedesk/colliedesk/server/internal/serverconfig"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const sessionCookie = "colliedesk_session"

// Mailer hands one composed message to the mail relay (the shape of email.SMTPSender).
type Mailer interface {
	Send(ctx context.Context, from string, to []string, message []byte) error
}

type Config struct {
	Origin string
	// KBVideoMaxBytes (KB_VIDEO_MAX_BYTES) is the largest knowledge base video; 0 is
	// domain.MaxKBVideoBytes.
	KBVideoMaxBytes int64
	// HelpCenterDomain (HELP_CENTER_DOMAIN) gives each workspace the host name <slug>.<domain>: its root
	// leads to the desk's sign-in and its help center is under /help. Empty serves the help center on
	// <Origin>/help/<slug> only.
	HelpCenterDomain string
	// Version is the build stamped into the binary, reported by GET /api/v1/config; empty reports "dev".
	Version   string
	CloudAuth cloudauth.Config
	Email     email.Config
	// Google is the operator's OAuth client for connecting Gmail; with a setting missing, Gmail cannot be
	// connected and the settings name what is missing.
	Google google.Config
	// Microsoft is the operator's app registration for connecting Microsoft 365; with a setting missing,
	// Microsoft 365 cannot be connected and the settings name what is missing. It seals with Google.Keys.
	Microsoft microsoft.Config
	// GitHub is the operator's GitHub App that Autofix works through; with a setting missing, Autofix is
	// off and GET /api/v1/config names what is missing.
	GitHub github.Config
	// Cursor is Cursor's API for Autofix, shared with the worker of the process so they keep one call
	// budget. Nil uses Cursor itself; a Cursor without a Store gets the API's.
	Cursor *autofixworker.Cursor
	// ImportKeys holds the Freshdesk keys of in-app imports; the worker of the process shares it. Nil
	// makes one from Google.Keys (tests).
	ImportKeys *importjobs.Keys
	// AI is the server's AI provider and the rules for workspace providers (AI_SERVER_*,
	// AI_ALLOW_PRIVATE_BASE_URLS). Without its own key ring it seals keys with Google.Keys.
	AI          ai.Config
	Attachments attachments.Storage
	// MailImages fetches the remote images of incoming email for the image proxy; nil uses the
	// production fetcher (public addresses only). MailImageConcurrency caps fetches running at once
	// (default 8); more get 503 with Retry-After.
	MailImages *mailimage.Fetcher
	// MailImageConcurrency caps fetches running at once (default 8) and MailImageWorkspaceConcurrency the
	// share of one workspace (default 4). A request over the cap waits up to MailImageQueueWait (default
	// 10 s) for a slot, then gets 503 with Retry-After.
	MailImageConcurrency          int
	MailImageWorkspaceConcurrency int
	MailImageQueueWait            time.Duration
	// MailImageCacheBytes caps the image cache of one workspace (default 1 GiB, MAIL_IMAGE_CACHE_BYTES);
	// past it an image is served without being kept. The periodic sweep applies the same cap to the whole cache.
	MailImageCacheBytes int64
	// UploadTimeout is the longest one multipart message upload may take to arrive (default 3 minutes).
	UploadTimeout time.Duration
	// RequestPool, when set, runs workspace-scoped queries as a separate request login
	// (DATABASE_REQUEST_URL); nil runs them on the owner pool.
	RequestPool *pgxpool.Pool
	// Billing turns on Polar billing for the hosted cloud; the zero value leaves it off.
	Billing polar.Config
	// AlertEmail is the operator address told about a paid subscription that matches no workspace and about
	// a workspace whose email sending was paused for spam complaints (BILLING_ALERT_EMAIL). Empty, or without AlertMailer or Email.MailFromDomain, turns the alert off.
	AlertEmail  string
	AlertMailer Mailer
	// Now is the clock for the trial, the lock and plan limits; nil uses the system clock.
	Now func() time.Time
	// LookupTXT resolves DNS TXT records for domain verification; nil uses the system resolver.
	LookupTXT func(ctx context.Context, name string) ([]string, error)
	// LookupCNAME returns the end of a host name's CNAME chain, for the help center's custom domain
	// check; nil uses the system resolver.
	LookupCNAME func(ctx context.Context, host string) (string, error)
	// HelpCenterProbe fetches a help center page under a domain on trial (D12) and returns its HTTP status;
	// nil uses an HTTP client with a short timeout.
	HelpCenterProbe func(ctx context.Context, url string) (int, error)
	// MaxConcurrentExports caps workspace exports running at once on this server (default 2);
	// more get 429. ExportWriteTimeout is how long one write to a slow client may block
	// (default 60 s) before the export is aborted and its database connection released.
	MaxConcurrentExports int
	ExportWriteTimeout   time.Duration
	// ExportTimeout is the longest one export may run in total (default 2 h), however slowly the
	// client keeps reading.
	ExportTimeout time.Duration
	// TrustedProxies are the reverse proxies whose X-Forwarded-For is believed when finding the
	// client address of a sign-up or of a public web form submission (TRUSTED_PROXIES). Empty trusts
	// none: the peer address is the client.
	TrustedProxies []netip.Prefix
	// SignupHashKey keys the hashes of the sign-up records (SIGNUP_HASH_KEY, at least 32 bytes).
	SignupHashKey string
	// SignupDisabled turns cloud sign-up off (503 signup_unavailable) while its settings are missing;
	// serve sets it and logs why rather than refusing to start.
	SignupDisabled bool
	// InternalSignups marks the team's own sign-ups in the funnel (INTERNAL_SIGNUP_EMAILS, INTERNAL_SIGNUP_NETWORKS).
	InternalSignups InternalSignups
	// Random is the entropy for generated workspace slug suffixes; nil uses crypto/rand.
	Random io.Reader
	// Funnel receives the cloud's trial, activation (email channel, reply, teammate) and payment
	// events; nil records nothing.
	Funnel FunnelRecorder
	// ProductAnalytics is the PostHog project the web app reports to; nil (telemetry turned off
	// with DISABLE_TELEMETRY or DO_NOT_TRACK, or a development build) keeps it from loading.
	ProductAnalytics *ProductAnalytics
	// ProblemReportWorkspace is the slug of the workspace on this server that receives "Report a problem"
	// reports as conversations (PROBLEM_REPORT_WORKSPACE). Empty answers 404 not_configured, so the web app
	// offers a GitHub issue instead.
	ProblemReportWorkspace string
	// ExtraChannelTypes are channel types run after the registered ones; tests prove the frame with a
	// type no server ships (channeltest.Loopback). Each needs its channel_types row.
	ExtraChannelTypes []channels.Adapter
	// SwitchedOff are the features an operator switched off (serverconfig features.<name> = false). Their
	// settings and what they hold stay: the app hides them, the routes that use them answer 409 feature_off,
	// and what keeps their data whole (webhooks, inbound mail, keep-alive) runs on (feature-switches.go).
	SwitchedOff map[string]bool
	// Carry is the state the APIs of one process share across configurations (re-serve); nil makes a new one.
	Carry *Carry
	// OperatorOrigin is OPERATOR_ORIGIN: the operator console's own host. Empty serves it at Origin/operator.
	OperatorOrigin string
	// Operators signs the operators of the server in; nil makes one with the Gmail key ring.
	Operators *auth.OperatorService
	// ServerConfig is the server's own configuration, for the operator console; CheckConfig builds a
	// snapshot the way serve does (nil checks nothing); Environ reads the process environment (bootstrap
	// and leftovers; nil reads nothing).
	ServerConfig *serverconfig.Store
	CheckConfig  func(serverconfig.Snapshot) error
	Environ      func(string) string
}

// ProductAnalytics names the PostHog project key and host the web app sends usage events to, and
// the realm ("cloud" or "self-hosted") its events carry.
type ProductAnalytics struct {
	Key   string
	Host  string
	Realm string
}

// FunnelRecorder takes one sign-up, activation or payment event of a workspace. Record must not block the
// request; telemetry.Funnel sends in the background.
type FunnelRecorder interface {
	Record(event, workspaceID string, properties map[string]any)
}

type Route struct {
	Method     string
	Pattern    string
	Permission string
}

type API struct {
	// githubSync is Autofix's GitHub App (nil while its settings are missing): the calls, the webhook and
	// the reconcile pass.
	githubSync *githubsync.Service
	// dispatches counts the fixes being sent to GitHub by goroutines of this process.
	dispatches *sync.WaitGroup
	// limits is true on the hosted cloud, where the trial, lock and agent cap apply.
	limits bool
	// helpChat bounds the help center's AI chat per client address.
	helpChat *helpChatLimiters
	config   Config
	// db and preview count, across workspaces, what an operator's change would do (numbers only).
	db      *database.DB
	preview *serverconfig.Preview
	// alerted dedupes the operator alert for paid subscriptions without a workspace.
	alerted *alertedSubscriptions
	auth    *auth.Service
	cloud   *cloudauth.Verifier
	// operatorCloud checks the operator console's cloud sign-ins: the same identity provider, but only
	// tokens issued to the console's origin (cloud_auth.authorized_parties is the app's and stays so).
	operatorCloud *cloudauth.Verifier
	store         *store.Store
	// pipeline runs what channels receive into the store (internal/channels).
	pipeline *pipeline.Pipeline
	// exports admits workspace exports; see exportLimiter.
	exports     *exportLimiter
	exportLinks *exportLinks
	// mailImageSigner signs the proxy addresses of remote email images; mailImageFetcher fetches them;
	// mailImageLimits bounds the fetches running at once.
	mailImageSigner  *mailimage.Signer
	mailImageFetcher *mailimage.Fetcher
	mailImageLimits  *mailImageLimiter
	// webForms limits the public web form per client address and workspace.
	webForms       *webFormLimiter
	webFormClients *webFormLimiter
	// helpCenterPages and helpCenterSearches count each client address's help center pages and searches.
	helpCenterPages, helpCenterSearches *webFormLimiter
	helpCenter                          *helpcenter.Site
	// liveChecks bounds the DNS checks the reverse proxy's certificate question starts.
	liveChecks *helpcenter.LiveChecks
	// certificateNotes lets the certificate question record a certificate once an hour per host name.
	certificateNotes *webFormLimiter
	// operatorSignIns limits the tries of operator links, tickets and client addresses.
	operatorSignIns, operatorSignInIPs *webFormLimiter
	// helpCenterHostLookups counts each client address's lookups of host names the server does not know.
	helpCenterHostLookups *webFormLimiter
	// domainChecks limits how often agent tokens make the server look up DNS for one workspace.
	domainChecks *webFormLimiter
	// providerChecks limits the domain checks of one workspace that reach the email provider.
	providerChecks *webFormLimiter
	// cursorCalls limits the calls of one workspace a person makes reach Cursor (check, set the key),
	// so they cannot spend the call budget Cursor gives the key.
	cursorCalls *webFormLimiter
	// problemReports limits the problem reports one agent may file per hour.
	problemReports *webFormLimiter
	// testMessages limits the test messages a workspace sends itself per hour.
	testMessages *webFormLimiter
	// migrationRequests limits the Freshdesk migration requests a workspace sends per hour.
	migrationRequests *webFormLimiter
	polar             *polar.Client
	// resend verifies sending domains through the provider; nil when RESEND_API_KEY is not set.
	resend   *resend.Client
	founding *foundingState
	now      func() time.Time
	// kbUploads serializes the parts of one video upload and bounds the parts a workspace sends at once.
	kbUploads *kbUploadGate
	// kbUploadStart lets one upload start at a time, so each disk check counts the uploads started
	// before it (the server runs as one process).
	kbUploadStart *sync.Mutex
	// imports starts and reads in-app imports; the worker in `collie serve` runs them.
	imports *importjobs.Service
	// ai reads and changes the workspace's AI providers and features.
	ai *ai.Service
	// cursor is config.Cursor, filled in.
	cursor *autofixworker.Cursor
	// mux serves every route; an approved agent request is replayed through it as the approver.
	mux *http.ServeMux
}

type routeSpec struct {
	Route
	handler http.HandlerFunc
}

type principalKey struct{}

// Server is the HTTP handler of the API. RefreshFounding is the one thing the process starts besides
// serving.
type Server struct {
	http.Handler
	api *API
}

// Operators signs in the operators of the server, for the operator MCP endpoint.
func (s *Server) Operators() *auth.OperatorService { return s.api.config.Operators }

// AI is the service of the workspaces' AI settings and task queue, which the worker of the process shares.
func (s *Server) AI() *ai.Service { return s.api.ai }

// GitHubSync is Autofix's GitHub side (the reconcile pass, the repository check), nil while the server's
// GitHub App is not set up.
func (s *Server) GitHubSync() *githubsync.Service { return s.api.githubSync }

// RefreshFounding counts the Founding slots at Polar and closes Founding when they are used up. It
// does nothing when billing is off.
func (s *Server) RefreshFounding(ctx context.Context) error { return s.api.refreshFounding(ctx) }

// Outbound is the outbound email policy of a configuration: whether replies can be emailed now and
// from which identity.
func Outbound(config Config) store.OutboundPolicy {
	return store.OutboundPolicy{
		// Switched off reads as not set up: the channel says it cannot send, and its replies wait in the outbox.
		GmailEnabled:     config.Google.Enabled() && !config.SwitchedOff[serverconfig.FeatureGmail],
		MicrosoftEnabled: config.Microsoft.Enabled() && !config.SwitchedOff[serverconfig.FeatureMicrosoft],
		Enabled:          config.Email.SMTP.Configured(),
		// The same condition as the email outbox worker serve runs (cmd/collie generation.go).
		Worker:                config.Email.SMTP.Configured() || config.Google.Enabled() || config.Microsoft.Enabled(),
		RequireVerifiedDomain: config.Email.FromPolicy != email.FromPolicyAny,
		FallbackDomain:        config.Email.MailFromDomain,
	}
}

// Channels is the registry of channel types a configuration runs. revoke, when given, revokes the
// mailbox grant of a removed email channel at its provider.
func Channels(config Config, outbound store.OutboundPolicy, helpChat helpchat.Config, revoke ...func(*store.SealedGrant, string, string)) *channels.Registry {
	email := channelemail.Config{TrustedAuthServID: config.Email.TrustedAuthServID,
		HostedMailDomain: config.Email.HostedMailDomain, PlatformForwarding: config.Email.PlatformInboundSecret != "",
		Cloud: config.Billing.Enabled(), Outbound: outbound}
	if len(revoke) > 0 {
		email.RevokeGrant = revoke[0]
	}
	registered := registry.New(registry.Config{Email: email, HelpChat: helpChat})
	if len(config.ExtraChannelTypes) == 0 {
		return registered
	}
	adapters := make([]channels.Adapter, 0, len(registered.Types())+len(config.ExtraChannelTypes))
	for _, kind := range registered.Types() {
		adapter, _ := registered.Adapter(kind)
		adapters = append(adapters, adapter)
	}
	extended, err := channels.NewRegistry(append(adapters, config.ExtraChannelTypes...)...)
	if err != nil {
		panic(err) // a programming error of the test that added the type
	}
	return extended
}

func New(pool *pgxpool.Pool, config Config) *Server {
	config.Origin = strings.TrimRight(config.Origin, "/")
	if config.LookupTXT == nil {
		config.LookupTXT = net.DefaultResolver.LookupTXT
	}
	if config.LookupCNAME == nil {
		config.LookupCNAME = net.DefaultResolver.LookupCNAME
	}
	outbound := Outbound(config)
	requestPool := config.RequestPool
	if requestPool == nil {
		requestPool = pool
	}
	db := database.NewSplit(pool, requestPool)
	clock := config.Now
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}
	// Billing switched off bills and locks no workspace; its subscriptions stay as they are (feature-switches.go).
	limits := config.Billing.Enabled() && !config.SwitchedOff[serverconfig.FeatureBilling]
	carry := config.Carry
	if carry == nil {
		carry = NewCarry(config)
	}
	api := &API{config: config, now: clock, limits: limits, exports: carry.exports, exportLinks: carry.exportLinks, webForms: carry.webForms, webFormClients: carry.webFormClients,
		domainChecks: carry.domainChecks, providerChecks: carry.providerChecks, cursorCalls: carry.cursorCalls, problemReports: carry.problemReports,
		testMessages: carry.testMessages, migrationRequests: carry.migrationRequests, helpCenterPages: carry.helpCenterPages, helpCenterSearches: carry.helpCenterSearches,
		helpCenterHostLookups: carry.helpCenterHostLookups, certificateNotes: carry.certificateNotes, operatorSignIns: carry.operatorSignIns, operatorSignInIPs: carry.operatorSignInIPs, mailImageLimits: carry.mailImageLimits, liveChecks: carry.liveChecks,
		alerted: carry.alerted, kbUploads: carry.kbUploads, kbUploadStart: carry.kbUploadStart, dispatches: carry.dispatches, founding: carry.founding, helpChat: carry.helpChat,
		auth: auth.NewWithDB(db, config.Origin).WithWorkspaceHosts(config.HelpCenterDomain).WithClock(clock).WithPlanLimits(limits).WithPlansOnSale(config.Billing.PlansOnSale()),
		store: store.NewWithDB(db).WithObjects(config.Attachments).WithOutbound(outbound).WithClock(clock).WithPlanLimits(limits).WithPlansOnSale(config.Billing.PlansOnSale()).
			WithSealKeys(config.Google.Keys)}
	api.store = api.store.WithChannels(Channels(config, outbound, helpchat.Config{HelpCenterRoutes: api.helpChatRoutes(), HelpCenterAdditions: api.helpChatAdditions,
		ChatActive: func(ctx context.Context, tx pgx.Tx, workspaceID string) (bool, error) {
			return api.ai.ChatActiveIn(ctx, tx, workspaceID)
		}}, api.revokeSealed))
	api.pipeline = pipeline.New(api.store, clock)
	api.auth = api.auth.WithSignupKey(config.SignupHashKey)
	importKeys := config.ImportKeys
	if importKeys == nil {
		importKeys = importjobs.NewKeys(config.Google.Keys)
	}
	api.imports = importjobs.NewService(db, importKeys)
	api.imports.SetClock(clock)
	api.cursor = config.Cursor
	if api.cursor == nil {
		api.cursor = &autofixworker.Cursor{}
	}
	if api.cursor.Store == nil {
		api.cursor.Store = api.store
	}
	if api.cursor.Now == nil {
		api.cursor.Now = clock
	}
	aiConfig := config.AI
	if !aiConfig.Keys.Configured() {
		aiConfig.Keys = config.Google.Keys
	}
	aiConfig.PlanLimits = limits
	if config.SwitchedOff[serverconfig.FeatureAIServer] {
		aiConfig.Server, aiConfig.ServerOff = nil, true
	}
	if aiConfig.ChatGate == nil {
		aiConfig.ChatGate = carry.chatGate
	}
	api.ai = ai.NewService(db, aiConfig)
	api.ai.SetClock(clock)
	api.mailImageSigner = mailimage.NewSigner(api.mailImageKey())
	api.mailImageFetcher = config.MailImages
	if api.mailImageFetcher == nil {
		api.mailImageFetcher = mailimage.NewFetcher(mailimage.Options{})
	}
	if config.Email.ResendAPIKey != "" {
		api.resend = &resend.Client{APIKey: config.Email.ResendAPIKey, Base: config.Email.ResendAPIBase}
	}
	if config.Random != nil {
		api.auth = api.auth.WithRandom(config.Random)
	}
	if config.CloudAuth.Enabled() {
		api.cloud = cloudauth.New(config.CloudAuth)
		api.operatorCloud = cloudauth.New(config.CloudAuth.ForParty(api.operatorOrigin()))
	}
	if config.Billing.Enabled() {
		api.polar = polar.NewClient(config.Billing)
	}
	if config.GitHub.Enabled() {
		// serve checks the key first (github.Config.Check); a key that does not parse leaves Autofix off.
		if app, err := github.NewApp(config.GitHub); err == nil {
			api.githubSync = githubsync.New(api.store, app)
		} else {
			slog.Error("autofix: the GitHub App settings do not work", "error", err)
		}
	}
	if api.config.Operators == nil {
		base := strings.TrimRight(config.Origin, "/") + "/operator"
		if config.OperatorOrigin != "" {
			base = config.OperatorOrigin
		}
		api.config.Operators = auth.NewOperatorService(db, config.Google.Keys, base, func() bool { return api.hot().RequireTOTP }).WithClock(clock)
	}
	if api.config.ServerConfig == nil {
		api.config.ServerConfig = serverconfig.NewStore(db, config.Google.Keys, api.environ())
	}
	api.db, api.preview = db, serverconfig.NewPreview(db)
	api.helpCenter = api.newHelpCenter()
	mux := http.NewServeMux()
	api.mux = mux
	for _, route := range append(api.routeSpecs(), api.channelTypeRoutes()...) {
		if billingRoute(route.Route) && api.polar == nil {
			continue
		}
		if route.Pattern == resendWebhookPath && config.Email.ResendWebhookSecret == "" {
			continue
		}
		if route.Permission == auth.PermissionOperator {
			mux.Handle(route.Method+" "+route.Pattern, api.authorizeOperator(route.Route, route.handler))
			continue
		}
		if route.Pattern == "/api/v1/agent-requests/{id}/approve" || route.Pattern == "/api/v1/agent-requests/{id}/reject" {
			// The two approval routes serve both realms: the console names the operator realm, and a
			// workspace credential is never looked up for it (docs/internals/operator-console.md).
			workspace, operator := api.authorize(route.Route, route.handler), api.decideOperatorRequest(strings.HasSuffix(route.Pattern, "/approve"))
			mux.HandleFunc(route.Method+" "+route.Pattern, func(response http.ResponseWriter, request *http.Request) {
				if request.Header.Get("X-Colliedesk-Realm") == "operator" {
					operator(response, request)
					return
				}
				workspace(response, request)
			})
			continue
		}
		// The switch is checked before authorize: an agent token hears feature_off at once, never a request
		// filed for a person to approve into the same refusal.
		mux.Handle(route.Method+" "+route.Pattern, api.featureGate(route.Route, api.authorize(route.Route, route.handler)))
	}
	// The CORS preflight of the public web form is sent by the browser, never by an agent.
	mux.HandleFunc("OPTIONS "+webFormSubmitPattern, api.preflightWebForm)
	mux.HandleFunc("OPTIONS "+webFormChannelPattern, api.preflightWebForm)
	return &Server{Handler: withRequestID(api.observeTrial(mux)), api: api}
}

// observeTrial counts, while a trial change runs, the distinct client addresses this process sees, so the
// operator can tell whether trusted proxies read them right (one address for everyone is a sign they do not).
func (a *API) observeTrial(next http.Handler) http.Handler {
	if a.config.Carry == nil || a.config.Carry.Hot == nil {
		return next
	}
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if version := a.config.Carry.Hot.Trial(); version != 0 {
			a.config.Carry.trial.observe(version, clientIP(request, a.config.TrustedProxies))
		}
		next.ServeHTTP(response, request)
	})
}

func Routes() []Route {
	return []Route{
		{http.MethodGet, "/api/v1/config", auth.PermissionPublic},
		{http.MethodPost, "/api/v1/auth/cloud/exchange", auth.PermissionPublic},
		{http.MethodPost, cloudSignupPath, auth.PermissionPublic},
		{http.MethodPost, "/api/v1/auth/redeem", auth.PermissionPublic},
		{http.MethodPost, "/api/v1/operator/auth/redeem", auth.PermissionPublic},
		{http.MethodPost, "/api/v1/operator/auth/totp", auth.PermissionPublic},
		{http.MethodPost, "/api/v1/operator/auth/cloud", auth.PermissionPublic},
		{http.MethodGet, "/api/v1/operator/auth/me", auth.PermissionOperator},
		{http.MethodPost, "/api/v1/operator/auth/logout", auth.PermissionOperator},
		{http.MethodPost, "/api/v1/operator/tokens", auth.PermissionOperator},
		{http.MethodGet, "/api/v1/operator/status", auth.PermissionOperator},
		{http.MethodGet, "/api/v1/operator/config", auth.PermissionOperator},
		{http.MethodPatch, "/api/v1/operator/config", auth.PermissionOperator},
		{http.MethodPost, "/api/v1/operator/config/check", auth.PermissionOperator},
		{http.MethodGet, "/api/v1/operator/config/versions", auth.PermissionOperator},
		{http.MethodPost, "/api/v1/operator/config/versions/{id}/restore", auth.PermissionOperator},
		{http.MethodPost, "/api/v1/operator/config/trial/confirm", auth.PermissionOperator},
		{http.MethodPost, "/api/v1/operator/config/trial/revert", auth.PermissionOperator},
		{http.MethodGet, "/api/v1/operator/billing/products", auth.PermissionOperator},
		{http.MethodPost, "/api/v1/operator/billing/products", auth.PermissionOperator},
		{http.MethodPatch, "/api/v1/operator/billing/products/{id}", auth.PermissionOperator},
		{http.MethodGet, "/api/v1/operator/audit", auth.PermissionOperator},
		{http.MethodGet, "/api/v1/operator/requests", auth.PermissionOperator},
		{http.MethodGet, "/api/v1/operator/requests/{id}", auth.PermissionOperator},
		{http.MethodPost, "/api/v1/operator/requests/{id}/cancel", auth.PermissionOperator},
		{http.MethodGet, "/api/v1/operator/operators", auth.PermissionOperator},
		{http.MethodPost, "/api/v1/operator/operators", auth.PermissionOperator},
		{http.MethodDelete, "/api/v1/operator/operators/{id}", auth.PermissionOperator},
		{http.MethodDelete, "/api/v1/operator/sessions/{id}", auth.PermissionOperator},
		{http.MethodPost, "/api/v1/auth/logout", auth.PermissionAuthenticated},
		{http.MethodGet, "/api/v1/auth/me", auth.PermissionAuthenticated},
		{http.MethodGet, "/api/v1/auth/sessions", auth.PermissionAccessRead},
		{http.MethodDelete, "/api/v1/auth/sessions/{id}", auth.PermissionAccessRead},
		{http.MethodPost, "/api/v1/auth/tokens", auth.PermissionAuthenticated},
		{http.MethodPost, "/api/v1/auth/invites", auth.PermissionAccessWrite},
		{http.MethodGet, "/api/v1/auth/invites", auth.PermissionAccessRead},
		{http.MethodDelete, "/api/v1/auth/invites/{id}", auth.PermissionAccessWrite},
		{http.MethodGet, "/api/v1/conversations", auth.PermissionConversationsRead},
		{http.MethodPost, "/api/v1/conversations", auth.PermissionConversationsWrite},
		{http.MethodPatch, "/api/v1/conversations", auth.PermissionConversationsWrite},
		{http.MethodGet, "/api/v1/conversations/{id}", auth.PermissionConversationsRead},
		{http.MethodPatch, "/api/v1/conversations/{id}", auth.PermissionConversationsWrite},
		{http.MethodPut, "/api/v1/conversations/{id}/tags/{tagId}", auth.PermissionConversationsWrite},
		{http.MethodDelete, "/api/v1/conversations/{id}/tags/{tagId}", auth.PermissionConversationsWrite},
		{http.MethodGet, "/api/v1/conversations/{id}/messages", auth.PermissionConversationsRead},
		{http.MethodPost, "/api/v1/conversations/{id}/messages", auth.PermissionConversationsWrite},
		{http.MethodGet, "/api/v1/conversations/{id}/time-entries", auth.PermissionConversationsRead},
		{http.MethodPut, "/api/v1/conversations/{id}/presence", auth.PermissionConversationsRead},
		{http.MethodDelete, "/api/v1/conversations/{id}/presence/{clientId}", auth.PermissionConversationsRead},
		{http.MethodGet, "/api/v1/inbox/activity", auth.PermissionConversationsRead},
		{http.MethodGet, "/api/v1/attachments/{id}", auth.PermissionConversationsRead},
		{http.MethodGet, mailimage.ProxyPath, auth.PermissionConversationsRead},
		{http.MethodGet, "/api/v1/contacts", auth.PermissionContactsRead},
		{http.MethodPost, "/api/v1/contacts", auth.PermissionContactsWrite},
		{http.MethodGet, "/api/v1/contacts/{id}", auth.PermissionContactsRead},
		{http.MethodPatch, "/api/v1/contacts/{id}", auth.PermissionContactsWrite},
		{http.MethodPost, "/api/v1/contacts/{id}/merges", auth.PermissionContactsWrite},
		{http.MethodDelete, "/api/v1/contacts/{id}/merges/{mergeId}", auth.PermissionContactsWrite},
		{http.MethodGet, "/api/v1/agents", auth.PermissionAccessRead},
		{http.MethodPatch, "/api/v1/workspace/timezone", auth.PermissionAccessWrite},
		{http.MethodPatch, "/api/v1/workspace/slug", auth.PermissionAccessWrite},
		{http.MethodPatch, "/api/v1/workspace/name", auth.PermissionAccessWrite},
		{http.MethodPatch, "/api/v1/agents/me/timezone", auth.PermissionAuthenticated},
		{http.MethodPatch, "/api/v1/agents/me/display-name", auth.PermissionAuthenticated},
		{http.MethodPatch, "/api/v1/agents/{id}", auth.PermissionAccessWrite},
		{http.MethodGet, "/api/v1/tags", auth.PermissionConversationsRead},
		{http.MethodPost, "/api/v1/tags", auth.PermissionConversationsWrite},
		{http.MethodPatch, "/api/v1/tags/{id}", auth.PermissionConversationsWrite},
		{http.MethodDelete, "/api/v1/tags/{id}", auth.PermissionConversationsWrite},
		{http.MethodGet, "/api/v1/saved-replies", auth.PermissionConversationsRead},
		{http.MethodPost, "/api/v1/saved-replies", auth.PermissionConversationsWrite},
		{http.MethodPatch, "/api/v1/saved-replies/{id}", auth.PermissionConversationsWrite},
		{http.MethodDelete, "/api/v1/saved-replies/{id}", auth.PermissionConversationsWrite},
		{http.MethodGet, "/api/v1/saved-replies/trash", auth.PermissionConversationsRead},
		{http.MethodPost, "/api/v1/saved-replies/{id}/restore", auth.PermissionConversationsWrite},
		{http.MethodGet, "/api/v1/saved-replies/{id}/versions", auth.PermissionConversationsRead},
		{http.MethodPost, "/api/v1/saved-replies/{id}/versions/{version}/restore", auth.PermissionConversationsWrite},
		{http.MethodGet, "/api/v1/saved-views", auth.PermissionConversationsRead},
		{http.MethodPost, "/api/v1/saved-views", auth.PermissionConversationsWrite},
		{http.MethodGet, "/api/v1/saved-views/trash", auth.PermissionConversationsRead},
		{http.MethodPatch, "/api/v1/saved-views/{id}", auth.PermissionConversationsWrite},
		{http.MethodDelete, "/api/v1/saved-views/{id}", auth.PermissionConversationsWrite},
		{http.MethodPost, "/api/v1/saved-views/{id}/restore", auth.PermissionConversationsWrite},
		{http.MethodGet, "/api/v1/kb/collections", auth.PermissionConversationsRead},
		{http.MethodPost, "/api/v1/kb/collections", auth.PermissionConversationsWrite},
		{http.MethodGet, "/api/v1/kb/collections/trash", auth.PermissionConversationsRead},
		{http.MethodPatch, "/api/v1/kb/collections/{id}", auth.PermissionConversationsWrite},
		{http.MethodDelete, "/api/v1/kb/collections/{id}", auth.PermissionConversationsWrite},
		{http.MethodPost, "/api/v1/kb/collections/{id}/restore", auth.PermissionConversationsWrite},
		{http.MethodPut, "/api/v1/kb/order", auth.PermissionConversationsWrite},
		{http.MethodGet, "/api/v1/kb/articles", auth.PermissionConversationsRead},
		{http.MethodPost, "/api/v1/kb/articles", auth.PermissionConversationsWrite},
		{http.MethodGet, "/api/v1/kb/articles/trash", auth.PermissionConversationsRead},
		{http.MethodGet, "/api/v1/kb/articles/{id}", auth.PermissionConversationsRead},
		{http.MethodPatch, "/api/v1/kb/articles/{id}", auth.PermissionConversationsWrite},
		{http.MethodDelete, "/api/v1/kb/articles/{id}", auth.PermissionConversationsWrite},
		{http.MethodPost, "/api/v1/kb/articles/{id}/restore", auth.PermissionConversationsWrite},
		{http.MethodGet, "/api/v1/kb/articles/{id}/versions", auth.PermissionConversationsRead},
		{http.MethodPost, "/api/v1/kb/articles/{id}/versions/{version}/restore", auth.PermissionConversationsWrite},
		{http.MethodPost, "/api/v1/kb/articles/{id}/ai-exclusion", auth.PermissionConversationsWrite},
		{http.MethodDelete, "/api/v1/kb/articles/{id}/ai-exclusion", auth.PermissionConversationsWrite},
		{http.MethodPost, "/api/v1/kb/images", auth.PermissionConversationsWrite},
		{http.MethodGet, "/api/v1/kb/images/{id}", auth.PermissionConversationsRead},
		{http.MethodGet, "/api/v1/kb/media", auth.PermissionConversationsRead},
		{http.MethodPatch, "/api/v1/kb/media/{id}", auth.PermissionConversationsWrite},
		{http.MethodGet, "/api/v1/kb/media/trash", auth.PermissionConversationsRead},
		{http.MethodPost, "/api/v1/kb/media/trash", auth.PermissionConversationsWrite},
		{http.MethodPost, "/api/v1/kb/media/trash-unused", auth.PermissionConversationsWrite},
		{http.MethodPost, "/api/v1/kb/media/restore", auth.PermissionConversationsWrite},
		{http.MethodPost, "/api/v1/kb/media/purge", auth.PermissionAccessWrite},
		{http.MethodDelete, "/api/v1/kb/media/{id}", auth.PermissionConversationsWrite},
		{http.MethodPost, "/api/v1/kb/media/{id}/restore", auth.PermissionConversationsWrite},
		{http.MethodPost, "/api/v1/kb/media/{id}/replace", auth.PermissionConversationsWrite},
		{http.MethodPost, "/api/v1/kb/media-replacements/{id}/undo", auth.PermissionConversationsWrite},
		{http.MethodPost, "/api/v1/kb/media-uploads", auth.PermissionConversationsWrite},
		{http.MethodGet, "/api/v1/kb/media-uploads", auth.PermissionConversationsRead},
		{http.MethodGet, "/api/v1/kb/media-uploads/{id}", auth.PermissionConversationsRead},
		{http.MethodPut, "/api/v1/kb/media-uploads/{id}", auth.PermissionConversationsWrite},
		{http.MethodDelete, "/api/v1/kb/media-uploads/{id}", auth.PermissionConversationsWrite},
		{http.MethodPost, "/api/v1/kb/media-uploads/{id}/complete", auth.PermissionConversationsWrite},
		{http.MethodGet, "/api/v1/kb/media/{id}", auth.PermissionConversationsRead},
		{http.MethodPut, "/api/v1/kb/media/{id}/poster", auth.PermissionConversationsWrite},
		{http.MethodGet, "/api/v1/kb/media/{id}/poster/{version}", auth.PermissionConversationsRead},
		{http.MethodGet, "/api/v1/kb/media/{id}/details", auth.PermissionConversationsRead},
		{http.MethodGet, "/api/v1/kb/media/{id}/thumbnail", auth.PermissionConversationsRead},
		{http.MethodGet, "/api/v1/help-center", auth.PermissionConversationsRead},
		{http.MethodPut, "/api/v1/help-center", auth.PermissionAccessWrite},
		{http.MethodGet, "/api/v1/help-center/theme", auth.PermissionConversationsRead},
		{http.MethodPut, "/api/v1/help-center/theme", auth.PermissionAccessWrite},
		{http.MethodGet, "/api/v1/help-center/search-console", auth.PermissionConversationsRead},
		{http.MethodPut, "/api/v1/help-center/search-console", auth.PermissionAccessWrite},
		{http.MethodGet, "/api/v1/help-center/freshdesk-redirect", auth.PermissionConversationsRead},
		{http.MethodPost, "/api/v1/help-center/theme/check", auth.PermissionConversationsRead},
		{http.MethodGet, "/api/v1/help-center/domain", auth.PermissionConversationsRead},
		{http.MethodPut, "/api/v1/help-center/domain", auth.PermissionAccessWrite},
		{http.MethodDelete, "/api/v1/help-center/domain", auth.PermissionAccessWrite},
		{http.MethodPost, "/api/v1/help-center/domain/check", auth.PermissionAccessWrite},
		{http.MethodPost, "/api/v1/help-center/theme/preview", auth.PermissionConversationsRead},
		{http.MethodGet, helpCenterTLSAskPath, auth.PermissionPublic},
		{http.MethodPost, "/api/v1/conversations/{id}/messages/{messageId}/retry", auth.PermissionConversationsWrite},
		{http.MethodGet, "/api/v1/channels/email", auth.PermissionAccessRead},
		{http.MethodPut, "/api/v1/channels/email", auth.PermissionAccessWrite},
		{http.MethodDelete, "/api/v1/channels/email", auth.PermissionAccessWrite},
		{http.MethodPost, "/api/v1/channels/email/domain-verification", auth.PermissionAccessWrite},
		{http.MethodPost, "/api/v1/channels/email/inbound", auth.PermissionChannelInbound},
		{http.MethodPost, "/api/v1/channels/email/inbound/recipient", auth.PermissionChannelInbound},
		{http.MethodPost, "/api/v1/channels/email/secret", auth.PermissionAccessWrite},
		{http.MethodPost, gmailPath, auth.PermissionAccessWrite},
		{http.MethodDelete, gmailPath, auth.PermissionAccessWrite},
		{http.MethodGet, gmailCallbackPath, auth.PermissionPublic},
		{http.MethodGet, "/api/v1/billing", auth.PermissionBillingManage},
		{http.MethodPost, "/api/v1/billing/checkout", auth.PermissionBillingManage},
		{http.MethodPost, "/api/v1/billing/portal", auth.PermissionBillingManage},
		{http.MethodPost, "/api/v1/billing/migration-request", auth.PermissionBillingManage},
		{http.MethodPost, polarWebhookPath, auth.PermissionPublic},
		{http.MethodPost, resendWebhookPath, auth.PermissionPublic},
		{http.MethodGet, foundingPath, auth.PermissionPublic},
		{http.MethodGet, "/api/v1/export", auth.PermissionWorkspaceExport},
		{http.MethodGet, "/api/v1/channels/web-form", auth.PermissionAccessRead},
		{http.MethodPut, "/api/v1/channels/web-form", auth.PermissionAccessWrite},
		{http.MethodPost, webFormSubmitPattern, auth.PermissionPublic},
		{http.MethodPost, "/api/v1/export/link", auth.PermissionWorkspaceExport},
		{http.MethodGet, exportLinkPath + "{token}", auth.PermissionPublic},
		{http.MethodGet, "/api/v1/agent-requests", auth.PermissionAuthenticated},
		{http.MethodGet, "/api/v1/agent-requests/{id}", auth.PermissionAuthenticated},
		{http.MethodPost, "/api/v1/agent-requests/{id}/approve", auth.PermissionAuthenticated},
		{http.MethodPost, "/api/v1/agent-requests/{id}/reject", auth.PermissionAuthenticated},
		{http.MethodPost, "/api/v1/agent-requests/{id}/cancel", auth.PermissionAuthenticated},
		{http.MethodPost, problemReportPath, auth.PermissionAuthenticated},
		// The order is the contract fixture's: each case finds its job in the state it needs.
		{http.MethodGet, "/api/v1/imports", auth.PermissionWorkspaceImport},
		{http.MethodGet, "/api/v1/imports/{id}", auth.PermissionWorkspaceImport},
		{http.MethodPut, "/api/v1/imports/{id}/key", auth.PermissionWorkspaceImport},
		{http.MethodPost, "/api/v1/imports/{id}/cancel", auth.PermissionWorkspaceImport},
		{http.MethodPost, "/api/v1/imports/{id}/start", auth.PermissionWorkspaceImport},
		{http.MethodPost, "/api/v1/imports", auth.PermissionWorkspaceImport},
		{http.MethodGet, "/api/v1/imports/{id}/report", auth.PermissionWorkspaceImport},
		{http.MethodPost, "/api/v1/imports/{id}/undo", auth.PermissionWorkspaceImport},
		{http.MethodGet, "/api/v1/imports/{id}/people", auth.PermissionWorkspaceImport},
		{http.MethodPut, "/api/v1/imports/{id}/people/{externalId}/link", auth.PermissionWorkspaceImport},
		{http.MethodDelete, "/api/v1/imports/{id}/people/{externalId}/link", auth.PermissionWorkspaceImport},
		{http.MethodGet, "/api/v1/ai", auth.PermissionAccessRead},
		{http.MethodPost, "/api/v1/ai/providers", auth.PermissionAccessWrite},
		{http.MethodPost, "/api/v1/ai/models", auth.PermissionAccessWrite},
		{http.MethodPatch, "/api/v1/ai/providers/{id}", auth.PermissionAccessWrite},
		{http.MethodPut, "/api/v1/ai/providers/{id}/key", auth.PermissionAccessWrite},
		{http.MethodPost, "/api/v1/ai/providers/{id}/check", auth.PermissionAccessWrite},
		{http.MethodPut, "/api/v1/ai/slots/{slot}", auth.PermissionAccessWrite},
		{http.MethodDelete, "/api/v1/ai/providers/{id}", auth.PermissionAccessWrite},
		{http.MethodGet, "/api/v1/ai/usage", auth.PermissionBillingManage},
		{http.MethodGet, "/api/v1/ai/chat/usage", auth.PermissionBillingManage},
		{http.MethodGet, "/api/v1/ai/chat/sessions", auth.PermissionConversationsRead},
		{http.MethodGet, "/api/v1/ai/chat/sessions/{id}", auth.PermissionConversationsRead},
		{http.MethodDelete, "/api/v1/ai/chat/sessions/{id}", auth.PermissionAccessWrite},
		{http.MethodPost, "/api/v1/ai/chat/preview", auth.PermissionConversationsWrite},
		{http.MethodPut, "/api/v1/ai/automatic-limits", auth.PermissionAccessWrite},
		{http.MethodPost, "/api/v1/conversations/{id}/ai-drafts", auth.PermissionConversationsWrite},
		{http.MethodGet, "/api/v1/conversations/{id}/ai-drafts", auth.PermissionConversationsRead},
		{http.MethodGet, "/api/v1/conversations/{id}/ai-drafts/{draftId}", auth.PermissionConversationsRead},
		{http.MethodPost, "/api/v1/conversations/{id}/ai-drafts/{draftId}/discard", auth.PermissionConversationsWrite},
		{http.MethodGet, "/api/v1/conversations/{id}/ai-triage", auth.PermissionConversationsRead},
		{http.MethodPost, "/api/v1/conversations/{id}/ai-triage", auth.PermissionConversationsWrite},
		{http.MethodPost, "/api/v1/conversations/{id}/ai-triage/{suggestionId}/accept", auth.PermissionConversationsWrite},
		{http.MethodPost, "/api/v1/conversations/{id}/ai-triage/{suggestionId}/dismiss", auth.PermissionConversationsWrite},
		{http.MethodPost, "/api/v1/conversations/{id}/ai-triage/{suggestionId}/undo", auth.PermissionConversationsWrite},
		{http.MethodGet, "/api/v1/results", auth.PermissionConversationsRead},
		{http.MethodGet, "/api/v1/results/share-link", auth.PermissionAccessWrite},
		{http.MethodPost, "/api/v1/results/share-link", auth.PermissionAccessWrite},
		{http.MethodDelete, "/api/v1/results/share-link", auth.PermissionAccessWrite},
		{http.MethodPost, "/api/v1/results/shared", auth.PermissionPublic},
		{http.MethodPatch, "/api/v1/agents/me/results-email", auth.PermissionAuthenticated},
		{http.MethodGet, "/api/v1/workspace/setup", auth.PermissionAuthenticated},
		{http.MethodPut, "/api/v1/agents/me/setup-guide", auth.PermissionAuthenticated},
		{http.MethodGet, "/api/v1/channels", auth.PermissionAccessRead},
		{http.MethodPost, "/api/v1/channels", auth.PermissionAccessWrite},
		{http.MethodGet, "/api/v1/channels/events", auth.PermissionAccessRead},
		{http.MethodGet, "/api/v1/channels/{id}", auth.PermissionAccessRead},
		{http.MethodPatch, "/api/v1/channels/{id}", auth.PermissionAccessWrite},
		{http.MethodDelete, "/api/v1/channels/{id}", auth.PermissionAccessWrite},
		{http.MethodPut, "/api/v1/channels/{id}/credentials", auth.PermissionAccessWrite},
		{http.MethodPost, "/api/v1/channels/{id}/check", auth.PermissionAccessWrite},
		{http.MethodPost, "/api/v1/channels/{id}/held/replay", auth.PermissionAccessWrite},
		{http.MethodPost, "/api/v1/channels/{id}/held/discard", auth.PermissionAccessWrite},
		// One email channel or web form of several; the routes above without an id act on the default one.
		{http.MethodGet, "/api/v1/channels/{id}/email", auth.PermissionAccessRead},
		{http.MethodPost, "/api/v1/channels/{id}/email/domain-verification", auth.PermissionAccessWrite},
		{http.MethodPost, "/api/v1/channels/{id}/email/secret", auth.PermissionAccessWrite},
		{http.MethodPost, channelGmailPath, auth.PermissionAccessWrite},
		{http.MethodDelete, channelGmailPath, auth.PermissionAccessWrite},
		{http.MethodPost, webFormChannelPattern, auth.PermissionPublic},
		// Microsoft 365, beside Gmail: the default email channel, the callbacks, then one channel of several.
		{http.MethodPost, microsoftPath, auth.PermissionAccessWrite},
		{http.MethodDelete, microsoftPath, auth.PermissionAccessWrite},
		{http.MethodGet, microsoftCallbackPath, auth.PermissionPublic},
		{http.MethodGet, microsoftAdminConsentPath, auth.PermissionPublic},
		{http.MethodPost, channelMicrosoftPath, auth.PermissionAccessWrite},
		{http.MethodDelete, channelMicrosoftPath, auth.PermissionAccessWrite},
		// Autofix: what was sent to coding agents and where each fix is.
		{http.MethodGet, "/api/v1/autofix/status", auth.PermissionConversationsRead},
		{http.MethodGet, "/api/v1/autofix/fixes", auth.PermissionConversationsRead},
		{http.MethodGet, "/api/v1/autofix/fixes/{id}", auth.PermissionConversationsRead},
		{http.MethodGet, "/api/v1/conversations/{id}/fixes", auth.PermissionConversationsRead},
		// Autofix settings: the GitHub App's installations and the connected repositories.
		{http.MethodGet, "/api/v1/autofix/settings", auth.PermissionAccessRead},
		{http.MethodPost, "/api/v1/autofix/github/installations", auth.PermissionAccessWrite},
		{http.MethodDelete, "/api/v1/autofix/github/installations/{id}", auth.PermissionAccessWrite},
		{http.MethodGet, "/api/v1/autofix/github/installations/{id}/repositories", auth.PermissionAccessRead},
		{http.MethodGet, autofixCallbackPath, auth.PermissionPublic},
		{http.MethodPost, "/api/v1/autofix/repos", auth.PermissionAccessWrite},
		{http.MethodPatch, "/api/v1/autofix/repos/{id}", auth.PermissionAccessWrite},
		{http.MethodDelete, "/api/v1/autofix/repos/{id}", auth.PermissionAccessWrite},
		{http.MethodPut, "/api/v1/autofix/repos/{id}/include-notes", auth.PermissionAccessWrite},
		{http.MethodDelete, "/api/v1/autofix/repos/{id}/include-notes", auth.PermissionAccessWrite},
		{http.MethodPost, githubWebhookPath, auth.PermissionPublic},
		// Autofix: sending a conversation to a coding agent and a person's changes to its fix.
		{http.MethodPost, "/api/v1/conversations/{id}/fixes/preview", auth.PermissionConversationsRead},
		{http.MethodPost, "/api/v1/conversations/{id}/fixes", auth.PermissionConversationsWrite},
		{http.MethodPost, "/api/v1/autofix/fixes/{id}/retry", auth.PermissionConversationsWrite},
		{http.MethodPost, "/api/v1/autofix/fixes/{id}/cancel", auth.PermissionConversationsWrite},
		{http.MethodPut, "/api/v1/autofix/fixes/{id}/deployed", auth.PermissionConversationsWrite},
		{http.MethodDelete, "/api/v1/autofix/fixes/{id}/deployed", auth.PermissionConversationsWrite},
		{http.MethodPut, "/api/v1/autofix/fixes/{id}/pull-request", auth.PermissionConversationsWrite},
		{http.MethodDelete, "/api/v1/autofix/fixes/{id}/pull-request", auth.PermissionConversationsWrite},
		// Autofix's coding agents: turned on with the admin's data consent, checked, and Cursor's key.
		{http.MethodPut, "/api/v1/autofix/connectors/{kind}", auth.PermissionAccessWrite},
		{http.MethodDelete, "/api/v1/autofix/connectors/{kind}", auth.PermissionAccessWrite},
		{http.MethodPost, "/api/v1/autofix/connectors/{kind}/check", auth.PermissionAccessRead},
		{http.MethodPut, "/api/v1/autofix/connectors/cursor_api/key", auth.PermissionAccessWrite},
		{http.MethodDelete, "/api/v1/autofix/connectors/cursor_api/key", auth.PermissionAccessWrite},
		// A test message from the signed-in agent, on the default email channel or one of several.
		{http.MethodPost, "/api/v1/channels/email/test-message", auth.PermissionConversationsWrite},
		{http.MethodPost, "/api/v1/channels/{id}/email/test-message", auth.PermissionConversationsWrite},
		// What Gmail sent to confirm a new forwarding address, shown in the email setup.
		{http.MethodGet, "/api/v1/channels/{id}/email/forwarding-confirmation", auth.PermissionAccessRead},
	}
}

func (a *API) authorize(route Route, next http.HandlerFunc) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		permission := route.Permission
		if permission == auth.PermissionPublic || permission == auth.PermissionChannelInbound {
			// Channel routes authenticate their own secret in the handler; sessions never grant them.
			next(response, request)
			return
		}
		// An approved agent request replays its call in process as the approving person, who was
		// authenticated (and checked for CSRF) on the approve call itself.
		replay, replaying := approvalOf(request.Context())
		var raw string
		var viaCookie bool
		var principal auth.Principal
		if replaying {
			principal = replay.approver
		} else {
			var ok bool
			raw, viaCookie, ok = credential(request)
			if !ok {
				writeError(response, http.StatusUnauthorized, "unauthorized", "A valid session is required.")
				return
			}
			var err error
			principal, err = a.auth.Authenticate(request.Context(), raw, viaCookie)
			if err != nil {
				// A cookie that no longer signs in here (revoked, expired, or another workspace's on a
				// workspace host) is dropped, so the browser stops sending it.
				if viaCookie && errors.Is(err, auth.ErrInvalidCredential) {
					clearSessionCookie(response)
				}
				writeError(response, http.StatusUnauthorized, "unauthorized", "A valid session is required.")
				return
			}
			if a.gateToken(response, request, route, principal) {
				return
			}
		}
		if !auth.HasPermission(principal.Agent.Role, permission) {
			writeError(response, http.StatusForbidden, "forbidden", "This session does not have permission.")
			return
		}
		if viaCookie && writes(request.Method) && request.Header.Get("Origin") != a.requestOrigin(request) {
			writeError(response, http.StatusForbidden, "csrf_rejected", "Origin does not match this Colliedesk instance.")
			return
		}
		if a.refuseIfLocked(response, route, principal) || a.refuseIfSeatPaused(response, route, principal) {
			return
		}
		if viaCookie && request.URL.Path != "/api/v1/auth/logout" {
			setSessionCookie(response, raw)
		}
		ctx := context.WithValue(request.Context(), principalKey{}, principal)
		next(response, request.WithContext(ctx))
	}
}

func credential(request *http.Request) (string, bool, bool) {
	if header := request.Header.Get("Authorization"); header != "" {
		const prefix = "Bearer "
		if !strings.HasPrefix(header, prefix) || len(header) == len(prefix) {
			return "", false, false
		}
		return strings.TrimSpace(strings.TrimPrefix(header, prefix)), false, true
	}
	cookie, err := request.Cookie(sessionCookie)
	if err != nil || cookie.Value == "" {
		return "", false, false
	}
	return cookie.Value, true, true
}

func writes(method string) bool {
	return method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions
}

func principal(request *http.Request) auth.Principal {
	return request.Context().Value(principalKey{}).(auth.Principal)
}
