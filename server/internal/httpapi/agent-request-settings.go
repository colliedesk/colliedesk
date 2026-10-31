package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// settingsFields lists, for each settings route a token may ask for, the fields its call takes. A
// request is stored with its body for days and shown to people, so a field the route does not take
// (where a pasted password or key could hide) is refused before anything is stored; the call would
// be refused on approval anyway.
var settingsFields = map[string][]string{
	"PATCH /api/v1/workspace/timezone":                {"timezone"},
	"PATCH /api/v1/workspace/name":                    {"name"},
	"PATCH /api/v1/agents/me/timezone":                {"timezone"},
	"PATCH /api/v1/agents/me/display-name":            {"display_name"},
	"PATCH /api/v1/agents/{id}":                       {"display_name", "role", "active", "reassign_to"},
	"POST /api/v1/auth/invites":                       {"email", "role_ceiling"},
	"DELETE /api/v1/auth/invites/{id}":                {},
	"PUT /api/v1/channels/web-form":                   {"enabled", "allowed_origins"},
	"PUT /api/v1/channels/email":                      {"support_address", "sender_name"},
	"POST /api/v1/channels/email/domain-verification": {},
	"DELETE /api/v1/auth/sessions/{id}":               {},
	"GET /api/v1/billing":                             {},
	"PUT /api/v1/help-center/theme":                   themeFields,
	"PUT /api/v1/help-center/search-console":          searchConsoleFields,
	"PUT /api/v1/help-center/domain":                  {"hostname"},
	"DELETE /api/v1/help-center/domain":               {},
	"POST /api/v1/help-center/domain/check":           {},
	"PUT " + channelAIRoute:                           {"scope", "level", "options"},
	"PUT /api/v1/ai/source-consents/{class}":          {},
}

// summarizeSettingsRequest is what a person reads before approving a settings change: what changes,
// from what to what. It returns "" for any other route.
func (a *API) summarizeSettingsRequest(ctx context.Context, request *http.Request, route Route, who auth.Principal, body json.RawMessage) string {
	var call struct {
		Timezone       *string  `json:"timezone"`
		Name           *string  `json:"name"`
		DisplayName    *string  `json:"display_name"`
		Role           *string  `json:"role"`
		Active         *bool    `json:"active"`
		ReassignTo     *string  `json:"reassign_to"`
		Email          string   `json:"email"`
		RoleCeiling    string   `json:"role_ceiling"`
		Enabled        *bool    `json:"enabled"`
		AllowedOrigins []string `json:"allowed_origins"`
		SupportAddress *string  `json:"support_address"`
		SenderName     *string  `json:"sender_name"`
	}
	_ = json.Unmarshal(body, &call)
	id := request.PathValue("id")
	if action, ok := channelRequestActions[route.Method+" "+route.Pattern]; ok {
		return action + " " + a.channelLabel(ctx, who.Workspace.ID, id)
	}
	switch route.Method + " " + route.Pattern {
	case "PUT " + channelAIRoute:
		return a.summarizeChannelAIRequest(ctx, request, who, body)
	case "PUT /api/v1/ai/source-consents/{class}":
		return "Let AI intake send " + request.PathValue("class") + " to the intake feature's provider"
	case "PATCH /api/v1/workspace/timezone":
		return fmt.Sprintf("Change the workspace timezone: %s → %s", who.Workspace.Timezone, orNone(call.Timezone, "(none)"))
	case "PATCH /api/v1/workspace/name":
		return fmt.Sprintf("Change the workspace name: %s → %s", who.Workspace.Name, orNone(call.Name, "(none)"))
	case "PATCH /api/v1/agents/me/timezone":
		before := "the workspace timezone"
		if who.Agent.TimezoneOverride != nil {
			before = *who.Agent.TimezoneOverride
		}
		return fmt.Sprintf("Change the timezone of %s: %s → %s", who.Agent.Email, before, orNone(call.Timezone, "the workspace timezone"))
	case "PATCH /api/v1/agents/me/display-name":
		return fmt.Sprintf("Change the display name of %s: %s", who.Agent.Email, nameChange(who.Agent.DisplayName, call.DisplayName))
	case "PATCH /api/v1/agents/{id}":
		return a.summarizeTeamChange(ctx, who.Workspace.ID, id, call.DisplayName, call.Role, call.Active, call.ReassignTo)
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
		if err != nil {
			return ""
		}
		var changes []string
		if call.Enabled != nil && *call.Enabled != form.Enabled {
			changes = append(changes, onOff(form.Enabled)+" → "+onOff(*call.Enabled))
		}
		if call.AllowedOrigins != nil && !slices.Equal(call.AllowedOrigins, form.AllowedOrigins) {
			changes = append(changes, "allowed origins: "+originList(form.AllowedOrigins)+" → "+originList(call.AllowedOrigins))
		}
		if len(changes) == 0 {
			changes = append(changes, "no change")
		}
		return "Change the web form: " + strings.Join(changes, "; ")
	case "PUT /api/v1/channels/email":
		before, beforeName := "not connected", who.Workspace.Name
		if channel, err := a.store.GetEmailChannel(ctx, who.Workspace.ID); err == nil && channel != nil {
			before, beforeName = channel.SupportAddress, channel.SenderName
		}
		var changes []string
		if call.SupportAddress != nil {
			changes = append(changes, fmt.Sprintf("support address: %s → %s", before, *call.SupportAddress))
		}
		if call.SenderName != nil {
			changes = append(changes, "sender name: "+beforeName+" → "+*call.SenderName)
		}
		return "Change the email channel's " + strings.Join(changes, "; ")
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

func (a *API) summarizeTeamChange(ctx context.Context, workspaceID, id string, displayName, role *string, active *bool, reassignTo *string) string {
	members, err := a.store.ListAgents(ctx, workspaceID)
	if err != nil {
		return ""
	}
	names := map[string]string{}
	var who, currentName, currentRole string
	activeNow := true
	for _, member := range members {
		names[member.ID] = member.Email
		if member.ID == id {
			who, currentName, currentRole, activeNow = member.Email, member.DisplayName, member.Role, member.DeactivatedAt == nil
		}
	}
	if who == "" {
		return "Change the team member " + id
	}
	var changes []string
	if displayName != nil {
		changes = append(changes, "display name "+nameChange(currentName, displayName))
	}
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

// workspaceSession finds a session of the caller's workspace by id, whoever owns it. It is for
// decisions and summaries only; what the caller may see is decided by its own role.
func (a *API) workspaceSession(ctx context.Context, who auth.Principal, id string) (domain.Session, bool, error) {
	everyone := who
	everyone.Agent.Role = "admin"
	sessions, err := a.auth.ListSessions(ctx, everyone)
	if err != nil {
		return domain.Session{}, false, err
	}
	for _, session := range sessions {
		if session.ID == id {
			return session, true, nil
		}
	}
	return domain.Session{}, false, nil
}

// summarizeSessionRevoke names the session only when the token's own role could list it.
func (a *API) summarizeSessionRevoke(ctx context.Context, who auth.Principal, id string) string {
	session, found, err := a.workspaceSession(ctx, who, id)
	if err != nil || !found {
		return "Revoke the session " + id
	}
	if who.Agent.Role != "admin" && session.AgentID != who.Agent.ID {
		return "Revoke a session"
	}
	return fmt.Sprintf("Revoke the session %q of %s", session.Label, session.AgentEmail)
}

// nameChange shows a display name change as "before → after"; an empty name is shown as (none).
func nameChange(before string, after *string) string {
	shown := func(name string) string {
		if name = strings.TrimSpace(name); name == "" {
			return "(none)"
		}
		return name
	}
	next := ""
	if after != nil {
		next = *after
	}
	return shown(before) + " → " + shown(next)
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

// channelRequestActions are the requests about one channel; the summary names the channel, never a
// secret the call carries.
var channelRequestActions = map[string]string{
	"DELETE /api/v1/channels/{id}":                         "Remove the channel",
	"POST /api/v1/channels/{id}/held/discard":              "Discard the held messages of the channel",
	"POST /api/v1/channels/{id}/email/secret":              "Rotate the inbound secret of the channel",
	"POST /api/v1/channels/{id}/email/domain-verification": "Check the DNS record of the domain of the channel",
	"POST " + channelGmailPath:                             "Connect Gmail to the channel",
	"DELETE " + channelGmailPath:                           "Disconnect Gmail from the channel",
	"POST " + channelMicrosoftPath:                         "Connect Microsoft 365 to the channel",
	"DELETE " + channelMicrosoftPath:                       "Disconnect Microsoft 365 from the channel",
}

// channelLabel names a channel of the workspace for a person: its name and what it is (an email
// channel's support address), or its id when it cannot be read.
func (a *API) channelLabel(ctx context.Context, workspaceID, id string) string {
	public, err := a.store.GetChannelPublic(ctx, workspaceID, id)
	if err != nil {
		return id
	}
	if public.Summary == "" {
		return public.Channel.Name
	}
	return public.Channel.Name + " (" + public.Summary + ")"
}
