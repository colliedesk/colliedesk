package httpapi_test

import (
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/mailimage"
)

var (
	// A hand-written PNG: signature plus an IHDR chunk. The proxy must return exactly these bytes.
	tinyPNG  = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00synthetic")
	tinySVG  = []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
	proxySrc = regexp.MustCompile(`src="(/api/v1/mail-image\?[^"]*)"`)
)

// imageHost is a local image host the proxy may dial in tests, with a counter of the requests it got.
type imageHost struct {
	*httptest.Server
	hits atomic.Int32
}

func newImageHost(t *testing.T, handler http.HandlerFunc) *imageHost {
	t.Helper()
	host := &imageHost{}
	host.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host.hits.Add(1)
		handler(w, r)
	}))
	t.Cleanup(host.Close)
	return host
}

func serveBytes(contentType string, body []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentType)
		w.Write(body)
	}
}

func allowing(hosts ...*imageHost) mailimage.Options {
	allow := map[string]bool{}
	for _, host := range hosts {
		allow[host.Listener.Addr().String()] = true
	}
	return mailimage.Options{AllowAddresses: allow}
}

// proxyDesk is an API whose proxy fetcher may dial the given local hosts, with a signed-in admin.
func proxyDesk(t *testing.T, options mailimage.Options) (api http.Handler, admin, secret, storageDir string) {
	t.Helper()
	storageDir = t.TempDir()
	storage, err := attachments.NewLocalDisk(storageDir)
	if err != nil {
		t.Fatal(err)
	}
	handler, service, _, _ := testAPIWithConfig(t, func(config *httpapi.Config) {
		config.Attachments = storage
		config.MailImages = mailimage.NewFetcher(options)
	})
	admin = createSession(t, handler, service, "Proxy Desk", "admin@proxy-desk.example.com")
	return handler, admin, connectEmail(t, handler, admin, "support@proxy-desk.example.com"), storageDir
}

// htmlMail is a customer mail whose only part is the given HTML.
func htmlMail(messageID, markup string) string {
	return mail([]string{
		"From: Vic Customer <vic@customer.example>", "To: support@proxy-desk.example.com", "Subject: Newsletter",
		"Message-ID: <" + messageID + ">", "MIME-Version: 1.0", "Content-Type: text/html; charset=utf-8",
	}, markup)
}

// proxyPaths delivers one HTML mail and returns the proxy paths the viewer receives for its images.
func proxyPaths(t *testing.T, api http.Handler, admin, secret, markup string) []string {
	t.Helper()
	result := inboundOK(t, api, secret, htmlMail(fmt.Sprintf("m-%d@customer.example", time.Now().UnixNano()), markup))
	items := storedMessages(t, api, admin, *result.ConversationID)
	if len(items) != 1 || items[0].BodyHTML == nil {
		t.Fatalf("message has no HTML: %+v", items)
	}
	var paths []string
	for _, match := range proxySrc.FindAllStringSubmatch(*items[0].BodyHTML, -1) {
		paths = append(paths, html.UnescapeString(match[1]))
	}
	return paths
}

func TestListedMailHTMLSendsRemoteImagesThroughTheProxy(t *testing.T) {
	host := newImageHost(t, serveBytes("image/png", tinyPNG))
	api, admin, secret, _ := proxyDesk(t, allowing(host))
	result := inboundOK(t, api, secret, htmlMail("m1@customer.example", `<p>Hi <img src="`+host.URL+`/logo.png"> <img src="data:image/gif;base64,R0lGOD"></p>`))
	items := storedMessages(t, api, admin, *result.ConversationID)
	got := *items[0].BodyHTML
	if strings.Contains(got, host.URL) {
		t.Errorf("the viewer still receives the remote address: %s", got)
	}
	if !strings.Contains(got, `src="/api/v1/mail-image?u=`) || !strings.Contains(got, "data:image/gif;base64,R0lGOD") {
		t.Errorf("html = %s", got)
	}
	if host.hits.Load() != 0 {
		t.Errorf("listing the timeline called the image host %d times; text must not wait for images", host.hits.Load())
	}
}

func TestAgentTokenReadsTheStoredHTMLUnchanged(t *testing.T) {
	host := newImageHost(t, serveBytes("image/png", tinyPNG))
	api, admin, secret, _ := proxyDesk(t, allowing(host))
	result := inboundOK(t, api, secret, htmlMail("m2@customer.example", `<img src="`+host.URL+`/logo.png">`))
	agent := issueToken(t, api, admin, "Triage", "read")
	items := storedMessages(t, api, agent.Token, *result.ConversationID)
	if len(items) != 1 || items[0].BodyHTML == nil || !strings.Contains(*items[0].BodyHTML, host.URL+"/logo.png") {
		t.Fatalf("an agent token must read the original HTML, got %+v", items)
	}
}

func TestMailImageProxyServesTheImageAndCachesIt(t *testing.T) {
	host := newImageHost(t, serveBytes("image/png", tinyPNG))
	api, admin, secret, _ := proxyDesk(t, allowing(host))
	paths := proxyPaths(t, api, admin, secret, `<img src="`+host.URL+`/logo.png">`)
	if len(paths) != 1 {
		t.Fatalf("paths = %v", paths)
	}
	first := request(t, api, http.MethodGet, paths[0], nil, admin, nil)
	if first.Code != http.StatusOK || first.Body.String() != string(tinyPNG) {
		t.Fatalf("first view status=%d body=%q", first.Code, first.Body.String())
	}
	header := first.Header()
	if header.Get("Content-Type") != "image/png" || header.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("headers = %v", header)
	}
	if cache := header.Get("Cache-Control"); !strings.Contains(cache, "private") || !strings.Contains(cache, "max-age=2592000") || !strings.Contains(cache, "immutable") {
		t.Errorf("Cache-Control = %q", cache)
	}
	second := request(t, api, http.MethodGet, paths[0], nil, admin, nil)
	if second.Code != http.StatusOK || second.Body.String() != string(tinyPNG) {
		t.Fatalf("second view status=%d", second.Code)
	}
	if host.hits.Load() != 1 {
		t.Fatalf("the third party was called %d times, want 1", host.hits.Load())
	}
}

func TestMailImageProxyFetchesAgainWhenTheCachedFileIsGone(t *testing.T) {
	host := newImageHost(t, serveBytes("image/png", tinyPNG))
	api, admin, secret, storageDir := proxyDesk(t, allowing(host))
	paths := proxyPaths(t, api, admin, secret, `<img src="`+host.URL+`/logo.png">`)
	if request(t, api, http.MethodGet, paths[0], nil, admin, nil).Code != http.StatusOK {
		t.Fatal("first view failed")
	}
	removeStoredObjects(t, storageDir)
	if again := request(t, api, http.MethodGet, paths[0], nil, admin, nil); again.Code != http.StatusOK || again.Body.String() != string(tinyPNG) {
		t.Fatalf("status=%d", again.Code)
	}
	if host.hits.Load() != 2 {
		t.Fatalf("hits = %d, want 2", host.hits.Load())
	}
}

func TestMailImageProxyNeedsASignedInMember(t *testing.T) {
	host := newImageHost(t, serveBytes("image/png", tinyPNG))
	api, admin, secret, _ := proxyDesk(t, allowing(host))
	paths := proxyPaths(t, api, admin, secret, `<img src="`+host.URL+`/logo.png">`)
	if response := request(t, api, http.MethodGet, paths[0], nil, "", nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous status=%d", response.Code)
	}
	if host.hits.Load() != 0 {
		t.Fatalf("an anonymous request reached the third party")
	}
}

func TestMailImageProxyRefusesUnsignedAndTamperedLinks(t *testing.T) {
	host := newImageHost(t, serveBytes("image/png", tinyPNG))
	api, admin, secret, _ := proxyDesk(t, allowing(host))
	paths := proxyPaths(t, api, admin, secret, `<img src="`+host.URL+`/logo.png">`)
	query, _ := url.ParseQuery(strings.TrimPrefix(paths[0], "/api/v1/mail-image?"))
	other := url.Values{"u": {host.URL + "/other.png"}, "s": {query.Get("s")}}
	cases := map[string]string{
		"no signature":  "/api/v1/mail-image?u=" + url.QueryEscape(host.URL+"/logo.png"),
		"bad signature": "/api/v1/mail-image?u=" + url.QueryEscape(host.URL+"/logo.png") + "&s=" + strings.Repeat("0", 64),
		"other url":     "/api/v1/mail-image?" + other.Encode(),
		"no url":        "/api/v1/mail-image?s=" + query.Get("s"),
		"not a url":     "/api/v1/mail-image?u=%00&s=" + query.Get("s"),
	}
	for name, path := range cases {
		if response := request(t, api, http.MethodGet, path, nil, admin, nil); response.Code != http.StatusForbidden {
			t.Errorf("%s: status=%d, want 403", name, response.Code)
		}
	}
	if host.hits.Load() != 0 {
		t.Fatalf("a refused link reached the third party %d times", host.hits.Load())
	}
}

func TestMailImageLinkOfOneWorkspaceIsWorthlessInAnother(t *testing.T) {
	host := newImageHost(t, serveBytes("image/png", tinyPNG))
	storage, err := attachments.NewLocalDisk(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	api, service, _, _ := testAPIWithConfig(t, func(config *httpapi.Config) {
		config.Attachments = storage
		config.MailImages = mailimage.NewFetcher(allowing(host))
	})
	adminA := createSession(t, api, service, "Proxy A", "admin@proxy-a.example.com")
	adminB := createSession(t, api, service, "Proxy B", "admin@proxy-b.example.com")
	secretA := connectEmail(t, api, adminA, "support@proxy-a.example.com")
	paths := proxyPaths(t, api, adminA, secretA, `<img src="`+host.URL+`/logo.png">`)
	if response := request(t, api, http.MethodGet, paths[0], nil, adminB, nil); response.Code != http.StatusForbidden {
		t.Fatalf("workspace B used workspace A's link: status=%d", response.Code)
	}
	if host.hits.Load() != 0 {
		t.Fatal("the third party was called for a link of another workspace")
	}
}

func TestMailImageCacheIsPerWorkspace(t *testing.T) {
	host := newImageHost(t, serveBytes("image/png", tinyPNG))
	storage, err := attachments.NewLocalDisk(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	api, service, _, _ := testAPIWithConfig(t, func(config *httpapi.Config) {
		config.Attachments = storage
		config.MailImages = mailimage.NewFetcher(allowing(host))
	})
	adminA := createSession(t, api, service, "Cache A", "admin@cache-a.example.com")
	adminB := createSession(t, api, service, "Cache B", "admin@cache-b.example.com")
	markup := `<img src="` + host.URL + `/shared.png">`
	for _, desk := range []struct{ admin, address string }{{adminA, "support@cache-a.example.com"}, {adminB, "support@cache-b.example.com"}} {
		secret := connectEmail(t, api, desk.admin, desk.address)
		paths := proxyPaths(t, api, desk.admin, secret, markup)
		if response := request(t, api, http.MethodGet, paths[0], nil, desk.admin, nil); response.Code != http.StatusOK {
			t.Fatalf("%s: status=%d", desk.address, response.Code)
		}
	}
	if host.hits.Load() != 2 {
		t.Fatalf("hits = %d: a second workspace must not be served from the first one's cache", host.hits.Load())
	}
}

func TestMailImageProxyRefusesPrivateAndRedirectedTargets(t *testing.T) {
	secretFile := newImageHost(t, serveBytes("image/png", tinyPNG)) // loopback, not allowed to the proxy
	redirector := newImageHost(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, secretFile.URL+"/internal.png", http.StatusFound)
	})
	api, admin, secret, _ := proxyDesk(t, allowing(redirector))
	port := strings.TrimPrefix(secretFile.URL, "http://127.0.0.1:")
	targets := map[string]string{
		"loopback":            secretFile.URL + "/internal.png",
		"name for loopback":   "http://localhost:" + port + "/internal.png",
		"redirect to private": redirector.URL + "/start.png",
		"metadata":            "http://169.254.169.254/latest/meta-data/",
		"ipv6 metadata":       "http://[fd00:ec2::254]/latest/meta-data/",
		"private range":       "http://10.1.2.3/logo.png",
		"cgnat":               "http://100.64.0.9/logo.png",
	}
	for name, target := range targets {
		paths := proxyPaths(t, api, admin, secret, `<img src="`+target+`">`)
		if response := request(t, api, http.MethodGet, paths[0], nil, admin, nil); response.Code != http.StatusForbidden {
			t.Errorf("%s: status=%d body=%s, want 403", name, response.Code, response.Body.String())
		}
	}
	if secretFile.hits.Load() != 0 {
		t.Fatalf("the private host received %d requests", secretFile.hits.Load())
	}
}

func TestMailImageProxyServesRasterImagesOnly(t *testing.T) {
	big := make([]byte, 4096)
	copy(big, tinyPNG)
	cases := map[string]struct {
		body   []byte
		status int
	}{"svg that is not svg": {[]byte("<svg>no namespace</svg>"), http.StatusUnsupportedMediaType}, "html": {[]byte("<html>no</html>"), http.StatusUnsupportedMediaType}, "oversize": {big, http.StatusRequestEntityTooLarge}}
	for name, tc := range cases {
		host := newImageHost(t, serveBytes("image/png", tc.body))
		options := allowing(host)
		options.MaxBytes = 1024
		api, admin, secret, _ := proxyDesk(t, options)
		paths := proxyPaths(t, api, admin, secret, `<img src="`+host.URL+`/x.png">`)
		response := request(t, api, http.MethodGet, paths[0], nil, admin, nil)
		if response.Code != tc.status {
			t.Errorf("%s: status=%d, want %d", name, response.Code, tc.status)
		}
		if strings.HasPrefix(response.Header().Get("Content-Type"), "image/") {
			t.Errorf("%s: served as %s", name, response.Header().Get("Content-Type"))
		}
	}
}

// Someone may open a proxy address by itself. An SVG there must run nothing: the headers say so.
func TestProxiedSVGOpenedDirectlyRunsNoScript(t *testing.T) {
	host := newImageHost(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html") // the source's label means nothing
		w.Write(tinySVG)
	})
	api, admin, secret, _ := proxyDesk(t, allowing(host))
	paths := proxyPaths(t, api, admin, secret, `<img src="`+host.URL+`/logo.svg">`)
	for view, name := range []string{"fetched", "served from the cache"} {
		response := request(t, api, http.MethodGet, paths[0], nil, admin, nil)
		if response.Code != http.StatusOK || response.Body.String() != string(tinySVG) {
			t.Fatalf("%s: status=%d", name, response.Code)
		}
		header := response.Header()
		want := map[string]string{
			"Content-Type":            "image/svg+xml",
			"Content-Security-Policy": "default-src 'none'; style-src 'unsafe-inline'; sandbox",
			"X-Content-Type-Options":  "nosniff",
			"Content-Disposition":     "inline; filename=image.svg",
		}
		for key, value := range want {
			if header.Get(key) != value {
				t.Errorf("%s: %s = %q, want %q", name, key, header.Get(key), value)
			}
		}
		_ = view
	}
	if host.hits.Load() != 1 {
		t.Fatalf("the source was called %d times", host.hits.Load())
	}
}

func TestMailImageProxyAnswersBadGatewayWhenTheSourceFails(t *testing.T) {
	host := newImageHost(t, func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "gone", http.StatusNotFound) })
	api, admin, secret, _ := proxyDesk(t, allowing(host))
	paths := proxyPaths(t, api, admin, secret, `<img src="`+host.URL+`/x.png">`)
	if response := request(t, api, http.MethodGet, paths[0], nil, admin, nil); response.Code != http.StatusBadGateway {
		t.Fatalf("status=%d", response.Code)
	}
}

// removeStoredObjects deletes every object under the storage root, as a cleanup or a lost disk would.
func removeStoredObjects(t *testing.T, root string) {
	t.Helper()
	removed := 0
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err == nil && !entry.IsDir() && !strings.Contains(path, ".incoming") {
			removed++
			return os.Remove(path)
		}
		return err
	})
	if err != nil || removed == 0 {
		t.Fatalf("removed %d objects: %v", removed, err)
	}
}
