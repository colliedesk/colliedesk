package httpapi_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/auth"
)

// A token that may read the help centers but not change them is refused on every settings write that
// names a help center by id, the refusal names help_center:update, and that help center stays as it was;
// the same token still reads it.
func TestASettingsWriteByHelpCenterIDNeedsHelpCenterUpdate(t *testing.T) {
	f := newTwoHelpCenters(t)
	reader := issueCustomToken(t, f.api, f.admin, "Reader", auth.PermHelpCenterRead).Token
	before := hardwareSettings(t, f)
	writes := 0
	for _, route := range helpCenterRoutes {
		if route.method == http.MethodGet || strings.Contains(route.path, "/theme/") || strings.HasPrefix(route.path, "/api/v1/kb/") {
			continue
		}
		path := strings.Replace(route.path, "%s", f.hardware, 1)
		var body any
		if route.body != nil {
			body = route.body(f.hardware)
		}
		refused(t, f.api, route.method, path, body, reader, auth.PermHelpCenterUpdate)
		writes++
	}
	if writes != 10 {
		t.Fatalf("%d settings writes checked, want 10", writes)
	}
	if after := hardwareSettings(t, f); after != before {
		t.Errorf("hardware changed under a read-only token:\nbefore %s\nafter  %s", before, after)
	}
	wantPage(t, f.at(hostHardware, "/"), http.StatusOK, []string{"Hardware answers"}, nil)
}

func hardwareSettings(t *testing.T, f *twoHelpCenters) string {
	t.Helper()
	var read strings.Builder
	for _, path := range []string{"/api/v1/help-center/theme", "/api/v1/help-center/domain", "/api/v1/help-center/languages",
		"/api/v1/help-center/search-console", "/api/v1/help-center/analytics"} {
		read.WriteString(kbCall(t, f.api, http.MethodGet, path+"?help_center_id="+f.hardware, nil, f.admin, http.StatusOK, nil).Body.String())
	}
	read.WriteString(kbCall(t, f.api, http.MethodGet, "/api/v1/help-centers/"+f.hardware, nil, f.admin, http.StatusOK, nil).Body.String())
	return read.String()
}
