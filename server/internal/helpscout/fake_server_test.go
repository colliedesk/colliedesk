package helpscout_test

import (
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
)

const (
	testAppID     = "hs-app-id"
	testAppSecret = "hs-app-secret-DO-NOT-PRINT"
	pngSignature  = "\x89PNG\r\n\x1a\n"
	inlineImage   = pngSignature + "INLINE-1"
)

// attachmentBodies are the bytes behind the fixtures' attachments; their lengths match "size".
var attachmentBodies = map[string]string{"3001": "PDF-BYTES-1", "3002": pngSignature + "SHOT"}

var (
	threadsPath = regexp.MustCompile(`^/v2/conversations/(\d+)/threads$`)
	dataPath    = regexp.MustCompile(`^/v2/conversations/\d+/attachments/(\d+)/data$`)
	customer    = regexp.MustCompile(`^/v2/customers/(\d+)$`)
)

// fakeHelpScout serves JSON shaped like the Help Scout Inbox API 2.0 from testdata over TLS: OAuth2
// client credentials, HAL pages (_embedded + page), base64 attachment data and an image host.
// Fields toggle failure scenarios.
type fakeHelpScout struct {
	t            *testing.T
	api, images  *httptest.Server
	roots        *x509.CertPool
	mu           sync.Mutex
	requests     map[string]int
	tokensIssued int
	validTokens  map[string]bool
	authedCalls  int
	twoMailboxes bool
	// expireTokensAfter revokes every issued token once this many authorized calls were answered.
	expireTokensAfter int
	// throttle answers 429 with X-RateLimit-Retry-After 7 to the first n requests of a path.
	throttle map[string]int
	// shortThreads makes conversation 103 return one thread while the list says it has two.
	shortThreads bool
	// dataBody overrides the bytes behind an attachment id.
	dataBody map[string]string
}

func newFakeHelpScout(t *testing.T) *fakeHelpScout {
	t.Helper()
	fake := &fakeHelpScout{t: t, requests: map[string]int{}, validTokens: map[string]bool{}, throttle: map[string]int{}, dataBody: map[string]string{}}
	fake.api = httptest.NewTLSServer(http.HandlerFunc(fake.serveAPI))
	fake.images = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fake.hit("images" + r.URL.Path)
		if r.Header.Get("Authorization") != "" {
			t.Errorf("the access token was sent to the image host")
		}
		if r.URL.Path != "/file/inline-1.png" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte(inlineImage))
	}))
	fake.roots = x509.NewCertPool()
	fake.roots.AddCert(fake.api.Certificate())
	fake.roots.AddCert(fake.images.Certificate())
	t.Cleanup(func() { fake.api.Close(); fake.images.Close() })
	return fake
}

func (f *fakeHelpScout) hit(key string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests[key]++
	return f.requests[key]
}

func (f *fakeHelpScout) count(key string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests[key]
}

func (f *fakeHelpScout) serveAPI(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Path
	if page := r.URL.Query().Get("page"); page != "" && page != "1" {
		key += "?page=" + page
	}
	if status := r.URL.Query().Get("status"); status == "spam" {
		key += "?status=spam"
	}
	n := f.hit(key)
	if r.URL.Path == "/v2/oauth2/token" {
		f.issueToken(w, r)
		return
	}
	if !f.authorized(r) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_token"}`))
		return
	}
	f.mu.Lock()
	throttled := n <= f.throttle[key]
	f.mu.Unlock()
	if throttled {
		w.Header().Set("X-RateLimit-Retry-After", "7")
		w.WriteHeader(http.StatusTooManyRequests)
		return
	}
	f.serveResource(w, r)
}

func (f *fakeHelpScout) issueToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.ParseForm() != nil || r.PostForm.Get("grant_type") != "client_credentials" ||
		r.PostForm.Get("client_id") != testAppID || r.PostForm.Get("client_secret") != testAppSecret {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_client"}`))
		return
	}
	f.mu.Lock()
	f.tokensIssued++
	token := fmt.Sprintf("token-%d", f.tokensIssued)
	f.validTokens[token] = true
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"token_type": "bearer", "access_token": token, "expires_in": 172800})
}

func (f *fakeHelpScout) authorized(r *http.Request) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || !f.validTokens[token] {
		return false
	}
	f.authedCalls++
	if f.expireTokensAfter > 0 && f.authedCalls == f.expireTokensAfter {
		f.validTokens = map[string]bool{}
	}
	return true
}

func (f *fakeHelpScout) serveResource(w http.ResponseWriter, r *http.Request) {
	query, path := r.URL.Query(), r.URL.Path
	page := query.Get("page")
	if page == "" {
		page = "1"
	}
	switch {
	case path == "/v2/mailboxes" && f.twoMailboxes:
		f.fixture(w, "mailboxes-two.json")
	case path == "/v2/mailboxes":
		f.fixture(w, "mailboxes.json")
	case path == "/v2/users":
		f.fixture(w, "users-page-"+page+".json")
	case path == "/v2/conversations" && query.Get("mailbox") != "501":
		http.Error(w, `{"error":"unexpected mailbox"}`, http.StatusBadRequest)
	case path == "/v2/conversations" && query.Get("status") == "spam":
		f.fixture(w, "conversations-spam.json")
	case path == "/v2/conversations" && query.Get("status") == "all":
		f.fixture(w, "conversations-page-"+page+".json")
	case threadsPath.MatchString(path) && f.shortThreads && threadsPath.FindStringSubmatch(path)[1] == "103":
		f.fixture(w, "threads-103-page-1-short.json")
	case threadsPath.MatchString(path):
		f.fixture(w, "threads-"+threadsPath.FindStringSubmatch(path)[1]+"-page-"+page+".json")
	case dataPath.MatchString(path):
		id := dataPath.FindStringSubmatch(path)[1]
		f.mu.Lock()
		body, ok := f.dataBody[id]
		f.mu.Unlock()
		if !ok {
			body = attachmentBodies[id]
		}
		w.Header().Set("Content-Type", "application/hal+json")
		_ = json.NewEncoder(w).Encode(map[string]string{"data": base64.StdEncoding.EncodeToString([]byte(body))})
	case customer.MatchString(path):
		f.fixture(w, "customer-"+customer.FindStringSubmatch(path)[1]+".json")
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeHelpScout) fixture(w http.ResponseWriter, name string) {
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		http.NotFound(w, nil)
		return
	}
	body := strings.ReplaceAll(string(data), "{{IMAGES}}", f.images.Listener.Addr().String())
	w.Header().Set("Content-Type", "application/hal+json")
	_, _ = w.Write([]byte(body))
}
