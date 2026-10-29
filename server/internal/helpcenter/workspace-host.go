package helpcenter

import (
	"net/http"
	"net/url"
	"strings"
)

// workspaceHostBase is where the help center lives on a workspace's own host name <slug>.<domain>; the
// root of that host is the way into the workspace's desk.
const workspaceHostBase = "/help"

// serveWorkspaceHost answers a request on a workspace's own host name. Its root leads to the desk's
// sign-in for the workspace; the help center answers under /help; robots.txt stays at the root, where
// crawlers look for it; an old Freshdesk address leads to its page; any other path is an address from
// before the help center moved under /help (an article, a collection, a search) and moves to the same
// path there.
func (s *Site) serveWorkspaceHost(response http.ResponseWriter, request *http.Request, slug string) {
	path := request.URL.Path
	switch {
	case path == "/" || path == "":
		s.deskEntry(response, request, slug)
	case path == workspaceHostBase || strings.HasPrefix(path, workspaceHostBase+"/"):
		s.serve(response, request, address{slug: slug, workspaceHost: true, base: workspaceHostBase, sub: strings.TrimPrefix(path, workspaceHostBase)})
	case path == "/robots.txt":
		s.serve(response, request, address{slug: slug, workspaceHost: true, atRoot: true, base: workspaceHostBase, sub: path})
	case isLegacyPath(path):
		// An old Freshdesk address goes straight to its page under /help, in one step.
		s.serve(response, request, address{slug: slug, workspaceHost: true, base: workspaceHostBase, sub: path})
	default:
		// Only the path and query of the request are kept, so the target stays on this host.
		target := workspaceHostBase + request.URL.EscapedPath()
		if request.URL.RawQuery != "" {
			target += "?" + request.URL.RawQuery
		}
		securityHeaders(response.Header())
		response.Header().Set("Cache-Control", "no-store")
		http.Redirect(response, request, target, http.StatusMovedPermanently)
	}
}

// deskEntry sends a visitor of a workspace host's root to the app's sign-in page for that workspace,
// which opens the inbox when the browser is already signed in to it. A slug no workspace has is the
// help center's 404, as before.
func (s *Site) deskEntry(response http.ResponseWriter, request *http.Request, slug string) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		response.Header().Set("Allow", "GET, HEAD")
		securityHeaders(response.Header())
		response.Header().Set("Cache-Control", "no-store")
		http.Error(response, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.config.Allow != nil {
		if allowed, wait := s.config.Allow(request, false); !allowed {
			s.writeBusy(response, wait, workspaceHostBase)
			return
		}
	}
	_, found, err := s.config.Store.WorkspaceForMailSlug(request.Context(), slug)
	if err != nil {
		s.writeError(response, err)
		return
	}
	if !found {
		s.writeNotFound(response, false)
		return
	}
	securityHeaders(response.Header())
	response.Header().Set("Cache-Control", "no-store")
	http.Redirect(response, request, s.config.Origin+"/sign-in?"+url.Values{"workspace": {slug}}.Encode(), http.StatusFound)
}

func isLegacyPath(path string) bool {
	_, ok := ParseLegacyPath(path)
	return ok
}
