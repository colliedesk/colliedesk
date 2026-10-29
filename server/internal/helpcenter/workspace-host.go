package helpcenter

import (
	"net/http"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/workspacehost"
)

// workspaceHostBase is where the help center lives on a workspace's own host name <slug>.<domain>; the
// rest of that host runs the workspace's desk.
const workspaceHostBase = "/help"

// movedHelpPaths are the help center's paths from before it moved under /help on a workspace's host;
// none of them is a page of the app.
var movedHelpPaths = []string{"/a/", "/c/", "/images/", "/media/", stylePath}

// serveWorkspaceHost answers a request on a workspace's own host name. The help center answers under
// /help; robots.txt stays at the root, where crawlers look for it; an old Freshdesk address leads to its
// page; the help center's other addresses from before the move go to the same path under /help.
// Everything else is the desk: the app, its API and MCP, marked with the host's workspace so that only
// that workspace's sessions and tokens act there. A page of the app for a slug no workspace has is the
// help center's 404.
func (s *Site) serveWorkspaceHost(response http.ResponseWriter, request *http.Request, slug string, next http.Handler) {
	path := request.URL.Path
	switch {
	case path == workspaceHostBase || strings.HasPrefix(path, workspaceHostBase+"/"):
		s.serve(response, request, address{slug: slug, workspaceHost: true, base: workspaceHostBase, sub: strings.TrimPrefix(path, workspaceHostBase)})
	case path == "/robots.txt":
		s.serve(response, request, address{slug: slug, workspaceHost: true, atRoot: true, base: workspaceHostBase, sub: path})
	case isLegacyPath(path):
		// An old Freshdesk address goes straight to its page under /help, in one step.
		s.serve(response, request, address{slug: slug, workspaceHost: true, base: workspaceHostBase, sub: path})
	case movedHelpPath(path):
		// Only the path and query of the request are kept, so the target stays on this host.
		target := workspaceHostBase + request.URL.EscapedPath()
		if request.URL.RawQuery != "" {
			target += "?" + request.URL.RawQuery
		}
		securityHeaders(response.Header())
		response.Header().Set("Cache-Control", "no-store")
		http.Redirect(response, request, target, http.StatusMovedPermanently)
	default:
		if appPage(path) {
			_, found, err := s.config.Store.WorkspaceForMailSlug(request.Context(), slug)
			if err != nil {
				s.writeError(response, err)
				return
			}
			if !found {
				s.writeNotFound(response, false)
				return
			}
		}
		next.ServeHTTP(response, request.WithContext(workspacehost.With(request.Context(), slug)))
	}
}

func movedHelpPath(path string) bool {
	if path == "/search" || path == "/sitemap.xml" || path == "/llms.txt" {
		return true
	}
	for _, prefix := range movedHelpPaths {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// appPage reports whether path is a page of the web app rather than its API, MCP or a built asset: the
// API and MCP refuse every session and token of another workspace themselves, and assets are the same
// for every host.
func appPage(path string) bool {
	return !strings.HasPrefix(path, "/api/") && path != "/mcp" && !strings.HasPrefix(path, "/assets/")
}

func isLegacyPath(path string) bool {
	_, ok := ParseLegacyPath(path)
	return ok
}
