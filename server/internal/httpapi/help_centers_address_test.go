package httpapi_test

import (
	"context"
	"net/http"
	"testing"
)

// A help center without a custom domain of its own is served below /b/<handle> on the primary's
// addresses, with its own name, collections and canonical links; once its custom domain is active,
// that address moves there with a 301, keeping the path and query.
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

	wantPage(t, f.at(hostMain, "/b/hardware"), http.StatusOK,
		[]string{"Hardware answers", `href="/b/hardware/c/hardware"`, "Shared shelf", `href="/b/hardware/_/`},
		[]string{"Main answers", "Main shelf"})
	wantPage(t, f.at(hostMain, "/b/hardware"+cablePage), http.StatusOK,
		[]string{"Replace a cable", "copperline", `<link rel="canonical" href="https://` + hostMain + "/b/hardware" + cablePage + `">`}, nil)
	wantPage(t, f.at(hostMain, "/b/hardware/sitemap.xml"), http.StatusOK,
		[]string{"https://" + hostMain + "/b/hardware" + cablePage}, []string{articlePath(f.main)})
	wantPage(t, f.at(hostMain, "/b/hardware"+articlePath(f.main)), http.StatusNotFound, nil, []string{"bramble"})
	wantPage(t, f.at(hostMain, "/b/nothing-here"), http.StatusNotFound, nil, []string{"Hardware answers"})
	if response := f.vote(hostMain, "/b/hardware"+cablePage); response.Code != http.StatusSeeOther {
		t.Errorf("vote below the handle = %d, want 303", response.Code)
	}

	setStatus("active")
	wantMove(t, f.site, "/b/hardware"+cablePage+"?ref=mail", http.StatusMovedPermanently,
		"https://"+hostHardware+cablePage+"?ref=mail", "Host", hostMain)
	wantMove(t, f.site, "/b/hardware", http.StatusMovedPermanently, "https://"+hostHardware+"/", "Host", hostMain)
	wantMove(t, f.site, "/help/"+f.slug+"/b/hardware/c/hardware", http.StatusMovedPermanently,
		"https://"+hostMain+"/b/hardware/c/hardware")
	wantPage(t, f.at(hostHardware, "/b/hardware"), http.StatusNotFound, nil, []string{"Shared shelf"})
}
