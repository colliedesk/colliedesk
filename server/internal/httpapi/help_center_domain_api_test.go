package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/jackc/pgx/v5"
)

const domainPath = "/api/v1/help-center/domain"

type domainRecordView struct {
	Type    string `json:"type"`
	Name    string `json:"name"`
	Value   string `json:"value"`
	Purpose string `json:"purpose"`
}

type domainView struct {
	Hostname               *string            `json:"hostname"`
	Status                 string             `json:"status"`
	Records                []domainRecordView `json:"records"`
	CreatedAt              *string            `json:"created_at"`
	VerifiedAt             *string            `json:"verified_at"`
	CheckedAt              *string            `json:"checked_at"`
	CheckError             *string            `json:"check_error"`
	CertificateRequestedAt *string            `json:"certificate_requested_at"`
}

// token is the TXT record's value: the proof a workspace owns the host name.
func (d domainView) token() string {
	for _, record := range d.Records {
		if record.Type == "TXT" {
			return record.Value
		}
	}
	return ""
}

func setDomain(t *testing.T, api http.Handler, token, host string, want int) domainView {
	t.Helper()
	var view domainView
	response := kbCall(t, api, http.MethodPut, domainPath, map[string]any{"hostname": host}, token, want, nil)
	if want == http.StatusOK {
		decode(t, response, &view)
	}
	return view
}

func checkDomain(t *testing.T, api http.Handler, token string, want int) domainView {
	t.Helper()
	var view domainView
	response := kbCall(t, api, http.MethodPost, domainPath+"/check", nil, token, want, nil)
	if want == http.StatusOK {
		decode(t, response, &view)
	}
	return view
}

func onHost(site http.Handler, host, path string) *httptest.ResponseRecorder {
	return visit(site, path, "192.0.2.80", "Host", host)
}

func TestHelpCenterDomainIsSetCheckedAndRemoved(t *testing.T) {
	api, service, _, dns := testAPIWithConfig(t, func(config *httpapi.Config) { config.HelpCenterDomain = "help.example.net" })
	admin := createSession(t, api, service, "Domain Alpha", "admin@domain-alpha.example.com")
	slug := workspaceSlug(t, api, admin)

	var view domainView
	kbCall(t, api, http.MethodGet, domainPath, nil, admin, http.StatusOK, &view)
	if view.Status != "none" || view.Hostname != nil || len(view.Records) != 0 {
		t.Fatalf("no domain = %+v", view)
	}
	view = setDomain(t, api, admin, " Help.Acme.Example. ", http.StatusOK)
	token, target := view.token(), view.cnameTarget()
	if !strings.HasPrefix(target, "hc-") || !strings.HasSuffix(target, ".help.example.net") || strings.Contains(target, slug) {
		t.Fatalf("the CNAME target %q is not the claim's own", target)
	}
	want := []domainRecordView{
		{"CNAME", "help.acme.example", target, "route_and_verify"},
		{"TXT", "_help-center-verify.help.acme.example", token, "verify"},
	}
	if view.Status != "pending" || view.Hostname == nil || *view.Hostname != "help.acme.example" || len(token) != 32 || fmt.Sprint(view.Records) != fmt.Sprint(want) {
		t.Fatalf("set = %+v, want pending help.acme.example with %+v", view, want)
	}
	if again := setDomain(t, api, admin, "help.acme.example", http.StatusOK); again.token() != token || again.cnameTarget() != target || again.Status != "pending" {
		t.Fatalf("setting the same name again changed it: %+v", again)
	}

	view = checkDomain(t, api, admin, http.StatusOK)
	if view.Status != "pending" || view.CheckedAt == nil || view.CheckError == nil || !strings.Contains(*view.CheckError, target) {
		t.Fatalf("check without DNS = %+v", view)
	}
	dns.setCNAME("help.acme.example", target+".")
	view = checkDomain(t, api, admin, http.StatusOK)
	if view.Status != "active" || view.VerifiedAt == nil || view.CheckError != nil {
		t.Fatalf("check with the CNAME = %+v", view)
	}
	// A failed check later warns and never deactivates.
	dns.setCNAME("help.acme.example", "elsewhere.example.")
	view = checkDomain(t, api, admin, http.StatusOK)
	if view.Status != "active" || view.CheckError == nil {
		t.Fatalf("failed check of an active domain = %+v", view)
	}

	kbCall(t, api, http.MethodDelete, domainPath, nil, admin, http.StatusOK, &view)
	if view.Status != "none" {
		t.Fatalf("delete = %+v", view)
	}
	kbCall(t, api, http.MethodGet, domainPath, nil, admin, http.StatusOK, &view)
	if view.Status != "none" || view.Hostname != nil {
		t.Fatalf("after delete = %+v", view)
	}
	kbCall(t, api, http.MethodDelete, domainPath, nil, admin, http.StatusOK, nil)
	kbCall(t, api, http.MethodPost, domainPath+"/check", nil, admin, http.StatusNotFound, nil)
	// A new claim of the same name gets a new token and a new CNAME target.
	if renewed := setDomain(t, api, admin, "help.acme.example", http.StatusOK); renewed.token() == token || renewed.cnameTarget() == target || renewed.Status != "pending" {
		t.Fatalf("a removed name set again = %+v", renewed)
	}
}

func TestHelpCenterDomainRefusesNamesThatCannotBeOne(t *testing.T) {
	api, service, _, _ := testAPIWithConfig(t, func(config *httpapi.Config) { config.HelpCenterDomain = "help.example.net" })
	admin := createSession(t, api, service, "Domain Rules", "admin@domain-rules.example.com")
	for _, host := range []string{"192.0.2.1", "localhost", "desk.example.com", "help.example.net", "acme.help.example.net",
		strings.Repeat("a.", 126) + "com", "https://help.acme.example/", "a_b.example.com"} {
		response := request(t, api, http.MethodPut, domainPath, map[string]any{"hostname": host}, admin, nil)
		var body struct {
			Error struct {
				Code   string            `json:"code"`
				Fields map[string]string `json:"fields"`
			} `json:"error"`
		}
		decode(t, response, &body)
		if response.Code != http.StatusUnprocessableEntity || body.Error.Code != "validation_failed" || body.Error.Fields["hostname"] == "" {
			t.Errorf("PUT %q = %s", host, response.Body.String())
		}
	}
	if view := setDomain(t, api, admin, "hilfe.bücher.example", http.StatusOK); view.Hostname == nil || *view.Hostname != "hilfe.xn--bcher-kva.example" {
		t.Fatalf("a Unicode name = %+v", view)
	}
	if response := request(t, api, http.MethodPut, domainPath, map[string]any{"hostname": "x.example.com", "token": "mine"}, admin, nil); response.Code != http.StatusBadRequest {
		t.Fatalf("an unknown field status=%d", response.Code)
	}
	agent := agentSession(t, api, admin, "agent@domain-rules.example.com")
	for _, call := range []struct{ method, path string }{{http.MethodPut, domainPath}, {http.MethodPost, domainPath + "/check"}, {http.MethodDelete, domainPath}} {
		if response := request(t, api, call.method, call.path, map[string]any{"hostname": "docs.rules.example"}, agent, nil); response.Code != http.StatusForbidden {
			t.Errorf("%s %s by an agent status=%d", call.method, call.path, response.Code)
		}
	}
	kbCall(t, api, http.MethodGet, domainPath, nil, agent, http.StatusOK, nil)
}

func TestHelpCenterDomainTokenTiers(t *testing.T) {
	api, service, _, dns := testAPIWithConfig(t, nil)
	admin := createSession(t, api, service, "Domain Tokens", "admin@domain-tokens.example.com")
	read := issueToken(t, api, admin, "Reader", "read").Token
	send := issueToken(t, api, admin, "Writer", "send").Token
	adminToken := issueToken(t, api, admin, "Admin", "admin").Token
	kbCall(t, api, http.MethodGet, domainPath, nil, read, http.StatusOK, nil)
	for _, call := range []struct{ method, path string }{{http.MethodPut, domainPath}, {http.MethodDelete, domainPath}, {http.MethodPost, domainPath + "/check"}} {
		var body any
		if call.method == http.MethodPut {
			body = map[string]any{"hostname": "docs.tokens.example"}
		}
		if response := request(t, api, call.method, call.path, body, send, nil); response.Code != http.StatusAccepted {
			t.Fatalf("send token %s %s status=%d body=%s, want 202", call.method, call.path, response.Code, response.Body.String())
		}
	}
	view := setDomain(t, api, adminToken, "docs.tokens.example", http.StatusOK)
	dns.set("_help-center-verify.docs.tokens.example", view.token())
	before := dns.questions()
	for range 10 {
		checkDomain(t, api, adminToken, http.StatusOK)
	}
	if response := request(t, api, http.MethodPost, domainPath+"/check", nil, adminToken, nil); response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") == "" {
		t.Fatalf("11th token check status=%d", response.Code)
	}
	if asked := dns.questions() - before; asked > 10 {
		t.Fatalf("the refused check asked DNS: %d questions for 10 checks", asked)
	}
	// A person is not limited.
	checkDomain(t, api, admin, http.StatusOK)
}

// Pending claims never block, serve or prove anything for another workspace: each check uses its
// own workspace's CNAME target and token, and the first verified claim wins.
func TestHelpCenterDomainsNeverCrossWorkspaces(t *testing.T) {
	api, service, _, dns := testAPIWithConfig(t, func(config *httpapi.Config) { config.HelpCenterDomain = "help.example.net" })
	site := helpCenterSite(api)
	alpha := createSession(t, api, service, "Cross Alpha", "admin@cross-alpha.example.com")
	beta := createSession(t, api, service, "Cross Beta", "admin@cross-beta.example.com")
	slugAlpha, slugBeta := workspaceSlug(t, api, alpha), workspaceSlug(t, api, beta)
	enableHelpCenter(t, api, alpha, "Alpha answers", true)
	enableHelpCenter(t, api, beta, "Beta answers", true)

	const host = "help.shared.example"
	alphaClaim := setDomain(t, api, alpha, host, http.StatusOK)
	betaClaim := setDomain(t, api, beta, host, http.StatusOK)
	if alphaClaim.token() == betaClaim.token() {
		t.Fatal("two workspaces got the same token")
	}
	if alphaClaim.cnameTarget() == betaClaim.cnameTarget() {
		t.Fatal("two workspaces got the same CNAME target")
	}
	// Beta's token and a CNAME to beta's target prove nothing for alpha.
	dns.set("_help-center-verify."+host, betaClaim.token())
	dns.setCNAME(host, betaClaim.cnameTarget()+".")
	if view := checkDomain(t, api, alpha, http.StatusOK); view.Status != "pending" {
		t.Fatalf("alpha verified with beta's records: %+v", view)
	}
	if response := onHost(site, host, "/"); strings.Contains(response.Body.String(), "Alpha answers") || strings.Contains(response.Body.String(), "Beta answers") {
		t.Fatalf("a pending claim served: %s", response.Body.String())
	}
	if view := checkDomain(t, api, beta, http.StatusOK); view.Status != "active" {
		t.Fatalf("beta's check = %+v", view)
	}
	// Alpha now proves it too, but beta was first and its TXT record still proves its claim.
	dns.setCNAME(host, alphaClaim.cnameTarget()+".")
	if response := kbCall(t, api, http.MethodPost, domainPath+"/check", nil, alpha, http.StatusConflict, nil); !strings.Contains(response.Body.String(), "domain_taken") {
		t.Fatalf("alpha's check after beta = %s", response.Body.String())
	}
	var view domainView
	kbCall(t, api, http.MethodGet, domainPath, nil, alpha, http.StatusOK, &view)
	if view.Status != "pending" || view.CheckError == nil {
		t.Fatalf("alpha after losing = %+v", view)
	}
	if response := kbCall(t, api, http.MethodPut, domainPath, map[string]any{"hostname": strings.ToUpper(host)}, alpha, http.StatusConflict, nil); !strings.Contains(response.Body.String(), "domain_taken") {
		t.Fatalf("claiming beta's active name = %s", response.Body.String())
	}
	page := onHost(site, host, "/").Body.String()
	if !strings.Contains(page, "Beta answers") || strings.Contains(page, "Alpha answers") {
		t.Fatalf("the host serves %s", page)
	}
	// Alpha's slug addresses still serve alpha, and only beta's redirect to the custom domain.
	if response := visit(site, "/", "192.0.2.81", "Host", slugAlpha+".help.example.net"); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Alpha answers") {
		t.Fatalf("alpha's own host status=%d", response.Code)
	}
	if response := visit(site, "/", "192.0.2.81", "Host", slugBeta+".help.example.net"); response.Header().Get("Location") != "https://"+host+"/" {
		t.Fatalf("beta's slug host location=%q", response.Header().Get("Location"))
	}
	// Alpha removing its claim leaves beta's domain alone.
	kbCall(t, api, http.MethodDelete, domainPath, nil, alpha, http.StatusOK, nil)
	kbCall(t, api, http.MethodGet, domainPath, nil, beta, http.StatusOK, &view)
	if view.Status != "active" || *view.Hostname != host {
		t.Fatalf("beta after alpha removed its claim = %+v", view)
	}
}

// A custom host is the help center and nothing else: the app, the API, MCP, attachments and the other
// workspaces' help centers stay out of reach on it, even with a valid session. Removing the domain
// stops it at once.
func TestCustomHostServesOnlyTheHelpCenter(t *testing.T) {
	api, service, _, dns := testAPIWithConfig(t, nil)
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Host Alpha", "admin@host-alpha.example.com")
	other := createSession(t, api, service, "Host Beta", "admin@host-beta.example.com")
	slug, otherSlug := workspaceSlug(t, api, admin), workspaceSlug(t, api, other)
	enableHelpCenter(t, api, admin, "Alpha help", true)
	enableHelpCenter(t, api, other, "Beta help", true)
	const host = "help.alpha.example"
	view := setDomain(t, api, admin, host, http.StatusOK)
	dns.set("_help-center-verify."+host, view.token())
	checkDomain(t, api, admin, http.StatusOK)

	wantPage(t, visit(site, "/", "192.0.2.82", "Host", host), http.StatusOK, []string{"Alpha help"}, nil)
	wantPage(t, visit(site, "/", "192.0.2.82", "Host", "HELP.Alpha.Example.:443"), http.StatusOK, []string{"Alpha help"}, nil)
	for _, path := range []string{"/api/v1/auth/me", "/api/v1/config", "/api/v1/help-center", "/mcp", "/api/v1/attachments/0b6f8a8e-3c4d-4e5f-8a9b-0c1d2e3f4a5b",
		"/inbox", "/settings", "/help/" + otherSlug, "/help/" + slug, "/api/v1/public/help-center/tls-ask?domain=" + host} {
		req := newRequest(t, http.MethodGet, path, nil, admin, nil)
		req.Host = host
		response := serve(site, req)
		if response.Code != http.StatusNotFound || !strings.HasPrefix(response.Header().Get("Content-Type"), "text/html") || strings.Contains(response.Body.String(), "Beta help") {
			t.Errorf("%s on the custom host status=%d type=%q", path, response.Code, response.Header().Get("Content-Type"))
		}
	}
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		req := newRequest(t, method, "/api/v1/auth/redeem", map[string]any{"token": "x"}, "", nil)
		req.Host = host
		if response := serve(site, req); response.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s on the custom host status=%d", method, response.Code)
		}
	}

	kbCall(t, api, http.MethodDelete, domainPath, nil, admin, http.StatusOK, nil)
	if response := visit(site, "/", "192.0.2.83", "Host", host); strings.Contains(response.Body.String(), "Alpha help") {
		t.Fatalf("the removed domain still served the help center: status=%d", response.Code)
	}
	wantPage(t, visit(site, "/help/"+slug, "192.0.2.83"), http.StatusOK, []string{"Alpha help"}, nil)
}

// Every old address moves to the custom domain with its path and query, 301 and no-store, and never
// anywhere a request names.
func TestOldAddressesMoveToTheCustomDomain(t *testing.T) {
	for _, mode := range []struct{ name, domain string }{{"path", ""}, {"host", "help.example.net"}} {
		t.Run(mode.name, func(t *testing.T) {
			api, service, _, dns := testAPIWithConfig(t, func(config *httpapi.Config) { config.HelpCenterDomain = mode.domain })
			site := helpCenterSite(api)
			admin := createSession(t, api, service, "Moving "+mode.name, "admin@moving-"+mode.name+".example.com")
			slug := workspaceSlug(t, api, admin)
			enableHelpCenter(t, api, admin, "Moving help", true)
			kbCollection(t, api, admin, map[string]any{"name": "Guides"})
			const host = "help.moving.example"
			view := setDomain(t, api, admin, host, http.StatusOK)
			dns.set("_help-center-verify."+host, view.token())
			checkDomain(t, api, admin, http.StatusOK)

			type old struct{ host, path, want string }
			olds := []old{
				{"desk.example.com", "/help/" + slug + "/c/guides?page=2", "https://" + host + "/c/guides?page=2"},
				{"desk.example.com", "/help/" + slug, "https://" + host + "/"},
				{"evil.example", "/help/" + slug + "//evil.example/x?next=https://evil.example", "https://" + host + "//evil.example/x?next=https://evil.example"},
				{"desk.example.com", "/help/" + slug + "/a/1-x%0d%0aSet-Cookie:%20a=b", "https://" + host + "/a/1-x%0d%0aSet-Cookie:%20a=b"},
			}
			if mode.domain != "" {
				olds = append(olds, old{slug + ".help.example.net", "/c/guides?page=2", "https://" + host + "/c/guides?page=2"},
					old{slug + ".help.example.net", "//evil.example/%2e%2e/x", "https://" + host + "//evil.example/%2e%2e/x"})
			}
			for _, o := range olds {
				req := newRequest(t, http.MethodGet, "/", nil, "", nil)
				req.URL, _ = url.ParseRequestURI(o.path)
				req.RequestURI, req.Host = o.path, o.host
				req.Header.Set("X-Forwarded-Host", "evil.example")
				response := serve(site, req)
				location := response.Header().Get("Location")
				moved, err := url.Parse(location)
				if response.Code != http.StatusMovedPermanently || location != o.want || err != nil || moved.Host != host ||
					response.Header().Get("Cache-Control") != "no-store" || strings.ContainsAny(location, "\r\n") {
					t.Errorf("%s%s status=%d location=%q cache=%q, want 301 to %s", o.host, o.path, response.Code, location, response.Header().Get("Cache-Control"), o.want)
				}
			}
			// Removed: the old addresses serve again with the next request.
			kbCall(t, api, http.MethodDelete, domainPath, nil, admin, http.StatusOK, nil)
			if mode.domain == "" {
				wantPage(t, visit(site, "/help/"+slug, "192.0.2.84"), http.StatusOK, []string{"Moving help"}, nil)
			} else {
				wantPage(t, visit(site, "/", "192.0.2.84", "Host", slug+".help.example.net"), http.StatusOK, []string{"Moving help"}, nil)
			}
		})
	}
}

// Every address the server hands out names the custom domain while it is active: the help center's
// url, every article url, the canonical links and the ETag of each page.
func TestCustomDomainChangesEveryAddressTheServerHandsOut(t *testing.T) {
	api, service, _, dns := testAPIWithConfig(t, func(config *httpapi.Config) { config.HelpCenterDomain = "help.example.net" })
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Address Alpha", "admin@address-alpha.example.com")
	slug := workspaceSlug(t, api, admin)
	enableHelpCenter(t, api, admin, "Address help", true)
	collection := kbCollection(t, api, admin, map[string]any{"name": "Guides"})
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Start here", "status": "published", "body_md": "Hello"})
	slugPage := visit(site, "/", "192.0.2.85", "Host", slug+".help.example.net")
	const host = "help.address.example"
	view := setDomain(t, api, admin, host, http.StatusOK)
	dns.setCNAME(host, view.cnameTarget())
	if checkDomain(t, api, admin, http.StatusOK).Status != "active" {
		t.Fatal("not active")
	}
	home := "https://" + host
	articleURL := fmt.Sprintf("%s/a/%d-%s", home, article.Number, article.Slug)
	var center struct {
		URL string `json:"url"`
	}
	kbCall(t, api, http.MethodGet, "/api/v1/help-center", nil, admin, http.StatusOK, &center)
	if center.URL != home {
		t.Errorf("help center url=%q, want %q", center.URL, home)
	}
	kbCall(t, api, http.MethodPut, "/api/v1/help-center", map[string]any{"enabled": true, "title": "Address help", "locale": "en"}, admin, http.StatusOK, &center)
	if center.URL != home {
		t.Errorf("saved help center url=%q", center.URL)
	}
	var got kbArticleView
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles/"+article.ID, nil, admin, http.StatusOK, &got)
	if got.URL != articleURL {
		t.Errorf("article url=%q, want %q", got.URL, articleURL)
	}
	var list struct {
		Items []kbArticleView `json:"items"`
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles", nil, admin, http.StatusOK, &list)
	if len(list.Items) != 1 || list.Items[0].URL != articleURL {
		t.Errorf("listed url=%+v", list.Items)
	}
	created := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Second", "status": "published", "body_md": "Two"})
	if !strings.HasPrefix(created.URL, home+"/a/") {
		t.Errorf("created url=%q", created.URL)
	}
	wantPage(t, visit(site, "/", "192.0.2.85", "Host", host), http.StatusOK, []string{`<link rel="canonical" href="` + home + `">`}, nil)
	wantPage(t, visit(site, "/c/guides", "192.0.2.85", "Host", host), http.StatusOK, []string{`<link rel="canonical" href="` + home + `/c/guides">`}, nil)
	articlePage := visit(site, fmt.Sprintf("/a/%d-%s", article.Number, article.Slug), "192.0.2.85", "Host", host)
	wantPage(t, articlePage, http.StatusOK, []string{`<link rel="canonical" href="` + articleURL + `">`}, nil)
	if customPage := visit(site, "/", "192.0.2.85", "Host", host); customPage.Header().Get("ETag") == slugPage.Header().Get("ETag") || customPage.Header().Get("ETag") == "" {
		t.Errorf("the ETag did not change with the address: %q", customPage.Header().Get("ETag"))
	}
}

// The reverse proxy gets a certificate only for a name the server serves: an active custom domain
// whose help center is open, or a pending one whose DNS proves it now. Live checks are bounded.
func TestCertificateQuestionForCustomDomains(t *testing.T) {
	for _, mode := range []struct{ name, domain string }{{"txt without a help center domain", ""}, {"cname", "help.example.net"}} {
		t.Run(mode.name, func(t *testing.T) {
			api, service, _, dns := testAPIWithConfig(t, func(config *httpapi.Config) { config.HelpCenterDomain = mode.domain })
			admin := createSession(t, api, service, "Ask "+mode.domain, "admin@ask"+strings.ReplaceAll(mode.domain, ".", "-")+".example.com")
			slug := workspaceSlug(t, api, admin)
			enableHelpCenter(t, api, admin, "Ask help", true)
			const host = "help.ask.example"
			ask := func(name string) int {
				t.Helper()
				path := "/api/v1/public/help-center/tls-ask?domain=" + url.QueryEscape(name)
				req := newRequest(t, http.MethodGet, path, nil, "", nil)
				validateContractRequest(t, req)
				response := serve(api, req)
				validateContractResponse(t, newRequest(t, http.MethodGet, path, nil, "", nil), response)
				if response.Code != http.StatusOK && response.Code != http.StatusNotFound {
					t.Fatalf("tls-ask %q status=%d", name, response.Code)
				}
				if response.Code == http.StatusNotFound && !strings.Contains(response.Body.String(), `"not_found"`) {
					t.Fatalf("tls-ask %q said more than not found: %s", name, response.Body.String())
				}
				return response.Code
			}
			view := setDomain(t, api, admin, host, http.StatusOK)
			before := dns.questions()
			if got := ask(host); got != http.StatusNotFound {
				t.Fatalf("pending without DNS = %d", got)
			}
			if dns.questions() == before {
				t.Fatal("the pending name was not checked live")
			}
			if mode.domain != "" {
				dns.setCNAME(host, view.cnameTarget()+".")
			} else {
				dns.set("_help-center-verify."+host, view.token())
			}
			// One live check per name per 30 seconds: the DNS is right now, but the name waits.
			asked := dns.questions()
			if got := ask(host); got != http.StatusNotFound || dns.questions() != asked {
				t.Fatalf("a second ask within the back-off = %d, DNS questions %d → %d", got, asked, dns.questions())
			}
			kbCall(t, api, http.MethodGet, domainPath, nil, admin, http.StatusOK, &view)
			if view.Status != "pending" {
				t.Fatalf("activated without a check: %+v", view)
			}
			// Another pending name is checked live and becomes active on the spot.
			const second = "docs.ask.example"
			view = setDomain(t, api, admin, second, http.StatusOK)
			if mode.domain != "" {
				dns.setCNAME(second, view.cnameTarget()+".")
			} else {
				dns.set("_help-center-verify."+second, view.token())
			}
			if got := ask(second); got != http.StatusOK {
				t.Fatalf("pending with matching DNS = %d", got)
			}
			kbCall(t, api, http.MethodGet, domainPath, nil, admin, http.StatusOK, &view)
			if view.Status != "active" || view.CertificateRequestedAt == nil {
				t.Fatalf("after the live check = %+v", view)
			}
			asked = dns.questions()
			if got := ask(strings.ToUpper(second) + "."); got != http.StatusOK || dns.questions() != asked {
				t.Fatalf("active name = %d, asked DNS %d times", got, dns.questions()-asked)
			}
			for _, name := range []string{"192.0.2.1", "localhost", "desk.example.com", "unknown.ask.example", host, ""} {
				if got := ask(name); got != http.StatusNotFound {
					t.Errorf("tls-ask %q = %d", name, got)
				}
			}
			if dns.questions() != asked {
				t.Errorf("names that are no pending claim asked DNS %d times", dns.questions()-asked)
			}
			if mode.domain != "" {
				// The slug host keeps its certificate, so its redirect keeps working.
				if got := ask(slug + ".help.example.net"); got != http.StatusOK {
					t.Errorf("slug host = %d", got)
				}
			}
			enableHelpCenter(t, api, admin, "Ask help", false)
			if got := ask(second); got != http.StatusNotFound {
				t.Errorf("active name of a help center that is off = %d", got)
			}
		})
	}
}

// The live checks of the certificate question run a few at a time: a flood of questions about many
// pending names never makes more than a handful of DNS lookups wait at once.
func TestCertificateQuestionBoundsConcurrentLiveChecks(t *testing.T) {
	gate := make(chan struct{})
	var mu sync.Mutex
	running, most := 0, 0
	block := func(ctx context.Context, _ string) ([]string, error) {
		mu.Lock()
		running++
		most = max(most, running)
		mu.Unlock()
		select {
		case <-gate:
		case <-ctx.Done():
		}
		mu.Lock()
		running--
		mu.Unlock()
		return nil, ctx.Err()
	}
	api, service, _, _ := testAPIWithConfig(t, func(config *httpapi.Config) { config.LookupTXT = block })
	var wait sync.WaitGroup
	var hosts []string
	for index := range 12 {
		admin := createSession(t, api, service, fmt.Sprintf("Flood %d", index), fmt.Sprintf("admin@flood-%d.example.com", index))
		enableHelpCenter(t, api, admin, "", true)
		host := fmt.Sprintf("help%d.flood.example", index)
		setDomain(t, api, admin, host, http.StatusOK)
		hosts = append(hosts, host)
	}
	codes := make([]int, len(hosts))
	for index, host := range hosts {
		wait.Add(1)
		go func() {
			defer wait.Done()
			codes[index] = serve(api, newRequest(t, http.MethodGet, "/api/v1/public/help-center/tls-ask?domain="+host, nil, "", nil)).Code
		}()
	}
	wait.Wait()
	close(gate)
	if most > 4 {
		t.Fatalf("%d live checks ran at once", most)
	}
	for index, code := range codes {
		if code != http.StatusNotFound {
			t.Errorf("%s = %d", hosts[index], code)
		}
	}
}

// Requests to the app's own host and to help center slug hosts never look a custom host up.
func TestAppRequestsNeverLookUpACustomHost(t *testing.T) {
	tracer := &statementTracer{}
	api, service, _, _ := testAPIOnPool(t, testPoolTraced(t, tracer), func(config *httpapi.Config) { config.HelpCenterDomain = "help.example.net" })
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Quiet Alpha", "admin@quiet-alpha.example.com")
	slug := workspaceSlug(t, api, admin)
	enableHelpCenter(t, api, admin, "Quiet help", true)
	tracer.reset()
	for _, call := range []struct{ host, path string }{
		{"desk.example.com", "/api/v1/auth/me"}, {"DESK.example.com:443", "/api/v1/config"}, {"desk.example.com", "/help/" + slug},
		{slug + ".help.example.net", "/"}, {"help.example.net", "/"}, {"a.b.help.example.net", "/"}, {"127.0.0.1:8080", "/api/v1/config"}, {"localhost", "/"},
	} {
		req := newRequest(t, http.MethodGet, call.path, nil, admin, nil)
		req.Host = call.host
		serve(site, req)
	}
	if lookups := tracer.count("help_center_domains WHERE lower(hostname)"); lookups != 0 {
		t.Fatalf("app and slug host requests looked up a custom host %d times", lookups)
	}
	// An unknown host name is looked up once, then remembered.
	for range 3 {
		visit(site, "/", "192.0.2.86", "Host", "unknown.quiet.example")
	}
	if lookups := tracer.count("help_center_domains WHERE lower(hostname)"); lookups != 1 {
		t.Fatalf("an unknown host was looked up %d times, want once", lookups)
	}
}

// The daily re-check records what DNS shows for every active domain and never deactivates one.
func TestDailyRecheckRecordsAndNeverDeactivates(t *testing.T) {
	api, service, _, dns := testAPIWithConfig(t, nil)
	admin := createSession(t, api, service, "Recheck Alpha", "admin@recheck-alpha.example.com")
	pending := createSession(t, api, service, "Recheck Beta", "admin@recheck-beta.example.com")
	const host = "help.recheck.example"
	view := setDomain(t, api, admin, host, http.StatusOK)
	dns.set("_help-center-verify."+host, view.token())
	checkDomain(t, api, admin, http.StatusOK)
	setDomain(t, api, pending, "docs.recheck.example", http.StatusOK)
	dns.set("_help-center-verify."+host, "gone")
	checked, failed, err := api.(*httpapi.Server).RecheckHelpCenterDomains(context.Background())
	if err != nil || checked != 1 || failed != 1 {
		t.Fatalf("recheck = %d checked, %d failed, %v", checked, failed, err)
	}
	kbCall(t, api, http.MethodGet, domainPath, nil, admin, http.StatusOK, &view)
	if view.Status != "active" || view.CheckError == nil {
		t.Fatalf("after a failed re-check = %+v", view)
	}
	kbCall(t, api, http.MethodGet, domainPath, nil, pending, http.StatusOK, &view)
	if view.CheckedAt != nil {
		t.Fatalf("the re-check touched a pending domain: %+v", view)
	}
}

func TestExportCarriesTheCustomDomainButNeverItsToken(t *testing.T) {
	api, service, _, dns := testAPIWithConfig(t, nil)
	admin := createSession(t, api, service, "Export Domain", "admin@export-domain.example.com")
	view := setDomain(t, api, admin, "help.export.example", http.StatusOK)
	dns.set("_help-center-verify.help.export.example", view.token())
	checkDomain(t, api, admin, http.StatusOK)
	response := request(t, api, http.MethodGet, "/api/v1/export", nil, admin, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("export status=%d body=%s", response.Code, response.Body.String())
	}
	files := unzip(t, response.Body.Bytes())
	lines := ndjsonLines(t, files["help-center.ndjson"])
	if len(lines) != 1 || lines[0]["custom_domain"] != "help.export.example" || lines[0]["custom_domain_status"] != "active" || lines[0]["custom_domain_verified_at"] == nil {
		t.Fatalf("help-center.ndjson = %v", lines)
	}
	for name, data := range files {
		if strings.Contains(string(data), view.token()) {
			t.Errorf("%s holds the domain token", name)
		}
	}
}

// statementTracer records every SQL statement the pool runs.
type statementTracer struct {
	mu         sync.Mutex
	statements []string
}

func (s *statementTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.statements = append(s.statements, data.SQL)
	return ctx
}

func (s *statementTracer) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func (s *statementTracer) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.statements = nil
}

func (s *statementTracer) count(fragment string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, statement := range s.statements {
		if strings.Contains(statement, fragment) {
			count++
		}
	}
	return count
}
