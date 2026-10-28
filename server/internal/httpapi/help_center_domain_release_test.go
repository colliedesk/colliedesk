package httpapi_test

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

type releaseView struct {
	domainView
	FailingSince  *string `json:"failing_since"`
	PendingReason *string `json:"pending_reason"`
}

func getRelease(t *testing.T, api http.Handler, token string) releaseView {
	t.Helper()
	var view releaseView
	kbCall(t, api, http.MethodGet, domainPath, nil, token, http.StatusOK, &view)
	return view
}

// An active domain whose DNS no longer proves it keeps serving while the checks fail for less than 7
// days; the first check after 7 days puts it back to pending, says why, and its old addresses serve
// again. A check that passes meanwhile starts the 7 days over.
func TestAnActiveDomainWithoutProofForSevenDaysGoesBackToPending(t *testing.T) {
	start := time.Date(2026, 10, 1, 3, 0, 0, 0, time.UTC)
	now := start
	api, service, _, dns := testAPIWithConfig(t, func(config *httpapi.Config) {
		config.HelpCenterDomain = "help.example.net"
		config.Now = func() time.Time { return now }
	})
	server := api.(*httpapi.Server)
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Release Alpha", "admin@release-alpha.example.com")
	slug := workspaceSlug(t, api, admin)
	enableHelpCenter(t, api, admin, "Release help", true)
	const host = "help.release.example"
	view := setDomain(t, api, admin, host, http.StatusOK)
	dns.set("_help-center-verify."+host, view.token())
	checkDomain(t, api, admin, http.StatusOK)
	recheckAt := func(at time.Time) releaseView {
		t.Helper()
		now = at
		if _, _, _, err := server.RecheckHelpCenterDomains(context.Background()); err != nil {
			t.Fatal(err)
		}
		return getRelease(t, api, admin)
	}

	dns.set("_help-center-verify."+host, "gone")
	day := 24 * time.Hour
	if got := recheckAt(start.Add(day)); got.Status != "active" || got.FailingSince == nil || got.CheckError == nil || got.PendingReason != nil {
		t.Fatalf("first failed re-check = %+v", got)
	}
	// Proof back for one check: the 7 days start over.
	dns.set("_help-center-verify."+host, view.token())
	if got := recheckAt(start.Add(3 * day)); got.Status != "active" || got.FailingSince != nil || got.CheckError != nil {
		t.Fatalf("passing re-check = %+v", got)
	}
	dns.set("_help-center-verify."+host, "gone")
	recheckAt(start.Add(4 * day))
	if got := recheckAt(start.Add(10*day + 23*time.Hour)); got.Status != "active" {
		t.Fatalf("6 days and 23 hours without proof = %+v", got)
	}
	wantPage(t, onHost(site, host, "/"), http.StatusOK, []string{"Release help"}, nil)

	got := recheckAt(start.Add(11 * day))
	if got.Status != "pending" || got.PendingReason == nil || *got.PendingReason != "proof_lost" || got.FailingSince != nil || got.CheckError == nil {
		t.Fatalf("7 days without proof = %+v", got)
	}
	if response := onHost(site, host, "/"); response.Code != http.StatusNotFound || strings.Contains(response.Body.String(), "Release help") {
		t.Fatalf("the released host status=%d", response.Code)
	}
	wantPage(t, visit(site, "/", "192.0.2.90", "Host", slug+".help.example.net"), http.StatusOK, []string{"Release help"}, nil)

	// Proving it again makes it active and clears the reason.
	dns.set("_help-center-verify."+host, view.token())
	if view := checkDomain(t, api, admin, http.StatusOK); view.Status != "active" {
		t.Fatalf("check after the release = %+v", view)
	}
	if got := getRelease(t, api, admin); got.PendingReason != nil || got.FailingSince != nil {
		t.Fatalf("reactivated = %+v", got)
	}
}

// Another workspace that proves a host name while the active workspace's records no longer prove it
// takes the host over at once; the old holder goes back to pending and says why. While the holder
// still proves it, the other claim waits with domain_taken.
func TestAnotherWorkspaceTakesOverAHostItsHolderNoLongerProves(t *testing.T) {
	api, service, _, dns := testAPIWithConfig(t, func(config *httpapi.Config) { config.HelpCenterDomain = "help.example.net" })
	site := helpCenterSite(api)
	holder := createSession(t, api, service, "Takeover Holder", "admin@takeover-holder.example.com")
	owner := createSession(t, api, service, "Takeover Owner", "admin@takeover-owner.example.com")
	enableHelpCenter(t, api, holder, "Holder help", true)
	enableHelpCenter(t, api, owner, "Owner help", true)
	holderSlug := workspaceSlug(t, api, holder)
	const host = "help.squatted.example"
	held := setDomain(t, api, holder, host, http.StatusOK)
	dns.setCNAME(host, held.cnameTarget()+".")
	dns.set("_help-center-verify."+host, held.token())
	checkDomain(t, api, holder, http.StatusOK)

	// The real owner may claim a host name another workspace holds; it waits while the holder proves it.
	claim := setDomain(t, api, owner, host, http.StatusOK)
	if claim.Status != "pending" {
		t.Fatalf("claiming a held host name = %+v", claim)
	}
	dns.setCNAME(host, claim.cnameTarget()+".")
	if response := kbCall(t, api, http.MethodPost, domainPath+"/check", nil, owner, http.StatusConflict, nil); !strings.Contains(response.Body.String(), "domain_taken") {
		t.Fatalf("check while the holder's TXT still proves it = %s", response.Body.String())
	}
	if got := getRelease(t, api, holder); got.Status != "active" {
		t.Fatalf("the holder after a refused takeover = %+v", got)
	}

	dns.set("_help-center-verify."+host, "removed")
	if view := checkDomain(t, api, owner, http.StatusOK); view.Status != "active" {
		t.Fatalf("the owner's check once the holder no longer proves it = %+v", view)
	}
	got := getRelease(t, api, holder)
	if got.Status != "pending" || got.PendingReason == nil || *got.PendingReason != "taken_over" || got.CheckError == nil {
		t.Fatalf("the holder after the takeover = %+v", got)
	}
	page := onHost(site, host, "/").Body.String()
	if !strings.Contains(page, "Owner help") || strings.Contains(page, "Holder help") {
		t.Fatalf("the host serves %s", page)
	}
	wantPage(t, visit(site, "/", "192.0.2.91", "Host", holderSlug+".help.example.net"), http.StatusOK, []string{"Holder help"}, nil)
}

// Two workspaces whose TXT tokens both prove a host its holder no longer proves race for it: exactly
// one takes it over, the other gets domain_taken, and the host always has one active claim.
func TestATakeoverIsAtomic(t *testing.T) {
	api, service, pool, dns := testAPIWithConfig(t, nil)
	holder := createSession(t, api, service, "Race Holder", "admin@race-holder.example.com")
	const host = "help.raced.example"
	held := setDomain(t, api, holder, host, http.StatusOK)
	dns.set("_help-center-verify."+host, held.token())
	checkDomain(t, api, holder, http.StatusOK)
	var tokens, challengers []string
	for _, name := range []string{"Race One", "Race Two", "Race Three"} {
		session := createSession(t, api, service, name, "admin@"+strings.ReplaceAll(strings.ToLower(name), " ", "-")+".example.com")
		challengers = append(challengers, session)
		tokens = append(tokens, setDomain(t, api, session, host, http.StatusOK).token())
	}
	dns.set("_help-center-verify."+host, tokens...)
	codes := make([]int, len(challengers))
	var wait sync.WaitGroup
	for index, session := range challengers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			codes[index] = serve(api, newRequest(t, http.MethodPost, domainPath+"/check", nil, session, nil)).Code
		}()
	}
	wait.Wait()
	won := 0
	for _, code := range codes {
		switch code {
		case http.StatusOK:
			won++
		case http.StatusConflict:
		default:
			t.Errorf("a racing check answered %d", code)
		}
	}
	if won != 1 {
		t.Fatalf("%d racing checks won the host: %v", won, codes)
	}
	if active := queryString(t, pool, `SELECT count(*)::text FROM help_center_domains WHERE hostname=$1 AND status='active'`, host); active != "1" {
		t.Fatalf("%s active claims after the race", active)
	}
	if got := getRelease(t, api, holder); got.Status != "pending" || got.PendingReason == nil || *got.PendingReason != "taken_over" {
		t.Fatalf("the holder after the race = %+v", got)
	}
}

// A host name may have many pending claims, from as many workspaces: squatters' claims never keep the
// certificate question from activating the claim the DNS proves, whichever came first.
func TestPendingClaimsNeverBlockTheClaimDNSProves(t *testing.T) {
	api, service, _, dns := testAPIWithConfig(t, func(config *httpapi.Config) { config.HelpCenterDomain = "help.example.net" })
	const host = "help.crowded.example"
	for index := range 7 {
		squatter := createSession(t, api, service, "Squatter "+string(rune('A'+index)), "admin@squatter-"+string(rune('a'+index))+".example.com")
		enableHelpCenter(t, api, squatter, "", true)
		setDomain(t, api, squatter, host, http.StatusOK)
	}
	owner := createSession(t, api, service, "Crowd Owner", "admin@crowd-owner.example.com")
	enableHelpCenter(t, api, owner, "Owner help", true)
	claim := setDomain(t, api, owner, host, http.StatusOK)
	dns.setCNAME(host, claim.cnameTarget()+".")
	if code := askCertificate(t, api, host); code != http.StatusOK {
		t.Fatalf("tls-ask behind 7 older pending claims = %d", code)
	}
	if got := getRelease(t, api, owner); got.Status != "active" {
		t.Fatalf("the owner's claim = %+v", got)
	}
}
