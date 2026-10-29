package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/helpcenter"
)

const (
	helpCenterTLSAskPath = "/api/v1/public/help-center/tls-ask"
	// helpCenterPagesPerAddress is how many help center pages one client address may load per minute,
	// across every workspace; a reader never comes near it, a crawler slows down.
	helpCenterPagesPerAddress = 600
	// helpCenterSearchesPerAddress is how many help center searches one client address may run per
	// minute, across every workspace. A search reads every article of the workspace.
	helpCenterSearchesPerAddress = 30
	// helpCenterHostLookupsPerAddress is how many host names the server does not know one client
	// address may make it look up per minute; each is a privileged query, and a random Host header
	// is free to send.
	helpCenterHostLookupsPerAddress = 10
)

func (a *API) newHelpCenter() *helpcenter.Site {
	return helpcenter.New(helpcenter.Config{Store: a.store, Objects: a.config.Attachments, Origin: a.config.Origin,
		Domain: a.config.HelpCenterDomain, Version: a.config.Version, Allow: a.allowHelpCenter, AllowLookup: a.allowHelpCenterHostLookup,
		Client:        func(request *http.Request) string { return clientAddress(request, a.config.TrustedProxies) },
		ChannelRoutes: a.channelHelpCenterRoutes(), Additions: a.channelHelpCenterAdditions()})
}

// allowHelpCenter charges a help center request to its client address: every page to one limiter, and
// a search to a second, smaller one as well.
func (a *API) allowHelpCenter(request *http.Request, search bool) (bool, time.Duration) {
	client, now := clientAddress(request, a.config.TrustedProxies), a.now()
	if allowed, wait := a.helpCenterPages.allow(client, now); !allowed {
		return false, wait
	}
	if search {
		return a.helpCenterSearches.allow(client, now)
	}
	return true, 0
}

// allowHelpCenterHostLookup charges a lookup of a host name the server does not know to the request's
// client address, before the privileged query runs.
func (a *API) allowHelpCenterHostLookup(request *http.Request) (bool, time.Duration) {
	return a.helpCenterHostLookups.allow(clientAddress(request, a.config.TrustedProxies), a.now())
}

// helpCenterHome is the public address of the requesting workspace's help center, its custom domain
// while that is active and the workspace is paid (helpcenter.SiteURL); on a failure it answers and
// reports false.
func (a *API) helpCenterHome(response http.ResponseWriter, request *http.Request) (string, bool) {
	who := principal(request)
	if awaitingPayment(who.Access) {
		return helpcenter.SiteURL(a.config.Origin, a.config.HelpCenterDomain, who.Workspace.Slug, ""), true
	}
	home, err := a.helpCenter.Address(request.Context(), who.Workspace.ID, who.Workspace.Slug)
	if err != nil {
		writeStoreError(response, err)
		return "", false
	}
	return home, true
}

// WrapHelpCenter serves the public help centers in front of next (the web app and the API).
func (s *Server) WrapHelpCenter(next http.Handler) http.Handler { return s.api.helpCenter.Wrap(next) }

// askHelpCenterCertificate answers the reverse proxy's on-demand TLS question: 200 only for a host
// name the server serves a help center on that is open (on, workspace not locked): <slug>.<HELP_CENTER_DOMAIN>,
// a claim's CNAME target hc-<claim>.<HELP_CENTER_DOMAIN> (which redirects), an active custom domain, or a
// pending one whose DNS check passes now. Every other case is the same 404, so the proxy never gets a
// certificate for a name nobody serves, and the answer tells nothing about why. A claim target or custom
// domain is looked up through helpcenter.Site.CertifyHost: names nobody claims are remembered, and a
// client may make the server look up only helpCenterHostLookupsPerAddress unknown names a minute.
func (a *API) askHelpCenterCertificate(response http.ResponseWriter, request *http.Request) {
	name := request.URL.Query().Get("domain")
	if len(name) > 253 {
		writeError(response, http.StatusNotFound, "not_found", "The requested resource was not found.")
		return
	}
	var open bool
	var err error
	if slug, ok := helpcenter.DomainSlug(name, a.config.HelpCenterDomain); ok {
		_, open, err = a.store.OpenHelpCenter(request.Context(), slug)
	} else if claim, ok := helpcenter.DomainClaim(name, a.config.HelpCenterDomain); ok {
		open, err = a.helpCenter.CertifyHost(request, helpcenter.ClaimHost(a.config.HelpCenterDomain, claim),
			func(ctx context.Context) (bool, bool, error) { return a.certifyClaimHost(ctx, claim) })
	} else if host, invalid := helpcenter.CustomHost(name, a.config.Origin, a.config.HelpCenterDomain); invalid == nil {
		open, err = a.helpCenter.CertifyHost(request, host,
			func(ctx context.Context) (bool, bool, error) { return a.certifyCustomHost(ctx, host) })
	}
	if err != nil {
		writeInternalError(response, err)
		return
	}
	if !open {
		writeError(response, http.StatusNotFound, "not_found", "The requested resource was not found.")
		return
	}
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(http.StatusOK)
}

// HelpCenterRoutes lists the channel types' routes below every help center, "METHOD path", for the
// gate that holds them to the contract.
func (s *Server) HelpCenterRoutes() []string {
	var list []string
	for _, route := range s.api.channelHelpCenterRoutes() {
		list = append(list, route.Method+" "+route.Path)
	}
	return list
}
