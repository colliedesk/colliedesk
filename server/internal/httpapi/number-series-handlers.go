package httpapi

import (
	"errors"
	"net/http"

	"github.com/colliedesk/colliedesk/server/internal/store"
)

func (a *API) listNumberSeries(response http.ResponseWriter, request *http.Request) {
	series, err := a.store.ListNumberSeries(request.Context(), principal(request).Workspace.ID)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": series})
}

func (a *API) setNumberSeries(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Format    *string `json:"format"`
		Next      *int64  `json:"next"`
		Start     *int64  `json:"start"`
		StartYear *int    `json:"start_year"`
	}
	if err := decodeJSON(request, &input); err != nil || (input.Format == nil && input.Next == nil && input.Start == nil && input.StartYear == nil) {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send at least one of format, next, start or start_year.")
		return
	}
	series, err := a.store.SetNumberSeries(request.Context(), principal(request).Workspace.ID, request.PathValue("series"),
		store.NumberSeriesInput{Format: input.Format, Next: input.Next, Start: input.Start, StartYear: input.StartYear})
	var refused *store.ErrNumberSeries
	if errors.As(err, &refused) {
		writeJSON(response, http.StatusBadRequest, map[string]any{"error": errorBody(response, map[string]any{"code": "invalid_request",
			"message": refused.Message, "fields": map[string]string{refused.Field: refused.Message}})})
		return
	}
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, series)
}
