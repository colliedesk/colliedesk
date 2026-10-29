package httpapi

import (
	"net/http"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/workspacehost"
)

// workspaceHintCookie names, on APP_ORIGIN, the workspace whose own host a browser signed in to. It is
// no credential: it only sends a bookmark of APP_ORIGIN on to that host, which asks for a sign-in when
// its own session has ended.
const workspaceHintCookie = "colliedesk_workspace"

func setWorkspaceHint(response http.ResponseWriter, slug string) {
	http.SetCookie(response, &http.Cookie{
		Name: workspaceHintCookie, Value: slug, Path: "/", HttpOnly: true,
		Secure: true, SameSite: http.SameSiteLaxMode, MaxAge: int((30 * 24 * time.Hour).Seconds()),
	})
}

// workspaceHintAddress is the same page on the host of the workspace the request's hint names, or ""
// without a hint that names a workspace host.
func (a *API) workspaceHintAddress(request *http.Request) string {
	cookie, err := request.Cookie(workspaceHintCookie)
	if err != nil || workspacehost.Host(a.config.HelpCenterDomain, cookie.Value) == "" {
		return ""
	}
	origin := a.workspaceOrigin(cookie.Value)
	if origin == a.config.Origin {
		return ""
	}
	path := localPath(request.URL.RequestURI())
	if path == "" {
		path = "/"
	}
	return origin + path
}
