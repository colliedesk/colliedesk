package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

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
	if errors.Is(err, pgx.ErrNoRows) || strings.Contains(err.Error(), "not found") {
		writeError(response, http.StatusNotFound, "not_found", "The requested resource was not found.")
		return
	}
	writeError(response, http.StatusBadRequest, "invalid_request", "The request could not be completed.")
}

// writePlanLimit answers 402 when err is a plan limit, and reports whether it did.
func writePlanLimit(response http.ResponseWriter, err error) bool {
	var limit *domain.PlanLimitError
	if !errors.As(err, &limit) {
		return false
	}
	writeError(response, http.StatusPaymentRequired, "plan_limit_reached", limit.Error())
	return true
}
