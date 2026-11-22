package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/auth"
)

// Help center settings need help_center:update; deleting one that answers on a custom domain takes that
// domain off the public web, the same as removing the domain, so it also needs kb_articles:publish.
func TestDeletingAHelpCenterWithACustomDomainNeedsPublish(t *testing.T) {
	f := newTwoHelpCenters(t)
	path := "/api/v1/help-centers/" + f.hardware
	reader := issueCustomToken(t, f.api, f.admin, "Reader", auth.PermHelpCenterRead).Token
	writer := issueCustomToken(t, f.api, f.admin, "Settings only", auth.PermHelpCenterRead, auth.PermHelpCenterUpdate).Token
	publisher := issueCustomToken(t, f.api, f.admin, "Publisher", auth.PermHelpCenterRead, auth.PermHelpCenterUpdate,
		auth.PermKBArticlesPublish).Token

	kbCall(t, f.api, http.MethodGet, path, nil, reader, http.StatusOK, nil)
	refused(t, f.api, http.MethodPatch, path, map[string]any{"title": "Renamed"}, reader, auth.PermHelpCenterUpdate)
	refused(t, f.api, http.MethodPost, "/api/v1/help-centers", map[string]any{"name": "Billing", "handle": "billing", "locale": "en"},
		reader, auth.PermHelpCenterUpdate)
	refused(t, f.api, http.MethodDelete, path, nil, reader, auth.PermHelpCenterUpdate)
	refused(t, f.api, http.MethodDelete, path, nil, writer, auth.PermKBArticlesPublish)
	wantPage(t, f.at(hostHardware, "/"), http.StatusOK, []string{"Hardware answers"}, nil)

	// Without a custom domain the help center is only a path on the primary's address: settings suffice.
	spare := createHelpCenter(t, f.api, f.admin, "Spare", "spare")
	kbCall(t, f.api, http.MethodDelete, "/api/v1/help-centers/"+spare.ID, nil, writer, http.StatusNoContent, nil)
	kbCall(t, f.api, http.MethodDelete, path, nil, publisher, http.StatusNoContent, nil)
	wantPage(t, f.at(hostHardware, "/"), http.StatusNotFound, nil, []string{"Hardware answers"})
}
