package httpapi

import (
	"net/http"
	"net/netip"
	"testing"
)

func TestClientIP(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("172.18.0.0/16")}
	cases := []struct {
		name       string
		remoteAddr string
		forwarded  []string
		trusted    []netip.Prefix
		want       string
	}{
		{"direct client", "203.0.113.7:51000", nil, nil, "203.0.113.7"},
		{"forwarded header from an untrusted peer is ignored", "192.0.2.5:4000", []string{"198.51.100.9, 203.0.113.7"}, trusted, "192.0.2.5"},
		{"trusted proxy: right-most untrusted entry", "172.18.0.1:5000", []string{"198.51.100.9, 203.0.113.7"}, trusted, "203.0.113.7"},
		{"a client cannot pick its address by prepending", "172.18.0.1:5000", []string{"10.0.0.1, 203.0.113.7"}, trusted, "203.0.113.7"},
		{"trusted hops are skipped", "172.18.0.1:5000", []string{"203.0.113.7, 172.18.0.9"}, trusted, "203.0.113.7"},
		{"several header lines", "172.18.0.1:5000", []string{"198.51.100.9", "203.0.113.7"}, trusted, "203.0.113.7"},
		{"trusted proxy without the header", "172.18.0.1:5000", nil, trusted, "172.18.0.1"},
		{"garbage entry falls back to the peer", "172.18.0.1:5000", []string{"not-an-ip"}, trusted, "172.18.0.1"},
		{"only trusted entries: the first one", "172.18.0.1:5000", []string{"172.18.0.7, 172.18.0.9"}, trusted, "172.18.0.7"},
		{"ipv6 clients share a /64", "[2001:db8:1:2:aaaa:bbbb:cccc:dddd]:80", nil, nil, "2001:db8:1:2::"},
		{"ipv4-mapped ipv6", "[::ffff:203.0.113.7]:80", nil, nil, "203.0.113.7"},
	}
	for _, c := range cases {
		request, _ := http.NewRequest(http.MethodPost, "/", nil)
		request.RemoteAddr = c.remoteAddr
		for _, line := range c.forwarded {
			request.Header.Add("X-Forwarded-For", line)
		}
		if got := clientIP(request, c.trusted); got != c.want {
			t.Errorf("%s: clientIP = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestParseTrustedProxies(t *testing.T) {
	got, err := ParseTrustedProxies(" 172.18.0.0/16, 10.1.2.3 ,fd00::/8,")
	if err != nil || len(got) != 3 || got[0].String() != "172.18.0.0/16" || got[1].String() != "10.1.2.3/32" || got[2].String() != "fd00::/8" {
		t.Fatalf("ParseTrustedProxies = %v, %v", got, err)
	}
	if got, err := ParseTrustedProxies(""); err != nil || len(got) != 0 {
		t.Fatalf("empty = %v, %v", got, err)
	}
	if _, err := ParseTrustedProxies("172.18.0.0/16,bogus"); err == nil {
		t.Fatal("a bad entry must be an error, not silently ignored")
	}
}
