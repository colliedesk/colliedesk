package httpapi_test

import (
	"net/http"
	"net/netip"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

// zaloAvailable lists the workspace's channels as a browser at peer (and behind it, forwarded) sees them.
func zaloAvailable(t *testing.T, api http.Handler, token, peer, forwarded string) bool {
	t.Helper()
	req := newRequest(t, http.MethodGet, "/api/v1/channels", nil, token, nil)
	req.RemoteAddr = peer
	if forwarded != "" {
		req.Header.Set("X-Forwarded-For", forwarded)
	}
	response := serve(api, req)
	if response.Code != http.StatusOK {
		t.Fatalf("list channels status=%d body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Types []struct {
			Type      string `json:"type"`
			Available bool   `json:"available"`
		} `json:"types"`
	}
	decode(t, response, &body)
	for _, info := range body.Types {
		if info.Type == "zalo" {
			return info.Available
		}
	}
	t.Fatalf("no zalo type in %+v", body.Types)
	return false
}

// Zalo OA is offered to a team in Vietnam only: a person outside Vietnam is not offered it, unless
// their workspace already has one. An address that says nothing about where the person is (this
// machine, a private network) keeps it offered. 14.160.0.1 is in a block APNIC delegates to Vietnam,
// 8.8.8.8 is in the US.
func TestZaloIsOfferedOnlyToVisitorsFromVietnam(t *testing.T) {
	api, service, _, _ := testAPIWithConfig(t, func(config *httpapi.Config) {
		config.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("172.18.0.1/32")}
	})
	admin := createSession(t, api, service, "Zalo Offer", "admin@zalo-offer.example.com")

	cases := []struct {
		name, peer, forwarded string
		want                  bool
	}{
		{"from Vietnam", "14.160.0.1:4000", "", true},
		{"from the US", "8.8.8.8:4000", "", false},
		{"from Vietnam behind the trusted proxy", "172.18.0.1:4000", "14.160.0.1", true},
		{"from the US behind the trusted proxy", "172.18.0.1:4000", "8.8.8.8", false},
		{"claiming Vietnam to an untrusted peer", "8.8.8.8:4000", "14.160.0.1", false},
		{"from Vietnam over IPv6", "[2402:800:6311::1]:4000", "", true},
		{"from this machine", "127.0.0.1:4000", "", true},
		{"from a private network", "10.1.2.3:4000", "", true},
	}
	for _, c := range cases {
		if got := zaloAvailable(t, api, admin, c.peer, c.forwarded); got != c.want {
			t.Errorf("%s: zalo available = %v, want %v", c.name, got, c.want)
		}
	}

	// A workspace that has a Zalo OA keeps the type wherever its people travel.
	create(t, api, admin, "/api/v1/channels", map[string]any{"type": "zalo", "config": map[string]any{"oa_id": "4440071", "app_id": "700071"}})
	if !zaloAvailable(t, api, admin, "8.8.8.8:4000", "") {
		t.Errorf("a workspace with a Zalo OA, from the US: zalo not available, want available")
	}
}

// Every other type is offered wherever the person is.
func TestOtherChannelTypesAreOfferedEverywhere(t *testing.T) {
	api, service, _, _ := testAPIWithConfig(t, nil)
	admin := createSession(t, api, service, "Email Anywhere", "admin@email-anywhere.example.com")
	req := newRequest(t, http.MethodGet, "/api/v1/channels", nil, admin, nil)
	req.RemoteAddr = "8.8.8.8:4000"
	var body struct {
		Types []struct {
			Type      string `json:"type"`
			Available bool   `json:"available"`
		} `json:"types"`
	}
	decode(t, serve(api, req), &body)
	for _, info := range body.Types {
		if info.Type != "zalo" && !info.Available {
			t.Errorf("%s not available from the US", info.Type)
		}
	}
}
