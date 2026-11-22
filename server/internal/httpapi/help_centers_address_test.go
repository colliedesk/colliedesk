package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
)

// A help center without a custom domain of its own is served below /<handle> on the primary's
// addresses, with its own name, collections, languages and canonical links; once its custom domain
// is active, that address moves there with a 301, keeping the path and query. The primary's own
// pages stay where they were, and the old /b/<handle> address is gone.
func TestAHelpCenterIsServedBelowItsHandleUntilItsCustomDomainIsActive(t *testing.T) {
	f := newTwoHelpCenters(t)
	ctx := context.Background()
	setStatus := func(status string) {
		t.Helper()
		if _, err := f.pool.Exec(ctx, `UPDATE help_center_domains SET status=$3 WHERE workspace_id=$1 AND help_center_id=$2`,
			f.workspace, f.hardware, status); err != nil {
			t.Fatal(err)
		}
	}
	setStatus("pending")
	cablePage := articlePath(f.cable)
	// The French title is "Changer un câble"; links carry its slug escaped.
	cableFRPage := fmt.Sprintf("/fr/a/%d-changer-un-c%%C3%%A2ble", f.cable.Number)
	below := "https://" + hostMain + "/hardware"

	wantPage(t, f.at(hostMain, "/hardware"), http.StatusOK,
		[]string{"Hardware answers", `href="/hardware/c/hardware"`, "Shared shelf", `href="/hardware/_/`},
		[]string{"Main answers", "Main shelf", `href="/b/`})
	wantPage(t, f.at(hostMain, "/hardware"+cablePage), http.StatusOK,
		[]string{"Replace a cable", "copperline", `<link rel="canonical" href="` + below + cablePage + `">`,
			`<link rel="alternate" hreflang="fr" href="` + below + cableFRPage + `">`}, nil)
	wantPage(t, f.at(hostMain, "/hardware"+cableFRPage), http.StatusOK,
		[]string{"Changer un câble", `<link rel="canonical" href="` + below + cableFRPage + `">`}, nil)
	wantPage(t, f.at(hostMain, "/hardware/sitemap.xml"), http.StatusOK,
		[]string{"<loc>" + below + cablePage + "</loc>"}, []string{articlePath(f.main)})
	wantPage(t, f.at(hostMain, "/hardware/llms.txt"), http.StatusOK,
		[]string{"(" + below + cablePage + ".md)"}, []string{articlePath(f.main), "/b/"})
	wantPage(t, f.at(hostMain, "/hardware"+articlePath(f.main)), http.StatusNotFound, nil, []string{"bramble"})
	wantPage(t, f.at(hostMain, "/b/hardware"), http.StatusNotFound, nil, []string{"Hardware answers"})
	wantPage(t, f.at(hostMain, "/b/hardware"+cablePage), http.StatusNotFound, nil, []string{"copperline"})
	wantPage(t, f.at(hostMain, "/nothing-here"), http.StatusNotFound, []string{"Main answers"}, []string{"Hardware answers"})
	if response := f.vote(hostMain, "/hardware"+cablePage); response.Code != http.StatusSeeOther {
		t.Errorf("vote below the handle = %d, want 303", response.Code)
	}

	// The primary keeps its routes and its language segments.
	wantPage(t, f.at(hostMain, articlePath(f.main)), http.StatusOK, []string{"bramble"}, []string{"Hardware answers"})
	wantPage(t, f.at(hostMain, "/fr"), http.StatusOK, []string{"Main answers"}, []string{"Hardware answers"})
	wantPage(t, f.at(hostMain, "/search?q=bramble"), http.StatusOK, []string{"Reset the router"}, nil)

	setStatus("active")
	wantMove(t, f.site, "/hardware"+cablePage+"?ref=mail", http.StatusMovedPermanently,
		"https://"+hostHardware+cablePage+"?ref=mail", "Host", hostMain)
	wantMove(t, f.site, "/hardware", http.StatusMovedPermanently, "https://"+hostHardware+"/", "Host", hostMain)
	wantMove(t, f.site, "/help/"+f.slug+"/hardware/c/hardware", http.StatusMovedPermanently,
		"https://"+hostMain+"/hardware/c/hardware")
	wantPage(t, f.at(hostHardware, "/hardware"), http.StatusNotFound, nil, []string{"Shared shelf"})
	wantPage(t, f.at(hostHardware, cablePage), http.StatusOK,
		[]string{`<link rel="canonical" href="https://` + hostHardware + cablePage + `">`}, nil)
}
