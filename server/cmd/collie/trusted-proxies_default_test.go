package main

import (
	"bytes"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/serverconfig"
)

// The server trusts no proxy unless TRUSTED_PROXIES names one, exactly as sign-up requires. Unset
// and empty are the same, and the web form's startup warning covers both.
func TestUnsetTrustedProxiesTrustNobodyAndWarn(t *testing.T) {
	proxies, err := httpapi.ParseTrustedProxies("")
	if err != nil || len(proxies) != 0 {
		t.Fatalf("empty = %v, %v; want no proxies", proxies, err)
	}
	var out bytes.Buffer
	warnWithoutTrustedProxies(slog.New(slog.NewTextHandler(&out, nil)), proxies)
	if !strings.Contains(out.String(), "level=WARN") {
		t.Fatalf("log = %q, want the WARN", out.String())
	}
}

// A server whose trusted_proxies is not set trusts no proxy, as when TRUSTED_PROXIES was unset.
func TestUnsetTrustedProxiesTrustNoProxy(t *testing.T) {
	snap := serverconfig.NewSnapshot(0, nil, nil, "", func(string) string { return "10.0.0.0/8" }, true)
	if got := snap.Getenv("TRUSTED_PROXIES"); got != "" {
		t.Fatalf("unset trusted_proxies reads %q, want no proxy", got)
	}
}

// The installer binds the server to 127.0.0.1, so a reverse proxy on the host always sits in front.
// Docker delivers that proxy's connections from the bridge gateway, not from loopback, so what the
// installer saves must cover both or every visitor shares one rate-limit bucket.
func TestTheInstallerTrustsTheLocalProxy(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "TRUSTED_PROXIES="+serverconfig.InstallerTrustedProxies) {
		t.Fatalf("install.sh does not save trusted_proxies=%s", serverconfig.InstallerTrustedProxies)
	}
	proxies, err := httpapi.ParseTrustedProxies(serverconfig.InstallerTrustedProxies)
	if err != nil {
		t.Fatal(err)
	}
	for _, peer := range []string{"127.0.0.1", "::1", "172.18.0.1"} {
		if !covers(proxies, netip.MustParseAddr(peer)) {
			t.Errorf("the installer's %v does not trust %s", proxies, peer)
		}
	}
	for _, stranger := range []string{"203.0.113.9", "2001:db8::1"} {
		if covers(proxies, netip.MustParseAddr(stranger)) {
			t.Errorf("the installer's %v trusts the public address %s", proxies, stranger)
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
	if !strings.Contains(out.String(), "level=WARN") || !strings.Contains(out.String(), "TRUSTED_PROXIES") {
		t.Fatalf("log = %q, want a WARN naming TRUSTED_PROXIES", out.String())
	}
	out.Reset()
	warnWithoutTrustedProxies(logger, []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")})
	if out.Len() != 0 {
		t.Fatalf("log = %q, want nothing when a proxy is trusted", out.String())
	}
}
