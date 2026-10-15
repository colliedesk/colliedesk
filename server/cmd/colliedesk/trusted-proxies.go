package main

import (
	"log/slog"
	"net/netip"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

// warnWithoutTrustedProxies tells the operator once, at startup, that every visitor of the public
// web form will count as the reverse proxy until TRUSTED_PROXIES names it.
func warnWithoutTrustedProxies(logger *slog.Logger, proxies []netip.Prefix) {
	if len(proxies) == 0 {
		logger.Warn("TRUSTED_PROXIES is empty: the web form's rate limit sees the TCP peer, so behind a reverse proxy all visitors share one limit; set it to the proxy's address or range")
	}
}

// defaultTrustedProxies is used when TRUSTED_PROXIES is not set: a reverse proxy on the same
// host (loopback), which Docker delivers from the bridge gateway (172.16.0.0/12).
const defaultTrustedProxies = "127.0.0.0/8,::1/128,172.16.0.0/12"

// trustedProxiesFromEnv reads TRUSTED_PROXIES (see httpapi.ParseTrustedProxies). Unset means
// defaultTrustedProxies; set but empty means none.
func trustedProxiesFromEnv(lookup func(string) (string, bool)) ([]netip.Prefix, error) {
	value, set := lookup("TRUSTED_PROXIES")
	if !set {
		value = defaultTrustedProxies
	}
	return httpapi.ParseTrustedProxies(value)
}
