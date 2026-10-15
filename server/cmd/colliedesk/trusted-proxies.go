package main

import (
	"fmt"
	"net/netip"
	"strings"
)

// trustedProxiesFromEnv reads TRUSTED_PROXY_CIDRS: a comma-separated list of addresses or CIDR
// ranges of the reverse proxies in front of the server. Empty means there is none.
func trustedProxiesFromEnv(getenv func(string) string) ([]netip.Prefix, error) {
	var proxies []netip.Prefix
	for _, entry := range strings.Split(getenv("TRUSTED_PROXY_CIDRS"), ",") {
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
