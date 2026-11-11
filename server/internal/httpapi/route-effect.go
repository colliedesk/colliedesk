package httpapi

import (
	"maps"
	"strings"
)

// Effect is what a route does to the workspace and to the people outside it. The MCP tools take their
// readOnlyHint and destructiveHint from the routes they call, so a route that changes what it does
// changes those hints with it, and a client asks before the calls that need it.
type Effect int

const (
	// EffectUnknown: the route has not said what it does (a DELETE with no row in routeEffects).
	EffectUnknown Effect = iota
	// Reads: nothing stored changes and nothing leaves the server for a person.
	Reads
	// Changes: it changes the workspace, and another call can change it back (a delete into the trash
	// is one).
	Changes
	// Final: it cannot be undone. It reaches a customer, deletes for good, or revokes a sign-in, a
	// link or a key.
	Final
	// ByBody: the body decides (a note or a reply on the same route), so each tool says what the body
	// it always sends does.
	ByBody
)

// RouteEffect says what the route "METHOD pattern" does: its row in routeEffects, else Reads for a GET
// and Changes for a POST, PUT or PATCH. A DELETE has no default: it is in the trash or gone for good,
// and only its row says which.
func RouteEffect(route string) Effect {
	if effect, ok := routeEffects[route]; ok {
		return effect
	}
	method, _, _ := strings.Cut(route, " ")
	switch method {
	case "GET":
		return Reads
	case "POST", "PUT", "PATCH":
		return Changes
	}
	return EffectUnknown
}

// RouteEffectTable returns the routes that do not follow their method's default, for the gate tests.
func RouteEffectTable() map[string]Effect { return maps.Clone(routeEffects) }

// routeEffects lists every DELETE and every other route that does not do what its method says.
var routeEffects = map[string]Effect{
	// A POST that stores nothing: it checks or previews what the body would do.
	"POST /api/v1/ai/models":                        Reads,
	"POST /api/v1/conversations/{id}/fixes/preview": Reads,
	"POST /api/v1/help-center/theme/check":          Reads,
	"POST /api/v1/help-center/theme/preview":        Reads,
	"POST /api/v1/operator/config/check":            Reads,

	// The body decides: a note stays in the team, a reply or a started conversation reaches the customer.
	"POST /api/v1/conversations":               ByBody,
	"POST /api/v1/conversations/{id}/messages": ByBody,

	// A POST, PUT or PATCH that cannot be undone.
	"POST /api/v1/conversations/{id}/messages/{messageId}/retry": Final, // sends the reply again
	"POST /api/v1/kb/media/purge":                                Final, // deletes trashed files for good
	"POST /api/v1/auth/logout":                                   Final, // revokes the session or token
	"POST /api/v1/operator/auth/logout":                          Final,
	"POST /api/v1/channels/email/secret":                         Final, // the old forwarding address stops
	"POST /api/v1/channels/{id}/email/secret":                    Final,
	"POST /api/v1/imports/{id}/undo":                             Final, // deletes what the import made
	"POST /api/v1/channels/{id}/held/discard":                    Final, // held messages are dropped

	// A DELETE that cannot be undone: gone for good, or a revocation.
	"DELETE /api/v1/auth/sessions/{id}":                Final,
	"DELETE /api/v1/auth/invites/{id}":                 Final,
	"DELETE /api/v1/operator/operators/{id}":           Final,
	"DELETE /api/v1/operator/sessions/{id}":            Final,
	"DELETE /api/v1/tags/{id}":                         Final,
	"DELETE /api/v1/channels/{id}":                     Final,
	"DELETE /api/v1/channels/email":                    Final,
	"DELETE /api/v1/channels/email/gmail":              Final,
	"DELETE /api/v1/channels/{id}/email/gmail":         Final,
	"DELETE /api/v1/channels/email/microsoft":          Final,
	"DELETE /api/v1/channels/{id}/email/microsoft":     Final,
	"DELETE /api/v1/ai/providers/{id}":                 Final, // erases its key
	"DELETE /api/v1/ai/chat/sessions/{id}":             Final,
	"DELETE /api/v1/autofix/github/installations/{id}": Final,
	"DELETE /api/v1/autofix/repos/{id}":                Final,
	"DELETE /api/v1/autofix/connectors/cursor_api/key": Final, // erases the key
	"DELETE /api/v1/results/share-link":                Final, // the link stops; a new one has a new address

	// A DELETE that another call reverses: a trash with undelete, an unlink, a switch turned off.
	"DELETE /api/v1/conversations/{id}/tags/{tagId}":           Changes,
	"DELETE /api/v1/conversations/{id}/presence/{clientId}":    Changes,
	"DELETE /api/v1/contacts/{id}/merges/{mergeId}":            Changes,
	"DELETE /api/v1/saved-replies/{id}":                        Changes,
	"DELETE /api/v1/saved-views/{id}":                          Changes,
	"DELETE /api/v1/kb/collections/{id}":                       Changes,
	"DELETE /api/v1/kb/collections/{id}/translations/{locale}": Changes,
	"DELETE /api/v1/kb/articles/{id}":                          Changes,
	"DELETE /api/v1/kb/articles/{id}/translations/{locale}":    Changes,
	"DELETE /api/v1/kb/articles/{id}/ai-exclusion":             Changes,
	"DELETE /api/v1/kb/media/{id}":                             Changes,
	"DELETE /api/v1/kb/media-uploads/{id}":                     Changes,
	"DELETE /api/v1/help-center/domain":                        Changes,
	"DELETE /api/v1/imports/{id}/people/{externalId}/link":     Changes,
	"DELETE /api/v1/ai/source-consents/{class}":                Changes,
	"DELETE /api/v1/autofix/repos/{id}/include-notes":          Changes,
	"DELETE /api/v1/autofix/fixes/{id}/deployed":               Changes,
	"DELETE /api/v1/autofix/fixes/{id}/pull-request":           Changes,
	"DELETE /api/v1/autofix/connectors/{kind}":                 Changes,
	"DELETE /api/v1/roles/{id}":                                Changes,
}
