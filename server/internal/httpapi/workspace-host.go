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

// finishBrowserSignIn ends a browser's sign-in as a cookie session. On APP_ORIGIN, for a workspace with
// its own host, the browser continues there (result.ContinueURL) and APP_ORIGIN keeps no session: the
// one just made is revoked, and only the one-time hand-off crosses over, so signing out on the workspace
// host is the end of it. APP_ORIGIN keeps only the workspace's slug, which sends its bookmarks on.
// Everywhere else the session becomes this host's cookie. It answers the error and reports false when
// the sign-in could not be finished.
func (a *API) finishBrowserSignIn(response http.ResponseWriter, request *http.Request, result *auth.Redeemed, next string) bool {
	address, err := a.handoffURL(request.Context(), auth.Principal{Workspace: result.Workspace, Agent: result.Agent}, next)
	if err != nil {
		writeInternalError(response, err)
		return false
	}
	if address == "" {
		setSessionCookie(response, result.Token)
		result.Token = ""
		return true
	}
	if err := a.auth.EndSession(request.Context(), result.Workspace.ID, result.Token); err != nil {
		writeInternalError(response, err)
		return false
	}
	result.Token = ""
	result.ContinueURL = &address
	clearSessionCookie(response)
	setWorkspaceHint(response, result.Workspace.Slug)
	return true
}

// handOffAppPages sends a page load of the web app on APP_ORIGIN to the same page on the workspace's own
// host, so bookmarks of APP_ORIGIN keep working after each workspace moved to its own host. A session
// on APP_ORIGIN from before the move signs in there through a one-time hand-off and is revoked, its
// cookie cleared, in the same response: it hands off once. Without one, the workspace hint left by a
// sign-in sends the browser on without any credential; the workspace host asks for a sign-in when its
// own session has ended. The API, MCP, assets, the help center, the sign-in page and the results share
// page stay on APP_ORIGIN.
func (a *API) handOffAppPages(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if a.config.HelpCenterDomain == "" || !appPageLoad(request) {
			next.ServeHTTP(response, request)
			return
		}
		address := a.handOffAppSession(response, request)
		if address == "" {
			address = a.workspaceHintAddress(request)
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

// handOffAppSession is the hand-off address for the APP_ORIGIN session the request carries, once that
// session is revoked and its cookie cleared, or "" when it carries none to hand off.
func (a *API) handOffAppSession(response http.ResponseWriter, request *http.Request) string {
	cookie, err := request.Cookie(sessionCookie)
	if err != nil || cookie.Value == "" {
		return ""
	}
	who, err := a.auth.Authenticate(request.Context(), cookie.Value, true)
	if err != nil {
		return ""
	}
	address, err := a.handoffURL(request.Context(), who, request.URL.RequestURI())
	if err != nil {
		slog.Error("hand off to the workspace host", "workspace_id", who.Workspace.ID, "error", err)
		return ""
	}
	if address == "" {
		return ""
	}
	// A session that cannot be revoked is not handed off: it would sign the browser in again later.
	if err := a.auth.EndSession(request.Context(), who.Workspace.ID, cookie.Value); err != nil {
		slog.Error("end the handed-off session", "workspace_id", who.Workspace.ID, "error", err)
		return ""
	}
	clearSessionCookie(response)
	setWorkspaceHint(response, who.Workspace.Slug)
	return address
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
// a browser would read as another host ("//evil.example", "/\evil.example", "https://…"). A browser
// drops tabs and line breaks from an address, so "/\t/evil.example" is "//evil.example": every control
// character is refused.
func localPath(path string) string {
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || strings.Contains(path, "\\") || len(path) > 2048 {
		return ""
	}
	if strings.ContainsFunc(path, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
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
