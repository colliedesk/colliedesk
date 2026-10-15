package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
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
	writeJSON(response, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func writeStoreError(response http.ResponseWriter, err error) {
	if writePlanLimit(response, err) {
		return
	}
	switch {
	case errors.Is(err, domain.ErrLastAdmin):
		writeError(response, http.StatusConflict, "last_admin", domain.ErrLastAdmin.Error()+". Make another agent an admin first.")
		return
	case errors.Is(err, domain.ErrAgentDeactivated):
		writeError(response, http.StatusConflict, "agent_deactivated", domain.ErrAgentDeactivated.Error()+".")
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
		writeError(response, http.StatusForbidden, "trial_outbound_restricted", refusal.Error())
		return
	}
	seconds := int((refusal.RetryAfter + time.Second - 1) / time.Second)
	response.Header().Set("Retry-After", strconv.Itoa(seconds))
	writeJSON(response, http.StatusTooManyRequests, map[string]any{"error": map[string]any{
		"code": "trial_outbound_limit", "message": refusal.Error(), "retry_after_seconds": seconds}})
}
