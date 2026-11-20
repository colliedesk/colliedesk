package httpapi

import (
	"net/http"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// listKBArticleReviews lists the reviews of one article, newest first.
func (a *API) listKBArticleReviews(response http.ResponseWriter, request *http.Request) {
	a.writeKBReviews(response, request, domain.KBReviewFilter{ArticleID: request.PathValue("id"), State: request.URL.Query().Get("state")})
}

// listKBTranslationReviews lists the reviews of one translation, newest first.
func (a *API) listKBTranslationReviews(response http.ResponseWriter, request *http.Request) {
	locale := request.PathValue("locale")
	a.writeKBReviews(response, request, domain.KBReviewFilter{ArticleID: request.PathValue("id"), Locale: &locale, State: request.URL.Query().Get("state")})
}

// listKBReviews lists the workspace's reviews: state narrows them, reviewer=me to the ones the caller
// was asked to review (for a token, the person who made it).
func (a *API) listKBReviews(response http.ResponseWriter, request *http.Request) {
	query := request.URL.Query()
	filter := domain.KBReviewFilter{State: query.Get("state")}
	switch reviewer := query.Get("reviewer"); reviewer {
	case "":
	case "me":
		filter.ReviewerID = principal(request).Agent.ID
	default:
		filter.ReviewerID = reviewer
	}
	a.writeKBReviews(response, request, filter)
}

func (a *API) writeKBReviews(response http.ResponseWriter, request *http.Request, filter domain.KBReviewFilter) {
	reviews, err := a.store.ListKBReviews(request.Context(), principal(request).Workspace.ID, filter)
	if err != nil {
		writeKBError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"reviews": reviews})
}

// getKBReview reads one review with the live and proposed texts and their line diff.
func (a *API) getKBReview(response http.ResponseWriter, request *http.Request) {
	who := principal(request)
	review, err := a.store.GetKBReview(request.Context(), who.Workspace.ID, savedReplyActor(request.Context(), who), request.PathValue("id"))
	if err != nil {
		writeKBError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, review)
}

// requestKBReviewChanges closes an open review asking the author to change the text; note says what.
func (a *API) requestKBReviewChanges(response http.ResponseWriter, request *http.Request) {
	a.decideKBReview(response, request, domain.KBReviewChangesRequested, requestKBReviewChangesOperation)
}

// withdrawKBReview closes an open review its sender, or someone who may publish, no longer wants.
func (a *API) withdrawKBReview(response http.ResponseWriter, request *http.Request) {
	a.decideKBReview(response, request, domain.KBReviewWithdrawn, withdrawKBReviewOperation)
}
