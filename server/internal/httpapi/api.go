package httpapi

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/ai"
	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/cloudauth"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/colliedesk/colliedesk/server/internal/google"
	"github.com/colliedesk/colliedesk/server/internal/helpcenter"
	"github.com/colliedesk/colliedesk/server/internal/importjobs"
	"github.com/colliedesk/colliedesk/server/internal/mailimage"
	"github.com/colliedesk/colliedesk/server/internal/polar"
	"github.com/colliedesk/colliedesk/server/internal/resend"
	"github.com/colliedesk/colliedesk/server/internal/store"
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
	// HelpCenterDomain (HELP_CENTER_DOMAIN) serves each workspace's help center on <slug>.<domain>;
	// empty serves it on <Origin>/help/<slug> only.
	HelpCenterDomain string
	// Version is the build stamped into the binary, reported by GET /api/v1/config; empty reports "dev".
	Version   string
	CloudAuth cloudauth.Config
	Email     email.Config
	// Google is the operator's OAuth client for connecting Gmail; with a setting missing, Gmail cannot be
	// connected and the settings name what is missing.
	Google google.Config
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
	// InternalSignups marks the team's own sign-ups in the funnel (INTERNAL_SIGNUP_EMAILS).
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
	// limits is true on the hosted cloud, where the trial, lock and agent cap apply.
	limits bool
	config Config
	// alerted dedupes the operator alert for paid subscriptions without a workspace.
	alerted alertedSubscriptions
	auth    *auth.Service
	cloud   *cloudauth.Verifier
	store   *store.Store
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
	// domainChecks limits how often agent tokens make the server look up DNS for one workspace.
	domainChecks *webFormLimiter
	// providerChecks limits the domain checks of one workspace that reach the email provider.
	providerChecks *webFormLimiter
	// problemReports limits the problem reports one agent may file per hour.
	problemReports *webFormLimiter
	polar          *polar.Client
	// resend verifies sending domains through the provider; nil when RESEND_API_KEY is not set.
	resend   *resend.Client
	founding foundingState
	now      func() time.Time
	// kbUploads serializes the parts of one video upload and bounds the parts a workspace sends at once.
	kbUploads kbUploadGate
	// kbUploadStart lets one upload start at a time, so each disk check counts the uploads started
	// before it (the server runs as one process).
	kbUploadStart sync.Mutex
	// imports starts and reads in-app imports; the worker in `collie serve` runs them.
	imports *importjobs.Service
	// ai reads and changes the workspace's AI providers and features.
	ai *ai.Service
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

// AI is the service of the workspaces' AI settings and task queue, which the worker of the process shares.
func (s *Server) AI() *ai.Service { return s.api.ai }

// RefreshFounding counts the Founding slots at Polar and closes Founding when they are used up. It
// does nothing when billing is off.
func (s *Server) RefreshFounding(ctx context.Context) error { return s.api.refreshFounding(ctx) }

func New(pool *pgxpool.Pool, config Config) *Server {
	config.Origin = strings.TrimRight(config.Origin, "/")
	if config.LookupTXT == nil {
		config.LookupTXT = net.DefaultResolver.LookupTXT
	}
	outbound := store.OutboundPolicy{
		GmailEnabled:          config.Google.Enabled(),
		Enabled:               config.Email.SMTP.Configured(),
		RequireVerifiedDomain: config.Email.FromPolicy != email.FromPolicyAny,
		FallbackDomain:        config.Email.MailFromDomain,
	}
	requestPool := config.RequestPool
	if requestPool == nil {
		requestPool = pool
	}
	db := database.NewSplit(pool, requestPool)
	clock := config.Now
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}
	limits := config.Billing.Enabled()
	api := &API{config: config, now: clock, limits: limits, exports: newExportLimiter(config.MaxConcurrentExports), exportLinks: newExportLinks(), webForms: newWebFormLimiter(webFormMaxPerWorkspace, webFormMaxKeys), webFormClients: newWebFormLimiter(webFormMaxPerAddress, webFormMaxKeys),
		domainChecks:    newWindowLimiter(tokenDomainChecksPerHour, webFormMaxKeys, time.Hour),
		providerChecks:  newWindowLimiter(1, webFormMaxKeys, domain.ProviderCheckMinInterval),
		problemReports:  newWindowLimiter(problemReportsPerHour, webFormMaxKeys, time.Hour),
		helpCenterPages: newWebFormLimiter(helpCenterPagesPerAddress, webFormMaxKeys), helpCenterSearches: newWebFormLimiter(helpCenterSearchesPerAddress, webFormMaxKeys),
		auth:  auth.NewWithDB(db, config.Origin).WithClock(clock).WithPlanLimits(limits).WithPlansOnSale(config.Billing.PlansOnSale()),
		store: store.NewWithDB(db).WithObjects(config.Attachments).WithOutbound(outbound).WithClock(clock).WithPlanLimits(limits).WithPlansOnSale(config.Billing.PlansOnSale())}
	api.auth = api.auth.WithSignupKey(config.SignupHashKey)
	importKeys := config.ImportKeys
	if importKeys == nil {
		importKeys = importjobs.NewKeys(config.Google.Keys)
	}
	api.imports = importjobs.NewService(db, importKeys)
	api.imports.SetClock(clock)
	aiConfig := config.AI
	if !aiConfig.Keys.Configured() {
		aiConfig.Keys = config.Google.Keys
	}
	aiConfig.PlanLimits = limits
	api.ai = ai.NewService(db, aiConfig)
	api.ai.SetClock(clock)
	api.mailImageSigner = mailimage.NewSigner(api.mailImageKey())
	api.mailImageFetcher = config.MailImages
	if api.mailImageFetcher == nil {
		api.mailImageFetcher = mailimage.NewFetcher(mailimage.Options{})
	}
	api.mailImageLimits = newMailImageLimiter(config.MailImageConcurrency, config.MailImageWorkspaceConcurrency, config.MailImageQueueWait)
	if config.Email.ResendAPIKey != "" {
		api.resend = &resend.Client{APIKey: config.Email.ResendAPIKey, Base: config.Email.ResendAPIBase}
	}
	if config.Random != nil {
		api.auth = api.auth.WithRandom(config.Random)
	}
	if config.CloudAuth.Enabled() {
		api.cloud = cloudauth.New(config.CloudAuth)
	}
	if config.Billing.Enabled() {
		api.polar = polar.NewClient(config.Billing)
	}
	api.helpCenter = api.newHelpCenter()
	mux := http.NewServeMux()
	api.mux = mux
	for _, route := range api.routeSpecs() {
		if billingRoute(route.Route) && api.polar == nil {
			continue
		}
		if route.Pattern == resendWebhookPath && config.Email.ResendWebhookSecret == "" {
			continue
		}
		mux.Handle(route.Method+" "+route.Pattern, api.authorize(route.Route, route.handler))
	}
	// The CORS preflight of the public web form is sent by the browser, never by an agent.
	mux.HandleFunc("OPTIONS "+webFormSubmitPattern, api.preflightWebForm)
	return &Server{Handler: withRequestID(mux), api: api}
}

func Routes() []Route {
	return []Route{
		{http.MethodGet, "/api/v1/config", auth.PermissionPublic},
		{http.MethodPost, "/api/v1/auth/cloud/exchange", auth.PermissionPublic},
		{http.MethodPost, cloudSignupPath, auth.PermissionPublic},
		{http.MethodPost, "/api/v1/auth/redeem", auth.PermissionPublic},
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
		{http.MethodPatch, "/api/v1/ai/providers/{id}", auth.PermissionAccessWrite},
		{http.MethodPut, "/api/v1/ai/providers/{id}/key", auth.PermissionAccessWrite},
		{http.MethodPost, "/api/v1/ai/providers/{id}/check", auth.PermissionAccessWrite},
		{http.MethodPut, "/api/v1/ai/slots/{slot}", auth.PermissionAccessWrite},
		{http.MethodDelete, "/api/v1/ai/providers/{id}", auth.PermissionAccessWrite},
		{http.MethodGet, "/api/v1/ai/usage", auth.PermissionBillingManage},
		{http.MethodPut, "/api/v1/ai/automatic-limits", auth.PermissionAccessWrite},
		{http.MethodPost, "/api/v1/conversations/{id}/ai-drafts", auth.PermissionConversationsWrite},
		{http.MethodGet, "/api/v1/conversations/{id}/ai-drafts", auth.PermissionConversationsRead},
		{http.MethodGet, "/api/v1/conversations/{id}/ai-drafts/{draftId}", auth.PermissionConversationsRead},
		{http.MethodPost, "/api/v1/conversations/{id}/ai-drafts/{draftId}/discard", auth.PermissionConversationsWrite},
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
		if viaCookie && writes(request.Method) && request.Header.Get("Origin") != a.config.Origin {
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
