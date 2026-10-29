package mcpserver

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type restMember struct {
	ID            string     `json:"id"`
	Email         string     `json:"email"`
	DisplayName   string     `json:"display_name"`
	Role          string     `json:"role"`
	DeactivatedAt *time.Time `json:"deactivated_at"`
}

type restInvite struct {
	ID             string     `json:"id"`
	Email          string     `json:"email"`
	RoleCeiling    string     `json:"role_ceiling"`
	CreatedByEmail string     `json:"created_by_email"`
	CreatedAt      time.Time  `json:"created_at"`
	ExpiresAt      time.Time  `json:"expires_at"`
	UsedAt         *time.Time `json:"used_at"`
	RevokedAt      *time.Time `json:"revoked_at"`
}

type restSession struct {
	ID          string     `json:"id"`
	AgentEmail  string     `json:"agent_email"`
	Label       string     `json:"label"`
	Scope       *string    `json:"scope"`
	Permissions []string   `json:"permissions"`
	Current     bool       `json:"current"`
	LastSeenAt  time.Time  `json:"last_seen_at"`
	ExpiresAt   time.Time  `json:"expires_at"`
	RevokedAt   *time.Time `json:"revoked_at"`
}

type restEmailSettings struct {
	Channel *struct {
		ChannelID        string     `json:"channel_id"`
		SupportAddress   string     `json:"support_address"`
		SenderName       string     `json:"sender_name"`
		SecretSet        bool       `json:"secret_set"`
		Domain           string     `json:"domain"`
		DomainVerifiedAt *time.Time `json:"domain_verified_at"`
		DNSRecordName    string     `json:"dns_record_name"`
		DNSRecordValue   string     `json:"dns_record_value"`
	} `json:"channel"`
	InboundURL string `json:"inbound_url"`
	SMTP       struct {
		Configured bool `json:"configured"`
	} `json:"smtp"`
	SendingFrom   *string `json:"sending_from"`
	HostedAddress *string `json:"hosted_address"`
	Gmail         struct {
		Available  bool `json:"available"`
		Connection *struct {
			Address string  `json:"address"`
			Status  string  `json:"status"`
			Error   *string `json:"error"`
		} `json:"connection"`
	} `json:"gmail"`
	Microsoft struct {
		Available         bool    `json:"available"`
		AdminConsentURL   *string `json:"admin_consent_url"`
		ServerCredentials bool    `json:"server_credentials_refused"`
		Connection        *struct {
			Address string  `json:"address"`
			Status  string  `json:"status"`
			Error   *string `json:"error"`
		} `json:"connection"`
	} `json:"microsoft"`
}

func workspaceSettingsGet(c *call, _ map[string]any) (any, error) {
	me, err := c.me()
	if err != nil {
		return nil, err
	}
	yours, isDefault := me.Workspace.Timezone, true
	if me.Agent.TimezoneOverride != nil {
		yours, isDefault = *me.Agent.TimezoneOverride, false
	}
	return map[string]any{"workspace": map[string]any{"name": me.Workspace.Name, "slug": me.Workspace.Slug, "timezone": me.Workspace.Timezone},
		"your_timezone": yours, "your_timezone_is_the_workspace_default": isDefault}, nil
}

func workspaceSettingsUpdate(c *call, arguments map[string]any) (any, error) {
	var workspace struct {
		Name     string `json:"name"`
		Slug     string `json:"slug"`
		Timezone string `json:"timezone"`
	}
	if err := c.send(http.MethodPatch, "/api/v1/workspace/timezone", map[string]any{"timezone": arguments["timezone"]}, &workspace); err != nil {
		return nil, err
	}
	return map[string]any{"workspace": workspace}, nil
}

func workspaceRename(c *call, arguments map[string]any) (any, error) {
	var workspace struct {
		Name     string `json:"name"`
		Slug     string `json:"slug"`
		Timezone string `json:"timezone"`
	}
	if err := c.send(http.MethodPatch, "/api/v1/workspace/name", map[string]any{"name": arguments["name"]}, &workspace); err != nil {
		return nil, err
	}
	return map[string]any{"workspace": workspace}, nil
}

func myTimezoneSet(c *call, arguments map[string]any) (any, error) {
	var agent struct {
		TimezoneOverride *string `json:"timezone_override"`
	}
	if err := c.send(http.MethodPatch, "/api/v1/agents/me/timezone", map[string]any{"timezone": arguments["timezone"]}, &agent); err != nil {
		return nil, err
	}
	return map[string]any{"your_timezone": agent.TimezoneOverride}, nil
}

func myDisplayNameSet(c *call, arguments map[string]any) (any, error) {
	var agent restAgent
	if err := c.send(http.MethodPatch, "/api/v1/agents/me/display-name", map[string]any{"display_name": arguments["display_name"]}, &agent); err != nil {
		return nil, err
	}
	return map[string]any{"your_display_name": agent.DisplayName}, nil
}

// memberByEmailOrID finds a team member of this workspace, deactivated ones included.
func (c *call) memberByEmailOrID(value string) (restMember, error) {
	var page struct {
		Items []restMember `json:"items"`
	}
	if err := c.get("/api/v1/agents", nil, &page); err != nil {
		return restMember{}, err
	}
	value = strings.TrimSpace(value)
	for _, member := range page.Items {
		if member.ID == value || strings.EqualFold(member.Email, value) {
			return member, nil
		}
	}
	return restMember{}, &toolError{"No team member " + value + " in this workspace. List them with colliedesk_agents_list."}
}

func teamMemberUpdate(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		Member      string  `json:"member"`
		DisplayName *string `json:"display_name"`
		Role        *string `json:"role"`
		Active      *bool   `json:"active"`
		ReassignTo  string  `json:"reassign_to"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	if args.DisplayName == nil && args.Role == nil && args.Active == nil {
		return nil, &toolError{"Nothing to change. Give display_name, role, active, or several of them."}
	}
	member, err := c.memberByEmailOrID(args.Member)
	if err != nil {
		return nil, err
	}
	body := map[string]any{}
	if args.DisplayName != nil {
		body["display_name"] = *args.DisplayName
	}
	if args.Role != nil {
		body["role"] = *args.Role
	}
	if args.Active != nil {
		body["active"] = *args.Active
	}
	if args.ReassignTo != "" {
		target, err := c.memberByEmailOrID(args.ReassignTo)
		if err != nil {
			return nil, err
		}
		body["reassign_to"] = target.ID
	}
	c.notFound = "No team member " + args.Member + " in this workspace. List them with colliedesk_agents_list."
	var updated restMember
	if err := c.send(http.MethodPatch, "/api/v1/agents/"+url.PathEscape(member.ID), body, &updated); err != nil {
		return nil, err
	}
	return map[string]any{"member": map[string]any{"id": updated.ID, "email": updated.Email, "display_name": updated.DisplayName,
		"role": updated.Role, "active": updated.DeactivatedAt == nil}}, nil
}

func inviteView(invite restInvite) map[string]any {
	state := "pending"
	switch {
	case invite.UsedAt != nil:
		state = "accepted"
	case invite.RevokedAt != nil:
		state = "revoked"
	case !invite.ExpiresAt.After(time.Now()):
		state = "expired"
	}
	return map[string]any{"id": invite.ID, "email": invite.Email, "role": invite.RoleCeiling, "state": state,
		"invited_by": invite.CreatedByEmail, "created_at": utc(invite.CreatedAt), "expires_at": utc(invite.ExpiresAt)}
}

func invitesList(c *call, _ map[string]any) (any, error) {
	var page struct {
		Items []restInvite `json:"items"`
	}
	if err := c.get("/api/v1/auth/invites", nil, &page); err != nil {
		return nil, err
	}
	invites := make([]map[string]any, len(page.Items))
	for index, invite := range page.Items {
		invites[index] = inviteView(invite)
	}
	return map[string]any{"invites": invites}, nil
}

func inviteCreate(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	if args.Role == "" {
		args.Role = "agent"
	}
	return ask(c, http.MethodPost, "/api/v1/auth/invites", map[string]any{"email": strings.TrimSpace(args.Email), "role_ceiling": args.Role})
}

func inviteRevoke(c *call, arguments map[string]any) (any, error) {
	id, _ := arguments["invite_id"].(string)
	c.notFound = "No invite " + id + " in this workspace. List them with colliedesk_invites_list."
	if err := c.send(http.MethodDelete, "/api/v1/auth/invites/"+url.PathEscape(id), nil, nil); err != nil {
		return nil, err
	}
	return map[string]any{"revoked": true}, nil
}

func sessionsList(c *call, _ map[string]any) (any, error) {
	var page struct {
		Items []restSession `json:"items"`
	}
	if err := c.get("/api/v1/auth/sessions", nil, &page); err != nil {
		return nil, err
	}
	sessions := make([]map[string]any, len(page.Items))
	for index, session := range page.Items {
		state := "active"
		switch {
		case session.RevokedAt != nil:
			state = "revoked"
		case !session.ExpiresAt.After(time.Now()):
			state = "expired"
		}
		sessions[index] = map[string]any{"id": session.ID, "label": session.Label, "owner": session.AgentEmail, "token_tier": session.Scope, "permissions": session.Permissions,
			"this_token": session.Current, "state": state, "last_seen_at": utc(session.LastSeenAt), "expires_at": utc(session.ExpiresAt)}
	}
	return map[string]any{"sessions": sessions}, nil
}

func sessionRevoke(c *call, arguments map[string]any) (any, error) {
	id, _ := arguments["session_id"].(string)
	c.notFound = "No session " + id + " in this workspace. List them with colliedesk_sessions_list."
	if err := c.send(http.MethodDelete, "/api/v1/auth/sessions/"+url.PathEscape(id), nil, nil); err != nil {
		return nil, err
	}
	return map[string]any{"revoked": true}, nil
}

func webFormGet(c *call, _ map[string]any) (any, error) {
	var form json.RawMessage
	if err := c.get("/api/v1/channels/web-form", nil, &form); err != nil {
		return nil, err
	}
	return map[string]any{"web_form": form}, nil
}

func webFormUpdate(c *call, arguments map[string]any) (any, error) {
	var form json.RawMessage
	if err := c.send(http.MethodPut, "/api/v1/channels/web-form", arguments, &form); err != nil {
		return nil, err
	}
	return map[string]any{"web_form": form}, nil
}

// emailChannelView keeps only what an agent needs; the REST answer has no secret, and this view has
// no room for one either.
func emailChannelView(settings restEmailSettings) map[string]any {
	view := map[string]any{"connected": settings.Channel != nil, "inbound_url": settings.InboundURL, "hosted_address": settings.HostedAddress,
		"smtp_configured": settings.SMTP.Configured, "sending_from": settings.SendingFrom}
	gmail := map[string]any{"available": settings.Gmail.Available, "connected": false}
	if connection := settings.Gmail.Connection; connection != nil {
		gmail["connected"], gmail["address"], gmail["status"], gmail["error"] = true, connection.Address, connection.Status, connection.Error
	}
	view["gmail"] = gmail
	microsoft := map[string]any{"available": settings.Microsoft.Available, "connected": false, "admin_consent_url": settings.Microsoft.AdminConsentURL,
		"server_credentials_refused": settings.Microsoft.ServerCredentials}
	if connection := settings.Microsoft.Connection; connection != nil {
		microsoft["connected"], microsoft["address"], microsoft["status"], microsoft["error"] = true, connection.Address, connection.Status, connection.Error
	}
	view["microsoft"] = microsoft
	if channel := settings.Channel; channel != nil {
		view["channel_id"], view["support_address"], view["sender_name"] = channel.ChannelID, channel.SupportAddress, channel.SenderName
		view["inbound_secret_set"], view["domain"] = channel.SecretSet, channel.Domain
		view["domain_verified"], view["dns_record_name"], view["dns_record_value"] = channel.DomainVerifiedAt != nil, channel.DNSRecordName, channel.DNSRecordValue
	}
	return view
}

// emailChannelPath is the path of an email channel route: of the channel_id given, else of the default channel.
func emailChannelPath(c *call, arguments map[string]any, suffix string) string {
	id, _ := arguments["channel_id"].(string)
	if id == "" {
		return "/api/v1/channels/email" + suffix
	}
	c.notFound = "No email channel " + id + " in this workspace. List them with colliedesk_channels_list."
	return "/api/v1/channels/" + url.PathEscape(id) + "/email" + suffix
}

func emailChannelGet(c *call, arguments map[string]any) (any, error) {
	var settings restEmailSettings
	if err := c.get(emailChannelPath(c, arguments, ""), nil, &settings); err != nil {
		return nil, err
	}
	return map[string]any{"email_channel": emailChannelView(settings)}, nil
}

func emailChannelUpdate(c *call, arguments map[string]any) (any, error) {
	var settings restEmailSettings
	if err := c.send(http.MethodPut, "/api/v1/channels/email", arguments, &settings); err != nil {
		return nil, err
	}
	return map[string]any{"email_channel": emailChannelView(settings)}, nil
}

func emailDomainVerify(c *call, arguments map[string]any) (any, error) {
	c.notFound = "The email channel is not connected. Connect it first with colliedesk_email_channel_update."
	var settings restEmailSettings
	if err := c.send(http.MethodPost, emailChannelPath(c, arguments, "/domain-verification"), nil, &settings); err != nil {
		return nil, err
	}
	return map[string]any{"email_channel": emailChannelView(settings)}, nil
}

func emailTestMessageSend(c *call, arguments map[string]any) (any, error) {
	c.notFound = "The email channel is not connected. Connect it first with colliedesk_email_channel_update."
	var sent map[string]any
	if err := c.send(http.MethodPost, emailChannelPath(c, arguments, "/test-message"), nil, &sent); err != nil {
		return nil, err
	}
	return sent, nil
}

func emailForwardingConfirmationGet(c *call, arguments map[string]any) (any, error) {
	id, _ := arguments["channel_id"].(string)
	c.notFound = "No forwarding confirmation reached email channel " + id + " in the last 24 hours (or it is not an email channel of this workspace: list them with colliedesk_channels_list)."
	var confirmation map[string]any
	if err := c.get("/api/v1/channels/"+url.PathEscape(id)+"/email/forwarding-confirmation", nil, &confirmation); err != nil {
		return nil, err
	}
	return confirmation, nil
}

func billingGet(c *call, _ map[string]any) (any, error) {
	c.notFound = "This Colliedesk server has no billing (it is self-hosted), so there is no plan to show."
	var billing map[string]any
	if err := c.get("/api/v1/billing", nil, &billing); err != nil {
		return nil, err
	}
	delete(billing, "checkout_plans")
	return map[string]any{"billing": billing}, nil
}
