package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/channels/zalo"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

const (
	zaloCodeA = "OOMiTAgDDsCXlvT2meuJUq6BicIj_Ma_DJKs"
	zaloCodeB = "BBbbTAgDDsCXlvT2meuJUq6BicIj_Ma_9999"
)

// zaloFile asks for a file below the Zalo prefix as Zalo's checker does, from its own address.
func zaloFile(d zaloDesk, method, name string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/api/v1/channels/zalo/"+name, nil)
	req.RemoteAddr = "192.0.2.90:4242"
	return serve(d.api, req)
}

type zaloVerificationView struct {
	Config struct {
		SiteVerification       *string `json:"site_verification"`
		VerificationPrefixPath string  `json:"verification_prefix_path"`
		VerificationPrefixURL  *string `json:"verification_prefix_url"`
		WebhookURL             *string `json:"webhook_url"`
	} `json:"config"`
}

// A team on a domain it cannot change (Colliedesk Cloud) proves the URL prefix of the app's webhook to Zalo
// with the HTML file: it pastes Zalo's code on the channel, and the server serves Zalo's file below
// /api/v1/channels/zalo/ while a live channel holds the code. Every other name is the mux's own 404.
func TestAZaloChannelServesZalosSiteVerificationFileForItsCode(t *testing.T) {
	d := newZaloDesk(t)
	// The prefix to verify is shown before any code is pasted.
	var before zaloVerificationView
	decode(t, request(t, d.api, http.MethodGet, "/api/v1/channels/"+d.channelA, nil, d.adminA, nil), &before)
	if before.Config.SiteVerification != nil || before.Config.VerificationPrefixPath != "/api/v1/channels/zalo/" ||
		before.Config.VerificationPrefixURL == nil || *before.Config.VerificationPrefixURL != testOrigin+"/api/v1/channels/zalo/" ||
		before.Config.WebhookURL == nil || *before.Config.WebhookURL != testOrigin+"/api/v1/channels/zalo/webhook" {
		t.Fatalf("before a code = %+v", before.Config)
	}
	never := zaloFile(d, http.MethodGet, "zalo_verifier"+zaloCodeA+".html")
	unknown := zaloFile(d, http.MethodGet, "never-a-route")
	if never.Code != http.StatusNotFound || never.Body.String() != unknown.Body.String() || never.Header().Get("Content-Type") != unknown.Header().Get("Content-Type") {
		t.Fatalf("a code no channel holds = %d %q, an unknown path = %d %q", never.Code, never.Body.String(), unknown.Code, unknown.Body.String())
	}

	// The whole meta tag pasted is read as its code.
	patched := request(t, d.api, http.MethodPatch, "/api/v1/channels/"+d.channelA, map[string]any{"config": map[string]any{
		"site_verification": `<meta property="zalo-platform-site-verification" content="` + zaloCodeA + `" />`}}, d.adminA, nil)
	if patched.Code != http.StatusOK {
		t.Fatalf("paste the code: %d %s", patched.Code, patched.Body.String())
	}
	var after zaloVerificationView
	decode(t, request(t, d.api, http.MethodGet, "/api/v1/channels/"+d.channelA, nil, d.adminA, nil), &after)
	if after.Config.SiteVerification == nil || *after.Config.SiteVerification != zaloCodeA {
		t.Fatalf("after the code = %+v", after.Config)
	}

	served := zaloFile(d, http.MethodGet, "zalo_verifier"+zaloCodeA+".html")
	if served.Code != http.StatusOK || !strings.HasPrefix(served.Header().Get("Content-Type"), "text/html") ||
		served.Body.String() != string(zalo.SiteVerificationFile(zaloCodeA)) {
		t.Fatalf("the file = %d %s %q", served.Code, served.Header().Get("Content-Type"), served.Body.String())
	}
	// Only the file: nothing of the channel or its workspace.
	if strings.Contains(served.Body.String(), d.oaA) || strings.Contains(served.Body.String(), d.channelA) {
		t.Fatalf("the file tells of the channel: %s", served.Body.String())
	}
	if head := zaloFile(d, http.MethodHead, "zalo_verifier"+zaloCodeA+".html"); head.Code != http.StatusOK || head.Body.Len() != 0 {
		t.Fatalf("HEAD = %d %q", head.Code, head.Body.String())
	}
	// Another method, a near name, another code: as any path the server does not have.
	notAllowed := zaloFile(d, http.MethodPost, "never-a-route")
	if post := zaloFile(d, http.MethodPost, "zalo_verifier"+zaloCodeA+".html"); post.Code != notAllowed.Code || post.Body.String() != notAllowed.Body.String() {
		t.Fatalf("POST = %d %q, want %d %q", post.Code, post.Body.String(), notAllowed.Code, notAllowed.Body.String())
	}
	for _, name := range []string{"zalo_verifier" + zaloCodeB + ".html", "zalo_verifier" + zaloCodeA + ".htm", "zalo_verifier" + zaloCodeA + ".html.bak",
		"Zalo_verifier" + zaloCodeA + ".html", "zalo_verifier" + strings.ToLower(zaloCodeA) + ".html", "zalo_verifier.html", "zalo_verifier%2e%2e.html"} {
		got := zaloFile(d, http.MethodGet, name)
		if got.Code != http.StatusNotFound || got.Body.String() != unknown.Body.String() {
			t.Errorf("%s = %d %q", name, got.Code, got.Body.String())
		}
	}

	// Workspace isolation: B sees nothing of A's code, and B's channel holds none.
	var channelB string
	if err := d.pool.QueryRow(t.Context(), `SELECT id::text FROM channels WHERE external_account=$1`, d.oaB).Scan(&channelB); err != nil {
		t.Fatal(err)
	}
	other := request(t, d.api, http.MethodGet, "/api/v1/channels/"+channelB, nil, d.adminB, nil)
	var viewB zaloVerificationView
	decode(t, other, &viewB)
	if viewB.Config.SiteVerification != nil || strings.Contains(other.Body.String(), zaloCodeA) {
		t.Fatalf("B's channel = %s", other.Body.String())
	}
	if list := request(t, d.api, http.MethodGet, "/api/v1/channels", nil, d.adminB, nil); strings.Contains(list.Body.String(), zaloCodeA) {
		t.Fatalf("B's channel list shows A's code: %s", list.Body.String())
	}
	if patchA := request(t, d.api, http.MethodPatch, "/api/v1/channels/"+d.channelA, map[string]any{"config": map[string]any{"site_verification": zaloCodeB}}, d.adminB, nil); patchA.Code != http.StatusNotFound {
		t.Fatalf("B changes A's code: %d", patchA.Code)
	}
	// B's own code is served too, and both stay apart.
	if patchB := request(t, d.api, http.MethodPatch, "/api/v1/channels/"+channelB, map[string]any{"config": map[string]any{"site_verification": "zalo-platform-site-verification=" + zaloCodeB}}, d.adminB, nil); patchB.Code != http.StatusOK {
		t.Fatalf("B's code: %d %s", patchB.Code, patchB.Body.String())
	}
	if got := zaloFile(d, http.MethodGet, "zalo_verifier"+zaloCodeB+".html"); got.Code != http.StatusOK {
		t.Fatalf("B's file = %d", got.Code)
	}
	decode(t, request(t, d.api, http.MethodGet, "/api/v1/channels/"+d.channelA, nil, d.adminA, nil), &after)
	if after.Config.SiteVerification == nil || *after.Config.SiteVerification != zaloCodeA {
		t.Fatalf("A's code after B's = %+v", after.Config)
	}

	// A code that is not one is refused and says what to paste; the stored code stays.
	bad := request(t, d.api, http.MethodPatch, "/api/v1/channels/"+d.channelA, map[string]any{"config": map[string]any{"site_verification": "not a code"}}, d.adminA, nil)
	if bad.Code != http.StatusBadRequest || !strings.Contains(bad.Body.String(), "site_verification") {
		t.Fatalf("a wrong code: %d %s", bad.Code, bad.Body.String())
	}
	// The way back: an empty code clears it, and the file is gone.
	if cleared := request(t, d.api, http.MethodPatch, "/api/v1/channels/"+d.channelA, map[string]any{"config": map[string]any{"site_verification": ""}}, d.adminA, nil); cleared.Code != http.StatusOK {
		t.Fatalf("clear: %d %s", cleared.Code, cleared.Body.String())
	}
	decode(t, request(t, d.api, http.MethodGet, "/api/v1/channels/"+d.channelA, nil, d.adminA, nil), &after)
	if after.Config.SiteVerification != nil {
		t.Fatalf("after clearing = %+v", after.Config)
	}
	if got := zaloFile(d, http.MethodGet, "zalo_verifier"+zaloCodeA+".html"); got.Code != http.StatusNotFound || got.Body.String() != unknown.Body.String() {
		t.Fatalf("a cleared code's file = %d", got.Code)
	}
	// A removed channel's code is not served.
	if removed := request(t, d.api, http.MethodDelete, "/api/v1/channels/"+channelB, nil, d.adminB, nil); removed.Code != http.StatusNoContent {
		t.Fatalf("remove B: %d %s", removed.Code, removed.Body.String())
	}
	if got := zaloFile(d, http.MethodGet, "zalo_verifier"+zaloCodeB+".html"); got.Code != http.StatusNotFound || got.Body.String() != unknown.Body.String() {
		t.Fatalf("a removed channel's file = %d", got.Code)
	}
}

// The code is set when the channel is added, too.
func TestAZaloChannelTakesItsSiteVerificationCodeWhenAdded(t *testing.T) {
	d := newZaloDesk(t)
	id := create(t, d.api, d.adminA, "/api/v1/channels", map[string]any{"type": "zalo", "config": map[string]any{
		"oa_id": "4440001", "app_id": "700002", "site_verification": "zalo_verifier" + zaloCodeB + ".html"}})
	var view zaloVerificationView
	decode(t, request(t, d.api, http.MethodGet, "/api/v1/channels/"+id, nil, d.adminA, nil), &view)
	if view.Config.SiteVerification == nil || *view.Config.SiteVerification != zaloCodeB {
		t.Fatalf("added with a code = %+v", view.Config)
	}
	if got := zaloFile(d, http.MethodGet, "zalo_verifier"+zaloCodeB+".html"); got.Code != http.StatusOK {
		t.Fatalf("its file = %d", got.Code)
	}
}

// Each name shaped like a verification file is a lookup across workspaces, so one address may ask for 30 a
// minute; other paths below the prefix are not charged.
func TestZaloVerificationFileLookupsAreChargedToTheClientAddress(t *testing.T) {
	d := newZaloDesk(t)
	ask := func(name string) int {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/channels/zalo/"+name, nil)
		req.RemoteAddr = "192.0.2.91:4242"
		return serve(d.api, req).Code
	}
	for i := 0; i < 40; i++ {
		if code := ask("not-a-verification-file"); code != http.StatusNotFound {
			t.Fatalf("an uncharged path = %d", code)
		}
	}
	for i := 0; i < 30; i++ {
		if code := ask("zalo_verifier" + zaloCodeA + ".html"); code != http.StatusNotFound {
			t.Fatalf("lookup %d = %d", i, code)
		}
	}
	if code := ask("zalo_verifier" + zaloCodeA + ".html"); code != http.StatusTooManyRequests {
		t.Fatalf("lookup 31 = %d", code)
	}
}

// On a server that gives each workspace its own host, the addresses are on that host, as its people use it.
func TestTheZaloAddressesAreOnTheWorkspacesOwnHost(t *testing.T) {
	api, service, _, _ := testAPIWithConfig(t, func(config *httpapi.Config) {
		config.HelpCenterDomain = "colliedesk.example"
	})
	admin := createSession(t, api, service, "Zalo Host", "admin@zalo-host.example.com")
	id := create(t, api, admin, "/api/v1/channels", map[string]any{"type": "zalo", "config": map[string]any{"oa_id": "4440003", "app_id": "700005"}})
	var view zaloVerificationView
	decode(t, request(t, api, http.MethodGet, "/api/v1/channels/"+id, nil, admin, nil), &view)
	if view.Config.VerificationPrefixURL == nil || !strings.HasSuffix(*view.Config.VerificationPrefixURL, ".colliedesk.example/api/v1/channels/zalo/") ||
		!strings.HasPrefix(*view.Config.VerificationPrefixURL, "https://") {
		t.Fatalf("on a workspace host = %+v", view.Config)
	}
}
