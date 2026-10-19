package httpapi

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/cloudauth"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/email"
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
	// Version is the build stamped into the binary, reported by GET /api/v1/config; empty reports "dev".
	Version     string
	CloudAuth   cloudauth.Config
	Email       email.Config
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
	// Random is the entropy for generated workspace slug suffixes; nil uses crypto/rand.
	Random io.Reader
	// Funnel receives the cloud's trial, activation (email channel, reply, teammate) and payment
	// events; nil records nothing.
	Funnel FunnelRecorder
	// ProductAnalytics is the PostHog project the web app reports to; nil (telemetry turned off
	// with DISABLE_TELEMETRY or DO_NOT_TRACK, or a development build) keeps it from loading.
	ProductAnalytics *ProductAnalytics
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
	// domainChecks limits how often agent tokens make the server look up DNS for one workspace.
	domainChecks *webFormLimiter
	// providerChecks limits the domain checks of one workspace that reach the email provider.
	providerChecks *webFormLimiter
	polar          *polar.Client
	// resend verifies sending domains through the provider; nil when RESEND_API_KEY is not set.
	resend   *resend.Client
	founding foundingState
	now      func() time.Time
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

// RefreshFounding counts the Founding slots at Polar and closes Founding when they are used up. It
// does nothing when billing is off.
func (s *Server) RefreshFounding(ctx context.Context) error { return s.api.refreshFounding(ctx) }

func New(pool *pgxpool.Pool, config Config) *Server {
	config.Origin = strings.TrimRight(config.Origin, "/")
	if config.LookupTXT == nil {
		config.LookupTXT = net.DefaultResolver.LookupTXT
	}
	outbound := store.OutboundPolicy{
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
		domainChecks:   newWindowLimiter(tokenDomainChecksPerHour, webFormMaxKeys, time.Hour),
		providerChecks: newWindowLimiter(1, webFormMaxKeys, domain.ProviderCheckMinInterval),
		auth:           auth.NewWithDB(db, config.Origin).WithClock(clock).WithPlanLimits(limits).WithTeam25Offered(config.Billing.SellsTeam25()),
		store:          store.NewWithDB(db).WithObjects(config.Attachments).WithOutbound(outbound).WithClock(clock).WithPlanLimits(limits).WithTeam25Offered(config.Billing.SellsTeam25())}
	api.auth = api.auth.WithSignupKey(config.SignupHashKey)
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
	return &Server{Handler: mux, api: api}
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
		{http.MethodGet, "/api/v1/inbox/activity", auth.PermissionConversationsRead},
		{http.MethodGet, "/api/v1/attachments/{id}", auth.PermissionConversationsRead},
		{http.MethodGet, mailimage.ProxyPath, auth.PermissionConversationsRead},
		{http.MethodGet, "/api/v1/contacts", auth.PermissionContactsRead},
		{http.MethodPost, "/api/v1/contacts", auth.PermissionContactsWrite},
		{http.MethodGet, "/api/v1/contacts/{id}", auth.PermissionContactsRead},
		{http.MethodGet, "/api/v1/agents", auth.PermissionAccessRead},
		{http.MethodPatch, "/api/v1/workspace/timezone", auth.PermissionAccessWrite},
		{http.MethodPatch, "/api/v1/workspace/slug", auth.PermissionAccessWrite},
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
		{http.MethodPost, "/api/v1/conversations/{id}/messages/{messageId}/retry", auth.PermissionConversationsWrite},
		{http.MethodGet, "/api/v1/channels/email", auth.PermissionAccessRead},
		{http.MethodPut, "/api/v1/channels/email", auth.PermissionAccessWrite},
		{http.MethodDelete, "/api/v1/channels/email", auth.PermissionAccessWrite},
		{http.MethodPost, "/api/v1/channels/email/domain-verification", auth.PermissionAccessWrite},
		{http.MethodPost, "/api/v1/channels/email/inbound", auth.PermissionChannelInbound},
		{http.MethodPost, "/api/v1/channels/email/inbound/recipient", auth.PermissionChannelInbound},
		{http.MethodPost, "/api/v1/channels/email/secret", auth.PermissionAccessWrite},
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
		if a.refuseIfLocked(response, route, principal) {
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
