package httpapi_test

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

// roundTrip is an http.RoundTripper made of a function.
type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

// addressRequests records the requests the custom domain check sent, and answers each one with answer.
type addressRequests struct {
	mu   sync.Mutex
	seen []*http.Request
}

func (r *addressRequests) transport(answer http.RoundTripper) http.RoundTripper {
	return roundTrip(func(request *http.Request) (*http.Response, error) {
		r.mu.Lock()
		r.seen = append(r.seen, request)
		r.mu.Unlock()
		return answer.RoundTrip(request)
	})
}

func (r *addressRequests) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.seen)
}

// servedBy answers a request the way handler would on the request's own host name, from a client address
// no test uses, so the check's visit never spends another client's page or lookup allowance.
func servedBy(handler func() http.Handler) http.RoundTripper {
	return roundTrip(func(request *http.Request) (*http.Response, error) {
		inner := httptest.NewRequest(request.Method, request.URL.String(), nil)
		inner.Host, inner.RemoteAddr = request.URL.Host, "192.0.2.250:4242"
		for name, values := range request.Header {
			inner.Header[name] = values
		}
		return serve(handler(), inner).Result(), nil
	})
}

// dialing is a transport that sends every request to address, whatever its host name, with the
// certificate checks of a real client.
func dialing(address string, configure func(*http.Transport)) http.RoundTripper {
	transport := &http.Transport{DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}}
	if configure != nil {
		configure(transport)
	}
	return transport
}

// addressFixture is an API whose custom domain check opens the help center's address through
// transport, a workspace with its help center on, and the host name it set and proved by TXT.
func addressFixture(t *testing.T, transport func(api func() http.Handler) http.RoundTripper) (api http.Handler, admin string, dns *fakeDNS, requests *addressRequests) {
	t.Helper()
	requests = &addressRequests{}
	var site http.Handler
	api, service, _, dns := testAPIWithConfig(t, func(config *httpapi.Config) {
		config.HelpCenterAddressTransport = requests.transport(transport(func() http.Handler { return site }))
	})
	site = helpCenterSite(api)
	admin = createSession(t, api, service, "Address Alpha", "admin@address-alpha.example.com")
	enableHelpCenter(t, api, admin, "Alpha answers", true)
	view := setDomain(t, api, admin, "help.address-alpha.example", http.StatusOK)
	dns.set("_help-center-verify.help.address-alpha.example", view.token())
	return api, admin, dns, requests
}

func TestHelpCenterDomainCheckProvesTheAddressOpensWithThisHelpCenter(t *testing.T) {
	api, admin, _, requests := addressFixture(t, servedBy)
	view := checkDomain(t, api, admin, http.StatusOK)
	if view.Status != "active" || view.AddressURL == nil || *view.AddressURL != "https://help.address-alpha.example" {
		t.Fatalf("check = %+v, want active at https://help.address-alpha.example", view)
	}
	if view.AddressCheckedAt == nil || view.AddressError != nil {
		t.Fatalf("the address opened with this help center, but the check says %v at %v", view.AddressError, view.AddressCheckedAt)
	}
	if requests.count() != 1 {
		t.Fatalf("the check sent %d requests, want 1", requests.count())
	}
	sent := requests.seen[0]
	if sent.Method != http.MethodGet || sent.URL.String() != "https://help.address-alpha.example/" {
		t.Fatalf("the check asked %s %s", sent.Method, sent.URL)
	}
	for _, header := range []string{"Cookie", "Authorization"} {
		if sent.Header.Get(header) != "" {
			t.Fatalf("the check sent credentials in %s", header)
		}
	}
	// The result is the domain's state: reading it again shows the same answer without a new request.
	var again domainView
	kbCall(t, api, http.MethodGet, domainPath, nil, admin, http.StatusOK, &again)
	if again.AddressCheckedAt == nil || again.AddressError != nil || requests.count() != 1 {
		t.Fatalf("read after the check = %+v, requests %d", again, requests.count())
	}
}

func TestHelpCenterDomainCheckSaysWhenTheAddressHasNoCertificateYet(t *testing.T) {
	// A server whose certificate no public authority signed: what a visitor's browser refuses.
	untrusted := httptest.NewTLSServer(http.NotFoundHandler())
	t.Cleanup(untrusted.Close)
	api, admin, _, _ := addressFixture(t, func(func() http.Handler) http.RoundTripper {
		return dialing(untrusted.Listener.Addr().String(), nil)
	})
	view := checkDomain(t, api, admin, http.StatusOK)
	if view.Status != "active" || view.AddressCheckedAt == nil || view.AddressError == nil || !strings.Contains(*view.AddressError, "has no valid certificate") {
		t.Fatalf("check without a certificate = %+v", view)
	}
	if strings.Contains(*view.AddressError, "x509") || strings.Contains(*view.AddressError, untrusted.Listener.Addr().String()) {
		t.Fatalf("the cause names what the network answered: %q", *view.AddressError)
	}
}

func TestHelpCenterDomainCheckSaysWhenTheAddressAnswersFromSomewhereElse(t *testing.T) {
	foreign := roundTrip(func(request *http.Request) (*http.Response, error) {
		answer := httptest.NewRecorder()
		answer.Header().Set("Location", "https://parked.example.net/")
		answer.WriteHeader(http.StatusFound)
		return answer.Result(), nil
	})
	api, admin, _, requests := addressFixture(t, func(func() http.Handler) http.RoundTripper { return foreign })
	view := checkDomain(t, api, admin, http.StatusOK)
	if view.Status != "active" || view.AddressError == nil || !strings.Contains(*view.AddressError, "answers, but not with this help center") {
		t.Fatalf("check of a foreign answer = %+v", view)
	}
	if requests.count() != 1 {
		t.Fatalf("the check followed a redirect off the host: %d requests", requests.count())
	}
}

func TestHelpCenterDomainCheckSaysWhenTheAddressDoesNotAnswerInTime(t *testing.T) {
	// A server that accepts the connection and never answers.
	silent, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = silent.Close() })
	go func() {
		for {
			connection, err := silent.Accept()
			if err != nil {
				return
			}
			t.Cleanup(func() { _ = connection.Close() })
		}
	}()
	api, admin, _, _ := addressFixture(t, func(func() http.Handler) http.RoundTripper {
		return dialing(silent.Addr().String(), func(transport *http.Transport) { transport.TLSHandshakeTimeout = 100 * time.Millisecond })
	})
	view := checkDomain(t, api, admin, http.StatusOK)
	if view.Status != "active" || view.AddressError == nil || !strings.Contains(*view.AddressError, "did not answer in time") {
		t.Fatalf("check of a silent address = %+v", view)
	}
}

// The address is opened only once DNS proved the claim, so a host name an admin typed never makes the
// server reach a name the workspace does not control, and only where the help center serves.
func TestHelpCenterDomainCheckOpensTheAddressOnlyOnceDNSProvesItAndTheHelpCenterServesThere(t *testing.T) {
	api, admin, dns, requests := addressFixture(t, servedBy)
	dns.set("_help-center-verify.help.address-alpha.example", "someone else's token")
	if view := checkDomain(t, api, admin, http.StatusOK); view.Status != "pending" || view.AddressCheckedAt != nil || requests.count() != 0 {
		t.Fatalf("check without proof = %+v, requests %d", view, requests.count())
	}
	var current domainView
	kbCall(t, api, http.MethodGet, domainPath, nil, admin, http.StatusOK, &current)
	dns.set("_help-center-verify.help.address-alpha.example", current.token())
	if view := checkDomain(t, api, admin, http.StatusOK); view.AddressCheckedAt == nil || requests.count() != 1 {
		t.Fatalf("check with proof = %+v, requests %d", view, requests.count())
	}
	// With the help center off nothing serves on the domain: no request, and the earlier answer is gone.
	enableHelpCenter(t, api, admin, "Alpha answers", false)
	if view := checkDomain(t, api, admin, http.StatusOK); view.Status != "active" || view.AddressCheckedAt != nil || view.AddressError != nil || requests.count() != 1 {
		t.Fatalf("check with the help center off = %+v, requests %d", view, requests.count())
	}
	// A failed DNS check of an active domain proves nothing either.
	enableHelpCenter(t, api, admin, "Alpha answers", true)
	dns.set("_help-center-verify.help.address-alpha.example", "someone else's token")
	if view := checkDomain(t, api, admin, http.StatusOK); view.CheckError == nil || view.AddressCheckedAt != nil || requests.count() != 1 {
		t.Fatalf("failed check of an active domain = %+v, requests %d", view, requests.count())
	}
}

// During the trial the help center does not serve on its custom domain, so its address is not opened
// and no failure shows for it.
func TestTrialCustomDomainCheckDoesNotOpenTheAddress(t *testing.T) {
	requests := &addressRequests{}
	dns := &fakeDNS{records: map[string][]string{}, cnames: map[string]string{}}
	f := newLimitsFixtureConfigured(t, true, nil, func(config *httpapi.Config) {
		config.HelpCenterDomain = trialDomainZone
		config.LookupTXT, config.LookupCNAME = dns.lookupTXT, dns.lookupCNAME
		config.HelpCenterAddressTransport = requests.transport(roundTrip(func(*http.Request) (*http.Response, error) {
			t.Error("a trial workspace's address was opened")
			return nil, context.Canceled
		}))
	})
	enableHelpCenter(t, f.api, f.adminA, "Alpha Answers", true)
	verifyDomain(t, f.api, dns, f.adminA, "help.address-trial.example")
	if state := customDomainState(t, f.api, f.adminA); state.AddressCheckedAt != nil || state.AddressError != nil || requests.count() != 0 {
		t.Fatalf("trial domain = %+v, requests %d", state, requests.count())
	}
}

func TestDailyRecheckRecordsWhetherTheAddressOpens(t *testing.T) {
	untrusted := httptest.NewTLSServer(http.NotFoundHandler())
	t.Cleanup(untrusted.Close)
	var broken bool
	var mu sync.Mutex
	api, admin, _, _ := addressFixture(t, func(site func() http.Handler) http.RoundTripper {
		served, refused := servedBy(site), dialing(untrusted.Listener.Addr().String(), nil)
		return roundTrip(func(request *http.Request) (*http.Response, error) {
			mu.Lock()
			defer mu.Unlock()
			if broken {
				return refused.RoundTrip(request)
			}
			return served.RoundTrip(request)
		})
	})
	if view := checkDomain(t, api, admin, http.StatusOK); view.AddressCheckedAt == nil || view.AddressError != nil {
		t.Fatalf("check = %+v", view)
	}
	mu.Lock()
	broken = true
	mu.Unlock()
	if _, _, _, err := api.(*httpapi.Server).RecheckHelpCenterDomains(context.Background()); err != nil {
		t.Fatal(err)
	}
	var view domainView
	kbCall(t, api, http.MethodGet, domainPath, nil, admin, http.StatusOK, &view)
	if view.Status != "active" || view.AddressError == nil || !strings.Contains(*view.AddressError, "has no valid certificate") {
		t.Fatalf("after the daily re-check = %+v", view)
	}
}
