package httpapi

import (
	"fmt"
	"net/http"

	"github.com/colliedesk/colliedesk/server/internal/auth"
)

// gatedByHandler marks a route whose tier depends on the request body. Any token passes the route
// check; the handler refuses it by what the body asks for.
const gatedByHandler = "handler"

// tokenTiers is the lowest tier of an agent token that may call each route, keyed "METHOD pattern"
// as listed in Routes(). A route missing here is closed to every token, so a route added later
// stays out of their reach until someone classifies it on purpose.
//
// read: what triage needs to look at (conversations, messages, contacts, tags, the team list).
// draft: changes that never reach a customer (status, priority, assignee, tags on a conversation,
// creating a tag, private notes). send: everything else an agent may do, such as replying and creating
// contacts and conversations. Closed on purpose: sessions, tokens and invites (including listing
// them), and every workspace, channel, billing and export setting. Posting a message is gated by the
// handler: a note needs draft, an outgoing reply needs send.
var tokenTiers = map[string]string{
	"POST /api/v1/auth/logout":                                   auth.ScopeRead,
	"GET /api/v1/auth/me":                                        auth.ScopeRead,
	"GET /api/v1/conversations":                                  auth.ScopeRead,
	"POST /api/v1/conversations":                                 auth.ScopeSend,
	"GET /api/v1/conversations/{id}":                             auth.ScopeRead,
	"PATCH /api/v1/conversations/{id}":                           auth.ScopeDraft,
	"PUT /api/v1/conversations/{id}/tags/{tagId}":                auth.ScopeDraft,
	"DELETE /api/v1/conversations/{id}/tags/{tagId}":             auth.ScopeDraft,
	"GET /api/v1/conversations/{id}/messages":                    auth.ScopeRead,
	"POST /api/v1/conversations/{id}/messages":                   gatedByHandler,
	"GET /api/v1/conversations/{id}/time-entries":                auth.ScopeRead,
	"POST /api/v1/conversations/{id}/messages/{messageId}/retry": auth.ScopeSend,
	"GET /api/v1/attachments/{id}":                               auth.ScopeRead,
	"GET /api/v1/contacts":                                       auth.ScopeRead,
	"POST /api/v1/contacts":                                      auth.ScopeSend,
	"GET /api/v1/contacts/{id}":                                  auth.ScopeRead,
	"GET /api/v1/agents":                                         auth.ScopeRead,
	"GET /api/v1/tags":                                           auth.ScopeRead,
	"POST /api/v1/tags":                                          auth.ScopeDraft,
	"PATCH /api/v1/tags/{id}":                                    auth.ScopeSend,
	"DELETE /api/v1/tags/{id}":                                   auth.ScopeSend,
}

// refuseToken answers 403 scope_insufficient and reports true when who is an agent token and the
// route is outside its tier. A full session is never refused here.
func refuseToken(response http.ResponseWriter, route Route, who auth.Principal) bool {
	if who.Scope == "" {
		return false
	}
	need := tokenTiers[route.Method+" "+route.Pattern]
	if need == "" {
		writeError(response, http.StatusForbidden, "scope_insufficient", "Agent tokens cannot use this route. Sign in as a person to do this.")
		return true
	}
	if need == gatedByHandler || auth.ScopeAllows(who.Scope, need) {
		return false
	}
	writeScopeInsufficient(response, who.Scope, need)
	return true
}

// writeScopeInsufficient names the tier the action needs so an agent can tell its owner what to grant.
func writeScopeInsufficient(response http.ResponseWriter, have, need string) {
	writeError(response, http.StatusForbidden, "scope_insufficient",
		fmt.Sprintf("This token has the %s scope; this action needs the %s scope.", have, need))
}
