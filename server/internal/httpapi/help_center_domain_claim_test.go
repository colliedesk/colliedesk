package httpapi_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

// cnameTarget is the value of the CNAME record the card asks for.
func (d domainView) cnameTarget() string {
	for _, record := range d.Records {
		if record.Type == "CNAME" {
			return record.Value
		}
	}
	return ""
}

func askCertificate(t *testing.T, api http.Handler, name string) int {
	t.Helper()
	return serve(api, newRequest(t, http.MethodGet, "/api/v1/public/help-center/tls-ask?domain="+name, nil, "", nil)).Code
}

// A CNAME the owner of a domain leaves at its DNS provider proves nothing for anyone else: the victim
// verifies its domain, renames its slug and removes the domain, and the attacker takes the old slug and
// claims the same host name. The attacker's claim stays pending, by the check and by the proxy's
// certificate question, and the host never serves the attacker's help center.
func TestACNAMELeftBehindNeverProvesAnotherWorkspacesClaim(t *testing.T) {
	api, service, _, dns := testAPIWithConfig(t, func(config *httpapi.Config) { config.HelpCenterDomain = "help.example.net" })
	site := helpCenterSite(api)
	victim := createSession(t, api, service, "Takeover Victim", "admin@takeover-victim.example.com")
	attacker := createSession(t, api, service, "Takeover Attacker", "admin@takeover-attacker.example.com")
	enableHelpCenter(t, api, victim, "Victim help", true)
	enableHelpCenter(t, api, attacker, "Attacker help", true)
	victimSlug := workspaceSlug(t, api, victim)

	const host = "help.takeover.example"
	claim := setDomain(t, api, victim, host, http.StatusOK)
	target := claim.cnameTarget()
	if target == "" || target == victimSlug+".help.example.net" || !strings.HasPrefix(target, "hc-") {
		t.Errorf("the CNAME target %q is not the claim's own", target)
	}
	dns.setCNAME(host, target+".")
	if view := checkDomain(t, api, victim, http.StatusOK); view.Status != "active" {
		t.Fatalf("the victim's check = %+v", view)
	}
	// The slug does not move the target.
	kbCall(t, api, http.MethodPatch, "/api/v1/workspace/slug", map[string]any{"slug": victimSlug + "-renamed"}, victim, http.StatusOK, nil)
	var view domainView
	kbCall(t, api, http.MethodGet, domainPath, nil, victim, http.StatusOK, &view)
	if view.cnameTarget() != target || view.Status != "active" {
		t.Errorf("after the rename = %+v", view)
	}
	kbCall(t, api, http.MethodDelete, domainPath, nil, victim, http.StatusOK, nil)

	kbCall(t, api, http.MethodPatch, "/api/v1/workspace/slug", map[string]any{"slug": victimSlug}, attacker, http.StatusOK, nil)
	stolen := setDomain(t, api, attacker, host, http.StatusOK)
	if stolen.cnameTarget() == target {
		t.Errorf("a new claim got the removed claim's target %q", target)
	}
	if view := checkDomain(t, api, attacker, http.StatusOK); view.Status != "pending" {
		t.Fatalf("TAKEOVER: the attacker's check activated %s: %+v", host, view)
	}
	if code := askCertificate(t, api, host); code != http.StatusNotFound {
		t.Fatalf("TAKEOVER: tls-ask for the attacker's claim = %d", code)
	}
	kbCall(t, api, http.MethodGet, domainPath, nil, attacker, http.StatusOK, &view)
	if view.Status != "pending" {
		t.Fatalf("TAKEOVER: the attacker's claim = %+v", view)
	}
	if page := onHost(site, host, "/").Body.String(); strings.Contains(page, "Attacker help") || strings.Contains(page, "Victim help") {
		t.Fatalf("the host serves %s", page)
	}
}

// A check records only the claim it looked up: when the domain is removed and the same host name set
// again while DNS answers, the old claim's proof does not activate the new claim.
func TestACheckRecordsOnlyTheClaimItLookedUp(t *testing.T) {
	var swap func()
	var oldToken string
	lookupTXT := func(_ context.Context, _ string) ([]string, error) {
		if swap != nil {
			run := swap
			swap = nil
			run()
		}
		return []string{oldToken}, nil
	}
	api, service, _, _ := testAPIWithConfig(t, func(config *httpapi.Config) { config.LookupTXT = lookupTXT })
	admin := createSession(t, api, service, "Race Alpha", "admin@race-alpha.example.com")
	const host = "help.race.example"
	oldToken = setDomain(t, api, admin, host, http.StatusOK).token()
	var renewed domainView
	swap = func() {
		kbCall(t, api, http.MethodDelete, domainPath, nil, admin, http.StatusOK, nil)
		renewed = setDomain(t, api, admin, host, http.StatusOK)
	}
	if response := kbCall(t, api, http.MethodPost, domainPath+"/check", nil, admin, http.StatusConflict, nil); !strings.Contains(response.Body.String(), "domain_changed") {
		t.Fatalf("a check across a remove and re-add = %s", response.Body.String())
	}
	var view domainView
	kbCall(t, api, http.MethodGet, domainPath, nil, admin, http.StatusOK, &view)
	if view.Status != "pending" || view.token() != renewed.token() || view.token() == oldToken || view.CheckedAt != nil {
		t.Fatalf("the new claim after the old claim's check = %+v", view)
	}
}

// The claim's CNAME target leads to the claim's help center: to the custom domain while it is active,
// else to the help center's own address. A target nobody holds, or a removed one, is the 404, and no
// slug can take the target's shape.
func TestTheClaimsCNAMETargetLeadsToItsHelpCenter(t *testing.T) {
	api, service, _, dns := testAPIWithConfig(t, func(config *httpapi.Config) { config.HelpCenterDomain = "help.example.net" })
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Target Alpha", "admin@target-alpha.example.com")
	slug := workspaceSlug(t, api, admin)
	enableHelpCenter(t, api, admin, "Target help", true)
	const host = "help.target.example"
	target := setDomain(t, api, admin, host, http.StatusOK).cnameTarget()

	moved := func(want string) {
		t.Helper()
		response := onHost(site, target, "/c/guides?page=2")
		if response.Code != http.StatusMovedPermanently || response.Header().Get("Location") != want {
			t.Fatalf("the target while %s: status=%d location=%q", want, response.Code, response.Header().Get("Location"))
		}
	}
	moved("https://" + slug + ".help.example.net/help/c/guides?page=2")
	if code := askCertificate(t, api, target); code != http.StatusOK {
		t.Fatalf("tls-ask for the target = %d", code)
	}
	dns.setCNAME(host, target+".")
	checkDomain(t, api, admin, http.StatusOK)
	moved("https://" + host + "/c/guides?page=2")

	kbCall(t, api, http.MethodDelete, domainPath, nil, admin, http.StatusOK, nil)
	for _, name := range []string{target, "hc-0123456789abcdef01234567.help.example.net"} {
		if response := onHost(site, name, "/"); response.Code != http.StatusNotFound {
			t.Errorf("%s status=%d location=%q", name, response.Code, response.Header().Get("Location"))
		}
		if code := askCertificate(t, api, name); code != http.StatusNotFound {
			t.Errorf("tls-ask for %s = %d", name, code)
		}
	}
	response := request(t, api, http.MethodPatch, "/api/v1/workspace/slug", map[string]any{"slug": strings.TrimSuffix(target, ".help.example.net")}, admin, nil)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("a slug in the target's shape status=%d", response.Code)
	}
}
