package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type providerSettings struct {
	DomainVerification string `json:"domain_verification"`
	Channel            struct {
		Domain              string  `json:"domain"`
		DomainVerifiedAt    *string `json:"domain_verified_at"`
		DNSRecordName       string  `json:"dns_record_name"`
		DNSRecordValue      string  `json:"dns_record_value"`
		DNSRecordsExpiredAt *string `json:"dns_records_expired_at"`
		DNSRecords          []struct {
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

// prove publishes the workspace's TXT ownership record, which the server checks before it calls the provider.
func prove(t *testing.T, api http.Handler, dns *fakeDNS, admin string) {
	t.Helper()
	channel := providerState(t, api, admin).Channel
	dns.mu.Lock()
	dns.records[channel.DNSRecordName] = append(dns.records[channel.DNSRecordName], channel.DNSRecordValue)
	dns.mu.Unlock()
}

// verify moves the API clock past the minimum interval between provider checks, then asks for one.
func verify(t *testing.T, api http.Handler, fake *fakeResend, admin string) *httptest.ResponseRecorder {
	t.Helper()
	fake.advance(time.Minute)
	return request(t, api, http.MethodPost, verifyDomainPath, nil, admin, nil)
}

func TestVerifyWithoutTheTXTProofMakesNoProviderCall(t *testing.T) {
	fake, server := newFakeResend(t)
	api, service, _, dns := testAPIWithConfig(t, resendDomainConfig(fake, server))
	admin := createSession(t, api, service, "Proof Desk", "admin@proof-desk.example.com")
	putAddress(t, api, admin, "support@proof-desk.example.com")

	response := verify(t, api, fake, admin)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "dns_record_not_found") || !strings.Contains(response.Body.String(), "_colliedesk.proof-desk.example.com") {
		t.Fatalf("status=%d body=%s, want 409 asking for the TXT record", response.Code, response.Body.String())
	}
	if fake.callCount() != 0 {
		t.Fatalf("a domain nobody proved must cost no provider call, got %v", fake.calls)
	}

	prove(t, api, dns, admin)
	if response := verify(t, api, fake, admin); response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "dns_record_not_verified") {
		t.Fatalf("proven domain status=%d body=%s", response.Code, response.Body.String())
	}
	if fake.called("POST /domains") != 1 {
		t.Fatalf("a proven domain is created once: %v", fake.calls)
	}
}

func TestSavingTheAddressNeverCallsTheProviderAndVerifyCreatesTheDomainOnce(t *testing.T) {
	fake, server := newFakeResend(t)
	api, service, _, dns := testAPIWithConfig(t, resendDomainConfig(fake, server))
	admin := createSession(t, api, service, "Provider Desk", "admin@provider-desk.example.com")

	putAddress(t, api, admin, "support@provider-desk.example.com")
	putAddress(t, api, admin, "help@provider-desk.example.com")
	if fake.callCount() != 0 {
		t.Fatalf("saving the address called the provider: %v", fake.calls)
	}
	settings := providerState(t, api, admin)
	if settings.DomainVerification != "provider" || len(settings.Channel.DNSRecords) != 0 {
		t.Fatalf("before the first verify there are no records: %+v", settings)
	}

	prove(t, api, dns, admin)
	for range 2 {
		if response := verify(t, api, fake, admin); response.Code != http.StatusConflict {
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
	api, service, pool, dns := testAPIWithConfig(t, resendDomainConfig(fake, server))
	admin := createSession(t, api, service, "Provider Verify", "admin@provider-verify.example.com")
	_ = connectEmail(t, api, admin, "support@provider-verify.example.com")
	prove(t, api, dns, admin)
	_ = verify(t, api, fake, admin)

	for _, status := range []string{"pending", "failed"} {
		fake.setStatus("provider-verify.example.com", status)
		response := verify(t, api, fake, admin)
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
	if got := queryString(t, pool, `SELECT COALESCE(verified_at::text,'') FROM email_domains`); got != "" {
		t.Fatalf("domain recorded as verified while the provider says failed: %q", got)
	}

	fake.setStatus("provider-verify.example.com", "verified")
	if response := verify(t, api, fake, admin); response.Code != http.StatusOK {
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
	api, service, pool, dns := testAPIWithConfig(t, resendDomainConfig(fake, server))
	admin := createSession(t, api, service, "Provider Down", "admin@provider-down.example.com")
	_ = connectEmail(t, api, admin, "support@provider-down.example.com")
	prove(t, api, dns, admin)
	fake.set(func(f *fakeResend) { f.down = true })

	if response := verify(t, api, fake, admin); response.Code != http.StatusBadGateway {
		t.Fatalf("unreachable provider status=%d body=%s, want 502", response.Code, response.Body.String())
	}
	if got := queryString(t, pool, `SELECT COALESCE(verified_at::text,'')||COALESCE(provider_domain_id,'') FROM email_domains`); got != "" {
		t.Fatalf("nothing may be recorded while the provider is down: %q", got)
	}
}

func TestOnlyAValidationRefusalOfTheNameIsShownToTheTenant(t *testing.T) {
	fake, server := newFakeResend(t)
	api, service, _, dns := testAPIWithConfig(t, resendDomainConfig(fake, server))
	admin := createSession(t, api, service, "Provider Bad", "admin@provider-bad.example.com")
	_ = connectEmail(t, api, admin, "support@bad.example.com")
	prove(t, api, dns, admin)
	response := verify(t, api, fake, admin)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "email_domain_unavailable") || !strings.Contains(response.Body.String(), "Invalid domain name.") {
		t.Fatalf("status=%d body=%s, want 409 email_domain_unavailable with the provider's reason", response.Code, response.Body.String())
	}

	// Credential, permission and plan problems are the operator's: the tenant sees a generic 502.
	for name, forced := range map[string]forcedResendError{
		"bad key":        {400, `{"statusCode":400,"name":"validation_error","message":"API key is invalid"}`},
		"restricted key": {403, `{"statusCode":403,"name":"restricted_api_key","message":"API key is not active"}`},
		"suspended key":  {403, `{"statusCode":403,"name":"suspended_api_key","message":"API key is suspended"}`},
		"locked":         {409, `{"statusCode":409,"name":"resource_locked","message":"Resource is locked"}`},
		"rate limit":     {429, `{"statusCode":429,"name":"rate_limit_exceeded","message":"Too many requests"}`},
	} {
		putAddress(t, api, admin, "support@"+strings.ReplaceAll(name, " ", "-")+".example.com")
		channel := providerState(t, api, admin).Channel
		dns.set(channel.DNSRecordName, channel.DNSRecordValue)
		fake.set(func(f *fakeResend) { f.forcedCreate = &forced })
		response := verify(t, api, fake, admin)
		if response.Code != http.StatusBadGateway || strings.Contains(response.Body.String(), "API key") || strings.Contains(response.Body.String(), "locked") {
			t.Errorf("%s: status=%d body=%s, want a generic 502", name, response.Code, response.Body.String())
		}
	}
}

// A second workspace claiming a domain another one verified gets its own new provider domain, which
// stays unverified until its own DNS carries that domain's records.
func TestProviderDomainsAreWorkspaceScoped(t *testing.T) {
	fake, server := newFakeResend(t)
	api, service, pool, dns := testAPIWithConfig(t, resendDomainConfig(fake, server))
	adminA := createSession(t, api, service, "Provider Alpha", "admin@provider-alpha.example.com")
	adminB := createSession(t, api, service, "Provider Beta", "admin@provider-beta.example.com")
	_ = connectEmail(t, api, adminA, "support@alpha-brand.example.com")
	prove(t, api, dns, adminA)
	_ = verify(t, api, fake, adminA)
	fake.setStatus("alpha-brand.example.com", "verified")
	if response := verify(t, api, fake, adminA); response.Code != http.StatusOK {
		t.Fatalf("A verify status=%d", response.Code)
	}

	putAddress(t, api, adminB, "support@alpha-brand.example.com")
	callsBefore := fake.callCount()
	if response := verify(t, api, fake, adminB); response.Code != http.StatusConflict || fake.callCount() != callsBefore {
		t.Fatalf("B without its own TXT proof: status=%d, provider calls %d -> %d", response.Code, callsBefore, fake.callCount())
	}
	prove(t, api, dns, adminB)
	if response := verify(t, api, fake, adminB); response.Code != http.StatusConflict {
		t.Fatalf("B verify of A's domain status=%d body=%s, want 409 (its own copy is not verified)", response.Code, response.Body.String())
	}
	settingsA, settingsB := providerState(t, api, adminA), providerState(t, api, adminB)
	if settingsB.Channel.DomainVerifiedAt != nil {
		t.Fatalf("B inherited A's verification: %+v", settingsB.Channel)
	}
	if settingsA.Channel.DomainVerifiedAt == nil {
		t.Fatal("A lost its verification")
	}
	if settingsA.Channel.DNSRecords[0].Value == settingsB.Channel.DNSRecords[0].Value {
		t.Fatal("two workspaces must hold different DKIM keys for the same name")
	}
	if count := queryString(t, pool, `SELECT count(DISTINCT provider_domain_id)::text FROM email_domains WHERE provider_domain_id IS NOT NULL`); count != "2" {
		t.Fatalf("each workspace needs its own provider domain, got %s", count)
	}
}

func TestProviderChecksAreLimitedPerWorkspaceAndCostNothingWhenRefused(t *testing.T) {
	fake, server := newFakeResend(t)
	api, service, _, dns := testAPIWithConfig(t, resendDomainConfig(fake, server))
	admin := createSession(t, api, service, "Rate Desk", "admin@rate-desk.example.com")
	other := createSession(t, api, service, "Rate Other", "admin@rate-other.example.com")
	_ = connectEmail(t, api, admin, "support@rate-desk.example.com")
	_ = connectEmail(t, api, other, "support@rate-other.example.com")
	prove(t, api, dns, admin)
	prove(t, api, dns, other)

	fake.advance(time.Minute)
	if response := request(t, api, http.MethodPost, verifyDomainPath, nil, admin, nil); response.Code != http.StatusConflict {
		t.Fatalf("first check status=%d", response.Code)
	}
	calls := fake.callCount()
	for range 3 {
		response := request(t, api, http.MethodPost, verifyDomainPath, nil, admin, nil)
		if response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") == "" {
			t.Fatalf("repeated check status=%d retry-after=%q, want 429 with Retry-After", response.Code, response.Header().Get("Retry-After"))
		}
	}
	if fake.callCount() != calls {
		t.Fatalf("a refused check must not reach the provider: %v", fake.calls[calls:])
	}
	if response := request(t, api, http.MethodPost, verifyDomainPath, nil, other, nil); response.Code != http.StatusConflict {
		t.Fatalf("another workspace is not limited by this one: status=%d", response.Code)
	}
	fake.advance(31 * time.Second)
	if response := request(t, api, http.MethodPost, verifyDomainPath, nil, admin, nil); response.Code != http.StatusConflict {
		t.Fatalf("after the interval status=%d", response.Code)
	}
}

func TestTwoVerifiesAtOnceLeaveOneProviderDomain(t *testing.T) {
	fake, server := newFakeResend(t)
	// Every clock read is 31 s later, so the limiter lets both requests through and only the lock is left.
	fake.set(func(f *fakeResend) { f.autoAdvance = 31 * time.Second; f.createDelay = 150 * time.Millisecond })
	api, service, pool, dns := testAPIWithConfig(t, resendDomainConfig(fake, server))
	admin := createSession(t, api, service, "Race Desk", "admin@race-desk.example.com")
	_ = connectEmail(t, api, admin, "support@race-desk.example.com")
	prove(t, api, dns, admin)

	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			request(t, api, http.MethodPost, verifyDomainPath, nil, admin, nil)
		}()
	}
	wg.Wait()
	// The request that lost the race queued the domain it created; the sweep deletes it.
	clock := time.Now().Add(time.Minute)
	if err := newSweeper(pool, server.URL, &clock).Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if live := fake.live(); len(live) != 1 || live[0] != queryString(t, pool, `SELECT provider_domain_id FROM email_domains`) {
		t.Fatalf("two simultaneous verifies left %d provider domains: %v", len(live), live)
	}
}

func TestAProviderDomainDeletedOutsideTheAppIsCreatedAgain(t *testing.T) {
	fake, server := newFakeResend(t)
	api, service, pool, dns := testAPIWithConfig(t, resendDomainConfig(fake, server))
	admin := createSession(t, api, service, "Gone Desk", "admin@gone-desk.example.com")
	_ = connectEmail(t, api, admin, "support@gone-desk.example.com")
	prove(t, api, dns, admin)
	_ = verify(t, api, fake, admin)
	first := fake.idOf("gone-desk.example.com")
	fake.set(func(f *fakeResend) { delete(f.domains, first) })

	if response := verify(t, api, fake, admin); response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "dns_record_not_verified") {
		t.Fatalf("status=%d body=%s, want a normal pending answer, not 502", response.Code, response.Body.String())
	}
	second := fake.idOf("gone-desk.example.com")
	if second == first || queryString(t, pool, `SELECT provider_domain_id FROM email_domains`) != second || len(fake.live()) != 1 {
		t.Fatalf("the domain must be created again and stored: first=%s second=%s live=%v", first, second, fake.live())
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
