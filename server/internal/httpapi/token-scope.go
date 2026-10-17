package httpapi

import (
	"maps"
	"net/http"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// gatedByHandler marks a route whose tier depends on the request body. Any token passes the route
// check; the handler refuses it by what the body asks for.
const gatedByHandler = "handler"

// TokenAccess classifies one route of Routes() for agent tokens. Tier is the lowest tier that may
// call it (read, draft, send, or gatedByHandler); a token below that tier gets an agent request a
// person approves (202) instead of a refusal. Approval marks a route no tier may call directly: every
// token gets an agent request. Otherwise the route is closed to every token, and Closed says why;
// Target is the tier a TierPending route will open to, Tracking the issue that opens it, and Why the
// reason a PersonOnly route stays with people.
type TokenAccess struct {
	Tier     string
	Approval bool
	Closed   string
	Target   string
	Tracking int
	Why      string
}

// Why a route is closed to every token today.
const (
	// AdminTier: workspace settings, team and channels; it opens to the admin tier.
	AdminTier = "admin_tier"
	// TierPending: will open to Target once the issue in Tracking ships (secrets redacted, history kept).
	TierPending = "tier_pending"
	// PersonOnly: a person's own decision about an agent's request; an agent approving its own request
	// would make approval meaningless. It never opens to a token, and Why says so.
	PersonOnly = "person_only"
	// NotForPeople: no session reaches it (public pages, sign-in, provider webhooks), so no token does.
	NotForPeople = "not_for_people"
)

func tier(name string) TokenAccess { return TokenAccess{Tier: name} }

// pendingTier closes a route until issue ships, then it opens to target.
func pendingTier(target string, issue int) TokenAccess {
	return TokenAccess{Closed: TierPending, Target: target, Tracking: issue}
}

func personOnly(why string) TokenAccess { return TokenAccess{Closed: PersonOnly, Why: why} }

var (
	// approvalRequired: risky for any tier (new credentials, money, data leaving); every token's call
	// becomes an agent request a person approves.
	approvalRequired = TokenAccess{Approval: true}
	adminTier        = TokenAccess{Closed: AdminTier, Tracking: 128}
	notForPeople     = TokenAccess{Closed: NotForPeople}
)

// tokenTiers classifies every route in Routes(), keyed "METHOD pattern". gateToken lets a token
// through only when Tier is set, so a route missing here is still closed to every token, but
// TestEveryRouteIsClassifiedForAgentsAndCoveredByAnMCPTool keeps a new route from reaching main
// without a row.
//
// read: what triage needs to look at (conversations, messages, contacts, tags, the team list).
// draft: changes that never reach a customer (status, priority, assignee, tags on one conversation or
// many at once, creating a tag, private notes). send: everything else an agent may do, such as
// replying and creating contacts and conversations. Posting a message is gated by the handler: a note
// needs draft, an outgoing reply needs send.
//
// A token below a route's tier, and every token on an Approval route (new credentials, money, data
// leaving), gets an agent request a person approves instead of a refusal. Deciding a request is
// PersonOnly. Every other route is closed to every token today, with the reason and the issue that
// opens it: settings that wait for the admin tier (#128), reads that open once secrets and links are
// redacted (#128), saved reply writes that open with history and restore (#129), and routes no
// session reaches (NotForPeople). The closed rows may only shrink.
var tokenTiers = map[string]TokenAccess{
	"POST /api/v1/auth/logout":                                   tier(auth.ScopeRead),
	"GET /api/v1/auth/me":                                        tier(auth.ScopeRead),
	"GET /api/v1/conversations":                                  tier(auth.ScopeRead),
	"PATCH /api/v1/conversations":                                tier(auth.ScopeDraft),
	"GET /api/v1/inbox/activity":                                 tier(auth.ScopeRead),
	"GET /api/v1/saved-replies":                                  tier(auth.ScopeRead),
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
	"GET /api/v1/agents":                                         tier(auth.ScopeRead),
	"GET /api/v1/tags":                                           tier(auth.ScopeRead),
	"POST /api/v1/tags":                                          tier(auth.ScopeDraft),
	"PATCH /api/v1/tags/{id}":                                    tier(auth.ScopeSend),
	"DELETE /api/v1/tags/{id}":                                   tier(auth.ScopeSend),
	"GET /api/v1/agent-requests":                                 tier(auth.ScopeRead),
	"GET /api/v1/agent-requests/{id}":                            tier(auth.ScopeRead),
	"POST /api/v1/agent-requests/{id}/cancel":                    tier(auth.ScopeRead),

	"POST /api/v1/agent-requests/{id}/approve": personOnly("an agent approving its own request would make approval meaningless"),
	"POST /api/v1/agent-requests/{id}/reject":  personOnly("deciding an agent's request is a person's call"),

	"GET /api/v1/auth/sessions":         pendingTier(auth.ScopeRead, 128),
	"GET /api/v1/auth/invites":          pendingTier(auth.ScopeRead, 128),
	"GET /api/v1/channels/email":        pendingTier(auth.ScopeRead, 128),
	"GET /api/v1/channels/web-form":     pendingTier(auth.ScopeRead, 128),
	"GET /api/v1/billing":               pendingTier(auth.ScopeRead, 128),
	"PATCH /api/v1/agents/me/timezone":  pendingTier(auth.ScopeDraft, 128),
	"POST /api/v1/saved-replies":        pendingTier(auth.ScopeSend, 129),
	"PATCH /api/v1/saved-replies/{id}":  pendingTier(auth.ScopeSend, 129),
	"DELETE /api/v1/saved-replies/{id}": pendingTier(auth.ScopeSend, 129),

	"PATCH /api/v1/workspace/timezone": adminTier,
	"PATCH /api/v1/agents/{id}":        adminTier,
	// An invite with the admin role stays an approval request even for the admin tier (#127).
	"POST /api/v1/auth/invites":                       adminTier,
	"DELETE /api/v1/auth/invites/{id}":                adminTier,
	"PUT /api/v1/channels/web-form":                   adminTier,
	"PUT /api/v1/channels/email":                      adminTier,
	"POST /api/v1/channels/email/domain-verification": adminTier,
	"DELETE /api/v1/auth/sessions/{id}":               adminTier,

	"POST /api/v1/auth/tokens":           approvalRequired,
	"GET /api/v1/export":                 approvalRequired,
	"POST /api/v1/export/link":           approvalRequired,
	"POST /api/v1/billing/checkout":      approvalRequired,
	"POST /api/v1/billing/portal":        approvalRequired,
	"DELETE /api/v1/channels/email":      approvalRequired,
	"POST /api/v1/channels/email/secret": approvalRequired,
	"PATCH /api/v1/workspace/slug":       approvalRequired,

	"GET /api/v1/config":                            notForPeople,
	"POST /api/v1/auth/cloud/exchange":              notForPeople,
	"POST " + cloudSignupPath:                       notForPeople,
	"POST /api/v1/auth/redeem":                      notForPeople,
	"POST /api/v1/channels/email/inbound":           notForPeople,
	"POST /api/v1/channels/email/inbound/recipient": notForPeople,
	"POST " + polarWebhookPath:                      notForPeople,
	"POST " + resendWebhookPath:                     notForPeople,
	"GET " + foundingPath:                           notForPeople,
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
	a.fileAgentRequest(response, request, route, who, domain.AgentRequestAction, nil)
	return true
}
