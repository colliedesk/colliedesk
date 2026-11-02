package httpapi

import (
	"net/http"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/auth"
)

// tokensPage is where a person makes agent tokens in the web app.
const tokensPage = "/settings#agents-heading"

// refuseByAccess answers 403 for a call routeAccess does not let through, and reports true when it did:
// a person-only route called with an agent token (person_only), or a permission the call lacks
// (permission_missing). A route with no row is closed to everyone.
func (a *API) refuseByAccess(response http.ResponseWriter, request *http.Request, route Route, who auth.Principal) bool {
	access, found := routeAccess[route.Method+" "+route.Pattern]
	switch {
	case !found || access.Closed == NotForPeople:
		writeError(response, http.StatusForbidden, "forbidden", "No session can use this route.")
		return true
	case access.Closed == PersonOnly && who.IsToken():
		writeErrorWith(response, http.StatusForbidden, "person_only",
			"Only a person signed in to Colliedesk can do this; agent tokens cannot ("+access.Why+"). It is done in the web app at "+tokensPageFor(route)+".",
			map[string]any{"where": tokensPageFor(route)})
		return true
	case access.Closed == PersonOnly && access.PersonNeeds != "" && !who.Can(access.PersonNeeds):
		a.writePermissionMissing(response, request, who, access.PersonNeeds)
		return true
	case access.Anyone || access.ByHandler || access.Closed == PersonOnly:
		return false
	case access.Permission != "" && !who.Can(access.Permission):
		a.writePermissionMissing(response, request, who, access.Permission)
		return true
	}
	return false
}

// tokensPageFor is the page of the web app a person-only route is done on.
func tokensPageFor(route Route) string {
	if route.Pattern == "/api/v1/auth/tokens" {
		return tokensPage
	}
	return "/"
}

// writePermissionMissing answers 403 permission_missing for permissions the call lacks: whether the token
// or its person lacks them, and how to get them.
func (a *API) writePermissionMissing(response http.ResponseWriter, request *http.Request, who auth.Principal, permissions ...string) {
	a.writePermissionMissingHint(response, request, who, "", permissions...)
}

// writePermissionMissingHint is writePermissionMissing with one more sentence saying what to do instead.
func (a *API) writePermissionMissingHint(response http.ResponseWriter, request *http.Request, who auth.Principal, hint string, permissions ...string) {
	missing := who.Missing(permissions)
	if len(missing) == 0 {
		missing = permissions
	}
	list := strings.Join(missing, ", ")
	name := personName(who)
	fields := map[string]any{"missing_permissions": missing}
	personLacks := false
	for _, permission := range missing {
		if !who.PersonCan(permission) {
			personLacks = true
		}
	}
	var message string
	switch {
	case who.IsToken() && !personLacks:
		fields["missing_from"] = "token"
		message = "This agent token was not given " + list + ". " + name + " holds it: create a token that includes it in Settings → External AI agents, then use that token."
		if who.Token.Kind == "legacy" {
			message += " This token was created before roles; tokens made since act at once."
		}
	case who.IsToken() && who.Token.Kind == "role" && !who.Token.RoleHeld:
		fields["missing_from"] = "person"
		message = "This token follows the role " + who.Token.Role.Name + ", which " + name + " no longer holds. Giving the role back starts it again."
	default:
		fields["missing_from"] = "person"
		subject := "Your roles do"
		if who.IsToken() {
			subject = name + "'s roles do"
		}
		ask, _ := a.store.NamesHolding(request.Context(), who.Workspace.ID, auth.PermTeamManageRoles)
		fields["ask"] = ask
		message = subject + " not include " + list + ". Ask someone who can change roles"
		if len(ask) > 0 {
			message += " (" + namesPhrase(ask) + ")"
		}
		message += " for a role that includes it."
	}
	if hint != "" {
		message += " " + hint
	}
	writeErrorWith(response, http.StatusForbidden, "permission_missing", message, fields)
}

// personName is the person behind a call, by display name or email.
func personName(who auth.Principal) string {
	if name := strings.TrimSpace(who.Agent.DisplayName); name != "" {
		return name
	}
	return who.Agent.Email
}

// namesPhrase names at most two people and how many more.
func namesPhrase(names []string) string {
	if len(names) <= 2 {
		return strings.Join(names, ", ")
	}
	return strings.Join(names[:2], ", ") + " and " + itoa(len(names)-2) + " more"
}
