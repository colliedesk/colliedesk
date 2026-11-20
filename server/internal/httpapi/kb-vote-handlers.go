package httpapi

import (
	"context"
	"net/http"
)

// getKBArticleVotes answers what readers of the help center said about an article.
func (a *API) getKBArticleVotes(response http.ResponseWriter, request *http.Request) {
	votes, err := a.store.KBArticleVotes(request.Context(), principal(request).Workspace.ID, request.PathValue("id"))
	if err != nil {
		writeKBError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, votes)
}

// resetKBArticleVotes starts an article's count again, keeping the earlier counts as a reset.
func (a *API) resetKBArticleVotes(response http.ResponseWriter, request *http.Request) {
	id := request.PathValue("id")
	a.runOnce(response, request, http.StatusOK, resetKBVotesOperation, []string{id}, func(ctx context.Context) (any, error) {
		who := principal(request)
		votes, err := a.store.ResetKBArticleVotes(ctx, who.Workspace.ID, savedReplyActor(ctx, who), id)
		if err != nil {
			writeKBError(response, err)
			return nil, errAnswered
		}
		return votes, nil
	})
}
