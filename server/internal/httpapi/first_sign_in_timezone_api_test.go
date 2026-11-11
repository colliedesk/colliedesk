package httpapi_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/auth"
)

func redeemWithTimezone(t *testing.T, api http.Handler, link, timezone string) string {
	t.Helper()
	response := request(t, api, http.MethodPost, "/api/v1/auth/redeem", map[string]any{"token": linkToken(t, link), "timezone": timezone}, "", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("redeem: status=%d body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Token string `json:"token"`
	}
	decode(t, response, &body)
	return body.Token
}

func timezoneOf(t *testing.T, api http.Handler, token string) (string, *string) {
	t.Helper()
	var me struct {
		Timezone string `json:"timezone"`
		Agent    struct {
			TimezoneOverride *string `json:"timezone_override"`
		} `json:"agent"`
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/auth/me", nil, token, nil), &me)
	return me.Timezone, me.Agent.TimezoneOverride
}

func workspaceTimezoneOf(t *testing.T, api http.Handler, token string) string {
	t.Helper()
	var me struct {
		Workspace struct {
			Timezone string `json:"timezone"`
		} `json:"workspace"`
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/auth/me", nil, token, nil), &me)
	return me.Workspace.Timezone
}

// The person who created the workspace signs in first: the workspace, and the business hours its response
// targets count in, start on their browser's zone, and they follow it. It happens once.
func TestTheCreatorsFirstSignInStartsTheWorkspaceOnTheBrowsersTimezoneOnlyOnce(t *testing.T) {
	api, service := testAPI(t)
	workspace, link, err := service.CreateWorkspace(context.Background(), "London Desk", "admin@london.example.com")
	if err != nil {
		t.Fatal(err)
	}
	admin := redeemWithTimezone(t, api, link, "Europe/London")
	if zone := workspaceTimezoneOf(t, api, admin); zone != "Europe/London" {
		t.Fatalf("workspace timezone=%q after the creator's first sign-in, want the browser's Europe/London", zone)
	}
	if zone, override := timezoneOf(t, api, admin); zone != "Europe/London" || override != nil {
		t.Fatalf("the creator: timezone=%q override=%v, want Europe/London from the workspace", zone, override)
	}
	var targets struct {
		Timezone *string `json:"timezone"`
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/response-targets", nil, admin, nil), &targets)
	if targets.Timezone != nil {
		t.Fatalf("response targets timezone=%q, want null: they count in the workspace's Europe/London", *targets.Timezone)
	}
	again, err := service.Pair(context.Background(), workspace.Slug, "admin@london.example.com")
	if err != nil {
		t.Fatal(err)
	}
	second := redeemWithTimezone(t, api, again, "America/Denver")
	if zone, override := timezoneOf(t, api, second); zone != "Europe/London" || override != nil {
		t.Fatalf("after a later sign-in: timezone=%q override=%v, want the workspace's Europe/London kept", zone, override)
	}
}

// A teammate's first sign-in sets only their own timezone, once; the workspace keeps its zone.
func TestATeammatesFirstSignInSetsOnlyTheirOwnTimezoneOnlyOnce(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Team Zones Desk", "admin@team-zones.example.com")
	_, invite := createContractInvite(t, api, admin, "agent@team-zones.example.com", "agent")
	teammate := redeemWithTimezone(t, api, invite, "Asia/Ho_Chi_Minh")
	if zone, override := timezoneOf(t, api, teammate); zone != "Asia/Ho_Chi_Minh" || override == nil {
		t.Fatalf("after the teammate's first sign-in: timezone=%q override=%v, want the browser's Asia/Ho_Chi_Minh as their own", zone, override)
	}
	if zone := workspaceTimezoneOf(t, api, admin); zone != "UTC" {
		t.Fatalf("workspace timezone=%q after a teammate's first sign-in, want UTC unchanged", zone)
	}
	// The teammate goes back to the workspace's zone; a later sign-in from another browser keeps that choice.
	if response := request(t, api, http.MethodPatch, "/api/v1/agents/me/timezone", map[string]any{"timezone": nil}, teammate, nil); response.Code != http.StatusOK {
		t.Fatalf("same as workspace: status=%d body=%s", response.Code, response.Body.String())
	}
	again, err := service.Pair(context.Background(), "team-zones-desk", "agent@team-zones.example.com")
	if err != nil {
		t.Fatal(err)
	}
	second := redeemWithTimezone(t, api, again, "America/Denver")
	if zone, override := timezoneOf(t, api, second); zone != "UTC" || override != nil {
		t.Fatalf("after a later sign-in: timezone=%q override=%v, want the workspace's UTC kept", zone, override)
	}
	if zone := workspaceTimezoneOf(t, api, admin); zone != "UTC" {
		t.Fatalf("workspace timezone=%q after the teammate's later sign-in, want UTC unchanged", zone)
	}
}

// An admin who chose UTC on purpose keeps it, even when the choice came before the creator's first sign-in
// (with an agent token from collie token create): the creator's browser then sets only their own timezone.
func TestAChosenUTCWorkspaceTimezoneStaysUTCAtTheCreatorsFirstSignIn(t *testing.T) {
	api, service := testAPI(t)
	workspace, link, err := service.CreateWorkspace(context.Background(), "Chosen UTC Desk", "admin@chosen-utc.example.com")
	if err != nil {
		t.Fatal(err)
	}
	token, err := service.IssueAgentToken(context.Background(), workspace.Slug, "admin@chosen-utc.example.com", auth.TokenRequest{Label: "setup", LegacyScope: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	if response := request(t, api, http.MethodPatch, "/api/v1/workspace/timezone", map[string]any{"timezone": "UTC"}, token.Token, nil); response.Code != http.StatusOK {
		t.Fatalf("choose UTC: status=%d body=%s", response.Code, response.Body.String())
	}
	admin := redeemWithTimezone(t, api, link, "Europe/London")
	if zone := workspaceTimezoneOf(t, api, admin); zone != "UTC" {
		t.Fatalf("workspace timezone=%q, want the chosen UTC kept", zone)
	}
	if zone, override := timezoneOf(t, api, admin); zone != "Europe/London" || override == nil {
		t.Fatalf("the creator: timezone=%q override=%v, want their browser's Europe/London as their own", zone, override)
	}
}

func TestTheFirstSignInLeavesAChosenWorkspaceTimezoneAndRefusesAnUnknownZone(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Berlin Desk", "admin@berlin.example.com")
	if response := request(t, api, http.MethodPatch, "/api/v1/workspace/timezone", map[string]any{"timezone": "Europe/Berlin"}, admin, nil); response.Code != http.StatusOK {
		t.Fatalf("workspace timezone: status=%d body=%s", response.Code, response.Body.String())
	}
	_, invite := createContractInvite(t, api, admin, "agent@berlin.example.com", "agent")
	agent := redeemWithTimezone(t, api, invite, "Asia/Tokyo")
	if zone, override := timezoneOf(t, api, agent); zone != "Europe/Berlin" || override != nil {
		t.Fatalf("a team that chose Europe/Berlin: timezone=%q override=%v, want the workspace's zone", zone, override)
	}
	api2, service2 := testAPI(t)
	_, link, err := service2.CreateWorkspace(context.Background(), "Odd Zone Desk", "admin@odd-zone.example.com")
	if err != nil {
		t.Fatal(err)
	}
	odd := redeemWithTimezone(t, api2, link, "Mars/Olympus")
	if zone, override := timezoneOf(t, api2, odd); zone != "UTC" || override != nil {
		t.Fatalf("an unknown zone: timezone=%q override=%v, want nothing set", zone, override)
	}
}
