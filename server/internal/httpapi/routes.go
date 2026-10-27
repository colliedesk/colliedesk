package httpapi

import "net/http"

func (a *API) routeSpecs() []routeSpec {
	routes := Routes()
	handlers := []http.HandlerFunc{
		a.getConfig, a.exchangeCloudSession, a.signUpCloud,
		a.redeem, a.logout, a.me, a.listSessions, a.revokeSession, a.createAgentToken,
		a.createInvite, a.listInvites, a.revokeInvite,
		a.listConversations, a.createConversation, a.bulkUpdateConversations, a.getConversation, a.updateConversation,
		a.addConversationTag, a.removeConversationTag,
		a.listMessages, a.createMessage, a.listTimeEntries, a.putConversationPresence, a.deleteConversationPresence, a.inboxActivity, a.downloadAttachment, a.mailImage,
		a.listContacts, a.createContact, a.getContact, a.updateContact, a.mergeContact, a.unmergeContact,
		a.listAgents, a.updateWorkspaceTimezone, a.updateWorkspaceSlug, a.updateWorkspaceName, a.updateAgentTimezone, a.updateAgentDisplayName, a.updateAgent,
		a.listTags, a.createTag, a.updateTag, a.deleteTag,
		a.listSavedReplies, a.createSavedReply, a.updateSavedReply, a.deleteSavedReply,
		a.listTrashedSavedReplies, a.restoreSavedReply, a.listSavedReplyVersions, a.restoreSavedReplyVersion,
		a.listSavedViews, a.createSavedView, a.listDeletedSavedViews, a.updateSavedView, a.deleteSavedView, a.restoreSavedView,
		a.listKBCollections, a.createKBCollection, a.listTrashedKBCollections, a.updateKBCollection, a.deleteKBCollection, a.restoreKBCollection,
		a.listKBArticles, a.createKBArticle, a.listTrashedKBArticles, a.getKBArticle, a.updateKBArticle, a.deleteKBArticle, a.restoreKBArticle,
		a.listKBArticleVersions, a.restoreKBArticleVersion, a.excludeKBArticleFromAI, a.includeKBArticleInAI, a.uploadKBImage, a.getKBImage,
		a.listKBMedia, a.updateKBMedia,
		a.listTrashedKBMedia, a.trashKBMedia, a.trashUnusedKBMedia, a.restoreKBMediaBatch, a.purgeKBMedia, a.trashOneKBMedia, a.restoreKBMedia, a.replaceKBMedia, a.undoKBMediaReplacement,
		a.createKBMediaUpload, a.listKBMediaUploads, a.getKBMediaUpload, a.putKBMediaUploadPart, a.cancelKBMediaUpload, a.completeKBMediaUpload, a.getKBMediaFile, a.putKBVideoPoster, a.getKBVideoPoster,
		a.getKBMediaDetails, a.getKBMediaThumbnail, a.getHelpCenter, a.putHelpCenter, a.askHelpCenterCertificate,
		a.retryMessageDelivery,
		a.getEmailChannel, a.putEmailChannel, a.deleteEmailChannel, a.verifyEmailDomain, a.receiveInboundEmail, a.checkInboundRecipient, a.rotateEmailSecret,
		a.connectGmail, a.disconnectGmail, a.finishGmailConnect,
		a.getBilling, a.createCheckout, a.createPortal, a.receivePolarWebhook, a.receiveResendWebhook, a.getFounding,
		a.exportWorkspace,
		a.getWebForm, a.putWebForm, a.submitWebForm,
		a.createExportLink, a.downloadExport,
		a.listAgentRequests, a.getAgentRequest, a.approveAgentRequest, a.rejectAgentRequest, a.cancelAgentRequest,
		a.createProblemReport,
		a.listImports, a.getImport, a.setImportKey, a.cancelImport, a.startImport, a.previewImport, a.getImportReport, a.undoImport,
		a.listImportPeople, a.linkImportPerson, a.unlinkImportPerson,
		a.getAISettings, a.addAIProvider, a.listAIModels, a.updateAIProvider, a.setAIProviderKey, a.checkAIProvider, a.setAISlot, a.removeAIProvider,
		a.getAIUsage, a.setAIAutomaticLimits,
		a.createAIDraft, a.listAIDrafts, a.getAIDraft, a.discardAIDraft,
		a.getAITriage, a.createAITriage, a.acceptAITriage, a.dismissAITriage, a.undoAITriage,
		a.getResults, a.getResultsShareLink, a.createResultsShareLink, a.revokeResultsShareLink, a.getSharedResults, a.setResultsEmail,
		a.listChannels, a.createChannel, a.listChannelEvents, a.getChannel, a.updateChannel, a.removeChannel, a.setChannelCredential, a.checkChannel,
		a.replayHeldChannelMessages, a.discardHeldChannelMessages,
		a.getEmailChannel, a.verifyEmailDomain, a.rotateEmailSecret, a.connectGmail, a.disconnectGmail, a.submitWebForm,
		a.connectMicrosoft, a.disconnectMicrosoft, a.finishMicrosoftConnect, a.finishMicrosoftAdminConsent,
		a.connectMicrosoft, a.disconnectMicrosoft,
		a.getAutofixStatus, a.listFixes, a.getFix, a.listConversationFixes,
		a.getAutofixSettings, a.connectGitHub, a.disconnectGitHub, a.listGitHubRepositories, a.finishGitHubAuthorization,
		a.connectRepo, a.updateRepo, a.disconnectRepo, a.includeNotesInFixes, a.excludeNotesFromFixes, a.receiveGitHubWebhook,
	}
	result := make([]routeSpec, len(routes))
	for index := range routes {
		result[index] = routeSpec{Route: routes[index], handler: handlers[index]}
	}
	return result
}
