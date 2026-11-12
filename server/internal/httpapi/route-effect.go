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
	"POST /api/v1/auth/invite-preview":              Reads, // the token rides in the body, never in a logged address
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

// RouteReachesOutside says the route may reach people or systems outside the workspace (the MCP
// openWorldHint): it emails or messages a customer, calls a third party in the request or in a job it
// queues, or changes what the public help center shows. "May": a route that does so only for some
// bodies or states is listed too.
func RouteReachesOutside(route string) bool { _, ok := routesReachingOutside[route]; return ok }

// RoutesReachingOutsideTable returns routesReachingOutside, for the gate tests.
func RoutesReachingOutsideTable() map[string]string { return maps.Clone(routesReachingOutside) }

// routesReachingOutside lists each such route with what it reaches. On the two routes the body decides
// (ByBody), a tool reaches outside only when its body is Final: a reply, a started conversation.
var routesReachingOutside = map[string]string{
	// A customer or someone outside the team gets a message.
	"POST /api/v1/conversations":                                 "starts the conversation by email",
	"POST /api/v1/conversations/{id}/messages":                   "a reply is emailed",
	"POST /api/v1/conversations/{id}/messages/{messageId}/retry": "sends the reply again",
	"PATCH /api/v1/conversations/{id}":                           "a status change may tell the customer in their chat",
	"PATCH /api/v1/conversations":                                "status changes may tell the customers in their chats",
	"POST " + intakeDecideRoute:                                  "creating replays the chat, which may send the received notice",
	"POST /api/v1/channels/{id}/held/replay":                     "replaying may send the received notice",
	"POST /api/v1/billing/migration-request":                     "emails Colliedesk support",
	"POST " + problemReportPath:                                  "files the report with Colliedesk support",

	// A third party is called, in the request or in a job it queues.
	"POST /api/v1/ai/chat/preview":                               "AI provider",
	"POST /api/v1/conversations/{id}/ai-drafts":                  "AI provider",
	"POST /api/v1/conversations/{id}/ai-triage":                  "AI provider",
	"POST /api/v1/intake/gatherings/{id}/requeue":                "AI provider",
	"POST /api/v1/ai/models":                                     "AI provider",
	"POST /api/v1/ai/providers":                                  "AI provider",
	"PATCH /api/v1/ai/providers/{id}":                            "AI provider",
	"PUT /api/v1/ai/providers/{id}/key":                          "AI provider",
	"POST /api/v1/ai/providers/{id}/check":                       "AI provider",
	"POST /api/v1/imports":                                       "Freshdesk",
	"POST /api/v1/imports/{id}/start":                            "Freshdesk",
	"POST /api/v1/conversations/{id}/fixes":                      "Cursor or GitHub",
	"POST /api/v1/autofix/fixes/{id}/retry":                      "Cursor or GitHub",
	"POST /api/v1/autofix/fixes/{id}/cancel":                     "Cursor or GitHub",
	"PUT /api/v1/autofix/connectors/cursor_api/key":              "Cursor",
	"POST /api/v1/autofix/connectors/{kind}/check":               "Cursor",
	"GET /api/v1/autofix/github/installations/{id}/repositories": "GitHub",
	"POST /api/v1/autofix/repos":                                 "GitHub",
	"DELETE /api/v1/autofix/github/installations/{id}":           "GitHub",
	"POST /api/v1/billing/checkout":                              "billing provider",
	"POST /api/v1/billing/portal":                                "billing provider",
	"POST /api/v1/channels/email/domain-verification":            "DNS or the mail provider",
	"POST /api/v1/channels/{id}/email/domain-verification":       "DNS or the mail provider",
	"POST /api/v1/help-center/domain/check":                      "DNS and the custom domain",
	"DELETE /api/v1/channels/email":                              "revokes the Google grant",
	"DELETE /api/v1/channels/email/gmail":                        "revokes the Google grant",
	"DELETE /api/v1/channels/{id}/email/gmail":                   "revokes the Google grant",
	"DELETE /api/v1/channels/{id}":                               "revokes an email channel's Google grant",

	// What the public help center, a public link or the public web form shows.
	"PUT /api/v1/help-center":                                                        "public site",
	"PUT /api/v1/help-center/theme":                                                  "public site",
	"PUT /api/v1/help-center/languages":                                              "public site",
	"PUT /api/v1/help-center/search-console":                                         "public site",
	"PUT /api/v1/help-center/domain":                                                 "public site",
	"DELETE /api/v1/help-center/domain":                                              "public site",
	"PATCH /api/v1/workspace/slug":                                                   "public site address",
	"PATCH /api/v1/kb/collections/{id}":                                              "public site",
	"POST /api/v1/kb/collections/{id}/restore":                                       "public site",
	"PUT /api/v1/kb/order":                                                           "public site",
	"PUT /api/v1/kb/collections/{id}/translations/{locale}":                          "public site",
	"DELETE /api/v1/kb/collections/{id}/translations/{locale}":                       "public site",
	"POST /api/v1/kb/articles":                                                       "may publish",
	"PATCH /api/v1/kb/articles/{id}":                                                 "may change a published article",
	"DELETE /api/v1/kb/articles/{id}":                                                "may unpublish",
	"POST /api/v1/kb/articles/{id}/restore":                                          "may republish",
	"POST /api/v1/kb/articles/{id}/versions/{version}/restore":                       "may change a published article",
	"POST /api/v1/kb/articles/{id}/translations":                                     "may publish",
	"PATCH /api/v1/kb/articles/{id}/translations/{locale}":                           "may change a published translation",
	"DELETE /api/v1/kb/articles/{id}/translations/{locale}":                          "may unpublish",
	"POST /api/v1/kb/articles/{id}/translations/{locale}/restore":                    "may republish",
	"POST /api/v1/kb/articles/{id}/translations/{locale}/versions/{version}/restore": "may change a published translation",
	"POST /api/v1/kb/media/{id}/replace":                                             "published articles show it",
	"POST /api/v1/kb/media-replacements/{id}/undo":                                   "published articles show it",
	"POST /api/v1/results/share-link":                                                "a public link",
	"PUT /api/v1/channels/web-form":                                                  "the public form",
}
