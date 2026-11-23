package httpapi

import (
	"context"
	"net/http"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// getKBArticleConflicts answers which sent replies the small model found to contradict an article.
func (a *API) getKBArticleConflicts(response http.ResponseWriter, request *http.Request) {
	who := principal(request)
	conflicts, err := a.store.KBArticleConflicts(request.Context(), who.Workspace.ID, request.PathValue("id"))
	if err != nil {
		writeKBError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, hideConflictConversations(who, conflicts))
}

// clearKBArticleConflicts clears an article's "May be out of date" flag, recording who cleared it and when.
func (a *API) clearKBArticleConflicts(response http.ResponseWriter, request *http.Request) {
	id := request.PathValue("id")
	a.runOnce(response, request, http.StatusOK, clearKBConflictsOperation, []string{id}, func(ctx context.Context) (any, error) {
		who := principal(request)
		conflicts, err := a.store.ClearKBArticleConflicts(ctx, who.Workspace.ID, savedReplyActor(ctx, who), id, a.now())
		if err != nil {
			writeKBError(response, err)
			return nil, errAnswered
		}
		return hideConflictConversations(who, conflicts), nil
	})
}

// hideConflictConversations leaves out where each reply was sent for a caller who may not read
// conversations; the reply itself stays.
func hideConflictConversations(who auth.Principal, conflicts domain.KBArticleConflicts) domain.KBArticleConflicts {
	if who.Can(auth.PermConversationsRead) {
		return conflicts
	}
	for i := range conflicts.Conflicts {
		conflicts.Conflicts[i].Reply.Conversation = nil
	}
	return conflicts
}
