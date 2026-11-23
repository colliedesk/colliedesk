package httpapi_test

import (
	"fmt"
	"net/http"
	"testing"
)

// A language hidden on one help center leaves its pages there: that help center no longer serves or
// links its French address, and the primary, which keeps French visible, still serves its own.
func TestALanguageHiddenOnOneHelpCenterLeavesOnlyItsPages(t *testing.T) {
	f := newTwoHelpCenters(t)
	cablePage := articlePath(f.cable)
	frPage := fmt.Sprintf("/fr/a/%d-%s", f.cable.Number, f.cableFR.Slug)
	hreflang := `hreflang="fr"`
	wantPage(t, f.at(hostHardware, frPage), http.StatusOK, []string{"Changer un câble"}, nil)
	wantPage(t, f.at(hostHardware, cablePage), http.StatusOK, []string{hreflang}, nil)

	kbCall(t, f.api, http.MethodPut, "/api/v1/help-center/languages?help_center_id="+f.hardware,
		map[string]any{"languages": []map[string]any{{"locale": "fr", "visible": false}}}, f.admin, http.StatusOK, nil)

	wantPage(t, f.at(hostHardware, frPage), http.StatusNotFound, nil, []string{"Changer un câble"})
	wantPage(t, f.at(hostHardware, cablePage), http.StatusOK, []string{"Replace a cable"}, []string{hreflang})
	wantPage(t, f.at(hostMain, "/fr"), http.StatusOK, []string{"Main answers", `lang="fr"`}, nil)
}
