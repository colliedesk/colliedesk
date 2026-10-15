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

// downloadExport streams the export a link was made for. The token is the credential.
func (a *API) downloadExport(response http.ResponseWriter, request *http.Request) {
	grant, ok := a.exportLinks.redeem(request.PathValue("token"), a.now())
	if !ok {
		writeError(response, http.StatusNotFound, "not_found", "This download link is invalid, already used or expired. Start the export again.")
		return
	}
	a.streamExport(response, request, grant.workspaceID, grant.slug)
}
