package httpapi

import (
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
)

func (a *API) newHelpCenter() *helpcenter.Site {
	return helpcenter.New(helpcenter.Config{Store: a.store, Objects: a.config.Attachments, Origin: a.config.Origin,
		Domain: a.config.HelpCenterDomain, Version: a.config.Version, Allow: a.allowHelpCenter})
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

func (a *API) helpCenterURL(slug string) string {
	return helpcenter.URL(a.config.Origin, a.config.HelpCenterDomain, slug)
}

// WrapHelpCenter serves the public help centers in front of next (the web app and the API).
func (s *Server) WrapHelpCenter(next http.Handler) http.Handler { return s.api.helpCenter.Wrap(next) }

// askHelpCenterCertificate answers the reverse proxy's on-demand TLS question: 200 only for a host
// name <slug>.<HELP_CENTER_DOMAIN> whose help center is open (on, workspace not locked); every other
// case is the same 404, so the proxy never gets a certificate for a name nobody serves.
func (a *API) askHelpCenterCertificate(response http.ResponseWriter, request *http.Request) {
	name := request.URL.Query().Get("domain")
	slug, ok := helpcenter.DomainSlug(name, a.config.HelpCenterDomain)
	if !ok || len(name) > 253 {
		writeError(response, http.StatusNotFound, "not_found", "The requested resource was not found.")
		return
	}
	_, open, err := a.store.OpenHelpCenter(request.Context(), slug)
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
