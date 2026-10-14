package httpapi

import (
	"encoding/json"
	"net/http"
	"time"
	_ "time/tzdata"
)

func (a *API) updateWorkspaceTimezone(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Timezone string `json:"timezone"`
	}
	if err := decodeJSON(request, &input); err != nil || !validTimezone(input.Timezone) {
		writeError(response, http.StatusBadRequest, "invalid_request", "A valid IANA timezone is required.")
		return
	}
	workspace, err := a.store.UpdateWorkspaceTimezone(request.Context(), principal(request).Workspace.ID, input.Timezone)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, workspace)
}

func (a *API) updateAgentTimezone(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Timezone json.RawMessage `json:"timezone"`
	}
	if err := decodeJSON(request, &input); err != nil || len(input.Timezone) == 0 {
		writeError(response, http.StatusBadRequest, "invalid_request", "Timezone must be null or a valid IANA name.")
		return
	}
	var timezone *string
	if err := json.Unmarshal(input.Timezone, &timezone); err != nil || (timezone != nil && !validTimezone(*timezone)) {
		writeError(response, http.StatusBadRequest, "invalid_request", "Timezone must be null or a valid IANA name.")
		return
	}
	identity := principal(request)
	agent, err := a.store.UpdateAgentTimezone(request.Context(), identity.Workspace.ID, identity.Agent.ID, timezone)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	agent.Role = identity.Agent.Role
	writeJSON(response, http.StatusOK, agent)
}

func validTimezone(value string) bool {
	if value == "" || value == "Local" {
		return false
	}
	_, err := time.LoadLocation(value)
	return err == nil
}
