package httpapi

import "net/http"

func (a *API) routeSpecs() []routeSpec {
	routes := Routes()
	handlers := []http.HandlerFunc{
		a.getConfig, a.exchangeCloudSession,
		a.redeem, a.logout, a.me, a.listSessions, a.revokeSession,
		a.createInvite, a.listInvites, a.revokeInvite,
		a.listConversations, a.createConversation, a.getConversation, a.updateConversation,
		a.addConversationTag, a.removeConversationTag,
		a.listMessages, a.createMessage,
		a.listContacts, a.createContact, a.getContact,
		a.listAgents, a.updateWorkspaceTimezone, a.updateAgentTimezone,
		a.listTags, a.createTag, a.updateTag, a.deleteTag,
	}
	result := make([]routeSpec, len(routes))
	for index := range routes {
		result[index] = routeSpec{Route: routes[index], handler: handlers[index]}
	}
	return result
}
