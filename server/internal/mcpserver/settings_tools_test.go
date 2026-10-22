package mcpserver_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// joinTeam invites email with the agent role through REST and redeems the link, as a person would.
func joinTeam(t *testing.T, e *env, w workspace, email string) {
	t.Helper()
	status, created := e.rest(http.MethodPost, "/api/v1/auth/invites", map[string]any{"email": email, "role_ceiling": "agent"}, w.admin)
	if status != http.StatusCreated {
		t.Fatalf("invite %s: status=%d body=%v", email, status, created)
	}
	parsed, err := url.Parse(created["url"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if status, body := e.rest(http.MethodPost, "/api/v1/auth/redeem", map[string]any{"token": parsed.Query().Get("token")}, ""); status != http.StatusOK {
		t.Fatalf("redeem %s: status=%d body=%v", email, status, body)
	}
}

func TestAdminTierRunsTheWorkspaceThroughTheTools(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("Settings Tools Desk", "admin@settings-tools.example.com")
	joinTeam(t, e, w, "agent@settings-tools.example.com")
	session := e.connect(w.adminTier, "")

	who := e.ok(session, "colliedesk_whoami", nil)
	if who["token_tier"] != "admin" || at(t, who, "agent", "role") != "admin" || !strings.Contains(stringify(who), "colliedesk_session_revoke") {
		t.Fatalf("whoami of an admin token = %v", who)
	}
	updated := e.ok(session, "colliedesk_workspace_settings_update", map[string]any{"timezone": "Europe/Berlin"})
	if at(t, updated, "workspace", "timezone") != "Europe/Berlin" {
		t.Fatalf("workspace timezone = %v", updated)
	}
	if got := e.ok(session, "colliedesk_workspace_settings_get", nil); at(t, got, "workspace", "timezone") != "Europe/Berlin" || got["your_timezone"] != "Europe/Berlin" {
		t.Fatalf("settings = %v", got)
	}
	workspaceRenamed := e.ok(session, "colliedesk_workspace_rename", map[string]any{"name": " Settings Tools Renamed "})
	if at(t, workspaceRenamed, "workspace", "name") != "Settings Tools Renamed" || at(t, workspaceRenamed, "workspace", "timezone") != "Europe/Berlin" {
		t.Fatalf("workspace rename = %v", workspaceRenamed)
	}
	if got := e.ok(session, "colliedesk_workspace_settings_get", nil); at(t, got, "workspace", "name") != "Settings Tools Renamed" {
		t.Fatalf("settings after rename = %v", got)
	}
	if message := e.fails(session, "colliedesk_workspace_rename", map[string]any{"name": "Two\nlines"}); !strings.Contains(message, "name") {
		t.Errorf("a workspace name with a line break: %q", message)
	}
	e.ok(session, "colliedesk_workspace_rename", map[string]any{"name": "Settings Tools Desk"})
	if mine := e.ok(session, "colliedesk_my_timezone_set", map[string]any{"timezone": "Asia/Tokyo"}); mine["your_timezone"] != "Asia/Tokyo" {
		t.Fatalf("my timezone = %v", mine)
	}

	member := e.ok(session, "colliedesk_team_member_update", map[string]any{"member": "agent@settings-tools.example.com", "active": false})
	if at(t, member, "member", "active") != false {
		t.Fatalf("deactivate = %v", member)
	}
	e.ok(session, "colliedesk_team_member_update", map[string]any{"member": "agent@settings-tools.example.com", "active": true})

	asked := requestedForTier(t, e, w.adminTier, "colliedesk_invite_create", map[string]any{"email": "asked@settings-tools.example.com"})
	if strings.Contains(stringify(asked), "token=") {
		t.Fatalf("invite request = %v", asked)
	}
	status, invited := e.rest(http.MethodPost, "/api/v1/auth/invites", map[string]any{"email": "new@settings-tools.example.com", "role_ceiling": "agent"}, w.admin)
	if status != http.StatusCreated {
		t.Fatalf("a person invites: %d %v", status, invited)
	}
	inviteID := at(t, invited, "invite", "id").(string)
	if listed := stringify(e.ok(session, "colliedesk_invites_list", nil)); !strings.Contains(listed, inviteID) || strings.Contains(listed, "token=") {
		t.Fatalf("invites = %s", listed)
	}
	if revoked := e.ok(session, "colliedesk_invite_revoke", map[string]any{"invite_id": inviteID}); revoked["revoked"] != true {
		t.Fatalf("revoke invite = %v", revoked)
	}

	sessions := e.ok(session, "colliedesk_sessions_list", nil)["sessions"].([]any)
	var readTokenID string
	for _, item := range sessions {
		if at(t, item, "label") == "test read" {
			readTokenID = at(t, item, "id").(string)
		}
	}
	if readTokenID == "" || strings.Contains(stringify(map[string]any{"s": sessions}), "cdk_agent_") {
		t.Fatalf("sessions = %v", sessions)
	}
	e.ok(session, "colliedesk_session_revoke", map[string]any{"session_id": readTokenID})
	if status, _ := e.rest(http.MethodGet, "/api/v1/auth/me", nil, w.read); status != http.StatusUnauthorized {
		t.Fatalf("revoked read token: status=%d, want 401", status)
	}

	form := e.ok(session, "colliedesk_web_form_update", map[string]any{"enabled": true, "allowed_origins": []any{"https://shop.example.com"}})
	if at(t, form, "web_form", "enabled") != true {
		t.Fatalf("web form = %v", form)
	}
	if got := e.ok(session, "colliedesk_web_form_get", nil); !strings.Contains(stringify(got), "https://shop.example.com") {
		t.Fatalf("web form get = %v", got)
	}
	channel := e.ok(session, "colliedesk_email_channel_update", map[string]any{"support_address": "help@settings-tools.example.com"})
	if at(t, channel, "email_channel", "support_address") != "help@settings-tools.example.com" || at(t, channel, "email_channel", "connected") != true {
		t.Fatalf("email channel = %v", channel)
	}
	if got := e.ok(session, "colliedesk_email_channel_get", nil); at(t, got, "email_channel", "dns_record_name") == "" || at(t, got, "email_channel", "sender_name") != "Settings Tools Desk" {
		t.Fatalf("email channel get = %v", got)
	}
	renamed := e.ok(session, "colliedesk_email_channel_update", map[string]any{"support_address": "help@settings-tools.example.com", "sender_name": "Settings Support"})
	if at(t, renamed, "email_channel", "sender_name") != "Settings Support" {
		t.Fatalf("sender name update = %v", renamed)
	}
	if message := e.fails(session, "colliedesk_email_channel_update", map[string]any{"support_address": "help@settings-tools.example.com", "sender_name": "Two\nlines"}); !strings.Contains(message, "sender_name") {
		t.Errorf("a sender name with a line break: %q", message)
	}
	if message := e.fails(session, "colliedesk_email_domain_verify", nil); !strings.Contains(message, "TXT record") {
		t.Errorf("verify without the DNS record: %q", message)
	}
	if message := e.fails(session, "colliedesk_billing_get", nil); !strings.Contains(message, "self-hosted") {
		t.Errorf("billing on a self-hosted server: %q", message)
	}
}

func TestLowerTiersAskForSettingsChangesAndAdminGrantsAlwaysWait(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("Settings Requests Desk", "admin@settings-requests.example.com")
	joinTeam(t, e, w, "agent@settings-requests.example.com")

	timezone := requestedForTier(t, e, w.send, "colliedesk_workspace_settings_update", map[string]any{"timezone": "Asia/Tokyo"})
	if summary := at(t, timezone, "agent_request", "summary").(string); !strings.Contains(summary, "UTC → Asia/Tokyo") {
		t.Errorf("timezone request summary = %q", summary)
	}
	requestedForTier(t, e, w.read, "colliedesk_my_timezone_set", map[string]any{"timezone": "Asia/Tokyo"})
	requestedForTier(t, e, w.draft, "colliedesk_web_form_update", map[string]any{"enabled": true, "allowed_origins": []any{}})
	requestedForTier(t, e, w.adminTier, "colliedesk_invite_create", map[string]any{"email": "boss@settings-requests.example.com", "role": "admin"})
	requestedForTier(t, e, w.adminTier, "colliedesk_team_member_update", map[string]any{"member": "agent@settings-requests.example.com", "role": "admin"})
	if mine := e.ok(e.connect(w.draft, ""), "colliedesk_my_timezone_set", map[string]any{"timezone": nil}); mine["your_timezone"] != nil {
		t.Fatalf("draft clears its own timezone = %v", mine)
	}

	_, me := e.rest(http.MethodGet, "/api/v1/auth/me", nil, w.admin)
	if at(t, me, "workspace", "timezone") != "UTC" {
		t.Fatalf("a request changed the workspace before approval: %v", me)
	}
	_, team := e.rest(http.MethodGet, "/api/v1/agents", nil, w.admin)
	for _, item := range team["items"].([]any) {
		if at(t, item, "email") == "agent@settings-requests.example.com" && at(t, item, "role") != "agent" {
			t.Fatalf("a promotion ran before approval: %v", item)
		}
	}
	// Reading the settings needs only read, and shows no secret.
	read := e.connect(w.read, "")
	for _, tool := range []string{"colliedesk_invites_list", "colliedesk_sessions_list", "colliedesk_web_form_get", "colliedesk_email_channel_get", "colliedesk_workspace_settings_get"} {
		e.ok(read, tool, nil)
	}
}

func TestSettingsToolsStayInTheirWorkspace(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("Own Settings", "admin@own-settings.example.com")
	other := e.newWorkspace("Other Settings", "admin@other-settings.example.com")
	joinTeam(t, e, other, "agent@other-settings.example.com")
	status, invited := e.rest(http.MethodPost, "/api/v1/auth/invites", map[string]any{"email": "pending@other-settings.example.com", "role_ceiling": "agent"}, other.admin)
	if status != http.StatusCreated {
		t.Fatalf("invite: %d %v", status, invited)
	}
	otherInvite := at(t, invited, "invite", "id").(string)
	_, theirSessions := e.rest(http.MethodGet, "/api/v1/auth/sessions", nil, other.admin)
	otherSession := at(t, theirSessions["items"].([]any)[0], "id").(string)
	session := e.connect(w.adminTier, "")

	for tool, args := range map[string]map[string]any{
		"colliedesk_team_member_update": {"member": "agent@other-settings.example.com", "active": false},
		"colliedesk_invite_revoke":      {"invite_id": otherInvite},
		"colliedesk_session_revoke":     {"session_id": otherSession},
	} {
		if message := e.fails(session, tool, args); !strings.Contains(message, "No ") {
			t.Errorf("%s on another workspace: %q", tool, message)
		}
	}
	for _, tool := range []string{"colliedesk_invites_list", "colliedesk_sessions_list"} {
		if listed := stringify(e.ok(session, tool, nil)); strings.Contains(listed, "other-settings") {
			t.Errorf("%s shows another workspace: %s", tool, listed)
		}
	}
	if status, _ := e.rest(http.MethodGet, "/api/v1/auth/me", nil, other.admin); status != http.StatusOK {
		t.Fatalf("the other workspace's session was revoked: %d", status)
	}
}
