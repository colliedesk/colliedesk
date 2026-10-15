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
	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/colliedesk/colliedesk/server/internal/polar"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

const sessionCookie = "colliedesk_session"

type Config struct {
	Origin      string
	CloudAuth   cloudauth.Config
	Email       email.Config
	Attachments attachments.Storage
	// RequestPool, when set, runs workspace-scoped queries as a separate request login
	// (DATABASE_REQUEST_URL); nil runs them on the owner pool.
	RequestPool *pgxpool.Pool
	// Billing turns on Polar billing for the hosted cloud; the zero value leaves it off.
	Billing polar.Config
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
	// client address of a sign-up (TRUSTED_PROXIES). Empty trusts none: the peer address is the client.
	TrustedProxies []netip.Prefix
	// Random is the entropy for generated workspace slug suffixes; nil uses crypto/rand.
	Random io.Reader
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
	auth   *auth.Service
	cloud  *cloudauth.Verifier
	store  *store.Store
	// exports admits workspace exports; see exportLimiter.
	exports *exportLimiter
	polar   *polar.Client
	now     func() time.Time
}

type routeSpec struct {
	Route
	handler http.HandlerFunc
}

type principalKey struct{}

func New(pool *pgxpool.Pool, config Config) http.Handler {
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
	api := &API{config: config, now: clock, limits: limits, exports: newExportLimiter(config.MaxConcurrentExports),
		auth:  auth.NewWithDB(db, config.Origin).WithClock(clock).WithPlanLimits(limits),
		store: store.NewWithDB(db).WithOutbound(outbound).WithClock(clock).WithPlanLimits(limits)}
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
	for _, route := range api.routeSpecs() {
		if billingRoute(route.Route) && api.polar == nil {
			continue
		}
		mux.Handle(route.Method+" "+route.Pattern, api.authorize(route.Route, route.handler))
	}
	return mux
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
		{http.MethodPost, "/api/v1/auth/invites", auth.PermissionAccessWrite},
		{http.MethodGet, "/api/v1/auth/invites", auth.PermissionAccessRead},
		{http.MethodDelete, "/api/v1/auth/invites/{id}", auth.PermissionAccessWrite},
		{http.MethodGet, "/api/v1/conversations", auth.PermissionConversationsRead},
		{http.MethodPost, "/api/v1/conversations", auth.PermissionConversationsWrite},
		{http.MethodGet, "/api/v1/conversations/{id}", auth.PermissionConversationsRead},
		{http.MethodPatch, "/api/v1/conversations/{id}", auth.PermissionConversationsWrite},
		{http.MethodPut, "/api/v1/conversations/{id}/tags/{tagId}", auth.PermissionConversationsWrite},
		{http.MethodDelete, "/api/v1/conversations/{id}/tags/{tagId}", auth.PermissionConversationsWrite},
		{http.MethodGet, "/api/v1/conversations/{id}/messages", auth.PermissionConversationsRead},
		{http.MethodPost, "/api/v1/conversations/{id}/messages", auth.PermissionConversationsWrite},
		{http.MethodGet, "/api/v1/conversations/{id}/time-entries", auth.PermissionConversationsRead},
		{http.MethodGet, "/api/v1/attachments/{id}", auth.PermissionConversationsRead},
		{http.MethodGet, "/api/v1/contacts", auth.PermissionContactsRead},
		{http.MethodPost, "/api/v1/contacts", auth.PermissionContactsWrite},
		{http.MethodGet, "/api/v1/contacts/{id}", auth.PermissionContactsRead},
		{http.MethodGet, "/api/v1/agents", auth.PermissionAccessRead},
		{http.MethodPatch, "/api/v1/workspace/timezone", auth.PermissionAccessWrite},
		{http.MethodPatch, "/api/v1/workspace/slug", auth.PermissionAccessWrite},
		{http.MethodPatch, "/api/v1/agents/me/timezone", auth.PermissionAuthenticated},
		{http.MethodGet, "/api/v1/tags", auth.PermissionConversationsRead},
		{http.MethodPost, "/api/v1/tags", auth.PermissionConversationsWrite},
		{http.MethodPatch, "/api/v1/tags/{id}", auth.PermissionConversationsWrite},
		{http.MethodDelete, "/api/v1/tags/{id}", auth.PermissionConversationsWrite},
		{http.MethodPost, "/api/v1/conversations/{id}/messages/{messageId}/retry", auth.PermissionConversationsWrite},
		{http.MethodGet, "/api/v1/channels/email", auth.PermissionAccessRead},
		{http.MethodPut, "/api/v1/channels/email", auth.PermissionAccessWrite},
		{http.MethodDelete, "/api/v1/channels/email", auth.PermissionAccessWrite},
		{http.MethodPost, "/api/v1/channels/email/domain-verification", auth.PermissionAccessWrite},
		{http.MethodPost, "/api/v1/channels/email/inbound", auth.PermissionChannelInbound},
		{http.MethodPost, "/api/v1/channels/email/secret", auth.PermissionAccessWrite},
		{http.MethodGet, "/api/v1/billing", auth.PermissionBillingManage},
		{http.MethodPost, "/api/v1/billing/checkout", auth.PermissionBillingManage},
		{http.MethodPost, "/api/v1/billing/portal", auth.PermissionBillingManage},
		{http.MethodPost, polarWebhookPath, auth.PermissionPublic},
		{http.MethodGet, "/api/v1/export", auth.PermissionWorkspaceExport},
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
		raw, viaCookie, ok := credential(request)
		if !ok {
			writeError(response, http.StatusUnauthorized, "unauthorized", "A valid session is required.")
			return
		}
		principal, err := a.auth.Authenticate(request.Context(), raw, viaCookie)
		if err != nil {
			writeError(response, http.StatusUnauthorized, "unauthorized", "A valid session is required.")
			return
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
