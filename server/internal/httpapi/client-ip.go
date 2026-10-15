package httpapi

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// ParseTrustedProxies reads the TRUSTED_PROXIES list: comma-separated CIDRs or single addresses of the
// reverse proxies whose X-Forwarded-For header may be believed. Empty means no proxy is trusted.
// A bad entry is an error, so a typo cannot silently put every client behind one proxy address.
func ParseTrustedProxies(value string) ([]netip.Prefix, error) {
	var prefixes []netip.Prefix
	for _, entry := range strings.Split(value, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(entry)
		if err != nil {
			address, addressErr := netip.ParseAddr(entry)
			if addressErr != nil {
				return nil, fmt.Errorf("TRUSTED_PROXIES entry %q is not a CIDR or an IP address", entry)
			}
			prefix = netip.PrefixFrom(address, address.BitLen())
		}
		prefixes = append(prefixes, prefix.Masked())
	}
	return prefixes, nil
}

// clientIP is the address of the person behind the request, as a rate-limit key. The peer address is
// the answer unless it is a trusted proxy; then X-Forwarded-For is read from the right, skipping
// trusted hops, and the first untrusted entry is the client, because entries to its left were written
// by the client itself. An unparsable entry, or a missing header, falls back to the peer. IPv4-mapped
// IPv6 is unmapped and an IPv6 client is reduced to its /64, the smallest block one subscriber gets.
func clientIP(request *http.Request, trusted []netip.Prefix) string {
	peer := parseAddr(request.RemoteAddr)
	chosen := peer
	if isTrusted(peer, trusted) {
		var entries []string
		for _, line := range request.Header.Values("X-Forwarded-For") {
			entries = append(entries, strings.Split(line, ",")...)
		}
		for index := len(entries) - 1; index >= 0; index-- {
			address, err := netip.ParseAddr(strings.TrimSpace(entries[index]))
			if err != nil {
				break
			}
			address = address.Unmap()
			chosen = address
			if !isTrusted(address, trusted) {
				break
			}
		}
	}
	if chosen.Is6() {
		chosen = netip.PrefixFrom(chosen, 64).Masked().Addr()
	}
	return chosen.String()
}

func isTrusted(address netip.Addr, trusted []netip.Prefix) bool {
	for _, prefix := range trusted {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func parseAddr(hostPort string) netip.Addr {
	host, _, err := net.SplitHostPort(hostPort)
	if err != nil {
		host = hostPort
	}
	address, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}
	}
	return address.Unmap()
}
