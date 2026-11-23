package httpapi_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

// Another workspace that takes over the host name of one help center takes that name only: the
// holder's other help centers keep serving on their own names. A second help center of the same
// workspace that holds a pending claim on the name its primary serves on is refused with domain_taken
// when its check passes, never an internal error, and the primary keeps the name.
func TestATakeoverMovesOneHelpCenterAndLeavesTheHoldersOthers(t *testing.T) {
	api, service, pool, dns := testAPIWithConfig(t, func(config *httpapi.Config) { config.HelpCenterDomain = "help.example.net" })
	site := helpCenterSite(api)
	holder := createSession(t, api, service, "Takeover Holder", "admin@takeover-holder.example.com")
	owner := createSession(t, api, service, "Takeover Owner", "admin@takeover-owner.example.com")
	enableHelpCenter(t, api, holder, "Holder main", true)
	enableHelpCenter(t, api, owner, "Owner help", true)
	devices := createHelpCenter(t, api, holder, "Devices", "devices")
	const kept, squatted = "help.kept-main.example", "help.squatted-devices.example"
	onDevices := "?help_center_id=" + devices.ID

	main := setDomain(t, api, holder, kept, http.StatusOK)
	dns.setCNAME(kept, main.cnameTarget()+".")
	checkDomain(t, api, holder, http.StatusOK)
	var held domainView
	kbCall(t, api, http.MethodPut, domainPath+onDevices, map[string]any{"hostname": squatted}, holder, http.StatusOK, &held)
	dns.setCNAME(squatted, held.cnameTarget()+".")
	kbCall(t, api, http.MethodPost, domainPath+"/check"+onDevices, nil, holder, http.StatusOK, nil)
	wantPage(t, onHost(site, squatted, "/"), http.StatusOK, []string{"Devices answers"}, nil)

	claim := setDomain(t, api, owner, squatted, http.StatusOK)
	dns.setCNAME(squatted, claim.cnameTarget()+".")
	if view := checkDomain(t, api, owner, http.StatusOK); view.Status != "active" {
		t.Fatalf("the owner's check once the holder no longer proves the name = %+v", view)
	}
	var devicesAfter releaseView
	kbCall(t, api, http.MethodGet, domainPath+onDevices, nil, holder, http.StatusOK, &devicesAfter)
	if devicesAfter.Status != "pending" || devicesAfter.PendingReason == nil || *devicesAfter.PendingReason != "taken_over" {
		t.Errorf("the help center whose name was taken over = %+v, want pending taken_over", devicesAfter)
	}
	if got := getRelease(t, api, holder); got.Status != "active" || got.PendingReason != nil {
		t.Errorf("the holder's primary after another workspace took its devices name = %+v, want active", got)
	}
	wantPage(t, onHost(site, kept, "/"), http.StatusOK, []string{"Holder main"}, []string{"Owner help"})
	wantPage(t, onHost(site, squatted, "/"), http.StatusOK, []string{"Owner help"}, []string{"Devices answers"})

	// A claim on the primary's live name written past the refusal (two saves at once) loses its check.
	spare := createHelpCenter(t, api, holder, "Spare", "spare")
	if _, err := pool.Exec(context.Background(), `INSERT INTO help_center_domains (workspace_id, help_center_id, hostname, claim, token, status)
		SELECT workspace_id, $1, $2, 'aaaaaaaaaaaaaaaaaaaaaaaa', 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb', 'pending' FROM help_centers WHERE id=$1`,
		spare.ID, kept); err != nil {
		t.Fatal(err)
	}
	dns.set("_help-center-verify."+kept, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	taken := kbCall(t, api, http.MethodPost, domainPath+"/check?help_center_id="+spare.ID, nil, holder, http.StatusConflict, nil)
	if code := errorCode(t, taken); code != "domain_taken" {
		t.Errorf("a second help center's check of the primary's live name = %q, want domain_taken", code)
	}
	if got := getRelease(t, api, holder); got.Status != "active" {
		t.Errorf("the primary after its own workspace's second claim = %+v, want active", got)
	}
	wantPage(t, onHost(site, kept, "/"), http.StatusOK, []string{"Holder main"}, []string{"Spare answers"})
}
