package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

const fakeResendKey = "re_test_key_not_a_secret"

// fakeResend answers like the documented Resend Domains API (create, get, verify) and records the calls.
type fakeResend struct {
	mu      sync.Mutex
	domains map[string]*fakeResendDomain
	byName  map[string]string
	calls   []string
	// status is what a domain reports once created and after a verify call.
	status string
	down   bool
}

type fakeResendDomain struct {
	ID, Name, Status string
}

func newFakeResend(t *testing.T) (*fakeResend, *httptest.Server) {
	t.Helper()
	fake := &fakeResend{domains: map[string]*fakeResendDomain{}, byName: map[string]string{}, status: "not_started"}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	return fake, server
}

func (f *fakeResend) called(call string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	count := 0
	for _, entry := range f.calls {
		if entry == call {
			count++
		}
	}
	return count
}

func (f *fakeResend) setStatus(name, status string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.domains[f.byName[name]].Status = status
}

func (f *fakeResend) records(domain *fakeResendDomain) []map[string]any {
	status := domain.Status
	return []map[string]any{
		{"record": "DKIM", "name": "resend._domainkey", "value": "p=MIGfMA0GCSqGSIb3DQEBAQUAA4GN", "type": "TXT", "ttl": "Auto", "status": status},
		{"record": "SPF", "name": "send", "value": "v=spf1 include:amazonses.com ~all", "type": "TXT", "ttl": "Auto", "status": status},
		{"record": "SPF", "name": "send", "value": "feedback-smtp.us-east-1.amazonses.com", "type": "MX", "ttl": "Auto", "status": status, "priority": 10},
	}
}

func (f *fakeResend) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, request.Method+" "+request.URL.Path)
	response.Header().Set("Content-Type", "application/json")
	if request.Header.Get("Authorization") != "Bearer "+fakeResendKey {
		response.WriteHeader(http.StatusUnauthorized)
		_, _ = response.Write([]byte(`{"statusCode":401,"name":"restricted_api_key","message":"API key is invalid"}`))
		return
	}
	if f.down {
		response.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	path := strings.TrimPrefix(request.URL.Path, "/domains")
	switch {
	case request.Method == http.MethodPost && path == "":
		var body struct {
			Name string `json:"name"`
		}
		_ = json.NewDecoder(request.Body).Decode(&body)
		if _, taken := f.byName[body.Name]; taken {
			response.WriteHeader(http.StatusForbidden)
			_, _ = response.Write([]byte(`{"statusCode":403,"name":"validation_error","message":"The ` + body.Name + ` domain has been registered already."}`))
			return
		}
		domain := &fakeResendDomain{ID: "d91cd9bd-" + strings.ReplaceAll(body.Name, ".", "-"), Name: body.Name, Status: f.status}
		f.domains[domain.ID], f.byName[body.Name] = domain, domain.ID
		_ = json.NewEncoder(response).Encode(map[string]any{"id": domain.ID, "name": domain.Name, "created_at": "2026-10-18T00:00:00.000Z",
			"status": domain.Status, "capabilities": map[string]string{"sending": "enabled", "receiving": "disabled"}, "records": f.records(domain), "region": "us-east-1"})
	case request.Method == http.MethodGet && strings.HasPrefix(path, "/"):
		domain, ok := f.domains[strings.TrimPrefix(path, "/")]
		if !ok {
			response.WriteHeader(http.StatusNotFound)
			_, _ = response.Write([]byte(`{"statusCode":404,"name":"not_found","message":"Domain not found"}`))
			return
		}
		_ = json.NewEncoder(response).Encode(map[string]any{"object": "domain", "id": domain.ID, "name": domain.Name, "status": domain.Status,
			"created_at": "2026-10-18T00:00:00.000Z", "region": "us-east-1", "records": f.records(domain)})
	case request.Method == http.MethodPost && strings.HasSuffix(path, "/verify"):
		id := strings.TrimSuffix(strings.TrimPrefix(path, "/"), "/verify")
		if _, ok := f.domains[id]; !ok {
			response.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(response).Encode(map[string]any{"object": "domain", "id": id})
	default:
		response.WriteHeader(http.StatusNotFound)
	}
}

func resendDomainConfig(server *httptest.Server) func(*httpapi.Config) {
	return func(config *httpapi.Config) {
		config.Email.FromPolicy = email.FromPolicyVerified
		config.Email.MailFromDomain = "mail.colliedesk.test"
		config.Email.ResendAPIKey = fakeResendKey
		config.Email.ResendAPIBase = server.URL
	}
}

type providerSettings struct {
	DomainVerification string `json:"domain_verification"`
	Channel            struct {
		Domain           string  `json:"domain"`
		DomainVerifiedAt *string `json:"domain_verified_at"`
		DNSRecords       []struct {
			Record   string `json:"record"`
			Type     string `json:"type"`
			Name     string `json:"name"`
			Value    string `json:"value"`
			Priority *int   `json:"priority"`
			Status   string `json:"status"`
		} `json:"dns_records"`
	} `json:"channel"`
}

func providerState(t *testing.T, api http.Handler, token string) providerSettings {
	t.Helper()
	var settings providerSettings
	decode(t, request(t, api, http.MethodGet, "/api/v1/channels/email", nil, token, nil), &settings)
	return settings
}

const verifyDomainPath = "/api/v1/channels/email/domain-verification"

func TestSavingTheSupportAddressCreatesTheProviderDomainOnceAndListsItsRecords(t *testing.T) {
	fake, server := newFakeResend(t)
	api, service, _, _ := testAPIWithConfig(t, resendDomainConfig(server))
	admin := createSession(t, api, service, "Provider Desk", "admin@provider-desk.example.com")

	for _, address := range []string{"support@provider-desk.example.com", "help@provider-desk.example.com"} {
		if response := request(t, api, http.MethodPut, "/api/v1/channels/email", map[string]any{"support_address": address}, admin, nil); response.Code != http.StatusOK {
			t.Fatalf("PUT %s status=%d body=%s", address, response.Code, response.Body.String())
		}
	}
	if created := fake.called("POST /domains"); created != 1 {
		t.Fatalf("provider domain created %d times, want once for one domain", created)
	}
	settings := providerState(t, api, admin)
	if settings.DomainVerification != "provider" || len(settings.Channel.DNSRecords) != 3 {
		t.Fatalf("settings: %+v", settings)
	}
	mx := settings.Channel.DNSRecords[2]
	if mx.Record != "SPF" || mx.Type != "MX" || mx.Name != "send" || mx.Priority == nil || *mx.Priority != 10 || mx.Status != "not_started" {
		t.Fatalf("MX record: %+v", mx)
	}
	if first := settings.Channel.DNSRecords[0]; first.Record != "DKIM" || first.Priority != nil || first.Value == "" {
		t.Fatalf("DKIM record: %+v", first)
	}
	if settings.Channel.DomainVerifiedAt != nil {
		t.Fatal("a new domain must start unverified")
	}
	if fake.called("GET /domains/d91cd9bd-provider-desk-example-com") != 0 {
		t.Fatal("reading settings must not call the provider")
	}

	// Another domain gets its own provider domain.
	if response := request(t, api, http.MethodPut, "/api/v1/channels/email", map[string]any{"support_address": "help@other-brand.example.com"}, admin, nil); response.Code != http.StatusOK {
		t.Fatalf("PUT other domain status=%d", response.Code)
	}
	if created := fake.called("POST /domains"); created != 2 {
		t.Fatalf("provider domain created %d times, want 2 after changing domain", created)
	}
}

func TestProviderDomainVerificationFollowsTheProviderStatus(t *testing.T) {
	fake, server := newFakeResend(t)
	api, service, pool, _ := testAPIWithConfig(t, resendDomainConfig(server))
	admin := createSession(t, api, service, "Provider Verify", "admin@provider-verify.example.com")
	_ = connectEmail(t, api, admin, "support@provider-verify.example.com")

	for _, status := range []string{"pending", "failed"} {
		fake.setStatus("provider-verify.example.com", status)
		response := request(t, api, http.MethodPost, verifyDomainPath, nil, admin, nil)
		if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "DKIM") {
			t.Fatalf("%s status=%d body=%s, want 409 naming the records", status, response.Code, response.Body.String())
		}
		settings := providerState(t, api, admin)
		if settings.Channel.DomainVerifiedAt != nil || settings.Channel.DNSRecords[0].Status != status {
			t.Fatalf("%s: verified=%v records=%+v", status, settings.Channel.DomainVerifiedAt, settings.Channel.DNSRecords)
		}
	}
	if fake.called("POST /domains/d91cd9bd-provider-verify-example-com/verify") == 0 {
		t.Fatal("an unverified domain must ask the provider to check its records")
	}
	if got := queryString(t, pool, `SELECT COALESCE(domain_verified_at::text,'') FROM email_channels`); got != "" {
		t.Fatalf("domain recorded as verified while the provider says failed: %q", got)
	}

	fake.setStatus("provider-verify.example.com", "verified")
	if response := request(t, api, http.MethodPost, verifyDomainPath, nil, admin, nil); response.Code != http.StatusOK {
		t.Fatalf("verified status=%d body=%s", response.Code, response.Body.String())
	}
	settings := providerState(t, api, admin)
	if settings.Channel.DomainVerifiedAt == nil || settings.Channel.DNSRecords[0].Status != "verified" {
		t.Fatalf("verified state: %+v", settings.Channel)
	}
	var sending struct {
		SendingFrom string `json:"sending_from"`
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/channels/email", nil, admin, nil), &sending)
	if sending.SendingFrom != "support@provider-verify.example.com" {
		t.Fatalf("a verified domain sends as the support address, got %q", sending.SendingFrom)
	}
}

func TestProviderDomainVerificationWhenTheProviderIsUnreachableRecordsNothing(t *testing.T) {
	fake, server := newFakeResend(t)
	api, service, pool, _ := testAPIWithConfig(t, resendDomainConfig(server))
	admin := createSession(t, api, service, "Provider Down", "admin@provider-down.example.com")
	_ = connectEmail(t, api, admin, "support@provider-down.example.com")
	fake.setStatus("provider-down.example.com", "verified")
	fake.mu.Lock()
	fake.down = true
	fake.mu.Unlock()

	if response := request(t, api, http.MethodPost, verifyDomainPath, nil, admin, nil); response.Code != http.StatusBadGateway {
		t.Fatalf("unreachable provider status=%d body=%s, want 502", response.Code, response.Body.String())
	}
	if got := queryString(t, pool, `SELECT COALESCE(domain_verified_at::text,'') FROM email_channels`); got != "" {
		t.Fatalf("nothing may be recorded while the provider is down: %q", got)
	}
}

func TestSavingTheAddressSurvivesAnUnreachableProviderAndVerifyCreatesTheDomain(t *testing.T) {
	fake, server := newFakeResend(t)
	api, service, _, _ := testAPIWithConfig(t, resendDomainConfig(server))
	admin := createSession(t, api, service, "Provider Late", "admin@provider-late.example.com")
	fake.mu.Lock()
	fake.down = true
	fake.mu.Unlock()
	if response := request(t, api, http.MethodPut, "/api/v1/channels/email", map[string]any{"support_address": "support@provider-late.example.com"}, admin, nil); response.Code != http.StatusOK {
		t.Fatalf("PUT while provider down status=%d", response.Code)
	}
	if settings := providerState(t, api, admin); settings.Channel.Domain == "" || len(settings.Channel.DNSRecords) != 0 {
		t.Fatalf("channel must be saved without records: %+v", settings)
	}
	fake.mu.Lock()
	fake.down = false
	fake.mu.Unlock()
	if response := request(t, api, http.MethodPost, verifyDomainPath, nil, admin, nil); response.Code != http.StatusConflict {
		t.Fatalf("verify after recovery status=%d body=%s", response.Code, response.Body.String())
	}
	if settings := providerState(t, api, admin); len(settings.Channel.DNSRecords) != 3 {
		t.Fatalf("records after verify created the domain: %+v", settings.Channel.DNSRecords)
	}
}

func TestProviderDomainsAreWorkspaceScoped(t *testing.T) {
	fake, server := newFakeResend(t)
	api, service, pool, _ := testAPIWithConfig(t, resendDomainConfig(server))
	adminA := createSession(t, api, service, "Provider Alpha", "admin@provider-alpha.example.com")
	adminB := createSession(t, api, service, "Provider Beta", "admin@provider-beta.example.com")
	_ = connectEmail(t, api, adminA, "support@alpha-brand.example.com")
	fake.setStatus("alpha-brand.example.com", "verified")
	if response := request(t, api, http.MethodPost, verifyDomainPath, nil, adminA, nil); response.Code != http.StatusOK {
		t.Fatalf("A verify status=%d", response.Code)
	}

	// B claims A's verified domain: the provider refuses a second registration, and B stays unverified.
	if response := request(t, api, http.MethodPut, "/api/v1/channels/email", map[string]any{"support_address": "support@alpha-brand.example.com"}, adminB, nil); response.Code != http.StatusOK {
		t.Fatalf("B PUT status=%d", response.Code)
	}
	if response := request(t, api, http.MethodPost, verifyDomainPath, nil, adminB, nil); response.Code != http.StatusConflict ||
		!strings.Contains(response.Body.String(), "email_domain_unavailable") {
		t.Fatalf("B verify of A's domain status=%d body=%s, want 409 email_domain_unavailable", response.Code, response.Body.String())
	}
	settingsB := providerState(t, api, adminB)
	if settingsB.Channel.DomainVerifiedAt != nil || len(settingsB.Channel.DNSRecords) != 0 {
		t.Fatalf("B received A's domain state: %+v", settingsB.Channel)
	}
	if providerState(t, api, adminA).Channel.DomainVerifiedAt == nil {
		t.Fatal("A lost its verification")
	}
	if count := queryString(t, pool, `SELECT count(*)::text FROM email_channels WHERE provider_domain_id IS NOT NULL`); count != "1" {
		t.Fatalf("provider domain ids stored: %s, want only A's", count)
	}
}

func TestWithoutAProviderKeyDomainVerificationStaysOnTheTXTRecord(t *testing.T) {
	api, service, _, _ := testAPIWithConfig(t, verifiedPolicy("mail.colliedesk.test"))
	admin := createSession(t, api, service, "No Provider", "admin@no-provider.example.com")
	_ = connectEmail(t, api, admin, "support@no-provider.example.com")
	settings := providerState(t, api, admin)
	if settings.DomainVerification != "txt" || len(settings.Channel.DNSRecords) != 0 {
		t.Fatalf("TXT mode: %+v", settings)
	}
}
