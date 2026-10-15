package main

import (
	"log/slog"
	"net/netip"
)

// warnWithoutTrustedProxies tells the operator once, at startup, that every visitor of the public
// web form will count as the reverse proxy until TRUSTED_PROXIES names it. Unset and empty both
// trust no proxy.
func warnWithoutTrustedProxies(logger *slog.Logger, proxies []netip.Prefix) {
	if len(proxies) == 0 {
		logger.Warn("TRUSTED_PROXIES is empty: the web form's rate limit sees the TCP peer, so behind a reverse proxy all visitors share one limit; set it to the proxy's address or range")
	}
}
