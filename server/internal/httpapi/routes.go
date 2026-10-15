package httpapi

import "net/http"

func (a *API) routeSpecs() []routeSpec {
	routes := Routes()
	handlers := []http.HandlerFunc{
		a.getConfig, a.exchangeCloudSession, a.signUpCloud,
		a.redeem, a.logout, a.me, a.listSessions, a.revokeSession,
		a.createInvite, a.listInvites, a.revokeInvite,
		a.listConversations, a.createConversation, a.bulkUpdateConversations, a.getConversation, a.updateConversation,
		a.addConversationTag, a.removeConversationTag,
		a.listMessages, a.createMessage, a.listTimeEntries, a.downloadAttachment,
		a.listContacts, a.createContact, a.getContact,
		a.listAgents, a.updateWorkspaceTimezone, a.updateWorkspaceSlug, a.updateAgentTimezone, a.updateAgent,
		a.listTags, a.createTag, a.updateTag, a.deleteTag,
		a.listSavedReplies, a.createSavedReply, a.updateSavedReply, a.deleteSavedReply,
		a.retryMessageDelivery,
		a.getEmailChannel, a.putEmailChannel, a.deleteEmailChannel, a.verifyEmailDomain, a.receiveInboundEmail, a.checkInboundRecipient, a.rotateEmailSecret,
		a.getBilling, a.createCheckout, a.createPortal, a.receivePolarWebhook,
		a.exportWorkspace,
		a.getWebForm, a.putWebForm, a.submitWebForm,
		a.createExportLink, a.downloadExport,
	}
	result := make([]routeSpec, len(routes))
	for index := range routes {
		result[index] = routeSpec{Route: routes[index], handler: handlers[index]}
	}
	return result
}
