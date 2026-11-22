package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
)

// Each help center of a workspace shows the collections placed on it, in its own name, and a
// collection placed on both shows on both, each article with the canonical address of the help
// center that serves it.
func TestEachHelpCenterShowsItsOwnCollectionsAndTheSharedOnes(t *testing.T) {
	f := newTwoHelpCenters(t)

	wantPage(t, f.at(hostMain, "/"), http.StatusOK, []string{"Main answers", "Main shelf", "Shared shelf"},
		[]string{"Hardware answers", `href="/c/hardware"`, "Cables"})
	wantPage(t, f.at(hostHardware, "/"), http.StatusOK, []string{"Hardware answers", `href="/c/hardware"`, "Shared shelf"},
		[]string{"Main answers", "Main shelf"})
	wantPage(t, f.at(hostHardware, "/c/hardware"), http.StatusOK, []string{"Cables"}, nil)

	wantPage(t, f.at(hostHardware, articlePath(f.cable)), http.StatusOK,
		[]string{"Replace a cable", "copperline", `<link rel="canonical" href="https://` + hostHardware + articlePath(f.cable) + `">`}, nil)
	for _, host := range []string{hostMain, hostHardware} {
		wantPage(t, f.at(host, articlePath(f.shared)), http.StatusOK,
			[]string{"Warranty terms", "thistle", `<link rel="canonical" href="https://` + host + articlePath(f.shared) + `">`}, nil)
	}
	wantPage(t, f.at(hostMain, articlePath(f.main)), http.StatusOK, []string{"Reset the router", "bramble"}, nil)
	wantPage(t, f.at(hostHardware, articlePath(f.main)), http.StatusNotFound, nil, []string{"Reset the router", "bramble"})
}

// An article that is only on another help center of the same workspace is not there at all: its
// page in any language, its collections, its images, video and files, search, sitemap.xml,
// llms.txt, votes and old Freshdesk addresses all answer as if it did not exist.
func TestAHelpCenterNeverServesWhatIsOnlyOnAnother(t *testing.T) {
	f := newTwoHelpCenters(t)
	cablePage := articlePath(f.cable)
	frPage := fmt.Sprintf("/fr/a/%d-%s", f.cable.Number, f.cableFR.Slug)
	shown := []string{cablePage, frPage, "/c/hardware", "/c/hardware/cables",
		"/images/" + f.image.ID, "/media/" + f.video.ID, "/files/" + f.file.ID + "/cable-chart.csv"}
	for _, path := range append(shown, fmt.Sprintf("/a/%d", f.cable.Number)) {
		wantPage(t, f.at(hostMain, path), http.StatusNotFound, nil, []string{"Replace a cable", "copperline", "cuivreline"})
	}
	for _, path := range shown {
		if response := f.at(hostHardware, path); response.Code != http.StatusOK {
			t.Errorf("GET %s on its own help center = %d, want 200", path, response.Code)
		}
	}

	wantPage(t, f.at(hostMain, "/search?q=copperline"), http.StatusOK, nil, []string{"Replace a cable", cablePage})
	wantPage(t, f.at(hostHardware, "/search?q=copperline"), http.StatusOK, []string{"Replace a cable"}, nil)
	wantPage(t, f.at(hostMain, "/fr/search?q=cuivreline"), http.StatusOK, nil, []string{"Changer un câble"})
	wantPage(t, f.at(hostHardware, "/fr/search?q=cuivreline"), http.StatusOK, []string{"Changer un câble"}, nil)
	wantPage(t, f.at(hostMain, "/search?q=thistle"), http.StatusOK, []string{"Warranty terms"}, nil)

	for _, file := range []string{"/sitemap.xml", "/llms.txt"} {
		wantPage(t, f.at(hostMain, file), http.StatusOK, []string{"https://" + hostMain + articlePath(f.shared)},
			[]string{cablePage, "/c/hardware", hostHardware})
		wantPage(t, f.at(hostHardware, file), http.StatusOK,
			[]string{"https://" + hostHardware + cablePage, "https://" + hostHardware + articlePath(f.shared)},
			[]string{articlePath(f.main), hostMain})
	}

	if response := f.vote(hostMain, cablePage); response.Code != http.StatusNotFound {
		t.Errorf("vote on another help center's article = %d, want 404", response.Code)
	}
	var votes int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM kb_article_votes WHERE article_id=$1`, f.cable.ID).Scan(&votes); err != nil || votes != 0 {
		t.Fatalf("votes=%d err=%v, want none", votes, err)
	}
	if response := f.vote(hostHardware, cablePage); response.Code != http.StatusSeeOther {
		t.Errorf("vote on its own help center = %d, want 303", response.Code)
	}

	ledgerRow(t, f.pool, f.slug, "freshdesk:account:1", "kb_article", "7301", f.cable.ID, "done", "2026-11-20 10:00:00+00")
	old := "/support/solutions/articles/7301-replace-a-cable"
	wantMove(t, f.site, old, http.StatusFound, "https://"+hostMain+"/search?q=replace+a+cable", "Host", hostMain)
	wantMove(t, f.site, old, http.StatusMovedPermanently, "https://"+hostHardware+cablePage, "Host", hostHardware)
}
