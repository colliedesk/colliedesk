package main

import (
	"bytes"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var composeDefault = regexp.MustCompile(`TRUSTED_PROXY_CIDRS: \$\{TRUSTED_PROXY_CIDRS-([^}]*)\}`)

// The shipped compose files bind the server to 127.0.0.1, so a reverse proxy on the host always
// sits in front. Docker delivers that proxy's connections from the bridge gateway, not from
// loopback, so the default must cover both or every visitor shares one rate-limit bucket.
func TestShippedComposeFilesTrustTheLocalProxyByDefault(t *testing.T) {
	for _, file := range []string{"compose.yaml", "install.sh"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "..", file))
		if err != nil {
			t.Fatal(err)
		}
		match := composeDefault.FindSubmatch(raw)
		if match == nil {
			t.Fatalf("%s sets no default for TRUSTED_PROXY_CIDRS", file)
		}
		if string(match[1]) != defaultTrustedProxies {
			t.Errorf("%s default %q differs from the server's %q", file, match[1], defaultTrustedProxies)
		}
		proxies, err := trustedProxiesFromEnv(func(string) (string, bool) { return string(match[1]), true })
		if err != nil {
			t.Fatalf("%s default %q: %v", file, match[1], err)
		}
		for _, peer := range []string{"127.0.0.1", "::1", "172.18.0.1"} {
			if !covers(proxies, netip.MustParseAddr(peer)) {
				t.Errorf("%s default %q does not trust %s", file, match[1], peer)
			}
		}
		for _, stranger := range []string{"203.0.113.9", "2001:db8::1"} {
			if covers(proxies, netip.MustParseAddr(stranger)) {
				t.Errorf("%s default %q trusts the public address %s", file, match[1], stranger)
			}
		}
	}
}

func covers(proxies []netip.Prefix, address netip.Addr) bool {
	for _, prefix := range proxies {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func TestServerWarnsOnceAtStartupWhenNoProxyIsTrusted(t *testing.T) {
	var out bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&out, nil))
	warnWithoutTrustedProxies(logger, nil)
	if !strings.Contains(out.String(), "level=WARN") || !strings.Contains(out.String(), "TRUSTED_PROXY_CIDRS") {
		t.Fatalf("log = %q, want a WARN naming TRUSTED_PROXY_CIDRS", out.String())
	}
	out.Reset()
	warnWithoutTrustedProxies(logger, []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")})
	if out.Len() != 0 {
		t.Fatalf("log = %q, want nothing when a proxy is trusted", out.String())
	}
}
