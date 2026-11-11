package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/auth"
)

// A browser zone that is UTC under another name is no zone to start on: the workspace stays on its
// default, nobody's own timezone is set, and an admin can still choose one.
func TestTheCreatorsFirstSignInIgnoresEveryNameOfUTC(t *testing.T) {
	api, service := testAPI(t)
	for i, alias := range []string{"UTC", "Etc/UTC", "GMT", "Etc/GMT", "UCT", "Etc/UCT", "Etc/Universal", "Universal", "Zulu", "Etc/Zulu",
		"Etc/GMT0", "GMT0", "Etc/GMT+0", "Etc/GMT-0", "Greenwich", "Etc/Greenwich"} {
		email := "admin@alias-" + string(rune('a'+i)) + ".example.com"
		_, link, err := service.CreateWorkspace(context.Background(), "Alias Desk "+string(rune('a'+i)), email)
		if err != nil {
			t.Fatal(err)
		}
		admin := redeemWithTimezone(t, api, link, alias)
		if zone, override := timezoneOf(t, api, admin); zone != "UTC" || override != nil {
			t.Errorf("browser %s: timezone=%q override=%v, want the workspace's UTC and no own timezone", alias, zone, override)
		}
		if zone := workspaceTimezoneOf(t, api, admin); zone != "UTC" {
			t.Errorf("browser %s: workspace timezone=%q, want UTC", alias, zone)
		}
	}
}

// Business hours set before the creator's first sign-in (with an agent token) count in the workspace's
// timezone: when that sign-in moves the workspace to the browser's zone, an open conversation's clock is
// counted again in it. Every day is open 08:00-17:00 with a first reply due in an hour; the conversation
// opened on Monday 2026-11-02 12:00 UTC: due 13:00 UTC in UTC, and in Asia/Tokyo (21:00 there, closed)
// an hour after Tuesday's 08:00 opening, 2026-11-03 00:00 UTC.
func TestTheCreatorsFirstSignInCountsTheClocksAgainInTheBrowsersTimezone(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	workspace, link, err := service.CreateWorkspace(context.Background(), "Tokyo Clock Desk", "admin@tokyo-clock.example.com")
	if err != nil {
		t.Fatal(err)
	}
	token, err := service.IssueAgentToken(context.Background(), workspace.Slug, "admin@tokyo-clock.example.com", auth.TokenRequest{Label: "setup", LegacyScope: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	targets := roundTheClock()
	targets["timezone"] = nil
	hours := []map[string]any{}
	for _, day := range []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"} {
		hours = append(hours, map[string]any{"day": day, "open": true, "from": "08:00", "to": "17:00"})
	}
	targets["hours"] = hours
	if response := request(t, api, http.MethodPut, "/api/v1/response-targets", targets, token.Token, nil); response.Code != http.StatusOK {
		t.Fatalf("targets: status=%d body=%s", response.Code, response.Body.String())
	}
	contact := create(t, api, token.Token, "/api/v1/contacts", map[string]any{"name": "Ada", "email": "ada@tokyo-clock.example.com"})
	id := create(t, api, token.Token, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Clock"})
	// The conversation opened on a known Monday, after the targets were turned on.
	if _, err := pool.Exec(context.Background(), `UPDATE response_targets SET enabled_at='2026-11-01T00:00:00Z' WHERE workspace_id=$1`, workspace.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `UPDATE conversations SET created_at='2026-11-02T12:00:00Z',sla_stale=true WHERE workspace_id=$1 AND id=$2`, workspace.ID, id); err != nil {
		t.Fatal(err)
	}
	due := func(token string) time.Time {
		t.Helper()
		var item slaConversation
		decode(t, request(t, api, http.MethodGet, "/api/v1/conversations/"+id, nil, token, nil), &item)
		if item.SLA == nil || item.SLA.FirstResponse == nil || item.SLA.FirstResponse.DueAt == nil {
			t.Fatalf("clock = %+v", item.SLA)
		}
		return *item.SLA.FirstResponse.DueAt
	}
	if before := due(token.Token); !before.Equal(time.Date(2026, time.November, 2, 13, 0, 0, 0, time.UTC)) {
		t.Fatalf("due %s in UTC, want 2026-11-02T13:00:00Z", before)
	}
	admin := redeemWithTimezone(t, api, link, "Asia/Tokyo")
	if zone := workspaceTimezoneOf(t, api, admin); zone != "Asia/Tokyo" {
		t.Fatalf("workspace timezone=%q, want the creator's browser's Asia/Tokyo", zone)
	}
	if after := due(admin); !after.Equal(time.Date(2026, time.November, 3, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("due %s after the workspace moved to Asia/Tokyo, want 2026-11-03T00:00:00Z", after)
	}
}

// A Cloud sign-up is its creator's first sign-in: the new workspace starts on the browser's zone.
func TestACloudSignupStartsTheWorkspaceOnTheBrowsersTimezone(t *testing.T) {
	f := newSignupFixture(t, nil)
	req := newRequest(t, http.MethodPost, "/api/v1/auth/cloud/signup", map[string]any{"workspace_name": "Hanoi Support", "timezone": "Asia/Ho_Chi_Minh"},
		f.token(t, map[string]any{"email": "ada@hanoi.example.com"}), nil)
	req.Header.Set("Origin", testOrigin)
	req.RemoteAddr = defaultPeer
	response := serve(f.api, req)
	wantResponse(t, response, http.StatusCreated, "")
	var created struct {
		Workspace struct {
			Timezone string `json:"timezone"`
		} `json:"workspace"`
		Agent struct {
			TimezoneOverride *string `json:"timezone_override"`
		} `json:"agent"`
	}
	decode(t, response, &created)
	if created.Workspace.Timezone != "Asia/Ho_Chi_Minh" || created.Agent.TimezoneOverride != nil {
		t.Fatalf("sign-up answer: workspace timezone=%q override=%v, want Asia/Ho_Chi_Minh and none of their own", created.Workspace.Timezone, created.Agent.TimezoneOverride)
	}
	var me struct {
		Workspace struct {
			Timezone string `json:"timezone"`
		} `json:"workspace"`
	}
	decode(t, request(t, f.api, http.MethodGet, "/api/v1/auth/me", nil, "", sessionCookie(t, response)), &me)
	if me.Workspace.Timezone != "Asia/Ho_Chi_Minh" {
		t.Fatalf("stored workspace timezone=%q, want Asia/Ho_Chi_Minh", me.Workspace.Timezone)
	}
}

// The export carries whether an admin chose the workspace's timezone, so a workspace moved to another
// server keeps a chosen UTC instead of starting again on its creator's browser.
func TestTheExportSaysWhenTheWorkspaceTimezoneWasChosen(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Export Zone Desk", "admin@export-zone.example.com")
	chosenAt := func() any {
		t.Helper()
		response := request(t, api, http.MethodGet, "/api/v1/export", nil, admin, nil)
		if response.Code != http.StatusOK {
			t.Fatalf("export status=%d", response.Code)
		}
		var manifest struct {
			Workspace map[string]any `json:"workspace"`
		}
		if err := json.Unmarshal(unzip(t, response.Body.Bytes())["manifest.json"], &manifest); err != nil {
			t.Fatal(err)
		}
		value, ok := manifest.Workspace["timezone_chosen_at"]
		if !ok {
			t.Fatalf("manifest workspace = %v, want timezone_chosen_at", manifest.Workspace)
		}
		return value
	}
	if value := chosenAt(); value != nil {
		t.Fatalf("timezone_chosen_at=%v before anyone chose it, want null", value)
	}
	if response := request(t, api, http.MethodPatch, "/api/v1/workspace/timezone", map[string]any{"timezone": "UTC"}, admin, nil); response.Code != http.StatusOK {
		t.Fatalf("choose UTC: status=%d body=%s", response.Code, response.Body.String())
	}
	value, _ := chosenAt().(string)
	if at, err := time.Parse(time.RFC3339Nano, value); err != nil || at.Location() != time.UTC {
		t.Fatalf("timezone_chosen_at=%q after the admin chose UTC, want a UTC time", value)
	}
}
