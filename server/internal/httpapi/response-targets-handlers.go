package httpapi

import (
	"errors"
	"net/http"

	"github.com/colliedesk/colliedesk/server/internal/store"
)

func (a *API) getResponseTargets(response http.ResponseWriter, request *http.Request) {
	identity := principal(request)
	targets, err := a.store.GetResponseTargets(request.Context(), identity.Workspace.ID)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	if identity.Agent.ID != "" {
		mine, err := a.store.MySLAAlertEmail(request.Context(), identity.Workspace.ID, identity.Agent.ID)
		if err != nil {
			writeStoreError(response, err)
			return
		}
		targets.MyAlertEmail = &mine
	}
	writeJSON(response, http.StatusOK, targets)
}

func (a *API) setMySLAAlerts(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Email *bool `json:"email"`
	}
	if err := decodeJSON(request, &input); err != nil || input.Email == nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send email: true or false.")
		return
	}
	identity := principal(request)
	if err := a.store.SetMySLAAlertEmail(request.Context(), identity.Workspace.ID, identity.Agent.ID, *input.Email); err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]bool{"email": *input.Email})
}

func (a *API) setResponseTargets(response http.ResponseWriter, request *http.Request) {
	var input store.ResponseTargets
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send the response targets as one document.")
		return
	}
	identity := principal(request)
	targets, err := a.store.SetResponseTargets(request.Context(), identity.Workspace.ID, identity.Agent.ID, input)
	var wrong *store.ResponseTargetsError
	if errors.As(err, &wrong) {
		writeJSON(response, http.StatusBadRequest, map[string]any{"error": errorBody(response, map[string]any{"code": "invalid_request",
			"message": wrong.Message, "fields": map[string]string{wrong.Field: wrong.Message}})})
		return
	}
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, targets)
}
