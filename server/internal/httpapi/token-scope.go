package httpapi

import (
	"maps"
	"net/http"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/mailimage"
)

// gatedByHandler marks a route whose permission depends on the request body. Any token passes the
// route check; the handler refuses it by what the body asks for (Permission is the least it needs).
const gatedByHandler = "handler"

// TokenAccess classifies one route of Routes() for agent tokens. Permission is what a token must hold
// to call it (auth.Perm…); Tier is that permission's preset, the lowest tier that holds it (or
// gatedByHandler), kept for the tables people read. A token without the permission gets an agent
// request a person approves (202) instead of a refusal. Approval marks a route no token may call
// directly: every token gets an agent request, whatever it holds (Permission names the matrix cell).
// A route with a Tier and no Permission is open to every token (signing out, who am I). Otherwise
// the route is closed to every token, and Closed says why; Why is the reason a PersonOnly route stays
// with people.
type TokenAccess struct {
	Tier       string
	Permission string
	Approval   bool
	Closed     string
	Why        string
}

// Why a route is closed to every token.
const (
	// PersonOnly: a person's own decision about an agent's request; an agent approving its own request
	// would make approval meaningless. It never opens to a token, and Why says so.
	PersonOnly = "person_only"
	// NotForPeople: no session reaches it (public pages, sign-in, provider webhooks), so no token does.
	NotForPeople = "not_for_people"
)

func grant(permission string) TokenAccess {
	return TokenAccess{Tier: auth.PermissionPreset(permission), Permission: permission}
}

func byHandler(permission string) TokenAccess {
	return TokenAccess{Tier: gatedByHandler, Permission: permission}
}

// alwaysAsk: risky whatever the token holds (new credentials, money, data leaving); every token's call
// becomes an agent request a person approves.
func alwaysAsk(permission string) TokenAccess {
	return TokenAccess{Approval: true, Permission: permission}
}

func personOnly(why string) TokenAccess { return TokenAccess{Closed: PersonOnly, Why: why} }

var (
	// anyToken: the token's own session (who am I, sign out) and a problem report; every token may.
	anyToken     = TokenAccess{Tier: auth.ScopeRead}
	notForPeople = TokenAccess{Closed: NotForPeople}
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
	"POST /api/v1/auth/logout":                                   anyToken,
	"GET /api/v1/auth/me":                                        anyToken,
	"GET /api/v1/conversations":                                  grant(auth.PermConversationsRead),
	"PATCH /api/v1/conversations":                                grant(auth.PermConversationsUpdate),
	"GET /api/v1/inbox/activity":                                 grant(auth.PermConversationsRead),
	"GET /api/v1/saved-replies":                                  grant(auth.PermSavedRepliesRead),
	"GET /api/v1/saved-replies/trash":                            grant(auth.PermSavedRepliesRead),
	"GET /api/v1/saved-replies/{id}/versions":                    grant(auth.PermSavedRepliesRead),
	"POST /api/v1/saved-replies":                                 grant(auth.PermSavedRepliesCreate),
	"PATCH /api/v1/saved-replies/{id}":                           grant(auth.PermSavedRepliesUpdate),
	"DELETE /api/v1/saved-replies/{id}":                          grant(auth.PermSavedRepliesDelete),
	"POST /api/v1/saved-replies/{id}/restore":                    grant(auth.PermSavedRepliesDelete),
	"POST /api/v1/saved-replies/{id}/versions/{version}/restore": grant(auth.PermSavedRepliesUpdate),
	"GET /api/v1/saved-views":                                    grant(auth.PermSavedViewsRead),
	"GET /api/v1/saved-views/trash":                              grant(auth.PermSavedViewsRead),
	"POST /api/v1/saved-views":                                   grant(auth.PermSavedViewsCreate),
	"PATCH /api/v1/saved-views/{id}":                             grant(auth.PermSavedViewsUpdate),
	"DELETE /api/v1/saved-views/{id}":                            grant(auth.PermSavedViewsDelete),
	"POST /api/v1/saved-views/{id}/restore":                      grant(auth.PermSavedViewsDelete),
	"GET /api/v1/kb/collections":                                 grant(auth.PermKBArticlesRead),
	"GET /api/v1/kb/collections/trash":                           grant(auth.PermKBArticlesRead),
	"POST /api/v1/kb/collections":                                grant(auth.PermKBArticlesCreate),
	"PATCH /api/v1/kb/collections/{id}":                          grant(auth.PermKBArticlesUpdate),
	"DELETE /api/v1/kb/collections/{id}":                         grant(auth.PermKBArticlesDelete),
	"POST /api/v1/kb/collections/{id}/restore":                   grant(auth.PermKBArticlesDelete),
	"GET /api/v1/kb/articles":                                    grant(auth.PermKBArticlesRead),
	"GET /api/v1/kb/articles/trash":                              grant(auth.PermKBArticlesRead),
	"GET /api/v1/kb/articles/{id}":                               grant(auth.PermKBArticlesRead),
	"GET /api/v1/kb/articles/{id}/versions":                      grant(auth.PermKBArticlesRead),
	"POST /api/v1/kb/articles":                                   grant(auth.PermKBArticlesCreate),
	"PATCH /api/v1/kb/articles/{id}":                             grant(auth.PermKBArticlesUpdate),
	"DELETE /api/v1/kb/articles/{id}":                            grant(auth.PermKBArticlesDelete),
	"POST /api/v1/kb/articles/{id}/restore":                      grant(auth.PermKBArticlesDelete),
	"POST /api/v1/kb/articles/{id}/versions/{version}/restore":   grant(auth.PermKBArticlesUpdate),
	"POST /api/v1/kb/articles/{id}/ai-exclusion":                 grant(auth.PermKBArticlesUpdate),
	"DELETE /api/v1/kb/articles/{id}/ai-exclusion":               grant(auth.PermKBArticlesUpdate),
	"POST /api/v1/kb/images":                                     grant(auth.PermKBMediaCreate),
	"GET /api/v1/kb/images/{id}":                                 grant(auth.PermKBMediaRead),
	"GET /api/v1/kb/media":                                       grant(auth.PermKBMediaRead),
	"PATCH /api/v1/kb/media/{id}":                                grant(auth.PermKBMediaUpdate),
	"GET /api/v1/kb/media/trash":                                 grant(auth.PermKBMediaRead),
	"POST /api/v1/kb/media/trash":                                grant(auth.PermKBMediaDelete),
	// A lower tier's request names the ids it would move (proposeTrashOfUnusedKBMedia), never "all unused".
	"POST /api/v1/kb/media/trash-unused":                         byHandler(auth.PermKBMediaDelete),
	"POST /api/v1/kb/media/restore":                              grant(auth.PermKBMediaDelete),
	"POST /api/v1/kb/media/purge":                                grant(auth.PermKBMediaPurge),
	"DELETE /api/v1/kb/media/{id}":                               grant(auth.PermKBMediaDelete),
	"POST /api/v1/kb/media/{id}/restore":                         grant(auth.PermKBMediaDelete),
	"POST /api/v1/kb/media/{id}/replace":                         grant(auth.PermKBMediaUpdate),
	"POST /api/v1/kb/media-replacements/{id}/undo":               grant(auth.PermKBMediaUpdate),
	"POST /api/v1/kb/media-uploads":                              grant(auth.PermKBMediaCreate),
	"GET /api/v1/kb/media-uploads":                               grant(auth.PermKBMediaRead),
	"GET /api/v1/kb/media-uploads/{id}":                          grant(auth.PermKBMediaRead),
	"PUT /api/v1/kb/media-uploads/{id}":                          grant(auth.PermKBMediaCreate),
	"DELETE /api/v1/kb/media-uploads/{id}":                       grant(auth.PermKBMediaCreate),
	"POST /api/v1/kb/media-uploads/{id}/complete":                grant(auth.PermKBMediaCreate),
	"GET /api/v1/kb/media/{id}":                                  grant(auth.PermKBMediaRead),
	"PUT /api/v1/kb/media/{id}/poster":                           grant(auth.PermKBMediaUpdate),
	"GET /api/v1/kb/media/{id}/poster/{version}":                 grant(auth.PermKBMediaRead),
	"GET /api/v1/kb/media/{id}/details":                          grant(auth.PermKBMediaRead),
	"GET /api/v1/kb/media/{id}/thumbnail":                        grant(auth.PermKBMediaRead),
	"GET /api/v1/help-center":                                    grant(auth.PermHelpCenterRead),
	"POST /api/v1/conversations":                                 grant(auth.PermMessagesSend),
	"GET /api/v1/conversations/{id}":                             grant(auth.PermConversationsRead),
	"PATCH /api/v1/conversations/{id}":                           grant(auth.PermConversationsUpdate),
	"PUT /api/v1/conversations/{id}/tags/{tagId}":                grant(auth.PermConversationsUpdate),
	"DELETE /api/v1/conversations/{id}/tags/{tagId}":             grant(auth.PermConversationsUpdate),
	"GET /api/v1/conversations/{id}/messages":                    grant(auth.PermMessagesRead),
	"POST /api/v1/conversations/{id}/messages":                   byHandler(auth.PermMessagesNote),
	"GET /api/v1/conversations/{id}/time-entries":                grant(auth.PermConversationsRead),
	"POST /api/v1/conversations/{id}/messages/{messageId}/retry": grant(auth.PermMessagesSend),
	"GET /api/v1/attachments/{id}":                               grant(auth.PermMessagesRead),
	"GET /api/v1/contacts":                                       grant(auth.PermContactsRead),
	"POST /api/v1/contacts":                                      grant(auth.PermContactsCreate),
	"GET /api/v1/contacts/{id}":                                  grant(auth.PermContactsRead),
	"PATCH /api/v1/contacts/{id}":                                grant(auth.PermContactsUpdate),
	"POST /api/v1/contacts/{id}/merges":                          grant(auth.PermContactsUpdate),
	"DELETE /api/v1/contacts/{id}/merges/{mergeId}":              grant(auth.PermContactsUpdate),
	"GET /api/v1/agents":                                         grant(auth.PermTeamRead),
	"GET /api/v1/tags":                                           grant(auth.PermTagsRead),
	"GET /api/v1/results":                                        grant(auth.PermResultsRead),
	"GET /api/v1/results/share-link":                             grant(auth.PermResultsShare),
	// A share link puts the numbers outside sign-in: data leaving, so a person approves every token's call.
	"POST /api/v1/results/share-link":         alwaysAsk(auth.PermResultsShare),
	"DELETE /api/v1/results/share-link":       grant(auth.PermResultsShare),
	"POST /api/v1/results/shared":             notForPeople,
	"PATCH /api/v1/agents/me/results-email":   grant(auth.PermProfileUpdate),
	"POST /api/v1/tags":                       grant(auth.PermTagsCreate),
	"PATCH /api/v1/tags/{id}":                 grant(auth.PermTagsUpdate),
	"DELETE /api/v1/tags/{id}":                grant(auth.PermTagsDelete),
	"GET /api/v1/agent-requests":              grant(auth.PermAgentRequestsRead),
	"GET /api/v1/agent-requests/{id}":         grant(auth.PermAgentRequestsRead),
	"POST /api/v1/agent-requests/{id}/cancel": grant(auth.PermAgentRequestsCancel),
	"POST " + problemReportPath:               anyToken,

	// Saying a conversation is open changes nothing anyone wrote; a row lives minutes at most.
	"PUT /api/v1/conversations/{id}/presence":               grant(auth.PermConversationsRead),
	"DELETE /api/v1/conversations/{id}/presence/{clientId}": grant(auth.PermConversationsRead),

	"POST /api/v1/agent-requests/{id}/approve": personOnly("an agent approving its own request would make approval meaningless"),
	"GET " + mailimage.ProxyPath:               personOnly("a signed address for the screen: an agent token reads the original HTML and fetches its own images"),
	"POST /api/v1/agent-requests/{id}/reject":  personOnly("deciding an agent's request is a person's call"),

	"GET /api/v1/auth/sessions":              grant(auth.PermTeamRead),
	"GET /api/v1/auth/invites":               grant(auth.PermTeamRead),
	"GET /api/v1/channels":                   grant(auth.PermChannelsRead),
	"GET /api/v1/channels/events":            grant(auth.PermChannelsRead),
	"GET /api/v1/channels/{id}":              grant(auth.PermChannelsRead),
	"POST /api/v1/channels":                  grant(auth.PermChannelsUpdate),
	"PATCH /api/v1/channels/{id}":            grant(auth.PermChannelsUpdate),
	"POST /api/v1/channels/{id}/check":       grant(auth.PermChannelsUpdate),
	"POST /api/v1/channels/{id}/held/replay": grant(auth.PermChannelsUpdate),
	// A channel's secret is a credential route (credentialRoutes): an admin token stores it, a lower one
	// is refused, so it never waits in a request. Removing a channel is what DELETE /channels/email
	// already is: a person approves every token's call. Discarding held messages cannot be undone.
	"PUT /api/v1/channels/{id}/credentials":   grant(auth.PermChannelsManageCredentials),
	"DELETE /api/v1/channels/{id}":            alwaysAsk(auth.PermChannelsManageCredentials),
	"POST /api/v1/channels/{id}/held/discard": alwaysAsk(auth.PermChannelsUpdate),
	// One email channel of several: the same tiers as the routes of the default one.
	"GET /api/v1/channels/{id}/email":                      grant(auth.PermChannelsRead),
	"POST /api/v1/channels/{id}/email/domain-verification": grant(auth.PermChannelsUpdate),
	"POST /api/v1/channels/{id}/email/secret":              alwaysAsk(auth.PermChannelsManageCredentials),
	"POST " + channelGmailPath:                             alwaysAsk(auth.PermChannelsManageCredentials),
	"DELETE " + channelGmailPath:                           alwaysAsk(auth.PermChannelsManageCredentials),
	"POST " + channelMicrosoftPath:                         alwaysAsk(auth.PermChannelsManageCredentials),
	"DELETE " + channelMicrosoftPath:                       alwaysAsk(auth.PermChannelsManageCredentials),
	"GET /api/v1/channels/email":                           grant(auth.PermChannelsRead),
	"GET /api/v1/channels/web-form":                        grant(auth.PermChannelsRead),
	"PATCH /api/v1/agents/me/timezone":                     grant(auth.PermProfileUpdate),
	"PATCH /api/v1/agents/me/display-name":                 grant(auth.PermProfileUpdate),
	"GET /api/v1/billing":                                  grant(auth.PermBillingRead),
	"PATCH /api/v1/workspace/timezone":                     grant(auth.PermWorkspaceUpdate),
	"PATCH /api/v1/workspace/name":                         grant(auth.PermWorkspaceUpdate),
	"PATCH /api/v1/agents/{id}":                            grant(auth.PermTeamManageRoles),
	"DELETE /api/v1/auth/invites/{id}":                     grant(auth.PermTeamRevoke),
	"PUT /api/v1/channels/web-form":                        grant(auth.PermChannelsUpdate),
	"PUT /api/v1/help-center":                              grant(auth.PermHelpCenterUpdate),
	"PUT /api/v1/channels/email":                           grant(auth.PermChannelsUpdate),
	"POST /api/v1/channels/email/domain-verification":      grant(auth.PermChannelsUpdate),
	"DELETE /api/v1/auth/sessions/{id}":                    grant(auth.PermTeamRevoke),

	"POST /api/v1/auth/tokens":           alwaysAsk(auth.PermTeamCreateTokens),
	"POST /api/v1/auth/invites":          alwaysAsk(auth.PermTeamInvite),
	"GET /api/v1/export":                 alwaysAsk(auth.PermWorkspaceExport),
	"POST /api/v1/export/link":           alwaysAsk(auth.PermWorkspaceExport),
	"POST /api/v1/billing/checkout":      alwaysAsk(auth.PermBillingManage),
	"POST /api/v1/billing/portal":        alwaysAsk(auth.PermBillingManage),
	"DELETE /api/v1/channels/email":      alwaysAsk(auth.PermChannelsManageCredentials),
	"POST /api/v1/channels/email/secret": alwaysAsk(auth.PermChannelsManageCredentials),
	"PATCH /api/v1/workspace/slug":       alwaysAsk(auth.PermWorkspaceUpdate),
	// Connecting Gmail grants Colliedesk a sending identity (and the consent address is for a person's
	// browser); disconnecting changes the address customers get replies from.
	"POST " + gmailPath:   alwaysAsk(auth.PermChannelsManageCredentials),
	"DELETE " + gmailPath: alwaysAsk(auth.PermChannelsManageCredentials),
	// Microsoft 365 the same way: the sign-in address is for a person's browser.
	"POST " + microsoftPath:   alwaysAsk(auth.PermChannelsManageCredentials),
	"DELETE " + microsoftPath: alwaysAsk(auth.PermChannelsManageCredentials),

	// Imports read and write the whole workspace: admin. An undo deletes what an import brought in, so
	// every token asks a person. The two routes that take a Freshdesk key are refused below admin
	// (credentialRoutes) rather than filed: an agent request keeps its body.
	"GET /api/v1/imports":              grant(auth.PermImportsRead),
	"POST /api/v1/imports":             grant(auth.PermImportsManageCredentials),
	"GET /api/v1/imports/{id}":         grant(auth.PermImportsRead),
	"POST /api/v1/imports/{id}/start":  grant(auth.PermImportsRun),
	"POST /api/v1/imports/{id}/cancel": grant(auth.PermImportsRun),
	"PUT /api/v1/imports/{id}/key":     grant(auth.PermImportsManageCredentials),
	"GET /api/v1/imports/{id}/report":  grant(auth.PermImportsRead),
	"POST /api/v1/imports/{id}/undo":   alwaysAsk(auth.PermImportsUndo),
	// A link only changes who wrote imported replies and holds imported tickets, and is taken back
	// the same way.
	"GET /api/v1/imports/{id}/people":                      grant(auth.PermImportsRead),
	"PUT /api/v1/imports/{id}/people/{externalId}/link":    grant(auth.PermImportsRun),
	"DELETE /api/v1/imports/{id}/people/{externalId}/link": grant(auth.PermImportsRun),
	// AI settings show no key, so reading them is read. Providers are admin; the three routes that carry
	// a provider key (add, list its models, new key) are credential routes. Setting a feature decides whether ticket data leaves for a
	// provider, so every token's call waits for a person.
	"GET /api/v1/ai":                       grant(auth.PermAISettingsRead),
	"POST /api/v1/ai/providers":            grant(auth.PermAISettingsManageCredentials),
	"POST /api/v1/ai/models":               grant(auth.PermAISettingsManageCredentials),
	"PATCH /api/v1/ai/providers/{id}":      grant(auth.PermAISettingsUpdate),
	"PUT /api/v1/ai/providers/{id}/key":    grant(auth.PermAISettingsManageCredentials),
	"POST /api/v1/ai/providers/{id}/check": grant(auth.PermAISettingsUpdate),
	"DELETE /api/v1/ai/providers/{id}":     grant(auth.PermAISettingsUpdate),
	"PUT /api/v1/ai/slots/{slot}":          alwaysAsk(auth.PermAISettingsUpdate),
	// Usage shows what the workspace spent, admin like billing. The automatic limits only lower or
	// raise how much automatic work runs; the monthly allowance still bounds the money: admin.
	"GET /api/v1/ai/usage":            grant(auth.PermBillingRead),
	"PUT /api/v1/ai/automatic-limits": grant(auth.PermAISettingsUpdate),
	// An AI draft never reaches a customer: a person edits and sends it, like a note, so asking for one
	// and discarding one are draft; reading them is read.
	"POST /api/v1/conversations/{id}/ai-drafts":                   grant(auth.PermAIAssistCreate),
	"GET /api/v1/conversations/{id}/ai-drafts":                    grant(auth.PermAIAssistRead),
	"GET /api/v1/conversations/{id}/ai-drafts/{draftId}":          grant(auth.PermAIAssistRead),
	"POST /api/v1/conversations/{id}/ai-drafts/{draftId}/discard": grant(auth.PermAIAssistCreate),
	// A triage suggestion changes nothing until accepted; accepting changes tags, priority and assignee,
	// like PATCH /api/v1/conversations/{id} (draft).
	"GET /api/v1/conversations/{id}/ai-triage":                         grant(auth.PermAIAssistRead),
	"POST /api/v1/conversations/{id}/ai-triage":                        grant(auth.PermAIAssistCreate),
	"POST /api/v1/conversations/{id}/ai-triage/{suggestionId}/accept":  grant(auth.PermAIAssistCreate),
	"POST /api/v1/conversations/{id}/ai-triage/{suggestionId}/dismiss": grant(auth.PermAIAssistCreate),
	"POST /api/v1/conversations/{id}/ai-triage/{suggestionId}/undo":    grant(auth.PermAIAssistCreate),

	"GET /api/v1/config": notForPeople,
	// Google's redirect after consent: it finishes only with the session cookie of the person who started.
	"GET " + gmailCallbackPath: notForPeople,
	// Microsoft's redirects: the consent finishes like Gmail's; the admin consent stores nothing.
	"GET " + microsoftCallbackPath:                  notForPeople,
	"GET " + microsoftAdminConsentPath:              notForPeople,
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
	"POST " + webFormChannelPattern:                 notForPeople,
	"GET " + exportLinkPath + "{token}":             notForPeople,
}

// TokenAccessTable returns a copy of the classification, for the tests that hold it to Routes() and
// the MCP catalog.
func TokenAccessTable() map[string]TokenAccess { return maps.Clone(tokenTiers) }

// gateToken answers for an agent token whose call may not run as it is and reports true when it did:
// a call the token holds no permission for, or on an Approval route, becomes an agent request (202);
// a PersonOnly or closed route is refused (403). A full session is never stopped here.
func (a *API) gateToken(response http.ResponseWriter, request *http.Request, route Route, who auth.Principal) bool {
	if !who.IsToken() {
		return false
	}
	access := tokenTiers[route.Method+" "+route.Pattern]
	switch {
	case access.Approval:
	case access.Tier == gatedByHandler:
		return false
	case access.Tier != "" && (access.Permission == "" || who.Allows(access.Permission)):
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
			"This call carries another service's API key, which must never be kept in an agent request for approval. Use an agent token that holds "+access.Permission+" (the admin tier does), or let an admin do it in Settings.")
		return true
	}
	a.fileAgentRequest(response, request, route, who, domain.AgentRequestAction, nil, "")
	return true
}

// credentialRoutes carry another service's API key (a helpdesk to import from, an AI provider, a channel's
// account): a token
// below their tier is refused, never filed, because an agent request stores the call's body (and the
// export carries it).
var credentialRoutes = map[string]bool{
	"POST /api/v1/imports":                  true,
	"PUT /api/v1/imports/{id}/key":          true,
	"POST /api/v1/ai/providers":             true,
	"POST /api/v1/ai/models":                true,
	"PUT /api/v1/ai/providers/{id}/key":     true,
	"PUT /api/v1/channels/{id}/credentials": true,
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
