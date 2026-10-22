package httpapi_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
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

// The proxy's certificate question costs as little for names nobody serves: each client (behind
// TRUSTED_PROXIES) may make the server look up only a few unknown names a minute, an unknown name is
// remembered, and every refusal is the same 404. The proxy's own questions for an active domain and
// for a pending claim whose DNS proves it still answer 200.
func TestCertificateQuestionsForRandomNamesCostABoundedNumberOfLookups(t *testing.T) {
	const (
		pendingLookup = "status='pending')"
		claimLookup   = "FROM help_center_domains WHERE claim=$1"
	)
	tracer := &statementTracer{}
	api, service, _, dns := testAPIOnPool(t, testPoolTraced(t, tracer), func(config *httpapi.Config) {
		config.HelpCenterDomain = "help.example.net"
		config.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}
	})
	site := helpCenterSite(api)
	active := createSession(t, api, service, "Ask Flood Active", "admin@ask-flood-active.example.com")
	enableHelpCenter(t, api, active, "Active help", true)
	const activeHost = "help.ask-flood-active.example"
	view := setDomain(t, api, active, activeHost, http.StatusOK)
	dns.set("_help-center-verify."+activeHost, view.token())
	checkDomain(t, api, active, http.StatusOK)
	wantPage(t, onHost(site, activeHost, "/"), http.StatusOK, []string{"Active help"}, nil)
	pending := createSession(t, api, service, "Ask Flood Pending", "admin@ask-flood-pending.example.com")
	enableHelpCenter(t, api, pending, "Pending help", true)
	const pendingHost = "help.ask-flood-pending.example"
	dns.set("_help-center-verify."+pendingHost, setDomain(t, api, pending, pendingHost, http.StatusOK).token())
	tracer.reset()

	// ask puts the question from client; through the proxy (loopback) when client is not the proxy.
	ask := func(name, client string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/public/help-center/tls-ask?domain="+name, nil)
		req.Host, req.RemoteAddr = testOriginHost, "127.0.0.1:4242"
		if client != "127.0.0.1" {
			req.Header.Set("X-Forwarded-For", client)
		}
		return serve(api, req)
	}
	var names []string
	for index := range 20 {
		names = append(names, fmt.Sprintf("random-%d.ask-flood.example", index), fmt.Sprintf("hc-%024x.help.example.net", index))
	}
	for _, name := range names {
		response := ask(name, "198.51.100.7")
		var body struct {
			Error struct{ Code, Message string } `json:"error"`
		}
		if response.Code != http.StatusNotFound || json.Unmarshal(response.Body.Bytes(), &body) != nil ||
			body.Error.Code != "not_found" || body.Error.Message != "The requested resource was not found." {
			t.Fatalf("tls-ask %q status=%d body=%s", name, response.Code, response.Body.String())
		}
	}
	if lookups, pendings := tracer.count(hostLookup)+tracer.count(claimLookup), tracer.count(pendingLookup); lookups > 10 || pendings > 10 {
		t.Fatalf("%d random names from one client: %d host lookups, %d pending lookups", len(names), lookups, pendings)
	}
	// Remembered unknown names cost nothing more, from any client.
	before := tracer.count("")
	for _, name := range names[:10] {
		if code := ask(name, "198.51.100.8").Code; code != http.StatusNotFound {
			t.Fatalf("remembered %q status=%d", name, code)
		}
	}
	if queries := tracer.count("") - before; queries != 0 {
		t.Fatalf("remembered unknown names ran %d queries", queries)
	}
	// The active domain answers from its cache, even to the flooding client; the proxy's question for
	// the pending claim checks it live and activates it.
	for _, client := range []string{"198.51.100.7", "127.0.0.1"} {
		if code := ask(activeHost, client).Code; code != http.StatusOK {
			t.Fatalf("active host for %s = %d", client, code)
		}
	}
	if code := ask(pendingHost, "127.0.0.1").Code; code != http.StatusOK {
		t.Fatalf("pending host with proving DNS = %d", code)
	}
	if view := checkDomain(t, api, pending, http.StatusOK); view.Status != "active" {
		t.Fatalf("pending host after the question = %+v", view)
	}
}
