package httpapi

import (
	"context"
	"net/http"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// requestKBArticle files the conversation's request for an article in Knowledge › Gaps: in the gap
// gap_id names, else in an open gap with close words, else in a new one. 201 when it changed the
// request, 200 when the same request was already filed.
func (a *API) requestKBArticle(response http.ResponseWriter, request *http.Request) {
	var input domain.KBArticleRequestInput
	if request.ContentLength != 0 {
		if err := decodeJSON(request, &input); err != nil {
			writeError(response, http.StatusBadRequest, "invalid_request", "Send an optional title, note and gap_id, or an empty body to use the conversation subject.")
			return
		}
	}
	who := principal(request)
	if a.refuseUnlessHeld(response, request, who, auth.PermKBArticlesRead, nil) {
		return
	}
	filed, err := a.store.RequestKBArticle(request.Context(), who.Workspace.ID, request.PathValue("id"), input)
	if err != nil {
		writeKBError(response, err)
		return
	}
	status := http.StatusOK
	if filed.Created {
		status = http.StatusCreated
	}
	writeJSON(response, status, filed)
}

// withdrawKBArticleRequest takes back the conversation's request for an article.
func (a *API) withdrawKBArticleRequest(response http.ResponseWriter, request *http.Request) {
	who := principal(request)
	if a.refuseUnlessHeld(response, request, who, auth.PermKBArticlesRead, nil) {
		return
	}
	if err := a.store.WithdrawKBArticleRequest(request.Context(), who.Workspace.ID, request.PathValue("id")); err != nil {
		writeKBError(response, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

// startKBGapArticle opens a draft for the gap, titled as the gap, in the chosen collection (the first
// one when none is chosen), with the conversations that asked for it as its sources. No AI writes it.
func (a *API) startKBGapArticle(response http.ResponseWriter, request *http.Request) {
	collectionID, ok := a.conversationArticleCollection(response, request)
	if !ok {
		return
	}
	who := principal(request)
	gapID := request.PathValue("id")
	a.createOnce(response, request, startKBGapArticleOperation, []string{gapID, collectionID}, func(ctx context.Context) (any, error) {
		title, conversations, err := a.store.KBGapForArticle(ctx, who.Workspace.ID, gapID)
		if err != nil {
			writeKBError(response, err)
			return nil, errAnswered
		}
		return a.createConversationArticle(ctx, response, request, store.KBArticleInput{CollectionID: collectionID, Title: title,
			Status: domain.KBDraft, GapID: gapID, GapConversationIDs: conversations, GapRequested: true})
	})
}

// makeKBArticleFromReply keeps a sent reply as a draft article, with the customer question it
// answered beside it; names, email addresses and long numbers are masked.
func (a *API) makeKBArticleFromReply(response http.ResponseWriter, request *http.Request) {
	who := principal(request)
	if a.refuseUnlessHeld(response, request, who, auth.PermConversationsRead, nil) {
		return
	}
	collectionID, ok := a.conversationArticleCollection(response, request)
	if !ok {
		return
	}
	conversationID, messageID := request.PathValue("id"), request.PathValue("messageId")
	a.createOnce(response, request, makeKBReplyArticleOperation, []string{conversationID, messageID, collectionID}, func(ctx context.Context) (any, error) {
		title, body, source, err := a.store.KBReplyForArticle(ctx, who.Workspace.ID, conversationID, messageID)
		if err != nil {
			writeKBError(response, err)
			return nil, errAnswered
		}
		return a.createConversationArticle(ctx, response, request, store.KBArticleInput{CollectionID: collectionID, Title: title,
			BodyMD: body, Status: domain.KBDraft, Reply: &source})
	})
}

// conversationArticleCollection reads the optional collection_id of a request that makes an article
// from conversations, and answers the request itself when it is invalid.
func (a *API) conversationArticleCollection(response http.ResponseWriter, request *http.Request) (string, bool) {
	var input struct {
		CollectionID string `json:"collection_id"`
	}
	if request.ContentLength != 0 {
		if err := decodeJSON(request, &input); err != nil {
			writeError(response, http.StatusBadRequest, "invalid_request", "Send an optional collection_id, or an empty body for the first collection.")
			return "", false
		}
	}
	return a.gapDraftCollection(response, request, input.CollectionID)
}

func (a *API) createConversationArticle(ctx context.Context, response http.ResponseWriter, request *http.Request, input store.KBArticleInput) (any, error) {
	who := principal(request)
	article, err := a.store.CreateKBArticle(ctx, who.Workspace.ID, savedReplyActor(ctx, who), input)
	if err != nil {
		writeKBError(response, err)
		return nil, errAnswered
	}
	home, ok := a.helpCenterHome(response, request)
	if !ok {
		return nil, errAnswered
	}
	addressKBArticle(home, &article.KBArticleSummary)
	if !a.addressKBArticleTranslations(response, request, home, &article) {
		return nil, errAnswered
	}
	hideGapConversations(who, &article)
	return article, nil
}
