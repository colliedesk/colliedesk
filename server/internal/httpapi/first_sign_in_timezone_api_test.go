package httpapi_test

import (
	"context"
	"net/http"
	"testing"
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

func TestTheFirstSignInStartsAnAgentOnTheBrowsersTimezoneOnlyOnce(t *testing.T) {
	api, service := testAPI(t)
	workspace, link, err := service.CreateWorkspace(context.Background(), "Zones Desk", "admin@zones.example.com")
	if err != nil {
		t.Fatal(err)
	}
	admin := redeemWithTimezone(t, api, link, "Asia/Ho_Chi_Minh")
	if zone, override := timezoneOf(t, api, admin); zone != "Asia/Ho_Chi_Minh" || override == nil {
		t.Fatalf("after the first sign-in: timezone=%q override=%v, want the browser's Asia/Ho_Chi_Minh as the agent's own", zone, override)
	}
	// The agent goes back to the workspace's zone; a later sign-in from another browser keeps that choice.
	if response := request(t, api, http.MethodPatch, "/api/v1/agents/me/timezone", map[string]any{"timezone": nil}, admin, nil); response.Code != http.StatusOK {
		t.Fatalf("same as workspace: status=%d body=%s", response.Code, response.Body.String())
	}
	again, err := service.Pair(context.Background(), workspace.Slug, "admin@zones.example.com")
	if err != nil {
		t.Fatal(err)
	}
	second := redeemWithTimezone(t, api, again, "America/Denver")
	if zone, override := timezoneOf(t, api, second); zone != "UTC" || override != nil {
		t.Fatalf("after a later sign-in: timezone=%q override=%v, want the workspace's UTC kept", zone, override)
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
