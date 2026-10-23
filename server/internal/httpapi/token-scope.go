package httpapi

import (
	"maps"
	"net/http"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/mailimage"
)

// gatedByHandler marks a route whose tier depends on the request body. Any token passes the route
// check; the handler refuses it by what the body asks for.
const gatedByHandler = "handler"

// TokenAccess classifies one route of Routes() for agent tokens. Tier is the lowest tier that may
// call it (read, draft, send, admin, or gatedByHandler); a token below that tier gets an agent request
// a person approves (202) instead of a refusal. Approval marks a route no tier may call directly: every
// token gets an agent request. Otherwise the route is closed to every token, and Closed says why; Why
// is the reason a PersonOnly route stays with people.
type TokenAccess struct {
	Tier     string
	Approval bool
	Closed   string
	Why      string
}

// Why a route is closed to every token.
const (
	// PersonOnly: a person's own decision about an agent's request; an agent approving its own request
	// would make approval meaningless. It never opens to a token, and Why says so.
	PersonOnly = "person_only"
	// NotForPeople: no session reaches it (public pages, sign-in, provider webhooks), so no token does.
	NotForPeople = "not_for_people"
)

func tier(name string) TokenAccess { return TokenAccess{Tier: name} }

func personOnly(why string) TokenAccess { return TokenAccess{Closed: PersonOnly, Why: why} }

var (
	// approvalRequired: risky for any tier (new credentials, money, data leaving); every token's call
	// becomes an agent request a person approves.
	approvalRequired = TokenAccess{Approval: true}
	notForPeople     = TokenAccess{Closed: NotForPeople}
)

// tokenTiers classifies every route in Routes(), keyed "METHOD pattern". gateToken lets a token
// through only when Tier is set, so a route missing here is still closed to every token, but
// TestEveryRouteIsClassifiedForAgentsAndCoveredByAnMCPTool keeps a new route from reaching main
// without a row.
//
// read: what triage needs to look at (conversations, messages, contacts, tags, the team list), saying
// that a conversation is open (presence, which changes nothing anyone wrote), and the
// settings a person can see without secrets: the session and invite lists (never a token or a link),
// the email channel (never the inbound secret or the SMTP password) and the web form.
// draft: changes that never reach a customer (status, priority, assignee, tags on one conversation or
// many at once, creating a tag, private notes, the token owner's own timezone and display name, and
// saved inbox views: a view changes no conversation, only which filters a sidebar link opens, and a
// deleted one waits 30 days in the trash, so even a shared view is draft, unlike renaming or deleting
// a tag, which rewrites every conversation that carries it). send: everything else
// an agent may do, such as replying, creating, renaming and merging contacts (a merge is undone the same way), creating conversations, and writing the saved replies
// the team sends (every change is a version that can be restored, and a delete stays in the trash for
// 30 days); the knowledge base is send for the same reason (an article can be published, which reaches
// customers, but every change is a version and a delete waits 30 days in the trash), and so are its
// image uploads. Posting a message is gated by the handler: a note needs draft, an outgoing reply needs send.
// admin: workspace settings (the help center's on and off among them), the team, revoking invites, channels and sessions. An admin token acts
// with its creator's role, so it reaches these only while that person is an admin, and the role still
// decides the admin-only reads (billing is classified admin for that reason). The handlers file a
// request for every tier when the change is risky for people: a change that grants, keeps or takes
// the admin role (promoting, reactivating, demoting or deactivating an admin) and signing out another
// person. Creating an invite is Approval: its link is a credential only the approver sees.
//
// A token below a route's tier, and every token on an Approval route (new credentials, money, data
// leaving), gets an agent request a person approves instead of a refusal. Deciding a request is
// PersonOnly; routes no session reaches are NotForPeople. No other route is closed to tokens.
var tokenTiers = map[string]TokenAccess{
	"POST /api/v1/auth/logout":                                   tier(auth.ScopeRead),
	"GET /api/v1/auth/me":                                        tier(auth.ScopeRead),
	"GET /api/v1/conversations":                                  tier(auth.ScopeRead),
	"PATCH /api/v1/conversations":                                tier(auth.ScopeDraft),
	"GET /api/v1/inbox/activity":                                 tier(auth.ScopeRead),
	"GET /api/v1/saved-replies":                                  tier(auth.ScopeRead),
	"GET /api/v1/saved-replies/trash":                            tier(auth.ScopeRead),
	"GET /api/v1/saved-replies/{id}/versions":                    tier(auth.ScopeRead),
	"POST /api/v1/saved-replies":                                 tier(auth.ScopeSend),
	"PATCH /api/v1/saved-replies/{id}":                           tier(auth.ScopeSend),
	"DELETE /api/v1/saved-replies/{id}":                          tier(auth.ScopeSend),
	"POST /api/v1/saved-replies/{id}/restore":                    tier(auth.ScopeSend),
	"POST /api/v1/saved-replies/{id}/versions/{version}/restore": tier(auth.ScopeSend),
	"GET /api/v1/saved-views":                                    tier(auth.ScopeRead),
	"GET /api/v1/saved-views/trash":                              tier(auth.ScopeRead),
	"POST /api/v1/saved-views":                                   tier(auth.ScopeDraft),
	"PATCH /api/v1/saved-views/{id}":                             tier(auth.ScopeDraft),
	"DELETE /api/v1/saved-views/{id}":                            tier(auth.ScopeDraft),
	"POST /api/v1/saved-views/{id}/restore":                      tier(auth.ScopeDraft),
	"GET /api/v1/kb/collections":                                 tier(auth.ScopeRead),
	"GET /api/v1/kb/collections/trash":                           tier(auth.ScopeRead),
	"POST /api/v1/kb/collections":                                tier(auth.ScopeSend),
	"PATCH /api/v1/kb/collections/{id}":                          tier(auth.ScopeSend),
	"DELETE /api/v1/kb/collections/{id}":                         tier(auth.ScopeSend),
	"POST /api/v1/kb/collections/{id}/restore":                   tier(auth.ScopeSend),
	"GET /api/v1/kb/articles":                                    tier(auth.ScopeRead),
	"GET /api/v1/kb/articles/trash":                              tier(auth.ScopeRead),
	"GET /api/v1/kb/articles/{id}":                               tier(auth.ScopeRead),
	"GET /api/v1/kb/articles/{id}/versions":                      tier(auth.ScopeRead),
	"POST /api/v1/kb/articles":                                   tier(auth.ScopeSend),
	"PATCH /api/v1/kb/articles/{id}":                             tier(auth.ScopeSend),
	"DELETE /api/v1/kb/articles/{id}":                            tier(auth.ScopeSend),
	"POST /api/v1/kb/articles/{id}/restore":                      tier(auth.ScopeSend),
	"POST /api/v1/kb/articles/{id}/versions/{version}/restore":   tier(auth.ScopeSend),
	"POST /api/v1/kb/images":                                     tier(auth.ScopeSend),
	"GET /api/v1/kb/images/{id}":                                 tier(auth.ScopeRead),
	"GET /api/v1/kb/media":                                       tier(auth.ScopeRead),
	"PATCH /api/v1/kb/media/{id}":                                tier(auth.ScopeSend),
	"GET /api/v1/kb/media/trash":                                 tier(auth.ScopeRead),
	"POST /api/v1/kb/media/trash":                                tier(auth.ScopeSend),
	// A lower tier's request names the ids it would move (proposeTrashOfUnusedKBMedia), never "all unused".
	"POST /api/v1/kb/media/trash-unused":                         tier(gatedByHandler),
	"POST /api/v1/kb/media/restore":                              tier(auth.ScopeSend),
	"POST /api/v1/kb/media/purge":                                tier(auth.ScopeAdmin),
	"DELETE /api/v1/kb/media/{id}":                               tier(auth.ScopeSend),
	"POST /api/v1/kb/media/{id}/restore":                         tier(auth.ScopeSend),
	"POST /api/v1/kb/media/{id}/replace":                         tier(auth.ScopeSend),
	"POST /api/v1/kb/media-replacements/{id}/undo":               tier(auth.ScopeSend),
	"POST /api/v1/kb/media-uploads":                              tier(auth.ScopeSend),
	"GET /api/v1/kb/media-uploads":                               tier(auth.ScopeRead),
	"GET /api/v1/kb/media-uploads/{id}":                          tier(auth.ScopeRead),
	"PUT /api/v1/kb/media-uploads/{id}":                          tier(auth.ScopeSend),
	"DELETE /api/v1/kb/media-uploads/{id}":                       tier(auth.ScopeSend),
	"POST /api/v1/kb/media-uploads/{id}/complete":                tier(auth.ScopeSend),
	"GET /api/v1/kb/media/{id}":                                  tier(auth.ScopeRead),
	"PUT /api/v1/kb/media/{id}/poster":                           tier(auth.ScopeSend),
	"GET /api/v1/kb/media/{id}/poster/{version}":                 tier(auth.ScopeRead),
	"GET /api/v1/kb/media/{id}/details":                          tier(auth.ScopeRead),
	"GET /api/v1/kb/media/{id}/thumbnail":                        tier(auth.ScopeRead),
	"GET /api/v1/help-center":                                    tier(auth.ScopeRead),
	"POST /api/v1/conversations":                                 tier(auth.ScopeSend),
	"GET /api/v1/conversations/{id}":                             tier(auth.ScopeRead),
	"PATCH /api/v1/conversations/{id}":                           tier(auth.ScopeDraft),
	"PUT /api/v1/conversations/{id}/tags/{tagId}":                tier(auth.ScopeDraft),
	"DELETE /api/v1/conversations/{id}/tags/{tagId}":             tier(auth.ScopeDraft),
	"GET /api/v1/conversations/{id}/messages":                    tier(auth.ScopeRead),
	"POST /api/v1/conversations/{id}/messages":                   tier(gatedByHandler),
	"GET /api/v1/conversations/{id}/time-entries":                tier(auth.ScopeRead),
	"POST /api/v1/conversations/{id}/messages/{messageId}/retry": tier(auth.ScopeSend),
	"GET /api/v1/attachments/{id}":                               tier(auth.ScopeRead),
	"GET /api/v1/contacts":                                       tier(auth.ScopeRead),
	"POST /api/v1/contacts":                                      tier(auth.ScopeSend),
	"GET /api/v1/contacts/{id}":                                  tier(auth.ScopeRead),
	"PATCH /api/v1/contacts/{id}":                                tier(auth.ScopeSend),
	"POST /api/v1/contacts/{id}/merges":                          tier(auth.ScopeSend),
	"DELETE /api/v1/contacts/{id}/merges/{mergeId}":              tier(auth.ScopeSend),
	"GET /api/v1/agents":                                         tier(auth.ScopeRead),
	"GET /api/v1/tags":                                           tier(auth.ScopeRead),
	"POST /api/v1/tags":                                          tier(auth.ScopeDraft),
	"PATCH /api/v1/tags/{id}":                                    tier(auth.ScopeSend),
	"DELETE /api/v1/tags/{id}":                                   tier(auth.ScopeSend),
	"GET /api/v1/agent-requests":                                 tier(auth.ScopeRead),
	"GET /api/v1/agent-requests/{id}":                            tier(auth.ScopeRead),
	"POST /api/v1/agent-requests/{id}/cancel":                    tier(auth.ScopeRead),
	"POST " + problemReportPath:                                  tier(auth.ScopeRead),

	// Saying a conversation is open changes nothing anyone wrote; a row lives minutes at most.
	"PUT /api/v1/conversations/{id}/presence":               tier(auth.ScopeRead),
	"DELETE /api/v1/conversations/{id}/presence/{clientId}": tier(auth.ScopeRead),

	"POST /api/v1/agent-requests/{id}/approve": personOnly("an agent approving its own request would make approval meaningless"),
	"GET " + mailimage.ProxyPath:               personOnly("a signed address for the screen: an agent token reads the original HTML and fetches its own images"),
	"POST /api/v1/agent-requests/{id}/reject":  personOnly("deciding an agent's request is a person's call"),

	"GET /api/v1/auth/sessions":                       tier(auth.ScopeRead),
	"GET /api/v1/auth/invites":                        tier(auth.ScopeRead),
	"GET /api/v1/channels/email":                      tier(auth.ScopeRead),
	"GET /api/v1/channels/web-form":                   tier(auth.ScopeRead),
	"PATCH /api/v1/agents/me/timezone":                tier(auth.ScopeDraft),
	"PATCH /api/v1/agents/me/display-name":            tier(auth.ScopeDraft),
	"GET /api/v1/billing":                             tier(auth.ScopeAdmin),
	"PATCH /api/v1/workspace/timezone":                tier(auth.ScopeAdmin),
	"PATCH /api/v1/workspace/name":                    tier(auth.ScopeAdmin),
	"PATCH /api/v1/agents/{id}":                       tier(auth.ScopeAdmin),
	"DELETE /api/v1/auth/invites/{id}":                tier(auth.ScopeAdmin),
	"PUT /api/v1/channels/web-form":                   tier(auth.ScopeAdmin),
	"PUT /api/v1/help-center":                         tier(auth.ScopeAdmin),
	"PUT /api/v1/channels/email":                      tier(auth.ScopeAdmin),
	"POST /api/v1/channels/email/domain-verification": tier(auth.ScopeAdmin),
	"DELETE /api/v1/auth/sessions/{id}":               tier(auth.ScopeAdmin),

	"POST /api/v1/auth/tokens":           approvalRequired,
	"POST /api/v1/auth/invites":          approvalRequired,
	"GET /api/v1/export":                 approvalRequired,
	"POST /api/v1/export/link":           approvalRequired,
	"POST /api/v1/billing/checkout":      approvalRequired,
	"POST /api/v1/billing/portal":        approvalRequired,
	"DELETE /api/v1/channels/email":      approvalRequired,
	"POST /api/v1/channels/email/secret": approvalRequired,
	"PATCH /api/v1/workspace/slug":       approvalRequired,
	// Connecting Gmail grants Colliedesk a sending identity (and the consent address is for a person's
	// browser); disconnecting changes the address customers get replies from.
	"POST " + gmailPath:   approvalRequired,
	"DELETE " + gmailPath: approvalRequired,

	// Imports read and write the whole workspace: admin. An undo deletes what an import brought in, so
	// every token asks a person. The two routes that take a Freshdesk key are refused below admin
	// (credentialRoutes) rather than filed: an agent request keeps its body.
	"GET /api/v1/imports":              tier(auth.ScopeAdmin),
	"POST /api/v1/imports":             tier(auth.ScopeAdmin),
	"GET /api/v1/imports/{id}":         tier(auth.ScopeAdmin),
	"POST /api/v1/imports/{id}/start":  tier(auth.ScopeAdmin),
	"POST /api/v1/imports/{id}/cancel": tier(auth.ScopeAdmin),
	"PUT /api/v1/imports/{id}/key":     tier(auth.ScopeAdmin),
	"GET /api/v1/imports/{id}/report":  tier(auth.ScopeAdmin),
	"POST /api/v1/imports/{id}/undo":   approvalRequired,
	// A link only changes who wrote imported replies and holds imported tickets, and is taken back
	// the same way.
	"GET /api/v1/imports/{id}/people":                      tier(auth.ScopeAdmin),
	"PUT /api/v1/imports/{id}/people/{externalId}/link":    tier(auth.ScopeAdmin),
	"DELETE /api/v1/imports/{id}/people/{externalId}/link": tier(auth.ScopeAdmin),
	// AI settings show no key, so reading them is read. Providers are admin; the two routes that carry
	// a provider key are credential routes. Setting a feature decides whether ticket data leaves for a
	// provider, so every token's call waits for a person.
	"GET /api/v1/ai":                       tier(auth.ScopeRead),
	"POST /api/v1/ai/providers":            tier(auth.ScopeAdmin),
	"PATCH /api/v1/ai/providers/{id}":      tier(auth.ScopeAdmin),
	"PUT /api/v1/ai/providers/{id}/key":    tier(auth.ScopeAdmin),
	"POST /api/v1/ai/providers/{id}/check": tier(auth.ScopeAdmin),
	"DELETE /api/v1/ai/providers/{id}":     tier(auth.ScopeAdmin),
	"PUT /api/v1/ai/slots/{slot}":          approvalRequired,
	// Usage shows what the workspace spent, admin like billing. The automatic limits only lower or
	// raise how much automatic work runs; the monthly allowance still bounds the money: admin.
	"GET /api/v1/ai/usage":            tier(auth.ScopeAdmin),
	"PUT /api/v1/ai/automatic-limits": tier(auth.ScopeAdmin),

	"GET /api/v1/config": notForPeople,
	// Google's redirect after consent: it finishes only with the session cookie of the person who started.
	"GET " + gmailCallbackPath:                      notForPeople,
	"POST /api/v1/auth/cloud/exchange":              notForPeople,
	"POST " + cloudSignupPath:                       notForPeople,
	"POST /api/v1/auth/redeem":                      notForPeople,
	"POST /api/v1/channels/email/inbound":           notForPeople,
	"POST /api/v1/channels/email/inbound/recipient": notForPeople,
	"POST " + polarWebhookPath:                      notForPeople,
	"POST " + resendWebhookPath:                     notForPeople,
	"GET " + foundingPath:                           notForPeople,
	"GET " + helpCenterTLSAskPath:                   notForPeople,
	"POST " + webFormSubmitPattern:                  notForPeople,
	"GET " + exportLinkPath + "{token}":             notForPeople,
}

// TokenAccessTable returns a copy of the classification, for the tests that hold it to Routes() and
// the MCP catalog.
func TokenAccessTable() map[string]TokenAccess { return maps.Clone(tokenTiers) }

// gateToken answers for an agent token whose call may not run as it is and reports true when it did:
// a call above the token's tier, or on an Approval route, becomes an agent request (202); a PersonOnly
// or closed route is refused (403). A full session is never stopped here.
func (a *API) gateToken(response http.ResponseWriter, request *http.Request, route Route, who auth.Principal) bool {
	if who.Scope == "" {
		return false
	}
	access := tokenTiers[route.Method+" "+route.Pattern]
	switch {
	case access.Approval:
	case access.Tier == gatedByHandler:
		return false
	case access.Tier != "" && auth.ScopeAllows(who.Scope, access.Tier):
		return false
	case access.Tier != "":
	case access.Closed == PersonOnly:
		writeError(response, http.StatusForbidden, "person_only", "Only a person signed in to Colliedesk can do this; agent tokens cannot.")
		return true
	default:
		writeError(response, http.StatusForbidden, "scope_insufficient", "Agent tokens cannot use this route. Sign in as a person to do this.")
		return true
	}
	if kbFileRoutes[route.Method+" "+route.Pattern] {
		writeFilesNeedApproval(response)
		return true
	}
	if credentialRoutes[route.Method+" "+route.Pattern] {
		writeError(response, http.StatusForbidden, "credentials_need_admin_token",
			"This call carries another service's API key, which must never be kept in an agent request for approval. Use an agent token of the admin tier, or let an admin do it in Settings.")
		return true
	}
	a.fileAgentRequest(response, request, route, who, domain.AgentRequestAction, nil, "")
	return true
}

// credentialRoutes carry another service's API key (a helpdesk to import from, an AI provider): a token
// below their tier is refused, never filed, because an agent request stores the call's body (and the
// export carries it).
var credentialRoutes = map[string]bool{
	"POST /api/v1/imports":              true,
	"PUT /api/v1/imports/{id}/key":      true,
	"POST /api/v1/ai/providers":         true,
	"PUT /api/v1/ai/providers/{id}/key": true,
}

// kbFileRoutes carry a video's bytes or start, finish or cancel an upload of them: a file never waits
// for approval, so a token below their tier is refused (files_need_approval), not filed.
var kbFileRoutes = map[string]bool{
	"POST /api/v1/kb/media-uploads":               true,
	"PUT /api/v1/kb/media-uploads/{id}":           true,
	"DELETE /api/v1/kb/media-uploads/{id}":        true,
	"POST /api/v1/kb/media-uploads/{id}/complete": true,
	"PUT /api/v1/kb/media/{id}/poster":            true,
}
