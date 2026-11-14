package httpapi

import (
	"maps"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/mailimage"
)

// TokenAccess says what one route of Routes() needs, for a person's session and an agent token alike:
// the one table both are checked against (authorize).
//
//   - need(p): the caller must hold p (a token: was given p, and its person holds p). Permission is p.
//   - byHandler(p): the handler checks by what the body asks; p is the least any call needs. ByHandler.
//   - anyone: every signed-in session and every token (who am I, sign out, one's own sessions). Anyone.
//   - personOnly(why) / personOnlyNeeding(p, why): a person's session only, needing p when given; an
//     agent token gets 403 person_only. Closed is PersonOnly, PersonNeeds p, Why the reason.
//   - notForPeople: no session reaches it (public pages, sign-in, webhooks).
//
// Operator console rows keep their own scheme (Tier "operator", Approval for operator requests); no
// workspace session or token reaches them.
type TokenAccess struct {
	Permission  string
	ByHandler   bool
	Anyone      bool
	Tier        string
	Approval    bool
	Closed      string
	Why         string
	PersonNeeds string
}

// Why a route is closed to every token.
const (
	// PersonOnly: only a person's own session; an agent token gets 403 person_only, and Why says why.
	PersonOnly = "person_only"
	// NotForPeople: no session reaches it (public pages, sign-in, provider webhooks), so no token does.
	NotForPeople = "not_for_people"
)

func need(permission string) TokenAccess { return TokenAccess{Permission: permission} }

func byHandler(permission string) TokenAccess {
	return TokenAccess{Permission: permission, ByHandler: true}
}

func personOnly(why string) TokenAccess { return TokenAccess{Closed: PersonOnly, Why: why} }

func personOnlyNeeding(permission, why string) TokenAccess {
	return TokenAccess{Closed: PersonOnly, Why: why, PersonNeeds: permission}
}

// Operator console rows: an operator token holds the permission, or every token's call is an operator
// request an operator approves (alwaysAsk).
func grant(permission string) TokenAccess {
	return TokenAccess{Tier: auth.ScopeOperator, Permission: permission}
}

func alwaysAsk(permission string) TokenAccess {
	return TokenAccess{Approval: true, Permission: permission}
}

var (
	// anyone: every signed-in session and token of the workspace.
	anyone       = TokenAccess{Anyone: true}
	notForPeople = TokenAccess{Closed: NotForPeople}
	// anyOperatorToken: an operator token's own session (who am I, sign out).
	anyOperatorToken = TokenAccess{Tier: auth.ScopeOperator, Anyone: true}
)

// routeAccess says what every route in Routes() needs, keyed "METHOD pattern". A route missing here is
// closed to everyone but the routes no session reaches; TestEveryRouteIsClassifiedForAgentsAndCoveredByAnMCPTool
// keeps a new route from reaching main without a row. A person and an agent token are checked against
// the same row: what a person may do in the app, a token given the same permission does at once, and
// a refusal (403 permission_missing) names the permission and who lacks it.
var routeAccess = map[string]TokenAccess{
	"GET /api/v1/roles":                                          need(auth.PermTeamRead),
	"POST /api/v1/roles":                                         need(auth.PermTeamManageRoles),
	"GET /api/v1/roles/{id}":                                     need(auth.PermTeamRead),
	"PATCH /api/v1/roles/{id}":                                   need(auth.PermTeamManageRoles),
	"DELETE /api/v1/roles/{id}":                                  need(auth.PermTeamManageRoles),
	"POST /api/v1/roles/{id}/reset":                              need(auth.PermTeamManageRoles),
	"POST /api/v1/roles/{id}/restore":                            need(auth.PermTeamManageRoles),
	"PUT /api/v1/agents/{id}/roles":                              need(auth.PermTeamManageRoles),
	"POST " + replyDraftsPath:                                    need(auth.PermAIAssistCreate),
	"POST /api/v1/auth/logout":                                   anyone,
	"GET /api/v1/auth/me":                                        anyone,
	"GET /api/v1/conversations":                                  need(auth.PermConversationsRead),
	"PATCH /api/v1/conversations":                                need(auth.PermConversationsUpdate),
	"GET /api/v1/inbox/activity":                                 need(auth.PermConversationsRead),
	"GET /api/v1/saved-replies":                                  need(auth.PermSavedRepliesRead),
	"GET /api/v1/saved-replies/trash":                            need(auth.PermSavedRepliesRead),
	"GET /api/v1/saved-replies/{id}/versions":                    need(auth.PermSavedRepliesRead),
	"POST /api/v1/saved-replies":                                 need(auth.PermSavedRepliesCreate),
	"PATCH /api/v1/saved-replies/{id}":                           need(auth.PermSavedRepliesUpdate),
	"DELETE /api/v1/saved-replies/{id}":                          need(auth.PermSavedRepliesDelete),
	"POST /api/v1/saved-replies/{id}/restore":                    need(auth.PermSavedRepliesDelete),
	"POST /api/v1/saved-replies/{id}/versions/{version}/restore": need(auth.PermSavedRepliesUpdate),
	"GET /api/v1/saved-views":                                    need(auth.PermSavedViewsRead),
	"GET /api/v1/saved-views/trash":                              need(auth.PermSavedViewsRead),
	"POST /api/v1/saved-views":                                   need(auth.PermSavedViewsCreate),
	"PATCH /api/v1/saved-views/{id}":                             need(auth.PermSavedViewsUpdate),
	"DELETE /api/v1/saved-views/{id}":                            need(auth.PermSavedViewsDelete),
	"POST /api/v1/saved-views/{id}/restore":                      need(auth.PermSavedViewsDelete),
	"GET /api/v1/macros":                                         need(auth.PermMacrosRead),
	"GET /api/v1/macros/trash":                                   need(auth.PermMacrosRead),
	"POST /api/v1/macros":                                        need(auth.PermMacrosCreate),
	"PATCH /api/v1/macros/{id}":                                  need(auth.PermMacrosUpdate),
	"DELETE /api/v1/macros/{id}":                                 need(auth.PermMacrosDelete),
	"POST /api/v1/macros/{id}/restore":                           need(auth.PermMacrosDelete),
	"POST /api/v1/conversations/{id}/macros/{macroId}/apply":     need(auth.PermConversationsUpdate),
	"POST /api/v1/conversations/{id}/macro-applications/{applicationId}/undo": need(auth.PermConversationsUpdate),
	"GET /api/v1/kb/collections":                                                     need(auth.PermKBArticlesRead),
	"GET /api/v1/kb/collections/trash":                                               need(auth.PermKBArticlesRead),
	"POST /api/v1/kb/collections":                                                    need(auth.PermKBArticlesCreate),
	"PATCH /api/v1/kb/collections/{id}":                                              need(auth.PermKBArticlesUpdate),
	"DELETE /api/v1/kb/collections/{id}":                                             need(auth.PermKBArticlesDelete),
	"POST /api/v1/kb/collections/{id}/restore":                                       need(auth.PermKBArticlesDelete),
	"GET /api/v1/kb/articles":                                                        need(auth.PermKBArticlesRead),
	"GET /api/v1/kb/articles/trash":                                                  need(auth.PermKBArticlesRead),
	"GET /api/v1/kb/articles/{id}":                                                   need(auth.PermKBArticlesRead),
	"GET /api/v1/kb/articles/{id}/versions":                                          need(auth.PermKBArticlesRead),
	"POST /api/v1/kb/articles":                                                       need(auth.PermKBArticlesCreate),
	"PATCH /api/v1/kb/articles/{id}":                                                 need(auth.PermKBArticlesUpdate),
	"DELETE /api/v1/kb/articles/{id}":                                                need(auth.PermKBArticlesDelete),
	"POST /api/v1/kb/articles/{id}/restore":                                          need(auth.PermKBArticlesDelete),
	"POST /api/v1/kb/articles/{id}/versions/{version}/restore":                       need(auth.PermKBArticlesUpdate),
	"POST /api/v1/kb/articles/{id}/ai-exclusion":                                     need(auth.PermKBArticlesUpdate),
	"DELETE /api/v1/kb/articles/{id}/ai-exclusion":                                   need(auth.PermKBArticlesUpdate),
	"GET /api/v1/kb/translations":                                                    need(auth.PermKBArticlesRead),
	"GET /api/v1/kb/translations/trash":                                              need(auth.PermKBArticlesRead),
	"POST /api/v1/kb/articles/{id}/translations":                                     need(auth.PermKBArticlesCreate),
	"GET /api/v1/kb/articles/{id}/translations/{locale}":                             need(auth.PermKBArticlesRead),
	"PATCH /api/v1/kb/articles/{id}/translations/{locale}":                           need(auth.PermKBArticlesUpdate),
	"DELETE /api/v1/kb/articles/{id}/translations/{locale}":                          need(auth.PermKBArticlesDelete),
	"POST /api/v1/kb/articles/{id}/translations/{locale}/restore":                    need(auth.PermKBArticlesDelete),
	"GET /api/v1/kb/articles/{id}/translations/{locale}/versions":                    need(auth.PermKBArticlesRead),
	"POST /api/v1/kb/articles/{id}/translations/{locale}/versions/{version}/restore": need(auth.PermKBArticlesUpdate),
	"PUT /api/v1/kb/collections/{id}/translations/{locale}":                          need(auth.PermKBArticlesUpdate),
	"DELETE /api/v1/kb/collections/{id}/translations/{locale}":                       need(auth.PermKBArticlesUpdate),
	"POST /api/v1/kb/images":                                                         need(auth.PermKBMediaCreate),
	"GET /api/v1/kb/images/{id}":                                                     need(auth.PermKBMediaRead),
	"GET /api/v1/kb/media":                                                           need(auth.PermKBMediaRead),
	"PATCH /api/v1/kb/media/{id}":                                                    need(auth.PermKBMediaUpdate),
	"GET /api/v1/kb/media/trash":                                                     need(auth.PermKBMediaRead),
	"POST /api/v1/kb/media/trash":                                                    need(auth.PermKBMediaDelete),
	"PUT /api/v1/kb/order":                                                           need(auth.PermKBArticlesUpdate),
	"POST /api/v1/kb/media/trash-unused":                                             need(auth.PermKBMediaDelete),
	"POST /api/v1/kb/media/restore":                                                  need(auth.PermKBMediaDelete),
	"POST /api/v1/kb/media/purge":                                                    need(auth.PermKBMediaPurge),
	"DELETE /api/v1/kb/media/{id}":                                                   need(auth.PermKBMediaDelete),
	"POST /api/v1/kb/media/{id}/restore":                                             need(auth.PermKBMediaDelete),
	"POST /api/v1/kb/media/{id}/replace":                                             need(auth.PermKBMediaUpdate),
	"POST /api/v1/kb/media-replacements/{id}/undo":                                   need(auth.PermKBMediaUpdate),
	"POST /api/v1/kb/media-uploads":                                                  need(auth.PermKBMediaCreate),
	"GET /api/v1/kb/media-uploads":                                                   need(auth.PermKBMediaRead),
	"GET /api/v1/kb/media-uploads/{id}":                                              need(auth.PermKBMediaRead),
	"PUT /api/v1/kb/media-uploads/{id}":                                              need(auth.PermKBMediaCreate),
	"DELETE /api/v1/kb/media-uploads/{id}":                                           need(auth.PermKBMediaCreate),
	"POST /api/v1/kb/media-uploads/{id}/complete":                                    need(auth.PermKBMediaCreate),
	"GET /api/v1/kb/media/{id}":                                                      need(auth.PermKBMediaRead),
	"PUT /api/v1/kb/media/{id}/poster":                                               need(auth.PermKBMediaUpdate),
	"GET /api/v1/kb/media/{id}/poster/{version}":                                     need(auth.PermKBMediaRead),
	"GET /api/v1/kb/media/{id}/details":                                              need(auth.PermKBMediaRead),
	"GET /api/v1/kb/media/{id}/thumbnail":                                            need(auth.PermKBMediaRead),
	"GET /api/v1/help-center/domain":                                                 need(auth.PermHelpCenterRead),
	"PUT /api/v1/help-center/domain":                                                 need(auth.PermHelpCenterUpdate),
	"DELETE /api/v1/help-center/domain":                                              need(auth.PermHelpCenterUpdate),
	"POST /api/v1/help-center/domain/check":                                          need(auth.PermHelpCenterUpdate),
	"GET /api/v1/help-center":                                                        need(auth.PermHelpCenterRead),
	"POST /api/v1/conversations":                                                     need(auth.PermMessagesSend),
	"GET /api/v1/conversations/{id}":                                                 need(auth.PermConversationsRead),
	"PATCH /api/v1/conversations/{id}":                                               need(auth.PermConversationsUpdate),
	"PUT /api/v1/conversations/spam":                                                 need(auth.PermConversationsUpdate),
	"PUT /api/v1/conversations/{id}/tags/{tagId}":                                    need(auth.PermConversationsUpdate),
	"DELETE /api/v1/conversations/{id}/tags/{tagId}":                                 need(auth.PermConversationsUpdate),
	"GET /api/v1/conversations/{id}/messages":                                        need(auth.PermMessagesRead),
	"POST /api/v1/conversations/{id}/messages":                                       byHandler(auth.PermMessagesNote),
	"GET /api/v1/conversations/{id}/time-entries":                                    need(auth.PermConversationsRead),
	"POST /api/v1/conversations/{id}/messages/{messageId}/retry":                     need(auth.PermMessagesSend),
	"GET /api/v1/attachments/{id}":                                                   need(auth.PermMessagesRead),
	"GET /api/v1/attachments/{id}/thumbnail":                                         need(auth.PermMessagesRead),
	"GET /api/v1/contacts":                                                           need(auth.PermContactsRead),
	"POST /api/v1/contacts":                                                          need(auth.PermContactsCreate),
	"GET /api/v1/contacts/{id}":                                                      need(auth.PermContactsRead),
	"PATCH /api/v1/contacts/{id}":                                                    need(auth.PermContactsUpdate),
	"POST /api/v1/contacts/{id}/merges":                                              need(auth.PermContactsUpdate),
	"DELETE /api/v1/contacts/{id}/merges/{mergeId}":                                  need(auth.PermContactsUpdate),
	"GET /api/v1/agents":                                                             need(auth.PermTeamRead),
	"GET /api/v1/tags":                                                               need(auth.PermTagsRead),
	"GET /api/v1/results":                                                            need(auth.PermResultsRead),
	"GET /api/v1/results/share-link":                                                 need(auth.PermResultsShare),
	// A share link puts the numbers outside sign-in: data leaving, so it is a permission of its own.
	"POST /api/v1/results/share-link":            need(auth.PermResultsShare),
	"DELETE /api/v1/results/share-link":          need(auth.PermResultsShare),
	"POST /api/v1/results/shared":                notForPeople,
	"PATCH /api/v1/agents/me/results-email":      need(auth.PermProfileUpdate),
	"GET /api/v1/workspace/setup":                need(auth.PermConversationsRead),
	"PUT /api/v1/agents/me/setup-guide":          need(auth.PermProfileUpdate),
	"POST /api/v1/tags":                          need(auth.PermTagsCreate),
	"PATCH /api/v1/tags/{id}":                    need(auth.PermTagsUpdate),
	"DELETE /api/v1/tags/{id}":                   need(auth.PermTagsDelete),
	"POST " + problemReportPath:                  anyone,
	"GET /api/v1/help-center/theme":              need(auth.PermHelpCenterRead),
	"GET /api/v1/help-center/languages":          need(auth.PermHelpCenterRead),
	"GET /api/v1/help-center/search-console":     need(auth.PermHelpCenterRead),
	"GET /api/v1/help-center/freshdesk-redirect": need(auth.PermHelpCenterRead),
	"POST /api/v1/help-center/theme/check":       need(auth.PermHelpCenterRead),
	"POST /api/v1/help-center/theme/preview":     need(auth.PermHelpCenterRead),

	// Saying a conversation is open changes nothing anyone wrote; a row lives minutes at most.
	"PUT /api/v1/conversations/{id}/presence":               need(auth.PermConversationsRead),
	"DELETE /api/v1/conversations/{id}/presence/{clientId}": need(auth.PermConversationsRead),

	// The operator console: no workspace token reaches these (401); an operator token reads, and asks a
	// person for every change (operator requests, docs/internals/operator-console.md).
	"POST /api/v1/operator/auth/redeem":                  notForPeople,
	"POST /api/v1/operator/auth/totp":                    notForPeople,
	"POST /api/v1/operator/auth/cloud":                   notForPeople,
	"GET /api/v1/operator/auth/me":                       anyOperatorToken,
	"POST /api/v1/operator/auth/logout":                  anyOperatorToken,
	"POST /api/v1/operator/tokens":                       alwaysAsk(auth.PermOperatorManage),
	"GET /api/v1/operator/status":                        grant(auth.PermOperatorRead),
	"GET /api/v1/operator/config":                        grant(auth.PermOperatorRead),
	"PATCH /api/v1/operator/config":                      alwaysAsk(auth.PermOperatorConfigWrite),
	"POST /api/v1/operator/config/check":                 grant(auth.PermOperatorRead),
	"GET /api/v1/operator/config/versions":               grant(auth.PermOperatorRead),
	"POST /api/v1/operator/config/versions/{id}/restore": alwaysAsk(auth.PermOperatorConfigWrite),
	"POST /api/v1/operator/config/trial/confirm":         alwaysAsk(auth.PermOperatorConfigWrite),
	"POST /api/v1/operator/config/trial/revert":          alwaysAsk(auth.PermOperatorConfigWrite),
	"GET /api/v1/operator/billing/products":              grant(auth.PermOperatorRead),
	"POST /api/v1/operator/billing/products":             alwaysAsk(auth.PermOperatorConfigWrite),
	"PATCH /api/v1/operator/billing/products/{id}":       alwaysAsk(auth.PermOperatorConfigWrite),
	"GET /api/v1/operator/audit":                         grant(auth.PermOperatorRead),
	"GET /api/v1/operator/requests":                      grant(auth.PermOperatorRead),
	"GET /api/v1/operator/requests/{id}":                 grant(auth.PermOperatorRead),
	"POST /api/v1/operator/requests/{id}/cancel":         grant(auth.PermOperatorRequestsCancel),
	"GET /api/v1/operator/operators":                     grant(auth.PermOperatorRead),
	"POST /api/v1/operator/operators":                    alwaysAsk(auth.PermOperatorManage),
	"DELETE /api/v1/operator/operators/{id}":             alwaysAsk(auth.PermOperatorManage),
	"DELETE /api/v1/operator/sessions/{id}":              alwaysAsk(auth.PermOperatorManage),

	"POST /api/v1/agent-requests/{id}/approve": personOnly("only the operator console's requests remain, and an operator decides them in a browser; a workspace call answers 410"),
	"GET " + mailimage.ProxyPath:               personOnlyNeeding(auth.PermMessagesRead, "a signed address for the screen: an agent token reads the original HTML and fetches its own images"),
	"POST /api/v1/agent-requests/{id}/reject":  personOnly("only the operator console's requests remain, and an operator decides them in a browser; a workspace call answers 410"),

	"GET /api/v1/auth/sessions":              anyone,
	"GET /api/v1/auth/invites":               need(auth.PermTeamRead),
	"GET /api/v1/channels":                   need(auth.PermChannelsRead),
	"GET /api/v1/channels/events":            need(auth.PermChannelsRead),
	"GET /api/v1/channels/{id}":              need(auth.PermChannelsRead),
	"POST /api/v1/channels":                  need(auth.PermChannelsUpdate),
	"PATCH /api/v1/channels/{id}":            need(auth.PermChannelsUpdate),
	"POST /api/v1/channels/{id}/check":       need(auth.PermChannelsUpdate),
	"POST /api/v1/channels/{id}/held/replay": need(auth.PermChannelsUpdate),
	// A channel's secret needs manage_credentials, a permission apart from changing the channel.
	// Discarding held messages cannot be undone.
	"PUT /api/v1/channels/{id}/credentials":   need(auth.PermChannelsManageCredentials),
	"DELETE /api/v1/channels/{id}":            need(auth.PermChannelsManageCredentials),
	"POST /api/v1/channels/{id}/held/discard": need(auth.PermChannelsUpdate),
	// One email channel of several: the same permissions as the routes of the default one.
	"GET /api/v1/channels/{id}/email": need(auth.PermChannelsRead),
	// The code and link Gmail sent to the channel: read like the channel's settings; the email itself is
	// already a conversation every reader of the inbox sees.
	"GET /api/v1/channels/{id}/email/forwarding-confirmation": need(auth.PermChannelsRead),
	"POST /api/v1/channels/{id}/email/domain-verification":    need(auth.PermChannelsUpdate),
	"POST /api/v1/channels/{id}/email/secret":                 need(auth.PermChannelsManageCredentials),
	"POST " + channelGmailPath:                                need(auth.PermChannelsManageCredentials),
	"DELETE " + channelGmailPath:                              need(auth.PermChannelsManageCredentials),
	"POST " + channelMicrosoftPath:                            need(auth.PermChannelsManageCredentials),
	"DELETE " + channelMicrosoftPath:                          need(auth.PermChannelsManageCredentials),
	"GET /api/v1/channels/email":                              need(auth.PermChannelsRead),
	// A test message reaches nobody but the token's own agent: it opens a conversation, like a change
	// that never reaches a customer (draft). Answering it is a reply, which needs send as always.
	"POST /api/v1/channels/email/test-message":                     need(auth.PermConversationsUpdate),
	"POST /api/v1/channels/{id}/email/test-message":                need(auth.PermConversationsUpdate),
	"GET /api/v1/channels/web-form":                                need(auth.PermChannelsRead),
	"PATCH /api/v1/agents/me/timezone":                             need(auth.PermProfileUpdate),
	"PATCH /api/v1/agents/me/sla-alerts":                           need(auth.PermProfileUpdate),
	"PATCH /api/v1/agents/me/mention-email":                        need(auth.PermProfileUpdate),
	"GET /api/v1/mentions":                                         need(auth.PermMessagesRead),
	"PUT /api/v1/mentions/read":                                    need(auth.PermMessagesRead),
	"PUT /api/v1/conversations/{id}/messages/{messageId}/mentions": need(auth.PermMessagesNote),
	"PATCH /api/v1/agents/me/display-name":                         need(auth.PermProfileUpdate),
	"GET /api/v1/billing":                                          need(auth.PermBillingRead),
	"PATCH /api/v1/workspace/timezone":                             need(auth.PermWorkspaceUpdate),
	"PATCH /api/v1/workspace/name":                                 need(auth.PermWorkspaceUpdate),
	"PATCH /api/v1/agents/{id}":                                    need(auth.PermTeamManageRoles),
	"DELETE /api/v1/auth/invites/{id}":                             need(auth.PermTeamRevoke),
	"PUT /api/v1/channels/web-form":                                need(auth.PermChannelsUpdate),
	"PUT /api/v1/help-center":                                      need(auth.PermHelpCenterUpdate),
	"PUT /api/v1/channels/email":                                   need(auth.PermChannelsUpdate),
	"POST /api/v1/channels/email/domain-verification":              need(auth.PermChannelsUpdate),
	"DELETE /api/v1/auth/sessions/{id}":                            anyone,
	"PUT /api/v1/help-center/theme":                                need(auth.PermHelpCenterUpdate),
	"PUT /api/v1/help-center/languages":                            need(auth.PermHelpCenterUpdate),
	"PUT /api/v1/help-center/search-console":                       need(auth.PermHelpCenterUpdate),

	// Allowing an AI app makes a grant that acts with everything its person holds: a grant that allowed
	// apps would leave them connected after it was disconnected, like a token that made tokens.
	"POST " + oauthAllowRoutePath:            personOnly("an app connected by another app would outlive its disconnection: a person allows each app in a browser"),
	"POST /api/v1/auth/tokens":               personOnlyNeeding(auth.PermTeamCreateTokens, "a token that makes tokens outlives its own revocation: revoking it would leave the tokens it made behind"),
	"POST /api/v1/auth/invites":              need(auth.PermTeamInvite),
	"GET /api/v1/export":                     need(auth.PermWorkspaceExport),
	"POST /api/v1/export/link":               need(auth.PermWorkspaceExport),
	"POST /api/v1/billing/checkout":          need(auth.PermBillingManage),
	"POST /api/v1/billing/portal":            need(auth.PermBillingManage),
	"POST /api/v1/billing/migration-request": need(auth.PermBillingManage),
	// Any member of the offer's workspace may read a trial offer link they were sent; the answer names the
	// members who can accept it. Accepting changes the trial, which is billing.
	"GET /api/v1/trial-offer":                         anyone,
	"POST /api/v1/trial-offer/accept":                 need(auth.PermBillingManage),
	"DELETE /api/v1/channels/email":                   need(auth.PermChannelsManageCredentials),
	"POST /api/v1/channels/email/secret":              need(auth.PermChannelsManageCredentials),
	"PATCH /api/v1/workspace/slug":                    need(auth.PermWorkspaceUpdate),
	"GET /api/v1/conversation-number-series":          need(auth.PermConversationsRead),
	"PUT /api/v1/conversation-number-series/{series}": need(auth.PermWorkspaceUpdate),
	"GET /api/v1/response-targets":                    need(auth.PermConversationsRead),
	"PUT /api/v1/response-targets":                    need(auth.PermWorkspaceUpdate),
	// Connecting Gmail grants Colliedesk a sending identity; disconnecting changes the address customers
	// get replies from. Both are credentials.
	"POST " + gmailPath:   need(auth.PermChannelsManageCredentials),
	"DELETE " + gmailPath: need(auth.PermChannelsManageCredentials),
	// Microsoft 365 the same way: the sign-in address is for a person's browser.
	"POST " + microsoftPath:   need(auth.PermChannelsManageCredentials),
	"DELETE " + microsoftPath: need(auth.PermChannelsManageCredentials),

	// Imports read and write the whole workspace. An undo deletes what an import brought in, so it is a
	// permission of its own; the two routes that take a Freshdesk key need manage_credentials.
	"GET /api/v1/imports":              need(auth.PermImportsRead),
	"POST /api/v1/imports":             need(auth.PermImportsManageCredentials),
	"GET /api/v1/imports/{id}":         need(auth.PermImportsRead),
	"POST /api/v1/imports/{id}/start":  need(auth.PermImportsRun),
	"POST /api/v1/imports/{id}/cancel": need(auth.PermImportsRun),
	"PUT /api/v1/imports/{id}/key":     need(auth.PermImportsManageCredentials),
	"GET /api/v1/imports/{id}/report":  need(auth.PermImportsRead),
	"POST /api/v1/imports/{id}/undo":   need(auth.PermImportsUndo),
	// A link only changes who wrote imported replies and holds imported tickets, and is taken back
	// the same way.
	"GET /api/v1/imports/{id}/people":                      need(auth.PermImportsRead),
	"PUT /api/v1/imports/{id}/people/{externalId}/link":    need(auth.PermImportsRun),
	"DELETE /api/v1/imports/{id}/people/{externalId}/link": need(auth.PermImportsRun),
	// AI settings show no key, so reading them is read. The routes that carry a provider key need
	// manage_credentials; setting a feature decides whether ticket data leaves for a provider.
	"GET /api/v1/ai":                       need(auth.PermAISettingsRead),
	"POST /api/v1/ai/providers":            need(auth.PermAISettingsManageCredentials),
	"POST /api/v1/ai/models":               need(auth.PermAISettingsManageCredentials),
	"PATCH /api/v1/ai/providers/{id}":      need(auth.PermAISettingsUpdate),
	"PUT /api/v1/ai/providers/{id}/key":    need(auth.PermAISettingsManageCredentials),
	"POST /api/v1/ai/providers/{id}/check": need(auth.PermAISettingsUpdate),
	"DELETE /api/v1/ai/providers/{id}":     need(auth.PermAISettingsUpdate),
	"PUT /api/v1/ai/slots/{slot}":          need(auth.PermAISettingsUpdate),
	// A channel's AI level: raising it as well as lowering it needs ai_settings:update.
	"GET /api/v1/channels/{id}/ai": need(auth.PermChannelsRead),
	"PUT " + channelAIRoute:        byHandler(auth.PermAISettingsUpdate),
	// Agreeing that intake may send another kind of data (images, a contact's profile) is data
	// leaving, like turning a feature on; taking it back only lowers.
	"PUT /api/v1/ai/source-consents/{class}":    need(auth.PermAISettingsUpdate),
	"DELETE /api/v1/ai/source-consents/{class}": need(auth.PermAISettingsUpdate),
	// Readiness says whether AI may act on its own; recording an evaluation can open a gate, so it needs
	// ai_settings:update like a raise.
	"GET /api/v1/channels/{id}/ai/intake/readiness":    need(auth.PermChannelsRead),
	"POST /api/v1/channels/{id}/ai/intake/evaluations": need(auth.PermAISettingsUpdate),
	"GET /api/v1/channels/{id}/ai/intake/evaluations":  need(auth.PermChannelsRead),
	// What was said in a chat channel's places, as the conversations' messages are read.
	"GET /api/v1/conversations/{id}/chat":                need(auth.PermMessagesRead),
	"GET /api/v1/channels/{id}/threads":                  need(auth.PermMessagesRead),
	"GET /api/v1/channels/{id}/threads/{threadId}/items": need(auth.PermMessagesRead),
	"GET /api/v1/channels/{id}/senders":                  need(auth.PermMessagesRead),
	"GET /api/v1/channel-thread-items":                   need(auth.PermMessagesRead),
	// The intake queue shows what customers wrote; deciding a card opens a conversation (and may tell
	// the customer it was received), joins one or sets the card aside.
	"GET /api/v1/intake/gatherings":                                   need(auth.PermMessagesRead),
	"GET /api/v1/intake/gatherings/{id}":                              need(auth.PermMessagesRead),
	"GET /api/v1/intake/gatherings/{id}/items/{itemId}/files/{index}": need(auth.PermMessagesRead),
	"POST " + intakeDecideRoute:                                       byHandler(auth.PermConversationsUpdate),
	"POST /api/v1/intake/gatherings/{id}/requeue":                     need(auth.PermConversationsUpdate),
	"GET /api/v1/intake/summary":                                      need(auth.PermConversationsRead),
	"GET /api/v1/intake/stats":                                        need(auth.PermConversationsRead),
	"GET /api/v1/conversations/{id}/intake":                           need(auth.PermMessagesRead),
	// Moving customer messages between conversations sends nothing; reading the moves names no words.
	"POST /api/v1/conversations/{id}/messages/move": need(auth.PermConversationsUpdate),
	"GET /api/v1/conversations/{id}/message-moves":  need(auth.PermConversationsRead),
	"POST /api/v1/message-moves/{id}/undo":          need(auth.PermConversationsUpdate),
	// Usage shows what the workspace spent, like billing. The automatic limits only lower or raise how
	// much automatic work runs; the monthly allowance still bounds the money.
	"GET /api/v1/ai/usage":      need(auth.PermBillingRead),
	"GET /api/v1/ai/chat/usage": need(auth.PermBillingRead),
	// A chat's transcript is an end user's messages, read like a conversation's; deleting one is for good.
	"GET /api/v1/ai/chat/sessions":         need(auth.PermMessagesRead),
	"GET /api/v1/ai/chat/sessions/{id}":    need(auth.PermMessagesRead),
	"DELETE /api/v1/ai/chat/sessions/{id}": need(auth.PermAISettingsUpdate),
	// A trial answer costs money and reaches no customer, like a draft.
	"POST /api/v1/ai/chat/preview":    need(auth.PermAIAssistCreate),
	"PUT /api/v1/ai/automatic-limits": need(auth.PermAISettingsUpdate),
	// An AI draft never reaches a customer: a person edits and sends it, like a note.
	"POST /api/v1/conversations/{id}/ai-drafts":                         need(auth.PermAIAssistCreate),
	"GET /api/v1/conversations/{id}/ai-drafts":                          need(auth.PermAIAssistRead),
	"GET /api/v1/conversations/{id}/ai-drafts/{draftId}":                need(auth.PermAIAssistRead),
	"POST /api/v1/conversations/{id}/ai-drafts/{draftId}/discard":       need(auth.PermAIAssistCreate),
	"POST /api/v1/conversations/{id}/ai-drafts/{draftId}/restore":       need(auth.PermAIAssistCreate),
	"GET /api/v1/conversations/{id}/ai-drafts/{draftId}/files/{fileId}": need(auth.PermAIAssistRead),
	// A triage suggestion changes nothing until accepted; accepting changes tags, priority and assignee,
	// like PATCH /api/v1/conversations/{id} (draft).
	"GET /api/v1/conversations/{id}/ai-triage":                         need(auth.PermAIAssistRead),
	"POST /api/v1/conversations/{id}/ai-triage":                        need(auth.PermAIAssistCreate),
	"POST /api/v1/conversations/{id}/ai-triage/{suggestionId}/accept":  need(auth.PermAIAssistCreate),
	"POST /api/v1/conversations/{id}/ai-triage/{suggestionId}/dismiss": need(auth.PermAIAssistCreate),
	"POST /api/v1/conversations/{id}/ai-triage/{suggestionId}/undo":    need(auth.PermAIAssistCreate),
	// Autofix: reading fixes and their timelines changes nothing (what was sent is the team's masked
	// text, never a key).
	"GET /api/v1/autofix/status":           need(auth.PermAutofixRead),
	"GET /api/v1/autofix/fixes":            need(auth.PermAutofixRead),
	"GET /api/v1/autofix/fixes/{id}":       need(auth.PermAutofixRead),
	"GET /api/v1/conversations/{id}/fixes": need(auth.PermAutofixRead),
	// Autofix settings: reading them shows no key. Installing the App answers a one-time address,
	// removing it uninstalls the App on the customer's GitHub, and letting notes go with fixes sends
	// internal notes out.
	"GET /api/v1/autofix/settings":                               need(auth.PermAutofixSettingsRead),
	"GET /api/v1/autofix/github/installations/{id}/repositories": need(auth.PermAutofixSettingsRead),
	"POST /api/v1/autofix/github/installations":                  need(auth.PermAutofixSettingsUpdate),
	"DELETE /api/v1/autofix/github/installations/{id}":           need(auth.PermAutofixSettingsUpdate),
	"POST /api/v1/autofix/repos":                                 need(auth.PermAutofixSettingsUpdate),
	"PATCH /api/v1/autofix/repos/{id}":                           need(auth.PermAutofixSettingsUpdate),
	"DELETE /api/v1/autofix/repos/{id}":                          need(auth.PermAutofixSettingsUpdate),
	"PUT /api/v1/autofix/repos/{id}/include-notes":               need(auth.PermAutofixSettingsUpdate),
	"DELETE /api/v1/autofix/repos/{id}/include-notes":            need(auth.PermAutofixSettingsUpdate),
	// Autofix fixes: a preview changes nothing. Sending (and retrying) takes ticket data out to a coding
	// agent: an issue connector after the caller's GitHub authorization, Cursor at once with the
	// workspace's key; a token sends only the text the preview built. A person's changes to a fix stay in
	// Colliedesk, except that a cancel closes the fix's issue.
	"POST /api/v1/conversations/{id}/fixes/preview":  need(auth.PermAutofixRead),
	"POST /api/v1/conversations/{id}/fixes":          need(auth.PermAutofixSend),
	"POST /api/v1/autofix/fixes/{id}/retry":          need(auth.PermAutofixSend),
	"POST /api/v1/autofix/fixes/{id}/cancel":         need(auth.PermAutofixUpdate),
	"PUT /api/v1/autofix/fixes/{id}/deployed":        need(auth.PermAutofixUpdate),
	"DELETE /api/v1/autofix/fixes/{id}/deployed":     need(auth.PermAutofixUpdate),
	"PUT /api/v1/autofix/fixes/{id}/pull-request":    need(auth.PermAutofixUpdate),
	"DELETE /api/v1/autofix/fixes/{id}/pull-request": need(auth.PermAutofixUpdate),
	// Turning a coding agent on decides that ticket data leaves for that vendor, with the caller's
	// consent on record. Turning it off and removing Cursor's key only stop new fixes. A check reads what the
	// settings show and tries the key, changing nothing but its result. Setting Cursor's key carries
	// the key, a credential route.
	"PUT /api/v1/autofix/connectors/{kind}":            need(auth.PermAutofixSettingsUpdate),
	"DELETE /api/v1/autofix/connectors/{kind}":         need(auth.PermAutofixSettingsUpdate),
	"POST /api/v1/autofix/connectors/{kind}/check":     need(auth.PermAutofixSettingsRead),
	"PUT /api/v1/autofix/connectors/cursor_api/key":    need(auth.PermAutofixSettingsManageCredentials),
	"DELETE /api/v1/autofix/connectors/cursor_api/key": need(auth.PermAutofixSettingsManageCredentials),

	"GET /api/v1/config": notForPeople,
	// The consent page reads which app asks before anyone signs in; it answers no workspace data.
	"GET " + oauthClientRoutePath: notForPeople,
	// Google's redirect after consent: it finishes only with the session cookie of the person who started.
	"GET " + gmailCallbackPath: notForPeople,
	// Microsoft's redirects: the consent finishes like Gmail's; the admin consent stores nothing.
	"GET " + microsoftCallbackPath:     notForPeople,
	"GET " + microsoftAdminConsentPath: notForPeople,
	"POST /api/v1/auth/cloud/exchange": notForPeople,
	// The account menu's switch: it takes the identity provider's token, never a session or an agent token.
	"GET " + cloudWorkspacesPath:                    notForPeople,
	"POST " + cloudSignupPath:                       notForPeople,
	"POST /api/v1/auth/redeem":                      notForPeople,
	"GET /api/v1/auth/invite-preview":               notForPeople,
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

// RouteAccessTable returns a copy of routeAccess, for the tests that hold it to Routes() and the MCP
// catalog (an MCP tool needs what its routes need).
func RouteAccessTable() map[string]TokenAccess { return maps.Clone(routeAccess) }

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
	"POST /chat/handoff":                    "an end user handing the chat to the team (Talk to a person)",
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
