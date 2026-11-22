package httpapi_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

// A second help center's custom domain is proved, opened as a visitor would and re-checked every day,
// the same as the primary's.
func TestASecondHelpCentersDomainIsCheckedAndRecheckedAtItsOwnAddress(t *testing.T) {
	requests := &addressRequests{}
	var site http.Handler
	api, service, _, dns := testAPIWithConfig(t, func(config *httpapi.Config) {
		config.HelpCenterAddressTransport = requests.transport(servedBy(func() http.Handler { return site }))
	})
	site = helpCenterSite(api)
	admin := createSession(t, api, service, "Address Second", "admin@address-second.example.com")
	enableHelpCenter(t, api, admin, "Main answers", true)
	hardware := createHelpCenter(t, api, admin, "Hardware", "hardware")
	path := domainPath + "?help_center_id=" + hardware.ID

	var set domainView
	kbCall(t, api, http.MethodPut, path, map[string]any{"hostname": "help.address-second.example"}, admin, http.StatusOK, &set)
	dns.set("_help-center-verify.help.address-second.example", set.token())
	var checked domainView
	kbCall(t, api, http.MethodPost, domainPath+"/check?help_center_id="+hardware.ID, nil, admin, http.StatusOK, &checked)
	if checked.Status != "active" || checked.AddressCheckedAt == nil || checked.AddressError != nil {
		t.Fatalf("check of the hardware help center's domain = %+v, want active with its address opened", checked)
	}
	if requests.count() != 1 || requests.seen[0].URL.String() != "https://help.address-second.example/" {
		t.Fatalf("the check sent %d requests, want 1 to https://help.address-second.example/", requests.count())
	}

	rechecked, _, _, err := api.(*httpapi.Server).RecheckHelpCenterDomains(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rechecked != 1 || requests.count() != 2 {
		t.Fatalf("the daily re-check checked %d domains and sent %d requests in all, want 1 and 2", rechecked, requests.count())
	}
}
