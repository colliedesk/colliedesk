package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/workspacehost"
)

// workspaceOrigin is where the workspace's desk runs: its own host name with HELP_CENTER_DOMAIN, else
// APP_ORIGIN. Every link the server makes for a workspace's agents starts here.
func (a *API) workspaceOrigin(slug string) string {
	return workspacehost.Origin(a.config.Origin, a.config.HelpCenterDomain, slug)
}

// requestOrigin is the origin a browser request on this host must come from: the workspace host's own
// origin on a workspace host, APP_ORIGIN everywhere else. A page of one workspace's host can therefore
// never write with the cookie of another host.
func (a *API) requestOrigin(request *http.Request) string {
	if slug, ok := workspacehost.Slug(request.Context()); ok {
		return a.workspaceOrigin(slug)
	}
	return a.config.Origin
}

// handoffURL is where a browser signed in on APP_ORIGIN as who continues: its workspace's own host's
// sign-in page with next (the page to open) and a one-time token in the fragment, which no server
// receives or logs. "" when the request is already on a workspace host, the server has no workspace
// hosts, or the workspace's slug cannot be a host name: the browser stays.
func (a *API) handoffURL(ctx context.Context, who auth.Principal, next string) (string, error) {
	if _, onWorkspaceHost := workspacehost.Slug(ctx); onWorkspaceHost {
		return "", nil
	}
	return a.handoffTo(ctx, who, next)
}

// handoffTo is handoffURL from any host: also from a workspace's old host after its slug changed.
func (a *API) handoffTo(ctx context.Context, who auth.Principal, next string) (string, error) {
	origin := a.workspaceOrigin(who.Workspace.Slug)
	if origin == a.config.Origin || who.IsToken() {
		return "", nil
	}
	token, err := a.auth.Handoff(ctx, who)
	if err != nil {
		return "", err
	}
	address := origin + "/sign-in"
	if path := localPath(next); path != "" && path != "/" {
		address += "?" + url.Values{"next": {path}}.Encode()
	}
	return address + "#handoff=" + token, nil
}

// setContinueURL sets result.ContinueURL for a browser that just signed in on APP_ORIGIN; it answers
// the error and reports false when the hand-off could not be made.
func (a *API) setContinueURL(response http.ResponseWriter, request *http.Request, result *auth.Redeemed, next string) bool {
	address, err := a.handoffURL(request.Context(), auth.Principal{Workspace: result.Workspace, Agent: result.Agent}, next)
	if err != nil {
		writeInternalError(response, err)
		return false
	}
	if address != "" {
		result.ContinueURL = &address
	}
	return true
}

// handOffAppPages sends a page load of the web app on APP_ORIGIN that carries a session of a workspace
// with its own host to the same page there, signed in through a one-time hand-off: bookmarks of
// APP_ORIGIN keep working after each workspace moved to its own host. The API, MCP, assets, the help
// center, the sign-in page and the results share page stay on APP_ORIGIN.
func (a *API) handOffAppPages(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if a.config.HelpCenterDomain == "" || !appPageLoad(request) {
			next.ServeHTTP(response, request)
			return
		}
		cookie, err := request.Cookie(sessionCookie)
		if err != nil || cookie.Value == "" {
			next.ServeHTTP(response, request)
			return
		}
		who, err := a.auth.Authenticate(request.Context(), cookie.Value, true)
		if err != nil {
			next.ServeHTTP(response, request)
			return
		}
		address, err := a.handoffURL(request.Context(), who, request.URL.RequestURI())
		if err != nil {
			slog.Error("hand off to the workspace host", "workspace_id", who.Workspace.ID, "error", err)
		}
		if address == "" {
			next.ServeHTTP(response, request)
			return
		}
		response.Header().Set("Cache-Control", "no-store")
		response.Header().Set("Referrer-Policy", "no-referrer")
		http.Redirect(response, request, address, http.StatusFound)
	})
}

// appPageLoad reports whether request loads a page of the web app on APP_ORIGIN.
func appPageLoad(request *http.Request) bool {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		return false
	}
	if _, onWorkspaceHost := workspacehost.Slug(request.Context()); onWorkspaceHost {
		return false
	}
	path := request.URL.Path
	for _, prefix := range []string{"/api/", "/assets/", "/help/"} {
		if strings.HasPrefix(path, prefix) {
			return false
		}
	}
	return path != "/mcp" && path != "/help" && path != "/sign-in" && path != "/r" && !strings.Contains(path[strings.LastIndex(path, "/")+1:], ".")
}

// localPath is path when it is a path of this site ("/settings?tab=x"), else "": never an address that
// a browser would read as another host ("//evil.example", "/\evil.example", "https://…").
func localPath(path string) string {
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || strings.ContainsAny(path, "\\\r\n") || len(path) > 2048 {
		return ""
	}
	return path
}

// toWorkspaceHost sends a provider's callback that arrived on APP_ORIGIN to the same path and query on
// the host of the workspace that started it, found by the stored state. The callback address registered
// with a provider stays on APP_ORIGIN, but the session of the person who started it lives on the
// workspace host; there, the callback itself checks that session, spends the state and finishes. It
// reports false when the callback should finish here: on a workspace host already, without workspace
// hosts, or for a state no workspace host owns (unknown, spent or expired: the callback refuses it).
func (a *API) toWorkspaceHost(response http.ResponseWriter, request *http.Request, slugOf func(ctx context.Context) (string, error)) bool {
	if _, onWorkspaceHost := workspacehost.Slug(request.Context()); onWorkspaceHost || a.config.HelpCenterDomain == "" {
		return false
	}
	slug, err := slugOf(request.Context())
	if err != nil {
		slog.Error("find the workspace of a callback", "path", request.URL.Path, "error", err)
		return false
	}
	origin := a.workspaceOrigin(slug)
	if slug == "" || origin == a.config.Origin {
		return false
	}
	target := origin + request.URL.EscapedPath()
	if request.URL.RawQuery != "" {
		target += "?" + request.URL.RawQuery
	}
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("Referrer-Policy", "no-referrer")
	http.Redirect(response, request, target, http.StatusSeeOther)
	return true
}
