package httpapi

import (
	"net/http"
	"sort"

	"github.com/colliedesk/colliedesk/server/internal/auth"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// lockedAllowed are the only routes a locked workspace (trial over, nothing paid) can use: sign in
// and out, pay, take its data out, cut its mail channel, and tell us about a problem. Every other authenticated route,
// reads included, answers 402 workspace_locked, so a route added later is locked by default. The
// keys are "METHOD pattern" exactly as listed in Routes().
var lockedAllowed = map[string]bool{
	"GET /api/v1/auth/me":                true,
	"POST /api/v1/auth/logout":           true,
	"GET /api/v1/auth/sessions":          true,
	"DELETE /api/v1/auth/sessions/{id}":  true,
	"GET /api/v1/billing":                true,
	"GET /api/v1/channels/email":         true,
	"POST /api/v1/billing/checkout":      true,
	"POST /api/v1/billing/portal":        true,
	"GET /api/v1/export":                 true,
	"POST /api/v1/export/link":           true,
	"DELETE /api/v1/channels/email":      true,
	"POST /api/v1/channels/email/secret": true,
	// A customer locked out after the trial may be the one who most needs to report a problem.
	"POST /api/v1/support/problem-reports": true,
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
// route is not on the allowlist. The state was derived while the session was authenticated, so the
// check costs nothing.
func (a *API) refuseIfLocked(response http.ResponseWriter, route Route, who auth.Principal) bool {
	if !a.limits || !who.Access.Locked || lockedAllowed[route.Method+" "+route.Pattern] {
		return false
	}
	writeLocked(response)
	return true
}

func writeLocked(response http.ResponseWriter) {
	writeError(response, http.StatusPaymentRequired, "workspace_locked", (&domain.LockedError{}).Error())
}
