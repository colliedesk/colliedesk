package httpapi

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"sync"
	"time"
)

const (
	exportLinkTTL  = 60 * time.Second
	exportLinkPath = "/api/v1/export/download/"
)

// exportLinks holds the one-time download links of workspace exports. A browser cannot show an error
// page for a download it streams to disk, so the page first asks for a link (where the 409 and 429
// answers can still be shown), then navigates to it. A link works once, for the workspace that asked,
// for a short time, and a new link of a workspace replaces its previous one. Links live in this
// process: a restart invalidates them, which at most costs a second click.
type exportLinks struct {
	mu     sync.Mutex
	grants map[string]exportGrant // by SHA-256 of the token, so memory holds no usable token
}

type exportGrant struct {
	workspaceID, slug string
	expires           time.Time
}

func newExportLinks() *exportLinks { return &exportLinks{grants: map[string]exportGrant{}} }

func tokenKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// issue makes a link for the workspace and forgets its older one and every expired one.
func (l *exportLinks) issue(workspaceID, slug string, now time.Time) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	l.mu.Lock()
	defer l.mu.Unlock()
	for key, grant := range l.grants {
		if grant.workspaceID == workspaceID || !now.Before(grant.expires) {
			delete(l.grants, key)
		}
	}
	l.grants[tokenKey(token)] = exportGrant{workspaceID: workspaceID, slug: slug, expires: now.Add(exportLinkTTL)}
	return token, nil
}

// peek returns the grant a token would redeem, without spending it.
func (l *exportLinks) peek(token string, now time.Time) (exportGrant, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	grant, found := l.grants[tokenKey(token)]
	return grant, found && now.Before(grant.expires)
}

// redeem spends the token: it answers once, and only before it expires.
func (l *exportLinks) redeem(token string, now time.Time) (exportGrant, bool) {
	key := tokenKey(token)
	l.mu.Lock()
	defer l.mu.Unlock()
	grant, found := l.grants[key]
	delete(l.grants, key)
	if !found || !now.Before(grant.expires) {
		return exportGrant{}, false
	}
	return grant, true
}

// createExportLink answers what the export route would answer before it streams, then returns the link.
func (a *API) createExportLink(response http.ResponseWriter, request *http.Request) {
	workspace := principal(request).Workspace
	if !a.refuseExportIfBusy(response, a.exports.peek(workspace.ID)) {
		return
	}
	token, err := a.exportLinks.issue(workspace.ID, workspace.Slug, a.now())
	if err != nil {
		writeInternalError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, map[string]any{"url": exportLinkPath + token, "expires_in_seconds": int(exportLinkTTL / time.Second)})
}

// exportErrorPath is where a refused download sends the browser: the app, which says why. A download
// is a navigation, so a JSON error body would replace the whole app with raw text. The reason is a
// short code the settings page and the locked screen both understand.
func exportErrorPath(reason string) string { return "/settings?export_error=" + reason }

func redirectExportError(response http.ResponseWriter, request *http.Request, reason string) {
	http.Redirect(response, request, exportErrorPath(reason), http.StatusSeeOther)
}

// downloadExport streams the export a link was made for. The token is the credential. Order matters:
// the slot is taken before the token is spent, so a busy server costs the person a retry, not the link.
func (a *API) downloadExport(response http.ResponseWriter, request *http.Request) {
	// HEAD would match this route and spend the link on a request that has no body to receive it.
	if request.Method != http.MethodGet {
		response.Header().Set("Allow", http.MethodGet)
		writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "Open this link with GET.")
		return
	}
	token := request.PathValue("token")
	peeked, ok := a.exportLinks.peek(token, a.now())
	if !ok {
		redirectExportError(response, request, "link_invalid")
		return
	}
	release, admission := a.exports.acquire(peeked.workspaceID)
	switch admission {
	case exportWorkspaceBusy:
		redirectExportError(response, request, "busy")
		return
	case exportServerBusy:
		redirectExportError(response, request, "server_busy")
		return
	}
	defer release()
	grant, ok := a.exportLinks.redeem(token, a.now())
	if !ok {
		// Another request spent the link between the check and now.
		redirectExportError(response, request, "link_invalid")
		return
	}
	a.streamExport(response, request, grant.workspaceID, grant.slug, func(response http.ResponseWriter, status int, _, _ string) {
		if status == http.StatusConflict {
			redirectExportError(response, request, "busy")
			return
		}
		redirectExportError(response, request, "failed")
	})
}
