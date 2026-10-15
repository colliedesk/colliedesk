package main

import (
	"fmt"
	"log/slog"
	"net/netip"
	"strings"
)

// warnWithoutTrustedProxies tells the operator once, at startup, that every visitor of the public
// web form will count as the reverse proxy until TRUSTED_PROXY_CIDRS names it.
func warnWithoutTrustedProxies(logger *slog.Logger, proxies []netip.Prefix) {
	if len(proxies) == 0 {
		logger.Warn("TRUSTED_PROXY_CIDRS is empty: the web form's rate limit sees the TCP peer, so behind a reverse proxy all visitors share one limit; set it to the proxy's address or range")
	}
}

// defaultTrustedProxies is used when TRUSTED_PROXY_CIDRS is not set: a reverse proxy on the same
// host (loopback), which Docker delivers from the bridge gateway (172.16.0.0/12).
const defaultTrustedProxies = "127.0.0.0/8,::1/128,172.16.0.0/12"

// trustedProxiesFromEnv reads TRUSTED_PROXY_CIDRS: a comma-separated list of addresses or CIDR
// ranges of the reverse proxies in front of the server. Unset means defaultTrustedProxies; set but
// empty means none.
func trustedProxiesFromEnv(lookup func(string) (string, bool)) ([]netip.Prefix, error) {
	var proxies []netip.Prefix
	value, set := lookup("TRUSTED_PROXY_CIDRS")
	if !set {
		value = defaultTrustedProxies
	}
	for _, entry := range strings.Split(value, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(entry)
		if err != nil {
			address, addressErr := netip.ParseAddr(entry)
			if addressErr != nil {
				return nil, fmt.Errorf("TRUSTED_PROXY_CIDRS: %q is neither an address nor a CIDR range", entry)
			}
			prefix = netip.PrefixFrom(address, address.BitLen())
		}
		proxies = append(proxies, prefix.Masked())
	}
	return proxies, nil
}
