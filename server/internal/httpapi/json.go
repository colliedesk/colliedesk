package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func decodeJSON(request *http.Request, target any) error {
	decoder := json.NewDecoder(io.LimitReader(request.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode JSON: %w", err)
	}
	return nil
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}

func writeError(response http.ResponseWriter, status int, code, message string) {
	writeJSON(response, status, map[string]any{"error": errorBody(response, map[string]any{"code": code, "message": message})})
}

// errorBody adds the request id (the X-Request-Id header withRequestID set) to an error object, so a person
// reporting a failure can quote the id that finds it in the server log.
func errorBody(response http.ResponseWriter, body map[string]any) map[string]any {
	if id := response.Header().Get(requestIDHeader); id != "" {
		body["request_id"] = id
	}
	return body
}

func writeStoreError(response http.ResponseWriter, err error) {
	if writePlanLimit(response, err) {
		return
	}
	var database *pgconn.PgError
	switch {
	case errors.As(err, &database) && (database.Code == "40P01" || database.Code == "40001"):
		writeError(response, http.StatusConflict, "conflict", "Another change was in progress at the same time. Nothing was changed; try again.")
		return
	case errors.Is(err, domain.ErrResultsRefreshed):
		writeError(response, http.StatusGone, "results_refreshed", "These numbers were refreshed. Open Results again.")
		return
	case errors.Is(err, domain.ErrAIDraftNotReady):
		writeError(response, http.StatusConflict, "ai_draft_not_ready", "That AI draft is not ready to send from this conversation: it is still being written, or it was already sent, discarded or replaced. Send the reply without ai_draft_id, or ask for a new draft.")
		return
	case errors.Is(err, domain.ErrLastAdmin):
		writeError(response, http.StatusConflict, "last_admin", domain.ErrLastAdmin.Error()+". Make another agent an admin first.")
		return
	case errors.Is(err, domain.ErrAgentDeactivated):
		writeError(response, http.StatusConflict, "agent_deactivated", domain.ErrAgentDeactivated.Error()+".")
		return
	case errors.As(err, new(*domain.ContactExistsError)):
		var exists *domain.ContactExistsError
		errors.As(err, &exists)
		writeJSON(response, http.StatusConflict, map[string]any{"error": errorBody(response, map[string]any{
			"code": "contact_exists", "message": exists.Error(), "contact": exists.Use})})
		return
	case errors.As(err, new(*domain.ContactMergeError)):
		writeError(response, http.StatusConflict, "contact_merged", err.Error())
		return
	case errors.Is(err, domain.ErrMergeUndone):
		writeError(response, http.StatusConflict, "merge_undone", "This merge was undone already. Nothing was changed.")
		return
	case errors.Is(err, channels.ErrNotFound):
		writeError(response, http.StatusNotFound, "channel_not_found", "No channel of this workspace has that id.")
		return
	case errors.Is(err, channels.ErrChannelCannotSend):
		writeError(response, http.StatusBadRequest, "channel_cannot_send", "A conversation cannot start on this channel: choose an email channel.")
		return
	case errors.Is(err, channels.ErrChannelOff):
		writeError(response, http.StatusConflict, "channel_off", "This channel is off: turn it on, or start from another channel. Nothing was stored.")
		return
	case errors.Is(err, domain.ErrEmailChannelMissing):
		writeError(response, http.StatusConflict, "email_channel_missing", "Connect the email channel in Settings before starting a conversation by email. Nothing was stored.")
		return
	case errors.Is(err, domain.ErrInvalidReassignTarget):
		writeError(response, http.StatusBadRequest, "invalid_reassign_target", domain.ErrInvalidReassignTarget.Error()+".")
		return
	}
	if errors.Is(err, pgx.ErrNoRows) || strings.Contains(err.Error(), "not found") {
		writeError(response, http.StatusNotFound, "not_found", "The requested resource was not found.")
		return
	}
	writeError(response, http.StatusBadRequest, "invalid_request", "The request could not be completed.")
}

// writePlanLimit answers 402 when err is a plan limit or a locked workspace, 403 when the storage cap refuses a file, and 403 or 429 when the trial refuses a reply, and reports whether it did.
func writePlanLimit(response http.ResponseWriter, err error) bool {
	var limit *domain.PlanLimitError
	var locked *domain.LockedError
	var outbound *domain.TrialOutboundError
	var storage *domain.StorageLimitError
	switch {
	case errors.As(err, &storage):
		writeError(response, http.StatusForbidden, "storage_limit_reached", storage.Error())
	case errors.As(err, &outbound):
		writeTrialOutbound(response, outbound)
	case errors.As(err, &limit):
		writeError(response, http.StatusPaymentRequired, "plan_limit_reached", limit.Error())
	case errors.As(err, &locked):
		writeLocked(response)
	default:
		return false
	}
	return true
}

// writeTrialOutbound answers 403 trial_outbound_restricted for a reply the trial never allows (the
// customer did not write first) and 429 trial_outbound_limit, with Retry-After, for the daily cap.
func writeTrialOutbound(response http.ResponseWriter, refusal *domain.TrialOutboundError) {
	if !refusal.Limited() {
		body := map[string]any{"code": "trial_outbound_restricted", "message": refusal.Error()}
		if len(refusal.RefusedCC) > 0 {
			body["refused_cc"] = refusal.RefusedCC
		}
		if reason := refusal.Reason(); reason != "" {
			body["reason"] = reason
		}
		if refusal.ReplyTo != "" {
			body["reply_to"] = refusal.ReplyTo
		}
		writeJSON(response, http.StatusForbidden, map[string]any{"error": errorBody(response, body)})
		return
	}
	seconds := int((refusal.RetryAfter + time.Second - 1) / time.Second)
	response.Header().Set("Retry-After", strconv.Itoa(seconds))
	writeJSON(response, http.StatusTooManyRequests, map[string]any{"error": errorBody(response, map[string]any{
		"code": "trial_outbound_limit", "message": refusal.Error(), "retry_after_seconds": seconds})})
}

// readJSONBody reads a JSON object body of at most domain.MaxAgentRequestBody into target, refusing
// unknown fields, and returns its bytes for a call that may be filed for a person to approve.
func readJSONBody(request *http.Request, target any) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(request.Body, domain.MaxAgentRequestBody+1))
	if err != nil {
		return nil, err
	}
	if len(body) > domain.MaxAgentRequestBody {
		return nil, errors.New("the body is over 64 KiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return nil, fmt.Errorf("decode JSON: %w", err)
	}
	return body, nil
}
