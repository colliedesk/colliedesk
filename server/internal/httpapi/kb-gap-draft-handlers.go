package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/colliedesk/colliedesk/server/internal/ai"
	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// draftKBGapArticle has the reply-draft model write an article for the gap from the resolved
// conversations that match it, and keeps it as a draft in the chosen collection (the first one when
// none is chosen). Publishing it stays the normal publish call.
func (a *API) draftKBGapArticle(response http.ResponseWriter, request *http.Request) {
	var input struct {
		CollectionID string `json:"collection_id"`
	}
	if request.ContentLength != 0 {
		if err := decodeJSON(request, &input); err != nil {
			writeError(response, http.StatusBadRequest, "invalid_request", "Send an optional collection_id, or an empty body for the first collection.")
			return
		}
	}
	who := principal(request)
	collectionID, ok := a.gapDraftCollection(response, request, input.CollectionID)
	if !ok {
		return
	}
	gapID := request.PathValue("id")
	// A press sent again (its Idempotency-Key) answers the article the first one wrote: one paid answer per press.
	a.createOnce(response, request, draftKBGapArticleOperation, []string{gapID, collectionID}, func(ctx context.Context) (any, error) {
		draft, err := a.ai.DraftFromKBGap(ctx, who.Workspace.ID, aiActor(request), gapID)
		var failed *ai.KBGapDraftError
		switch {
		case errors.Is(err, domain.ErrKBGapNotFound):
			writeKBError(response, err)
		case errors.Is(err, ai.ErrAllowanceHeld):
			response.Header().Set("Retry-After", "5")
			writeError(response, http.StatusTooManyRequests, "rate_limited", "Other AI answers are being written. Try again in a moment.")
		case errors.As(err, &failed):
			writeError(response, http.StatusBadGateway, "ai_failed", "The AI did not answer ("+failed.Code+"). Try again.")
		case err != nil:
			writeAIError(response, err)
		}
		if err != nil {
			return nil, errAnswered
		}
		article, err := a.store.CreateKBArticle(ctx, who.Workspace.ID, savedReplyActor(ctx, who), store.KBArticleInput{CollectionID: collectionID,
			Title: draft.Title, BodyMD: draft.BodyMD, Status: domain.KBDraft, GapID: gapID, GapConversationIDs: draft.ConversationIDs})
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
		return article, nil
	})
}

// gapDraftCollection is the live collection of the workspace an article from a gap goes to: the one
// chosen, or the first one. It answers the request itself when there is none.
func (a *API) gapDraftCollection(response http.ResponseWriter, request *http.Request, chosen string) (string, bool) {
	collections, err := a.store.ListKBCollections(request.Context(), principal(request).Workspace.ID)
	if err != nil {
		writeKBError(response, err)
		return "", false
	}
	for _, collection := range collections {
		if chosen == "" || collection.ID == chosen {
			return collection.ID, true
		}
	}
	if chosen != "" {
		writeError(response, http.StatusBadRequest, "invalid_request", "The collection was not found, or it is in the trash. Leave collection_id out for the first collection.")
	} else {
		writeError(response, http.StatusBadRequest, "invalid_request", "Make a collection first: the article needs one.")
	}
	return "", false
}

// hideGapConversations leaves out the conversations an article was drafted or made from for a caller
// who may not read conversations; the gap stays named.
func hideGapConversations(who auth.Principal, article *domain.KBArticle) {
	if who.Can(auth.PermConversationsRead) {
		return
	}
	if article.GapSource != nil {
		article.GapSource.Conversations = []domain.KBArticleGapConversation{}
	}
	article.ReplySource = nil
}
