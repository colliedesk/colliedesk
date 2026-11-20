package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// requestKBReview sends the article's unpublished text for review to teammates who may publish.
func (a *API) requestKBReview(response http.ResponseWriter, request *http.Request) {
	var input struct {
		ReviewerIDs []string `json:"reviewer_ids"`
		Note        string   `json:"note"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send reviewer_ids (agent ids of teammates who may publish) and an optional one-line note.")
		return
	}
	articleID := request.PathValue("id")
	a.createOnce(response, request, createKBReviewOperation, append([]string{articleID, input.Note}, input.ReviewerIDs...), func(ctx context.Context) (any, error) {
		who := principal(request)
		review, err := a.store.RequestKBReview(ctx, who.Workspace.ID, savedReplyActor(ctx, who), articleID, store.KBReviewRequest{ReviewerIDs: input.ReviewerIDs, Note: input.Note})
		if err != nil {
			writeKBError(response, err)
			return nil, errAnswered
		}
		return review, nil
	})
}

// approveKBReview approves the text of an open review: it may then be published as it is.
func (a *API) approveKBReview(response http.ResponseWriter, request *http.Request) {
	a.decideKBReview(response, request, domain.KBReviewApproved, approveKBReviewOperation)
}

func (a *API) decideKBReview(response http.ResponseWriter, request *http.Request, state, operation string) {
	who := principal(request)
	if state != domain.KBReviewWithdrawn && a.refuseUnlessHeld(response, request, who, auth.PermKBArticlesPublish, nil) {
		return
	}
	var input struct {
		Note string `json:"note"`
	}
	if request.ContentLength != 0 {
		if err := decodeJSON(request, &input); err != nil {
			writeError(response, http.StatusBadRequest, "invalid_request", "Send an optional one-line note.")
			return
		}
	}
	reviewID := request.PathValue("id")
	a.runOnce(response, request, http.StatusOK, operation, []string{reviewID, input.Note}, func(ctx context.Context) (any, error) {
		decision := store.KBReviewDecision{State: state, Note: input.Note, WithdrawAny: who.Can(auth.PermKBArticlesPublish)}
		review, err := a.store.DecideKBReview(ctx, who.Workspace.ID, savedReplyActor(ctx, who), reviewID, decision)
		if err != nil {
			writeKBError(response, err)
			return nil, errAnswered
		}
		return review, nil
	})
}

// writeKBReviewError answers the refusals of reviews and of publishing without an approval; false for
// any other error.
func writeKBReviewError(response http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, domain.ErrKBApprovalRequired):
		writeError(response, http.StatusConflict, "approval_required", capitalize(err.Error())+
			": POST /api/v1/kb/articles/{id}/reviews with reviewer_ids; once a reviewer approves this exact text, publish it.")
	case errors.Is(err, domain.ErrKBReviewNotFound):
		writeError(response, http.StatusNotFound, "not_found", "The review was not found. GET /api/v1/kb/articles/{id}/reviews lists an article's reviews.")
	case errors.Is(err, domain.ErrKBReviewOpen), errors.Is(err, domain.ErrKBReviewClosed), errors.Is(err, domain.ErrKBNothingToReview):
		writeError(response, http.StatusConflict, "review_state", capitalize(err.Error())+".")
	case errors.Is(err, domain.ErrKBNotAReviewer), errors.Is(err, domain.ErrKBOwnReview):
		writeError(response, http.StatusForbidden, "not_reviewer", capitalize(err.Error())+".")
	default:
		return false
	}
	return true
}

func capitalize(text string) string {
	if text == "" {
		return text
	}
	return strings.ToUpper(text[:1]) + text[1:]
}
