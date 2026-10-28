package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
	_ "time/tzdata"

	"github.com/colliedesk/colliedesk/server/internal/auth"
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

// startOnBrowserTimezone gives an agent signing in for the first time the timezone of their browser (the
// timezone field of the sign-in request), so times match their clock instead of the UTC default. An
// unknown or missing zone changes nothing, and nothing here ever fails a sign-in.
func (a *API) startOnBrowserTimezone(ctx context.Context, signedIn *auth.Redeemed, timezone string) {
	if timezone == "" || timezone == "UTC" || timezone == "Etc/UTC" || !validTimezone(timezone) {
		return
	}
	set, err := a.store.StartOnBrowserTimezone(ctx, signedIn.Workspace.ID, signedIn.Agent.ID, timezone)
	if err != nil {
		slog.Error("start agent on browser timezone", "workspace_id", signedIn.Workspace.ID, "error", err)
		return
	}
	if set {
		signedIn.Agent.TimezoneOverride = &timezone
	}
}
