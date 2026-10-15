package httpapi

import (
	"net/http"
	"sort"

	"github.com/colliedesk/colliedesk/server/internal/auth"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// lockedAllowed are the only routes a locked workspace (trial over, nothing paid) can use: sign in
// and out, pay, take its data out, and cut its mail channel. Every other authenticated route,
// reads included, answers 402 workspace_locked, so a route added later is locked by default. The
// keys are "METHOD pattern" exactly as listed in Routes().
var lockedAllowed = map[string]bool{
	"GET /api/v1/auth/me":                true,
	"POST /api/v1/auth/logout":           true,
	"GET /api/v1/auth/sessions":          true,
	"DELETE /api/v1/auth/sessions/{id}":  true,
	"GET /api/v1/billing":                true,
	"POST /api/v1/billing/checkout":      true,
	"POST /api/v1/billing/portal":        true,
	"GET /api/v1/export":                 true,
	"DELETE /api/v1/channels/email":      true,
	"POST /api/v1/channels/email/secret": true,
}

// LockedAllowlist lists the allowlist as "METHOD pattern" strings, sorted.
func LockedAllowlist() []string {
	keys := make([]string, 0, len(lockedAllowed))
	for key := range lockedAllowed {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// refuseIfLocked answers 402 workspace_locked and reports true when the workspace is locked and the
// route is not on the allowlist. During the trial it costs nothing: the trial end came with the
// session. Only a request after the trial end reads the subscriptions.
func (a *API) refuseIfLocked(response http.ResponseWriter, request *http.Request, route Route, who auth.Principal) bool {
	if !a.limits || lockedAllowed[route.Method+" "+route.Pattern] {
		return false
	}
	now := a.now()
	if now.Before(who.TrialEndsAt) {
		return false
	}
	access, err := a.store.WorkspaceAccess(request.Context(), who.Workspace.ID, who.TrialEndsAt, now)
	if err != nil {
		writeInternalError(response, err)
		return true
	}
	if !access.Locked {
		return false
	}
	writeLocked(response)
	return true
}

func writeLocked(response http.ResponseWriter) {
	writeError(response, http.StatusPaymentRequired, "workspace_locked", (&domain.LockedError{}).Error())
}
