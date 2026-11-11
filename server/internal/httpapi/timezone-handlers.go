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
	writeJSON(response, http.StatusOK, agent)
}

func validTimezone(value string) bool {
	if value == "" || value == "Local" {
		return false
	}
	_, err := time.LoadLocation(value)
	return err == nil
}

// namesOfUTC are the IANA names of UTC itself (links of Etc/UTC and the zero-offset Etc zones): a browser
// that reports one says nothing about where the team works.
var namesOfUTC = map[string]bool{
	"UTC": true, "Etc/UTC": true, "UCT": true, "Etc/UCT": true, "Universal": true, "Etc/Universal": true, "Zulu": true, "Etc/Zulu": true,
	"GMT": true, "Etc/GMT": true, "GMT0": true, "Etc/GMT0": true, "GMT+0": true, "Etc/GMT+0": true, "GMT-0": true, "Etc/GMT-0": true,
	"Greenwich": true, "Etc/Greenwich": true,
}

// startOnBrowserTimezone starts a first sign-in on the timezone of the browser (the timezone field of the
// sign-in request), so times and business hours match the team's clock instead of the UTC default: the
// workspace's at the first sign-in of the person who created it, while nobody chose one, otherwise the
// agent's own. An unknown or missing zone changes nothing, and nothing here ever fails a sign-in.
func (a *API) startOnBrowserTimezone(ctx context.Context, signedIn *auth.Redeemed, timezone string) {
	if timezone == "" || namesOfUTC[timezone] || !validTimezone(timezone) {
		return
	}
	start, err := a.store.StartOnBrowserTimezone(ctx, signedIn.Workspace.ID, signedIn.Agent.ID, timezone)
	if err != nil {
		slog.Error("start on browser timezone", "workspace_id", signedIn.Workspace.ID, "error", err)
		return
	}
	if start.Workspace {
		signedIn.Workspace.Timezone = timezone
	}
	if start.Agent {
		signedIn.Agent.TimezoneOverride = &timezone
	}
}
