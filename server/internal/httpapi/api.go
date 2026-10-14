package httpapi

import (
	"context"
	"net"
	"net/http"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/cloudauth"
	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

const sessionCookie = "colliedesk_session"

type Config struct {
	Origin    string
	CloudAuth cloudauth.Config
	Email     email.Config
	// LookupTXT resolves DNS TXT records for domain verification; nil uses the system resolver.
	LookupTXT func(ctx context.Context, name string) ([]string, error)
}

type Route struct {
	Method     string
	Pattern    string
	Permission string
}

type API struct {
	config Config
	auth   *auth.Service
	cloud  *cloudauth.Verifier
	store  *store.Store
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
	api := &API{config: config, auth: auth.New(pool, config.Origin), store: store.New(pool).WithOutbound(outbound)}
	if config.CloudAuth.Enabled() {
		api.cloud = cloudauth.New(config.CloudAuth)
	}
	mux := http.NewServeMux()
	for _, route := range api.routeSpecs() {
		mux.Handle(route.Method+" "+route.Pattern, api.authorize(route.Permission, route.handler))
	}
	return mux
}

func Routes() []Route {
	return []Route{
		{http.MethodGet, "/api/v1/config", auth.PermissionPublic},
		{http.MethodPost, "/api/v1/auth/cloud/exchange", auth.PermissionPublic},
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
		{http.MethodGet, "/api/v1/contacts", auth.PermissionContactsRead},
		{http.MethodPost, "/api/v1/contacts", auth.PermissionContactsWrite},
		{http.MethodGet, "/api/v1/contacts/{id}", auth.PermissionContactsRead},
		{http.MethodGet, "/api/v1/agents", auth.PermissionAccessRead},
		{http.MethodPatch, "/api/v1/workspace/timezone", auth.PermissionAccessWrite},
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
	}
}

func (a *API) authorize(permission string, next http.HandlerFunc) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
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
			writeError(response, http.StatusForbidden, "csrf_rejected", "Origin does not match this CollieDesk instance.")
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
