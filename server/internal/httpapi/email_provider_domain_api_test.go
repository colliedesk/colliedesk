package httpapi_test

import (
	"encoding/json"
	"fmt"
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
	// byName holds the newest domain of each name: Resend lets the same name be added again as a new domain.
	byName  map[string]string
	created int
	calls   []string
	// failDelete makes DELETE answer 500.
	failDelete bool
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

// live lists the ids of the domains still in the account.
func (f *fakeResend) live() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	ids := make([]string, 0, len(f.domains))
	for id := range f.domains {
		ids = append(ids, id)
	}
	return ids
}

func (f *fakeResend) idOf(name string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.byName[name]
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
		if strings.HasPrefix(body.Name, "bad.") {
			response.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = response.Write([]byte(`{"statusCode":422,"message":"Invalid domain name.","name":"validation_error"}`))
			return
		}
		f.created++
		domain := &fakeResendDomain{ID: fmt.Sprintf("d91cd9bd-%d-%s", f.created, strings.ReplaceAll(body.Name, ".", "-")), Name: body.Name, Status: f.status}
		f.domains[domain.ID], f.byName[body.Name] = domain, domain.ID
		_ = json.NewEncoder(response).Encode(map[string]any{"id": domain.ID, "name": domain.Name, "created_at": "2026-10-18T00:00:00.000Z",
			"status": domain.Status, "capabilities": map[string]string{"sending": "enabled", "receiving": "disabled"}, "records": f.records(domain), "region": "us-east-1"})
	case request.Method == http.MethodDelete && strings.HasPrefix(path, "/"):
		id := strings.TrimPrefix(path, "/")
		if f.failDelete {
			response.WriteHeader(http.StatusInternalServerError)
			return
		}
		if _, ok := f.domains[id]; !ok {
			response.WriteHeader(http.StatusNotFound)
			_, _ = response.Write([]byte(`{"statusCode":404,"name":"not_found","message":"Domain not found"}`))
			return
		}
		delete(f.domains, id)
		_ = json.NewEncoder(response).Encode(map[string]any{"object": "domain", "id": id, "deleted": true})
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

func putAddress(t *testing.T, api http.Handler, admin, address string) {
	t.Helper()
	if response := request(t, api, http.MethodPut, "/api/v1/channels/email", map[string]any{"support_address": address}, admin, nil); response.Code != http.StatusOK {
		t.Fatalf("PUT %s status=%d body=%s", address, response.Code, response.Body.String())
	}
}

func TestSavingTheAddressNeverCallsTheProviderAndVerifyCreatesTheDomainOnce(t *testing.T) {
	fake, server := newFakeResend(t)
	api, service, _, _ := testAPIWithConfig(t, resendDomainConfig(server))
	admin := createSession(t, api, service, "Provider Desk", "admin@provider-desk.example.com")

	putAddress(t, api, admin, "support@provider-desk.example.com")
	putAddress(t, api, admin, "help@provider-desk.example.com")
	if len(fake.calls) != 0 {
		t.Fatalf("saving the address called the provider: %v", fake.calls)
	}
	settings := providerState(t, api, admin)
	if settings.DomainVerification != "provider" || len(settings.Channel.DNSRecords) != 0 {
		t.Fatalf("before the first verify there are no records: %+v", settings)
	}

	for range 2 {
		if response := request(t, api, http.MethodPost, verifyDomainPath, nil, admin, nil); response.Code != http.StatusConflict {
			t.Fatalf("verify status=%d body=%s", response.Code, response.Body.String())
		}
	}
	if created := fake.called("POST /domains"); created != 1 {
		t.Fatalf("provider domain created %d times, want once", created)
	}
	settings = providerState(t, api, admin)
	if len(settings.Channel.DNSRecords) != 3 {
		t.Fatalf("records after verify: %+v", settings.Channel.DNSRecords)
	}
	mx := settings.Channel.DNSRecords[2]
	if mx.Record != "SPF" || mx.Type != "MX" || mx.Name != "send" || mx.Priority == nil || *mx.Priority != 10 {
		t.Fatalf("MX record: %+v", mx)
	}
	if first := settings.Channel.DNSRecords[0]; first.Record != "DKIM" || first.Priority != nil || first.Value == "" {
		t.Fatalf("DKIM record: %+v", first)
	}
	if settings.Channel.DomainVerifiedAt != nil {
		t.Fatal("a new domain must start unverified")
	}
}

func TestProviderDomainVerificationFollowsTheProviderStatus(t *testing.T) {
	fake, server := newFakeResend(t)
	api, service, pool, _ := testAPIWithConfig(t, resendDomainConfig(server))
	admin := createSession(t, api, service, "Provider Verify", "admin@provider-verify.example.com")
	_ = connectEmail(t, api, admin, "support@provider-verify.example.com")
	// The first verify creates the domain; the fake reports the status it is given for the rest.
	_ = request(t, api, http.MethodPost, verifyDomainPath, nil, admin, nil)

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
	if fake.called("POST /domains/"+fake.idOf("provider-verify.example.com")+"/verify") == 0 {
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
	fake.mu.Lock()
	fake.down = true
	fake.mu.Unlock()

	if response := request(t, api, http.MethodPost, verifyDomainPath, nil, admin, nil); response.Code != http.StatusBadGateway {
		t.Fatalf("unreachable provider status=%d body=%s, want 502", response.Code, response.Body.String())
	}
	if got := queryString(t, pool, `SELECT COALESCE(domain_verified_at::text,'')||COALESCE(provider_domain_id,'') FROM email_channels`); got != "" {
		t.Fatalf("nothing may be recorded while the provider is down: %q", got)
	}
}

func TestAProviderRefusalOfTheDomainNameIsShownNotRetried(t *testing.T) {
	_, server := newFakeResend(t)
	api, service, _, _ := testAPIWithConfig(t, resendDomainConfig(server))
	admin := createSession(t, api, service, "Provider Bad", "admin@provider-bad.example.com")
	_ = connectEmail(t, api, admin, "support@bad.example.com")
	response := request(t, api, http.MethodPost, verifyDomainPath, nil, admin, nil)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "email_domain_unavailable") || !strings.Contains(response.Body.String(), "Invalid domain name.") {
		t.Fatalf("status=%d body=%s, want 409 email_domain_unavailable with the provider's reason", response.Code, response.Body.String())
	}
}

// A second workspace claiming a domain another one verified gets its own new provider domain, which
// stays unverified until its own DNS carries that domain's records.
func TestProviderDomainsAreWorkspaceScoped(t *testing.T) {
	fake, server := newFakeResend(t)
	api, service, pool, _ := testAPIWithConfig(t, resendDomainConfig(server))
	adminA := createSession(t, api, service, "Provider Alpha", "admin@provider-alpha.example.com")
	adminB := createSession(t, api, service, "Provider Beta", "admin@provider-beta.example.com")
	_ = connectEmail(t, api, adminA, "support@alpha-brand.example.com")
	_ = request(t, api, http.MethodPost, verifyDomainPath, nil, adminA, nil)
	fake.setStatus("alpha-brand.example.com", "verified")
	if response := request(t, api, http.MethodPost, verifyDomainPath, nil, adminA, nil); response.Code != http.StatusOK {
		t.Fatalf("A verify status=%d", response.Code)
	}

	putAddress(t, api, adminB, "support@alpha-brand.example.com")
	if response := request(t, api, http.MethodPost, verifyDomainPath, nil, adminB, nil); response.Code != http.StatusConflict {
		t.Fatalf("B verify of A's domain status=%d body=%s, want 409 (its own copy is not verified)", response.Code, response.Body.String())
	}
	if settingsB := providerState(t, api, adminB); settingsB.Channel.DomainVerifiedAt != nil {
		t.Fatalf("B inherited A's verification: %+v", settingsB.Channel)
	}
	if providerState(t, api, adminA).Channel.DomainVerifiedAt == nil {
		t.Fatal("A lost its verification")
	}
	if count := queryString(t, pool, `SELECT count(DISTINCT provider_domain_id)::text FROM email_channels WHERE provider_domain_id IS NOT NULL`); count != "2" {
		t.Fatalf("each workspace needs its own provider domain, got %s", count)
	}
	if len(fake.live()) != 2 {
		t.Fatalf("live provider domains: %v", fake.live())
	}
}

func TestChangingTheDomainRepeatedlyLeavesOneLiveProviderDomain(t *testing.T) {
	fake, server := newFakeResend(t)
	api, service, pool, _ := testAPIWithConfig(t, resendDomainConfig(server))
	adminA := createSession(t, api, service, "Churn Alpha", "admin@churn-alpha.example.com")
	adminB := createSession(t, api, service, "Churn Beta", "admin@churn-beta.example.com")
	_ = connectEmail(t, api, adminB, "support@churn-beta-brand.example.com")
	_ = request(t, api, http.MethodPost, verifyDomainPath, nil, adminB, nil)
	betaDomain := fake.idOf("churn-beta-brand.example.com")

	putAddress(t, api, adminA, "support@churn-0.example.com")
	for i := 0; i < 6; i++ {
		_ = request(t, api, http.MethodPost, verifyDomainPath, nil, adminA, nil)
		putAddress(t, api, adminA, fmt.Sprintf("support@churn-%d.example.com", i+1))
	}
	_ = request(t, api, http.MethodPost, verifyDomainPath, nil, adminA, nil)
	if live := fake.live(); len(live) != 2 {
		t.Fatalf("live provider domains after churn: %v, want A's current one and B's", live)
	}
	if got := queryString(t, pool, `SELECT provider_domain_id FROM email_channels e JOIN workspaces w ON w.id=e.workspace_id WHERE w.name='Churn Alpha'`); got != fake.idOf("churn-6.example.com") {
		t.Fatalf("A keeps %q, want its newest domain", got)
	}

	if response := request(t, api, http.MethodDelete, "/api/v1/channels/email", nil, adminA, nil); response.Code != http.StatusNoContent {
		t.Fatalf("DELETE status=%d", response.Code)
	}
	if live := fake.live(); len(live) != 1 || live[0] != betaDomain {
		t.Fatalf("disconnecting A must remove A's domain and only that: %v (B's is %s)", live, betaDomain)
	}
	if left := queryString(t, pool, `SELECT count(*)::text FROM email_provider_domain_trash`); left != "0" {
		t.Fatalf("cleanup records left: %s", left)
	}
}

func TestAFailedProviderDeleteIsRetriedAndAnAlreadyGoneDomainIsForgotten(t *testing.T) {
	fake, server := newFakeResend(t)
	api, service, pool, _ := testAPIWithConfig(t, resendDomainConfig(server))
	admin := createSession(t, api, service, "Retry Delete", "admin@retry-delete.example.com")
	_ = connectEmail(t, api, admin, "support@retry-one.example.com")
	_ = request(t, api, http.MethodPost, verifyDomainPath, nil, admin, nil)
	first := fake.idOf("retry-one.example.com")

	fake.mu.Lock()
	fake.failDelete = true
	fake.mu.Unlock()
	putAddress(t, api, admin, "support@retry-two.example.com")
	if live := fake.live(); len(live) != 1 || live[0] != first {
		t.Fatalf("the old domain should still exist while delete fails: %v", live)
	}
	if kept := queryString(t, pool, `SELECT provider_domain_id FROM email_provider_domain_trash`); kept != first {
		t.Fatalf("the id to delete must not be lost: %q", kept)
	}

	fake.mu.Lock()
	fake.failDelete = false
	fake.mu.Unlock()
	_ = request(t, api, http.MethodPost, verifyDomainPath, nil, admin, nil)
	if live := fake.live(); len(live) != 1 || live[0] != fake.idOf("retry-two.example.com") {
		t.Fatalf("next verify must finish the cleanup: %v", live)
	}
	if left := queryString(t, pool, `SELECT count(*)::text FROM email_provider_domain_trash`); left != "0" {
		t.Fatalf("cleanup records left: %s", left)
	}

	// A domain removed in the Resend dashboard answers 404 and counts as gone.
	fake.mu.Lock()
	delete(fake.domains, fake.byName["retry-two.example.com"])
	fake.mu.Unlock()
	putAddress(t, api, admin, "support@retry-three.example.com")
	if left := queryString(t, pool, `SELECT count(*)::text FROM email_provider_domain_trash`); left != "0" {
		t.Fatalf("a 404 on delete must clear the record: %s left", left)
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
