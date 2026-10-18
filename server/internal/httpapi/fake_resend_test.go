package httpapi_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

const fakeResendKey = "re_test_key_not_a_secret"

// fakeResend answers like the Resend Domains API (create, get, verify, delete), as observed: a name that
// is already in the account is added again as a new domain with its own id and DKIM key.
type fakeResend struct {
	mu      sync.Mutex
	domains map[string]*fakeResendDomain
	// byName holds the newest domain of each name.
	byName  map[string]string
	created int
	calls   []string
	// status is what a domain reports once created and after a verify call.
	status string
	down   bool
	// failDelete makes DELETE answer 500.
	failDelete bool
	// createDelay widens the window between two concurrent creates.
	createDelay time.Duration
	// forcedCreate, when set, is the answer to POST /domains.
	forcedCreate *forcedResendError
	// clock is the API's time source: tests move it instead of sleeping past the verify interval.
	offset      time.Duration
	autoAdvance time.Duration
}

type forcedResendError struct {
	Status int
	Body   string
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

// now is the API clock: real time plus what the test has moved it, and plus autoAdvance on every read.
func (f *fakeResend) now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.offset += f.autoAdvance
	return time.Now().UTC().Add(f.offset)
}

func (f *fakeResend) advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.offset += d
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

func (f *fakeResend) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
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

func (f *fakeResend) set(mutate func(*fakeResend)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	mutate(f)
}

// records lists the DNS records of a domain; the DKIM key differs for every domain, as in the real API.
func (f *fakeResend) records(domain *fakeResendDomain) []map[string]any {
	status := domain.Status
	return []map[string]any{
		{"record": "DKIM", "name": "resend._domainkey", "value": "p=MIGfMA0GCSqGSIb3DQEBAQUAA4GN-" + domain.ID, "type": "TXT", "ttl": "Auto", "status": status},
		{"record": "SPF", "name": "send", "value": "v=spf1 include:amazonses.com ~all", "type": "TXT", "ttl": "Auto", "status": status},
		{"record": "SPF", "name": "send", "value": "feedback-smtp.us-east-1.amazonses.com", "type": "MX", "ttl": "Auto", "status": status, "priority": 10},
	}
}

func (f *fakeResend) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	f.mu.Lock()
	f.calls = append(f.calls, request.Method+" "+request.URL.Path)
	delay, forced, down, failDelete := f.createDelay, f.forcedCreate, f.down, f.failDelete
	f.mu.Unlock()
	response.Header().Set("Content-Type", "application/json")
	if request.Header.Get("Authorization") != "Bearer "+fakeResendKey {
		response.WriteHeader(http.StatusUnauthorized)
		_, _ = response.Write([]byte(`{"statusCode":401,"name":"restricted_api_key","message":"API key is invalid"}`))
		return
	}
	if down {
		response.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	path := strings.TrimPrefix(request.URL.Path, "/domains")
	if request.Method == http.MethodPost && path == "" {
		time.Sleep(delay)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case request.Method == http.MethodPost && path == "":
		if forced != nil {
			response.WriteHeader(forced.Status)
			_, _ = response.Write([]byte(forced.Body))
			return
		}
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
		response.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(response).Encode(map[string]any{"id": domain.ID, "name": domain.Name, "created_at": "2026-10-18T00:00:00.000Z",
			"status": domain.Status, "capabilities": map[string]string{"sending": "enabled", "receiving": "disabled"}, "records": f.records(domain), "region": "us-east-1"})
	case request.Method == http.MethodDelete && strings.HasPrefix(path, "/"):
		id := strings.TrimPrefix(path, "/")
		if failDelete {
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
			_, _ = response.Write([]byte(`{"statusCode":404,"name":"not_found","message":"Domain not found"}`))
			return
		}
		_ = json.NewEncoder(response).Encode(map[string]any{"object": "domain", "id": id})
	default:
		response.WriteHeader(http.StatusNotFound)
	}
}

func resendDomainConfig(fake *fakeResend, server *httptest.Server) func(*httpapi.Config) {
	return func(config *httpapi.Config) {
		config.Email.FromPolicy = email.FromPolicyVerified
		config.Email.MailFromDomain = "mail.colliedesk.test"
		config.Email.ResendAPIKey = fakeResendKey
		config.Email.ResendAPIBase = server.URL
		config.Now = fake.now
	}
}
