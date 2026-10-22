package httpapi_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

// hostLookup is the privileged query that finds the workspace of an active custom host name.
const hostLookup = "FROM help_center_domains WHERE lower(hostname)=lower($1) AND status='active'"

// The proxy's certificate question for an active domain writes the database at most once per host
// name an hour, however often it is asked.
func TestCertificateQuestionNotesTheCertificateAtMostOncePerHour(t *testing.T) {
	now := time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)
	tracer := &statementTracer{}
	api, service, _, dns := testAPIOnPool(t, testPoolTraced(t, tracer), func(config *httpapi.Config) { config.Now = func() time.Time { return now } })
	admin := createSession(t, api, service, "Ask Often", "admin@ask-often.example.com")
	enableHelpCenter(t, api, admin, "Often help", true)
	const host = "help.often.example"
	view := setDomain(t, api, admin, host, http.StatusOK)
	dns.set("_help-center-verify."+host, view.token())
	checkDomain(t, api, admin, http.StatusOK)
	tracer.reset()
	for range 20 {
		if code := askCertificate(t, api, host); code != http.StatusOK {
			t.Fatalf("tls-ask = %d", code)
		}
	}
	if writes := tracer.count("SET certificate_requested_at"); writes != 1 {
		t.Fatalf("20 certificate questions wrote the database %d times, want 1", writes)
	}
	now = now.Add(61 * time.Minute)
	askCertificate(t, api, host)
	askCertificate(t, api, host)
	if writes := tracer.count("SET certificate_requested_at"); writes != 2 {
		t.Fatalf("after an hour: %d writes, want 2", writes)
	}
}

// Host names nobody serves cost a bounded number of database lookups: each client may make the server
// look up only a few unknown names a minute, and an unknown name is remembered. A refused lookup is the
// help center's busy page.
func TestRandomHostNamesCostABoundedNumberOfLookups(t *testing.T) {
	tracer := &statementTracer{}
	api, service, _, dns := testAPIOnPool(t, testPoolTraced(t, tracer), nil)
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Flooded Host", "admin@flooded-host.example.com")
	enableHelpCenter(t, api, admin, "Flooded help", true)
	const host = "help.flooded.example"
	view := setDomain(t, api, admin, host, http.StatusOK)
	dns.set("_help-center-verify."+host, view.token())
	checkDomain(t, api, admin, http.StatusOK)
	wantPage(t, onHost(site, host, "/"), http.StatusOK, []string{"Flooded help"}, nil)
	tracer.reset()

	busy := 0
	for index := range 20 {
		response := visit(site, "/", "198.51.100.7", "Host", fmt.Sprintf("random-%d.flood.example", index))
		switch response.Code {
		case http.StatusNotFound:
		case http.StatusTooManyRequests:
			busy++
		default:
			t.Fatalf("random host %d status=%d", index, response.Code)
		}
	}
	if lookups := tracer.count(hostLookup); lookups > 10 || busy == 0 {
		t.Fatalf("20 random hosts from one client: %d lookups, %d refused", lookups, busy)
	}
	// Remembered unknown names cost nothing more, and the real host still serves from its cache.
	before := tracer.count(hostLookup)
	visit(site, "/", "198.51.100.8", "Host", "random-0.flood.example")
	wantPage(t, visit(site, "/", "198.51.100.7", "Host", host), http.StatusOK, []string{"Flooded help"}, nil)
	if lookups := tracer.count(hostLookup) - before; lookups != 0 {
		t.Fatalf("a remembered name and the cached host made %d lookups", lookups)
	}
}

// A domain write forgets only the host names it changes, on this process: another workspace's active
// domain keeps its cached lookup, and a check that changes nothing forgets nothing. Checks of one
// workspace are bounded for people as for tokens.
func TestDomainWritesForgetOnlyTheirOwnHostNames(t *testing.T) {
	tracer := &statementTracer{}
	api, service, _, dns := testAPIOnPool(t, testPoolTraced(t, tracer), nil)
	site := helpCenterSite(api)
	alpha := createSession(t, api, service, "Forget Alpha", "admin@forget-alpha.example.com")
	beta := createSession(t, api, service, "Forget Beta", "admin@forget-beta.example.com")
	enableHelpCenter(t, api, alpha, "Alpha help", true)
	enableHelpCenter(t, api, beta, "Beta help", true)
	activate := func(token, host string) {
		view := setDomain(t, api, token, host, http.StatusOK)
		dns.set("_help-center-verify."+host, view.token())
		checkDomain(t, api, token, http.StatusOK)
	}
	activate(alpha, "help.forget-alpha.example")
	activate(beta, "help.forget-beta.example")
	wantPage(t, onHost(site, "help.forget-alpha.example", "/"), http.StatusOK, []string{"Alpha help"}, nil)
	wantPage(t, onHost(site, "help.forget-beta.example", "/"), http.StatusOK, []string{"Beta help"}, nil)

	alphaLookups := func() int {
		t.Helper()
		before := tracer.count(hostLookup)
		wantPage(t, onHost(site, "help.forget-alpha.example", "/"), http.StatusOK, []string{"Alpha help"}, nil)
		return tracer.count(hostLookup) - before
	}
	// Beta replaces and removes its domain: alpha's host stays cached, beta's old host stops at once.
	setDomain(t, api, beta, "docs.forget-beta.example", http.StatusOK)
	if response := onHost(site, "help.forget-beta.example", "/"); response.Code != http.StatusNotFound {
		t.Fatalf("beta's replaced host status=%d", response.Code)
	}
	kbCall(t, api, http.MethodDelete, domainPath, nil, beta, http.StatusOK, nil)
	if lookups := alphaLookups(); lookups != 0 {
		t.Fatalf("another workspace's writes made alpha's host look up again %d times", lookups)
	}
	// Alpha's failed check changes nothing and forgets nothing.
	dns.set("_help-center-verify.help.forget-alpha.example", "gone")
	if view := checkDomain(t, api, alpha, http.StatusOK); view.Status != "active" {
		t.Fatalf("failed check = %+v", view)
	}
	if lookups := alphaLookups(); lookups != 0 {
		t.Fatalf("a failed check made alpha's host look up again %d times", lookups)
	}
	// One workspace's checks: 10 an hour, whoever asks (a person here, after the ones above).
	for range 8 {
		checkDomain(t, api, alpha, http.StatusOK)
	}
	if response := request(t, api, http.MethodPost, domainPath+"/check", nil, alpha, nil); response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") == "" {
		t.Fatalf("11th check by a person status=%d", response.Code)
	}
}
