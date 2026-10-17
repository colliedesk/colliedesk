package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/auth"
)

// settingsFields lists, for each settings route a token may ask for, the fields its call takes. A
// request is stored with its body for days and shown to people, so a field the route does not take
// (where a pasted password or key could hide) is refused before anything is stored; the call would
// be refused on approval anyway.
var settingsFields = map[string][]string{
	"PATCH /api/v1/workspace/timezone":                {"timezone"},
	"PATCH /api/v1/agents/me/timezone":                {"timezone"},
	"PATCH /api/v1/agents/{id}":                       {"role", "active", "reassign_to"},
	"POST /api/v1/auth/invites":                       {"email", "role_ceiling"},
	"DELETE /api/v1/auth/invites/{id}":                {},
	"PUT /api/v1/channels/web-form":                   {"enabled", "allowed_origins"},
	"PUT /api/v1/channels/email":                      {"support_address"},
	"POST /api/v1/channels/email/domain-verification": {},
	"DELETE /api/v1/auth/sessions/{id}":               {},
	"GET /api/v1/billing":                             {},
}

// unknownSettingsField names the first field of body that route does not take, or "" when there is none.
func unknownSettingsField(route Route, body json.RawMessage) (string, bool) {
	allowed, ok := settingsFields[route.Method+" "+route.Pattern]
	if !ok || len(body) == 0 {
		return "", false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) != nil {
		return "", false
	}
	for _, name := range slices.Sorted(maps.Keys(fields)) {
		if !slices.Contains(allowed, name) {
			return name, true
		}
	}
	return "", false
}

// summarizeSettingsRequest is what a person reads before approving a settings change: what changes,
// from what to what. It returns "" for any other route.
func (a *API) summarizeSettingsRequest(ctx context.Context, request *http.Request, route Route, who auth.Principal, body json.RawMessage) string {
	var call struct {
		Timezone       *string  `json:"timezone"`
		Role           *string  `json:"role"`
		Active         *bool    `json:"active"`
		ReassignTo     *string  `json:"reassign_to"`
		Email          string   `json:"email"`
		RoleCeiling    string   `json:"role_ceiling"`
		Enabled        *bool    `json:"enabled"`
		AllowedOrigins []string `json:"allowed_origins"`
		SupportAddress string   `json:"support_address"`
	}
	_ = json.Unmarshal(body, &call)
	id := request.PathValue("id")
	switch route.Method + " " + route.Pattern {
	case "PATCH /api/v1/workspace/timezone":
		return fmt.Sprintf("Change the workspace timezone: %s → %s", who.Workspace.Timezone, orNone(call.Timezone, "(none)"))
	case "PATCH /api/v1/agents/me/timezone":
		before := "the workspace timezone"
		if who.Agent.TimezoneOverride != nil {
			before = *who.Agent.TimezoneOverride
		}
		return fmt.Sprintf("Change the timezone of %s: %s → %s", who.Agent.Email, before, orNone(call.Timezone, "the workspace timezone"))
	case "PATCH /api/v1/agents/{id}":
		return a.summarizeTeamChange(ctx, who.Workspace.ID, id, call.Role, call.Active, call.ReassignTo)
	case "POST /api/v1/auth/invites":
		return fmt.Sprintf("Invite %s to the team as %s", call.Email, call.RoleCeiling)
	case "DELETE /api/v1/auth/invites/{id}":
		invites, err := a.auth.ListInvites(ctx, who.Workspace.ID)
		if err == nil {
			for _, invite := range invites {
				if invite.ID == id {
					return fmt.Sprintf("Revoke the invite of %s (%s)", invite.Email, invite.RoleCeiling)
				}
			}
		}
		return "Revoke the invite " + id
	case "PUT /api/v1/channels/web-form":
		form, err := a.store.GetWebForm(ctx, who.Workspace.ID)
		if err != nil || call.Enabled == nil {
			return ""
		}
		return fmt.Sprintf("Change the web form: %s → %s; allowed origins: %s → %s",
			onOff(form.Enabled), onOff(*call.Enabled), originList(form.AllowedOrigins), originList(call.AllowedOrigins))
	case "PUT /api/v1/channels/email":
		before := "not connected"
		if channel, err := a.store.GetEmailChannel(ctx, who.Workspace.ID); err == nil && channel != nil {
			before = channel.SupportAddress
		}
		return fmt.Sprintf("Change the email channel's support address: %s → %s", before, call.SupportAddress)
	case "POST /api/v1/channels/email/domain-verification":
		if channel, err := a.store.GetEmailChannel(ctx, who.Workspace.ID); err == nil && channel != nil {
			return "Check the DNS record that verifies " + channel.Domain + " for sending"
		}
		return "Check the DNS record of the email channel's domain"
	case "DELETE /api/v1/auth/sessions/{id}":
		return a.summarizeSessionRevoke(ctx, who, id)
	case "GET /api/v1/billing":
		return "Read the plan and billing status"
	}
	return ""
}

func (a *API) summarizeTeamChange(ctx context.Context, workspaceID, id string, role *string, active *bool, reassignTo *string) string {
	members, err := a.store.ListAgents(ctx, workspaceID)
	if err != nil {
		return ""
	}
	names := map[string]string{}
	var who, currentRole string
	activeNow := true
	for _, member := range members {
		names[member.ID] = member.Email
		if member.ID == id {
			who, currentRole, activeNow = member.Email, member.Role, member.DeactivatedAt == nil
		}
	}
	if who == "" {
		return "Change the team member " + id
	}
	var changes []string
	if role != nil {
		changes = append(changes, "role "+currentRole+" → "+*role)
	}
	if active != nil && *active != activeNow {
		if *active {
			changes = append(changes, "reactivate")
		} else {
			change := "deactivate"
			if reassignTo != nil {
				change += ", open conversations to " + names[*reassignTo]
			}
			changes = append(changes, change)
		}
	}
	if len(changes) == 0 {
		changes = append(changes, "no change")
	}
	return "Change " + who + ": " + strings.Join(changes, "; ")
}

func (a *API) summarizeSessionRevoke(ctx context.Context, who auth.Principal, id string) string {
	// The lookup lists the whole workspace's sessions, so the summary names any session the approver
	// (an admin) could revoke; it reads only label and owner, never a token.
	everyone := who
	everyone.Agent.Role = "admin"
	sessions, err := a.auth.ListSessions(ctx, everyone)
	if err == nil {
		for _, session := range sessions {
			if session.ID == id {
				return fmt.Sprintf("Revoke the session %q of %s", session.Label, session.AgentEmail)
			}
		}
	}
	return "Revoke the session " + id
}

func orNone(value *string, none string) string {
	if value == nil {
		return none
	}
	return *value
}

func onOff(enabled bool) string {
	if enabled {
		return "on"
	}
	return "off"
}

func originList(origins []string) string {
	if len(origins) == 0 {
		return "none"
	}
	return strings.Join(origins, ", ")
}
