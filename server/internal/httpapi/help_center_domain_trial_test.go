package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

const trialDomainZone = "help.example.net"

// trialDomainView is the part of HelpCenterDomain these tests read.
type trialDomainView struct {
	domainView
	AwaitingPayment bool `json:"awaiting_payment"`
}

func customDomainState(t *testing.T, api http.Handler, admin string) trialDomainView {
	t.Helper()
	var view trialDomainView
	decode(t, kbCall(t, api, http.MethodGet, domainPath, nil, admin, http.StatusOK, nil), &view)
	return view
}

// cloudDomainFixture is two cloud workspaces (billing enabled, both on their trial) with a help
// center domain and a fake DNS.
func cloudDomainFixture(t *testing.T) (limitsFixture, *fakeDNS) {
	t.Helper()
	dns := &fakeDNS{records: map[string][]string{}, cnames: map[string]string{}}
	f := newLimitsFixtureConfigured(t, true, nil, func(config *httpapi.Config) {
		config.HelpCenterDomain = trialDomainZone
		config.LookupTXT, config.LookupCNAME = dns.lookupTXT, dns.lookupCNAME
	})
	return f, dns
}

// verifyDomain sets host as the workspace's custom domain and proves it by TXT.
func verifyDomain(t *testing.T, api http.Handler, dns *fakeDNS, admin, host string) {
	t.Helper()
	view := setDomain(t, api, admin, host, http.StatusOK)
	dns.set("_help-center-verify."+host, view.token())
	if checked := checkDomain(t, api, admin, http.StatusOK); checked.Status != "active" {
		t.Fatalf("domain %s status=%q after a passing check, want active", host, checked.Status)
	}
}

// A trial workspace may set and verify its custom domain, but the help center serves there only once
// the workspace is paid: trial pages are noindex, and the team's own domain must not be indexed as
// such. No certificate is issued for it before then; after payment it serves on the next request.
func TestTrialCustomDomainVerifiesButServesOnlyOncePaid(t *testing.T) {
	f, dns := cloudDomainFixture(t)
	site := helpCenterSite(f.api)
	slug := workspaceSlug(t, f.api, f.adminA)
	enableHelpCenter(t, f.api, f.adminA, "Alpha Answers", true)
	const host = "help.limits-alpha.example"

	view := setDomain(t, f.api, f.adminA, host, http.StatusOK)
	dns.set("_help-center-verify."+host, view.token())
	// The proxy's question on a pending domain whose DNS passes: still no certificate during the trial.
	if code := askCertificate(t, f.api, host); code != http.StatusNotFound {
		t.Fatalf("tls-ask for a trial workspace's pending domain = %d, want 404", code)
	}
	checked := checkDomain(t, f.api, f.adminA, http.StatusOK)
	if checked.Status != "active" {
		t.Fatalf("trial domain status=%q after a passing check, want active (verifying is allowed)", checked.Status)
	}
	if state := customDomainState(t, f.api, f.adminA); !state.AwaitingPayment {
		t.Fatalf("trial domain awaiting_payment=false, want true")
	}
	if code := askCertificate(t, f.api, host); code != http.StatusNotFound {
		t.Fatalf("tls-ask for a trial workspace's verified domain = %d, want 404", code)
	}
	wantPage(t, onHost(site, host, "/"), http.StatusNotFound, nil, []string{"Alpha Answers"})
	// The trial help center keeps its own subdomain, and its addresses do not move to the custom host.
	if code := askCertificate(t, f.api, slug+"."+trialDomainZone); code != http.StatusOK {
		t.Fatalf("tls-ask for the trial workspace's subdomain = %d, want 200", code)
	}
	wantPage(t, onHost(site, slug+"."+trialDomainZone, "/help"), http.StatusOK, []string{"Alpha Answers", `content="noindex"`}, nil)
	if moved := visit(site, "/help/"+slug+"/", "192.0.2.80"); strings.Contains(moved.Header().Get("Location"), host) {
		t.Fatalf("trial /help/ address moved to the custom host: %q", moved.Header().Get("Location"))
	}

	f.makePlan(t, f.workspaceA, "solo")
	if code := askCertificate(t, f.api, host); code != http.StatusOK {
		t.Fatalf("tls-ask right after payment = %d, want 200", code)
	}
	wantPage(t, onHost(site, host, "/"), http.StatusOK, []string{"Alpha Answers"}, []string{`content="noindex"`})
	if state := customDomainState(t, f.api, f.adminA); state.AwaitingPayment {
		t.Fatalf("paid domain awaiting_payment=true, want false")
	}
	if moved := onHost(site, slug+"."+trialDomainZone, "/help"); moved.Code != http.StatusMovedPermanently || !strings.HasPrefix(moved.Header().Get("Location"), "https://"+host) {
		t.Fatalf("paid subdomain status=%d Location=%q, want a 301 to the custom host", moved.Code, moved.Header().Get("Location"))
	}
}

// An ended, unpaid trial locks the workspace: its custom domain gets no certificate and no page.
func TestLockedWorkspaceCustomDomainServesNothing(t *testing.T) {
	f, dns := cloudDomainFixture(t)
	site := helpCenterSite(f.api)
	enableHelpCenter(t, f.api, f.adminA, "Alpha Answers", true)
	const host = "help.limits-alpha.example"
	verifyDomain(t, f.api, dns, f.adminA, host)
	f.clock.set(trialEnd.Add(time.Hour))
	if code := askCertificate(t, f.api, host); code != http.StatusNotFound {
		t.Fatalf("tls-ask for a locked workspace's domain = %d, want 404", code)
	}
	wantPage(t, onHost(site, host, "/"), http.StatusNotFound, nil, []string{"Alpha Answers"})
}

// Every paid plan serves its custom domain; a trial workspace beside it changes nothing for it, and
// its own domain stays dark.
func TestEveryPaidPlanServesItsCustomDomain(t *testing.T) {
	for _, plan := range []string{"solo", "small", "team", "team25", "founding"} {
		t.Run(plan, func(t *testing.T) {
			f, dns := cloudDomainFixture(t)
			site := helpCenterSite(f.api)
			enableHelpCenter(t, f.api, f.adminA, "Alpha Answers", true)
			enableHelpCenter(t, f.api, f.adminB, "Bravo Answers", true)
			const paidHost, trialHost = "help.limits-bravo.example", "help.limits-alpha.example"
			f.makePlan(t, f.workspaceB, plan)
			verifyDomain(t, f.api, dns, f.adminB, paidHost)
			verifyDomain(t, f.api, dns, f.adminA, trialHost)
			if code := askCertificate(t, f.api, paidHost); code != http.StatusOK {
				t.Fatalf("tls-ask for a %s workspace's domain = %d, want 200", plan, code)
			}
			wantPage(t, onHost(site, paidHost, "/"), http.StatusOK, []string{"Bravo Answers"}, []string{"Alpha Answers"})
			if state := customDomainState(t, f.api, f.adminB); state.AwaitingPayment {
				t.Fatalf("%s domain awaiting_payment=true", plan)
			}
			if code := askCertificate(t, f.api, trialHost); code != http.StatusNotFound {
				t.Fatalf("tls-ask for the trial workspace's domain = %d, want 404", code)
			}
			wantPage(t, onHost(site, trialHost, "/"), http.StatusNotFound, nil, []string{"Alpha Answers", "Bravo Answers"})
		})
	}
}

// A self-hosted server has no billing: every workspace counts as paid and serves its custom domain.
func TestSelfHostedCustomDomainNeverAwaitsPayment(t *testing.T) {
	api, service, _, dns := testAPIWithConfig(t, func(config *httpapi.Config) { config.HelpCenterDomain = trialDomainZone })
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Self Hosted", "admin@self-hosted.example.com")
	enableHelpCenter(t, api, admin, "Own Answers", true)
	const host = "help.self-hosted.example"
	verifyDomain(t, api, dns, admin, host)
	if state := customDomainState(t, api, admin); state.AwaitingPayment {
		t.Fatalf("self-hosted domain awaiting_payment=true")
	}
	if code := askCertificate(t, api, host); code != http.StatusOK {
		t.Fatalf("self-hosted tls-ask = %d, want 200", code)
	}
	wantPage(t, onHost(site, host, "/"), http.StatusOK, []string{"Own Answers"}, nil)
}
