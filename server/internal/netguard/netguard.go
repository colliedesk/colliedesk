// Package netguard keeps server-side fetches of URLs taken from outside data (an importer's source
// system, a customer's email) away from the host's own network and cloud metadata services.
package netguard

import (
	"errors"
	"fmt"
	"net"
	"syscall"
)

// ErrBlockedAddress is returned when a connection would reach a non-public address.
var ErrBlockedAddress = errors.New("refusing to connect to a private, loopback or link-local address")

// reservedNetworks are special-purpose ranges that net.IP methods do not
// cover: CGNAT, benchmarking, IETF protocol assignments, the former class E,
// NAT64 (which can embed any IPv4 address, including private ones), 6to4,
// Teredo and documentation prefixes.
var reservedNetworks = func() []*net.IPNet {
	var networks []*net.IPNet
	for _, cidr := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "198.18.0.0/15", "240.0.0.0/4",
		"64:ff9b::/96", "64:ff9b:1::/48", "2002::/16", "2001::/32", "2001:db8::/32"} {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			panic(err)
		}
		networks = append(networks, network)
	}
	return networks
}()

// Blocked says whether ip is loopback, private (RFC 1918, and fc00::/7 which holds fd00:ec2::254),
// link-local (169.254.169.254), multicast, unspecified or in one of the reserved ranges above.
func Blocked(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() || ip.IsMulticast() {
		return true
	}
	for _, network := range reservedNetworks {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// Control is a net.Dialer Control function. It runs after DNS resolution on every connection,
// including each redirect hop, so a name that resolves (or later re-resolves) to a blocked address
// cannot be dialled. Addresses in allow ("host:port") pass; tests use that for a local server.
func Control(allow map[string]bool) func(network, address string, c syscall.RawConn) error {
	return func(_, address string, _ syscall.RawConn) error {
		if allow[address] {
			return nil
		}
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return fmt.Errorf("%w: %s", ErrBlockedAddress, address)
		}
		ip := net.ParseIP(host)
		if ip == nil || Blocked(ip) {
			return fmt.Errorf("%w: %s", ErrBlockedAddress, host)
		}
		return nil
	}
}
