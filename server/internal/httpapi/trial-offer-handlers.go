package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

const trialOfferAcceptOperation = "trial_offer_accept"

// trialOfferBody is a trial offer as the API answers it: the offer, the workspace's trial now, and what
// the caller can do with it.
func trialOfferBody(view store.TrialOfferView, who auth.Principal, acceptBy []string) map[string]any {
	var refusal any
	switch {
	case view.State != store.TrialOfferOpen:
		refusal = view.State
	case view.HasPlan():
		refusal = store.TrialOfferHasPlan
	case !who.Can(auth.PermBillingManage):
		refusal = "not_admin"
	}
	return map[string]any{
		"id": view.ID, "state": view.State, "workspace_name": view.WorkspaceName, "expires_at": view.ExpiresAt.Format(time.RFC3339),
		"accepted_at": optionalRFC3339(view.AcceptedAt), "accepted_by": view.AcceptedBy, "revoked_at": optionalRFC3339(view.RevokedAt),
		"trial_ends_at": view.TrialEndsAt.Format(time.RFC3339), "plan": view.Plan, "extends_to": view.ExtendsTo.Format(time.RFC3339),
		"can_accept": refusal == nil, "refusal": refusal, "accept_by": acceptBy,
	}
}

func optionalRFC3339(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC().Format(time.RFC3339)
}

// writeTrialOfferError answers the errors of reading or accepting an offer, and reports false for none.
func writeTrialOfferError(response http.ResponseWriter, err error) bool {
	var refused *store.TrialOfferRefusedError
	switch {
	case err == nil:
		return false
	case errors.Is(err, store.ErrTrialOfferNotFound):
		writeError(response, http.StatusNotFound, "trial_offer_not_found",
			"No trial offer has this link. Check that the whole link was copied, or write to "+supportAddress+".")
	case errors.Is(err, store.ErrTrialOfferOtherWorkspace):
		writeError(response, http.StatusForbidden, "trial_offer_other_workspace",
			"This trial offer is for another workspace than the one you are signed in to. Sign in to the workspace that received the link, then open it again.")
	case errors.As(err, &refused):
		ends := refused.TrialEndsAt.UTC().Format(time.RFC3339)
		messages := map[string]string{
			store.TrialOfferAccepted: "This trial offer was already accepted.",
			store.TrialOfferExpired:  "This trial offer has expired.",
			store.TrialOfferRevoked:  "This trial offer was withdrawn.",
			store.TrialOfferHasPlan:  "This workspace has a plan, so it needs no trial; nothing changed.",
		}
		writeErrorWith(response, http.StatusConflict, "trial_offer_"+refused.Reason,
			messages[refused.Reason]+" The trial ends or ended at "+ends+". For more time, write to "+supportAddress+".",
			map[string]any{"trial_ends_at": ends})
	default:
		writeInternalError(response, err)
	}
	return true
}

func writeTrialOfferTokenMissing(response http.ResponseWriter) {
	writeJSON(response, http.StatusUnprocessableEntity, map[string]any{"error": errorBody(response, map[string]any{
		"code": "validation_failed", "message": "Give the token of the trial offer link.", "fields": map[string]string{"token": "Paste the token from the link."}})})
}

// getTrialOffer reads the offer of a link in the session's workspace. It changes nothing: mail scanners
// open links too.
func (a *API) getTrialOffer(response http.ResponseWriter, request *http.Request) {
	token := request.URL.Query().Get("token")
	if token == "" {
		writeTrialOfferTokenMissing(response)
		return
	}
	who := principal(request)
	view, err := a.store.TrialOfferByToken(request.Context(), who.Workspace.ID, token, a.now())
	if writeTrialOfferError(response, err) {
		return
	}
	acceptBy, err := a.store.NamesHolding(request.Context(), who.Workspace.ID, auth.PermBillingManage)
	if err != nil {
		writeInternalError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, trialOfferBody(view, who, acceptBy))
}

// acceptTrialOffer spends an open offer: the trial ends 14 days from now unless it already ends later.
func (a *API) acceptTrialOffer(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Token string `json:"token"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "The body must be a JSON object with the token of the trial offer link.")
		return
	}
	if input.Token == "" {
		writeTrialOfferTokenMissing(response)
		return
	}
	who := principal(request)
	// A retry with the same Idempotency-Key gets the first answer instead of "already accepted".
	a.runOnce(response, request, http.StatusOK, trialOfferAcceptOperation, []string{input.Token}, func(ctx context.Context) (any, error) {
		accepted, err := a.store.AcceptTrialOffer(ctx, who.Workspace.ID, input.Token, who.Agent.ID, personName(who), a.now())
		if writeTrialOfferError(response, err) {
			return nil, errAnswered
		}
		acceptBy, err := a.store.NamesHolding(ctx, who.Workspace.ID, auth.PermBillingManage)
		if err != nil {
			return nil, err
		}
		a.recordFunnel("trial_offer_accepted", who.Workspace.ID, map[string]any{"extended": accepted.Extended})
		return map[string]any{
			"offer": trialOfferBody(accepted.View, who, acceptBy), "previous_trial_ends_at": accepted.PreviousEnd.Format(time.RFC3339),
			"trial_ends_at": accepted.View.TrialEndsAt.Format(time.RFC3339), "extended": accepted.Extended,
		}, nil
	})
}
