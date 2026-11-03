package mcpserver_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/polar"
)

// joinTeam invites email with the agent role through REST and redeems the link, as a person would.
func joinTeam(t *testing.T, e *env, w workspace, email string) {
	t.Helper()
	status, created := e.rest(http.MethodPost, "/api/v1/auth/invites", map[string]any{"email": email, "roles": []string{"Agent"}}, w.admin)
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

func TestAnAdminRoleTokenRunsTheWorkspaceThroughTheTools(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("Settings Tools Desk", "admin@settings-tools.example.com")
	joinTeam(t, e, w, "agent@settings-tools.example.com")
	session := e.connect(w.adminTier, "")

	who := e.ok(session, "colliedesk_whoami", nil)
	if at(t, who, "token", "role") != "Admin" || !strings.Contains(stringify(who), "colliedesk_session_revoke") {
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

	invited := e.ok(session, "colliedesk_invite_create", map[string]any{"email": "new@settings-tools.example.com", "roles": []string{"agent"}})
	link, _ := invited["url"].(string)
	if !strings.Contains(link, "token=") || at(t, invited, "invite", "roles", 0) != "Agent" || !strings.Contains(stringify(invited), "privately") {
		t.Fatalf("invite = %v, want the one-time link once, the Agent role by name and how to pass the link on", invited)
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
	// The sender name alone: the address stays as it is.
	nameOnly := e.ok(session, "colliedesk_email_channel_update", map[string]any{"sender_name": "Settings Team"})
	if at(t, nameOnly, "email_channel", "sender_name") != "Settings Team" || at(t, nameOnly, "email_channel", "support_address") != "help@settings-tools.example.com" {
		t.Fatalf("sender name alone = %v", nameOnly)
	}
	if message := e.fails(session, "colliedesk_email_domain_verify", nil); !strings.Contains(message, "TXT record") {
		t.Errorf("verify without the DNS record: %q", message)
	}
	if message := e.fails(session, "colliedesk_billing_get", nil); !strings.Contains(message, "self-hosted") {
		t.Errorf("billing on a self-hosted server: %q", message)
	}
}

// A token without a permission is refused at once with the permission it lacks, and nothing changes:
// nothing waits for a person.
func TestATokenWithoutThePermissionIsRefusedAndNothingChanges(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("Settings Refusals Desk", "admin@settings-requests.example.com")
	joinTeam(t, e, w, "agent@settings-requests.example.com")

	refusedWithoutPermission(t, e, w.send, "colliedesk_workspace_settings_update", map[string]any{"timezone": "Asia/Tokyo"})
	refusedWithoutPermission(t, e, w.read, "colliedesk_my_timezone_set", map[string]any{"timezone": "Asia/Tokyo"})
	refusedWithoutPermission(t, e, w.draft, "colliedesk_web_form_update", map[string]any{"enabled": true, "allowed_origins": []any{}})
	refusedWithoutPermission(t, e, w.send, "colliedesk_invite_create", map[string]any{"email": "boss@settings-requests.example.com", "roles": []string{"Admin"}})
	refusedWithoutPermission(t, e, w.send, "colliedesk_member_roles_set", map[string]any{"member": "agent@settings-requests.example.com", "add_roles": []string{"Admin"}})
	if mine := e.ok(e.connect(w.draft, ""), "colliedesk_my_timezone_set", map[string]any{"timezone": nil}); mine["your_timezone"] != nil {
		t.Fatalf("draft clears its own timezone = %v", mine)
	}

	_, me := e.rest(http.MethodGet, "/api/v1/auth/me", nil, w.admin)
	if at(t, me, "workspace", "timezone") != "UTC" {
		t.Fatalf("a refused call changed the workspace: %v", me)
	}
	_, team := e.rest(http.MethodGet, "/api/v1/agents", nil, w.admin)
	for _, item := range team["items"].([]any) {
		if at(t, item, "email") == "agent@settings-requests.example.com" && strings.Contains(stringify(item.(map[string]any)), `"Admin"`) {
			t.Fatalf("a refused call gave the Admin role: %v", item)
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
	status, invited := e.rest(http.MethodPost, "/api/v1/auth/invites", map[string]any{"email": "pending@other-settings.example.com", "roles": []string{"Agent"}}, other.admin)
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

// The tools run through the REST handlers, so the cloud refuses a brand in the sender name and the
// workspace name for an AI agent as it does for a person; a self-hosted server accepts it.
func TestTheCloudRefusesBrandNamesThroughTheTools(t *testing.T) {
	cloud := newEnvWith(t, func(config *httpapi.Config) {
		config.Billing = polar.Config{AccessToken: "token-for-test", WebhookSecret: "secret-for-test", APIURL: "http://127.0.0.1:1"}
	})
	w := cloud.newWorkspace("Brand Tools Desk", "admin@brand-tools.example.com")
	session := cloud.connect(w.adminTier, "")
	cloud.ok(session, "colliedesk_email_channel_update", map[string]any{"support_address": "help@brand-tools.example.com"})
	if message := cloud.fails(session, "colliedesk_email_channel_update", map[string]any{"support_address": "help@brand-tools.example.com", "sender_name": "Pay Pal Support"}); !strings.Contains(message, "not available") {
		t.Errorf("a brand sender name on the cloud: %q", message)
	}
	if message := cloud.fails(session, "colliedesk_workspace_rename", map[string]any{"name": "Pay Pal Support"}); !strings.Contains(message, "not available") {
		t.Errorf("a brand workspace name on the cloud: %q", message)
	}
	if got := cloud.ok(session, "colliedesk_email_channel_get", nil); at(t, got, "email_channel", "sender_name") != "Brand Tools Desk" {
		t.Fatalf("a refused sender name was stored: %v", got)
	}

	selfHosted := newEnv(t)
	own := selfHosted.newWorkspace("Brand Tools Self", "admin@brand-tools-self.example.com")
	mine := selfHosted.connect(own.adminTier, "")
	updated := selfHosted.ok(mine, "colliedesk_email_channel_update", map[string]any{"support_address": "help@brand-tools-self.example.com", "sender_name": "Pay Pal Support"})
	if at(t, updated, "email_channel", "sender_name") != "Pay Pal Support" {
		t.Fatalf("self-hosted sender name = %v", updated)
	}
	if renamed := selfHosted.ok(mine, "colliedesk_workspace_rename", map[string]any{"name": "Pay Pal Support"}); at(t, renamed, "workspace", "name") != "Pay Pal Support" {
		t.Fatalf("self-hosted rename = %v", renamed)
	}
}

// A token that follows a role its person no longer holds all of is stopped, and whoami says so and how to
// start it again, as sessions_list does.
func TestWhoamiSaysATokenIsStopped(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("Whoami Stopped", "admin@whoami-stopped.example.com")
	exporter := e.created("/api/v1/roles", map[string]any{"name": "Exporter", "permissions": []string{"conversations:read", "workspace:export"}}, w.admin)
	status, invite := e.rest(http.MethodPost, "/api/v1/auth/invites", map[string]any{"email": "agent@whoami-stopped.example.com", "roles": []string{"Agent", "Exporter"}}, w.admin)
	if status != http.StatusCreated {
		t.Fatalf("invite: status=%d body=%v", status, invite)
	}
	parsed, err := url.Parse(invite["url"].(string))
	if err != nil {
		t.Fatal(err)
	}
	status, redeemed := e.rest(http.MethodPost, "/api/v1/auth/redeem", map[string]any{"token": parsed.Query().Get("token")}, "")
	if status != http.StatusOK {
		t.Fatalf("redeem: status=%d body=%v", status, redeemed)
	}
	agent := redeemed["token"].(string)
	agentID := redeemed["agent"].(map[string]any)["id"].(string)
	session := e.connect(e.token(agent, "Exporter bot", "Exporter", nil), "")
	if who := e.ok(session, "colliedesk_whoami", nil); at(t, who, "token", "role") != "Exporter" || strings.Contains(stringify(who), `"stopped"`) {
		t.Fatalf("whoami of a running token = %v", who)
	}
	if status, body := e.rest(http.MethodPut, "/api/v1/agents/"+agentID+"/roles", map[string]any{"role_ids": []string{roleIDNamed(t, e, w, "Agent")}}, w.admin); status != http.StatusOK {
		t.Fatalf("take Exporter: status=%d body=%v", status, body)
	}
	who := e.ok(session, "colliedesk_whoami", nil)
	if stopped, _ := who["token"].(map[string]any)["stopped"].(string); !strings.Contains(stopped, "no longer holds every permission") || len(who["permissions"].([]any)) != 0 {
		t.Fatalf("whoami of a stopped token = %v (exporter %s)", who, exporter)
	}
}

func roleIDNamed(t *testing.T, e *env, w workspace, name string) string {
	t.Helper()
	status, roles := e.rest(http.MethodGet, "/api/v1/roles", nil, w.admin)
	if status != http.StatusOK {
		t.Fatalf("roles: status=%d body=%v", status, roles)
	}
	for _, item := range roles["items"].([]any) {
		if role := item.(map[string]any); role["name"] == name {
			return role["id"].(string)
		}
	}
	t.Fatalf("no role %s in %v", name, roles)
	return ""
}
