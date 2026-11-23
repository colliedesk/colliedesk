package httpapi_test

import (
	"net/http"
	"testing"
)

// A host name serves one help center on the whole server. A second help center of the same workspace
// cannot take the name the primary answers on, another workspace's claim of a live name waits and serves
// nothing, and the name is released for others the moment its help center is deleted.
func TestAHostNameServesOneHelpCenterOnTheWholeServer(t *testing.T) {
	f := newTwoHelpCenters(t)
	spare := createHelpCenter(t, f.api, f.admin, "Spare", "spare")
	taken := kbCall(t, f.api, http.MethodPut, "/api/v1/help-center/domain?help_center_id="+spare.ID, map[string]any{"hostname": hostMain},
		f.admin, http.StatusConflict, nil)
	if code := errorCode(t, taken); code != "domain_taken" {
		t.Errorf("a second help center of the workspace on the primary's host name = %q, want domain_taken", code)
	}
	wantPage(t, f.at(hostMain, "/"), http.StatusOK, []string{"Main answers"}, nil)

	other := createSession(t, f.api, f.service, "Other Desk", "admin@other-desk.example.com")
	enableHelpCenter(t, f.api, other, "Other answers", true)
	var claim domainView
	kbCall(t, f.api, http.MethodPut, "/api/v1/help-center/domain", map[string]any{"hostname": hostHardware}, other, http.StatusOK, &claim)
	if claim.Status != "pending" {
		t.Fatalf("another workspace's claim of a live host name = %+v, want pending", claim)
	}
	wantPage(t, f.at(hostHardware, "/"), http.StatusOK, []string{"Hardware answers"}, []string{"Other answers"})

	kbCall(t, f.api, http.MethodDelete, "/api/v1/help-centers/"+f.hardware, nil, f.admin, http.StatusNoContent, nil)
	wantPage(t, f.at(hostHardware, "/"), http.StatusNotFound, nil, []string{"Hardware answers"})
	kbCall(t, f.api, http.MethodPut, "/api/v1/help-center/domain?help_center_id="+spare.ID, map[string]any{"hostname": hostHardware},
		f.admin, http.StatusOK, nil)
}
