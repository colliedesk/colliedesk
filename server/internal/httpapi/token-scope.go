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
// admin: workspace settings (the help center's on and off, its brand and its custom domain among them), the team, revoking invites, channels and sessions. An admin token acts
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
	"PUT /api/v1/kb/order":                                       grant(auth.PermKBArticlesUpdate),
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
	"GET /api/v1/help-center/domain":                             grant(auth.PermHelpCenterRead),
	"PUT /api/v1/help-center/domain":                             grant(auth.PermHelpCenterUpdate),
	"DELETE /api/v1/help-center/domain":                          grant(auth.PermHelpCenterUpdate),
	"POST /api/v1/help-center/domain/check":                      grant(auth.PermHelpCenterUpdate),
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
	"POST /api/v1/results/share-link":            alwaysAsk(auth.PermResultsShare),
	"DELETE /api/v1/results/share-link":          grant(auth.PermResultsShare),
	"POST /api/v1/results/shared":                notForPeople,
	"PATCH /api/v1/agents/me/results-email":      grant(auth.PermProfileUpdate),
	"POST /api/v1/tags":                          grant(auth.PermTagsCreate),
	"PATCH /api/v1/tags/{id}":                    grant(auth.PermTagsUpdate),
	"DELETE /api/v1/tags/{id}":                   grant(auth.PermTagsDelete),
	"GET /api/v1/agent-requests":                 grant(auth.PermAgentRequestsRead),
	"GET /api/v1/agent-requests/{id}":            grant(auth.PermAgentRequestsRead),
	"POST /api/v1/agent-requests/{id}/cancel":    grant(auth.PermAgentRequestsCancel),
	"POST " + problemReportPath:                  anyToken,
	"GET /api/v1/help-center/theme":              grant(auth.PermHelpCenterRead),
	"GET /api/v1/help-center/search-console":     grant(auth.PermHelpCenterRead),
	"GET /api/v1/help-center/freshdesk-redirect": grant(auth.PermHelpCenterRead),
	"POST /api/v1/help-center/theme/check":       grant(auth.PermHelpCenterRead),
	"POST /api/v1/help-center/theme/preview":     grant(auth.PermHelpCenterRead),

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
	// A test message reaches nobody but the token's own agent: it opens a conversation, like a change
	// that never reaches a customer (draft). Answering it is a reply, which needs send as always.
	"POST /api/v1/channels/email/test-message":        grant(auth.PermConversationsUpdate),
	"POST /api/v1/channels/{id}/email/test-message":   grant(auth.PermConversationsUpdate),
	"GET /api/v1/channels/web-form":                   grant(auth.PermChannelsRead),
	"PATCH /api/v1/agents/me/timezone":                grant(auth.PermProfileUpdate),
	"PATCH /api/v1/agents/me/display-name":            grant(auth.PermProfileUpdate),
	"GET /api/v1/billing":                             grant(auth.PermBillingRead),
	"PATCH /api/v1/workspace/timezone":                grant(auth.PermWorkspaceUpdate),
	"PATCH /api/v1/workspace/name":                    grant(auth.PermWorkspaceUpdate),
	"PATCH /api/v1/agents/{id}":                       grant(auth.PermTeamManageRoles),
	"DELETE /api/v1/auth/invites/{id}":                grant(auth.PermTeamRevoke),
	"PUT /api/v1/channels/web-form":                   grant(auth.PermChannelsUpdate),
	"PUT /api/v1/help-center":                         grant(auth.PermHelpCenterUpdate),
	"PUT /api/v1/channels/email":                      grant(auth.PermChannelsUpdate),
	"POST /api/v1/channels/email/domain-verification": grant(auth.PermChannelsUpdate),
	"DELETE /api/v1/auth/sessions/{id}":               grant(auth.PermTeamRevoke),
	"PUT /api/v1/help-center/theme":                   grant(auth.PermHelpCenterUpdate),
	"PUT /api/v1/help-center/search-console":          grant(auth.PermHelpCenterUpdate),

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
	"GET /api/v1/ai/usage":      grant(auth.PermBillingRead),
	"GET /api/v1/ai/chat/usage": grant(auth.PermBillingRead),
	// A chat's transcript is an end user's messages, read like a conversation's; deleting one is for good.
	"GET /api/v1/ai/chat/sessions":         grant(auth.PermMessagesRead),
	"GET /api/v1/ai/chat/sessions/{id}":    grant(auth.PermMessagesRead),
	"DELETE /api/v1/ai/chat/sessions/{id}": alwaysAsk(auth.PermAISettingsUpdate),
	// A trial answer costs money and reaches no customer, like a draft.
	"POST /api/v1/ai/chat/preview":    grant(auth.PermAIAssistCreate),
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
	// Autofix: reading fixes and their timelines changes nothing (what was sent is the team's masked
	// text, never a key).
	"GET /api/v1/autofix/status":           grant(auth.PermAutofixRead),
	"GET /api/v1/autofix/fixes":            grant(auth.PermAutofixRead),
	"GET /api/v1/autofix/fixes/{id}":       grant(auth.PermAutofixRead),
	"GET /api/v1/conversations/{id}/fixes": grant(auth.PermAutofixRead),
	// Autofix settings: reading them shows no key. Changing them is admin; installing the App answers a
	// one-time address, removing it uninstalls the App on the customer's GitHub, and letting notes go
	// with fixes sends internal notes out, so a person approves those three.
	"GET /api/v1/autofix/settings":                               grant(auth.PermAutofixSettingsRead),
	"GET /api/v1/autofix/github/installations/{id}/repositories": grant(auth.PermAutofixSettingsRead),
	"POST /api/v1/autofix/github/installations":                  alwaysAsk(auth.PermAutofixSettingsUpdate),
	"DELETE /api/v1/autofix/github/installations/{id}":           alwaysAsk(auth.PermAutofixSettingsUpdate),
	"POST /api/v1/autofix/repos":                                 grant(auth.PermAutofixSettingsUpdate),
	"PATCH /api/v1/autofix/repos/{id}":                           grant(auth.PermAutofixSettingsUpdate),
	"DELETE /api/v1/autofix/repos/{id}":                          grant(auth.PermAutofixSettingsUpdate),
	"PUT /api/v1/autofix/repos/{id}/include-notes":               alwaysAsk(auth.PermAutofixSettingsUpdate),
	"DELETE /api/v1/autofix/repos/{id}/include-notes":            grant(auth.PermAutofixSettingsUpdate),
	// Autofix fixes: a preview changes nothing. Sending (and retrying) takes ticket data out to a coding
	// agent as the person who approves, after their GitHub confirmation, so every token's call waits for
	// a person, who gets the confirmation address. A person's changes to a fix stay in Colliedesk, except
	// that a cancel closes the fix's issue.
	"POST /api/v1/conversations/{id}/fixes/preview":  grant(auth.PermAutofixRead),
	"POST /api/v1/conversations/{id}/fixes":          alwaysAsk(auth.PermAutofixSend),
	"POST /api/v1/autofix/fixes/{id}/retry":          alwaysAsk(auth.PermAutofixSend),
	"POST /api/v1/autofix/fixes/{id}/cancel":         grant(auth.PermAutofixUpdate),
	"PUT /api/v1/autofix/fixes/{id}/deployed":        grant(auth.PermAutofixUpdate),
	"DELETE /api/v1/autofix/fixes/{id}/deployed":     grant(auth.PermAutofixUpdate),
	"PUT /api/v1/autofix/fixes/{id}/pull-request":    grant(auth.PermAutofixUpdate),
	"DELETE /api/v1/autofix/fixes/{id}/pull-request": grant(auth.PermAutofixUpdate),
	// Turning a coding agent on decides that ticket data leaves for that vendor, with the admin's
	// consent on record, so every token's call waits for a person (like PUT /api/v1/ai/slots/{slot}).
	// Turning it off and removing Cursor's key only stop new fixes: admin. A check reads what the
	// settings show and tries the key, changing nothing but its result. Setting Cursor's key carries
	// the key, a credential route.
	"PUT /api/v1/autofix/connectors/{kind}":            alwaysAsk(auth.PermAutofixSettingsUpdate),
	"DELETE /api/v1/autofix/connectors/{kind}":         grant(auth.PermAutofixSettingsUpdate),
	"POST /api/v1/autofix/connectors/{kind}/check":     grant(auth.PermAutofixSettingsRead),
	"PUT /api/v1/autofix/connectors/cursor_api/key":    grant(auth.PermAutofixSettingsManageCredentials),
	"DELETE /api/v1/autofix/connectors/cursor_api/key": grant(auth.PermAutofixSettingsManageCredentials),

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
	// GitHub's redirect after installing the App: it finishes only with the session cookie of the person
	// who started. The App's webhook is signed with its secret.
	"GET " + autofixCallbackPath:        notForPeople,
	"POST " + githubWebhookPath:         notForPeople,
	"POST " + resendWebhookPath:         notForPeople,
	"GET " + foundingPath:               notForPeople,
	"GET " + helpCenterTLSAskPath:       notForPeople,
	"POST " + webFormSubmitPattern:      notForPeople,
	"POST " + webFormChannelPattern:     notForPeople,
	"GET " + exportLinkPath + "{token}": notForPeople,
}

// TokenAccessTable returns a copy of the classification, for the tests that hold it to Routes() and
// the MCP catalog.
func TokenAccessTable() map[string]TokenAccess { return maps.Clone(tokenTiers) }

// helpCenterPublicRoutes are the channel types' routes below every help center
// (channels.HelpCenterRoute), keyed "METHOD path below the help center's root", with why no token tier
// classifies them. Like the public form, they are end users': no session and no token ever reaches a
// help center host, so no MCP tool mirrors them; what the team does with the chat is under
// /api/v1/ai/chat. TestEveryHelpCenterRouteIsListedAndInTheContract keeps this list, the routes the
// types register and the contract's HelpCenterChat paths equal.
var helpCenterPublicRoutes = map[string]string{
	"GET /chat":                             "the chat page an end user opens",
	"GET /chat/chat.js":                     "the chat page's own script",
	"GET /chat/chat.css":                    "the chat page's own style sheet",
	"GET /chat/widget.js":                   "the script a company's site loads",
	"GET /chat/widget-config":               "what that script draws, for a site the channel lists",
	"POST /chat/transfer":                   "an end user moving the chat from a site's frame to the help center",
	"GET /chat/session":                     "an end user's chat, read back by its cookie",
	"POST /chat/session/end":                "an end user's New chat",
	"POST /chat/turns":                      "an end user's question",
	"GET /chat/turns/{turn_id}":             "an answer of the end user's chat, read back after a broken stream",
	"PUT /chat/turns/{turn_id}/feedback":    "an end user's rating of an answer",
	"DELETE /chat/turns/{turn_id}/feedback": "an end user taking a rating back",
	"POST /chat/prewarm":                    "the chat page warming the AI's cache as the end user starts typing",
}

// HelpCenterPublicRoutes is helpCenterPublicRoutes for the gate test.
func HelpCenterPublicRoutes() map[string]string { return maps.Clone(helpCenterPublicRoutes) }

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
	"POST /api/v1/imports":                          true,
	"PUT /api/v1/imports/{id}/key":                  true,
	"POST /api/v1/ai/providers":                     true,
	"POST /api/v1/ai/models":                        true,
	"PUT /api/v1/ai/providers/{id}/key":             true,
	"PUT /api/v1/channels/{id}/credentials":         true,
	"PUT /api/v1/autofix/connectors/cursor_api/key": true,
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
